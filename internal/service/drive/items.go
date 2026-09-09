package drive

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// The kinds a link can be. A response says what a thing is rather than which
// number Proton files it under.
const (
	TypeFolder = "folder"
	TypeFile   = "file"
)

// linkType names Proton's numeric link kind.
func linkType(t int) string {
	if t == protonFolder {
		return TypeFolder
	}
	return TypeFile
}

// protonFolder is Proton's number for a folder link.
const protonFolder = 1

type Child struct {
	LinkID     string `json:"link_id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"`
	Type       string `json:"type"`
	Size       int64  `json:"size"`
	CreateTime int64  `json:"create_time,omitempty"`
	ModifyTime int64  `json:"modify_time,omitempty"`
}

func (s *Service) List(ctx context.Context, dc *Context, path string) ([]Child, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if !res.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", res.Describe(path))
	}
	raw, err := s.listRawChildren(ctx, dc, res.LinkID)
	if err != nil {
		return nil, err
	}
	out := make([]Child, 0, len(raw))
	for _, r := range raw {
		name, err := decryptName(r.Name, res.NodeKR)
		if err != nil {
			name = "(decrypt failed)"
		}
		out = append(out, Child{LinkID: r.LinkID, Name: name, Type: linkType(r.Type), Size: r.Size, CreateTime: r.CreateTime, ModifyTime: r.ModifyTime})
	}
	return out, nil
}

// Walk lists all descendants depth-first; each Child carries its full
// decrypted Path.
func (s *Service) Walk(ctx context.Context, dc *Context, path string) ([]Child, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if !res.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", res.Describe(path))
	}
	return s.walk(ctx, dc, res.LinkID, res.NodeKR, strings.TrimRight(path, "/"))
}

func (s *Service) walk(ctx context.Context, dc *Context, linkID string, parentKR *pgp.KeyRing, prefix string) ([]Child, error) {
	raw, err := s.listRawChildren(ctx, dc, linkID)
	if err != nil {
		return nil, err
	}
	var out []Child
	for _, r := range raw {
		name, err := decryptName(r.Name, parentKR)
		if err != nil {
			// The row stays, so nothing has gone missing from the answer and there
			// is nothing to count: the name on the screen says what happened.
			slog.DebugContext(ctx, "drive: a child's name could not be decrypted",
				"link", r.LinkID, "parent", linkID, "error", err)
			name = "(decrypt failed)"
		}
		full := prefix + "/" + name
		out = append(out, Child{LinkID: r.LinkID, Name: name, Path: full, Type: linkType(r.Type), Size: r.Size, CreateTime: r.CreateTime, ModifyTime: r.ModifyTime})
		if r.Type == 1 {
			childKR, err := unlockNode(&r, parentKR, nil)
			if err != nil {
				skip.Record(ctx, skip.KindFolder, r.LinkID, skip.Unlockable, err)
				continue
			}
			nested, err := s.walk(ctx, dc, r.LinkID, childKR, full)
			if err != nil {
				skip.Record(ctx, skip.KindFolder, r.LinkID, skip.Unreadable, err)
				continue
			}
			out = append(out, nested...)
		}
	}
	return out, nil
}

// PlanFolders lists the folders that making a path exist would create, the
// outermost first, so a command can say how many it is about to make and a dry
// run can promise the same number.
//
// Only the folders above it are looked up, which is the same walk creating one
// folder needs anyway. Whether the last name is free is Proton's answer to the
// request that takes it, so a path whose plan is longer than one folder is one
// whose last folder cannot already be there.
func (s *Service) PlanFolders(ctx context.Context, dc *Context, fullPath string) ([]string, error) {
	st, err := s.resolveTo(ctx, dc, dirOf(fullPath))
	if err != nil {
		return nil, err
	}
	if !st.at.IsFolder {
		return nil, &errs.Exists{Kind: TypeFile, Name: st.at.Name, Where: dirOf(st.path)}
	}
	paths := make([]string, 0, len(st.missing)+1)
	at := st.path
	for _, name := range st.missing {
		at = join(at, name)
		paths = append(paths, at)
	}
	return append(paths, join(at, baseOf(fullPath))), nil
}

// CreateFolders makes each path in turn.
//
// A folder made here is the parent the next one goes under, and its keys are the
// ones just generated, so a chain of folders costs one lookup and one request
// per folder rather than a walk from the root for each.
func (s *Service) CreateFolders(ctx context.Context, dc *Context, paths []string) error {
	by, err := s.author(ctx, dc)
	if err != nil {
		return err
	}
	made := map[string]*folder{}
	for _, path := range paths {
		parent, ok := made[dirOf(path)]
		if !ok {
			res, err := s.ResolvePath(ctx, dc, dirOf(path))
			if err != nil {
				return err
			}
			if parent, err = folderOf(res, dirOf(path)); err != nil {
				return err
			}
		}
		child, err := s.createFolder(ctx, dc, by, parent, baseOf(path))
		if err != nil {
			return err
		}
		made[path] = child
	}
	return nil
}

// folder is a folder to make things in: what a request names it by, and the two
// keys a child of it needs - the one its name and passphrase are encrypted to,
// and the one its name is hashed under.
type folder struct {
	linkID  string
	path    string
	nodeKR  *pgp.KeyRing
	hashKey []byte
}

func folderOf(res *Resolved, path string) (*folder, error) {
	hashKey, err := hashKeyOf(res.Link, res.NodeKR)
	if err != nil {
		return nil, err
	}
	return &folder{linkID: res.LinkID, path: path, nodeKR: res.NodeKR, hashKey: hashKey}, nil
}

func (s *Service) createFolder(ctx context.Context, dc *Context, by author, parent *folder, name string) (*folder, error) {
	hash, err := lookupHash(strings.ToLower(name), parent.hashKey)
	if err != nil {
		return nil, err
	}
	signKR := by.key(parent.nodeKR)
	encName, err := encryptName(name, parent.nodeKR, signKR)
	if err != nil {
		return nil, err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(parent.nodeKR, signKR)
	if err != nil {
		return nil, err
	}
	nodeKR, err := pgp.NewKeyRing(nodePriv)
	if err != nil {
		return nil, err
	}
	hashKey, hashKeyEnc, err := genNodeHashKey(nodeKR, nodeKR)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"Name":                    encName,
		"Hash":                    hash,
		"ParentLinkID":            parent.linkID,
		"NodePassphrase":          nodePass,
		"NodePassphraseSignature": nodePassSig,
		"NodeKey":                 nodeKey,
		"NodeHashKey":             hashKeyEnc,
	}
	by.attribute(body, dc)
	var r struct{ Folder struct{ ID string } }
	err = s.C.Decode(ctx, folderRequest(dc, body), &r)
	if proton.AlreadyExists(err) {
		return nil, &errs.Exists{Kind: TypeFolder, Name: name, Where: parent.path}
	}
	if err != nil {
		return nil, err
	}
	return &folder{
		linkID: r.Folder.ID, path: join(parent.path, name),
		nodeKR: nodeKR, hashKey: hashKey,
	}, nil
}

// folderRequest makes a folder, of whichever endpoint serves the tree it is
// going into.
func folderRequest(dc *Context, body map[string]any) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "POST", Path: fmt.Sprintf("/drive/urls/%s/folders", dc.Token), Body: body,
		}
	}
	return proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/shares/%s/folders", dc.ShareID), Body: body,
	}
}

func (s *Service) Rename(ctx context.Context, dc *Context, path, newName string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	if res.Parent == nil {
		return errs.Problemf("A tree has no name of its own to change here.")
	}
	owned, err := s.ownership(ctx, dc, res.Link)
	if err != nil {
		return err
	}
	if refusal := owned.refuses(res.Describe(path), "rename"); refusal != nil {
		return refusal
	}
	hk, err := hashKeyOf(res.Parent.Link, res.ParentKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(strings.ToLower(newName), hk)
	if err != nil {
		return err
	}
	oldHash, err := lookupHash(strings.ToLower(res.Name), hk)
	if err != nil {
		return err
	}
	err = s.rename(ctx, dc, res, newName, newHash, oldHash)
	if proton.AlreadyExists(err) {
		// Proton is the one that knows the name is taken, so this is where that
		// answer gets its words - and what is in the way is worth one request
		// once the refusal has already happened.
		return &errs.Exists{Kind: s.kindAt(ctx, dc, join(dirOf(path), newName)), Name: newName, Where: dirOf(path)}
	}
	return err
}

// kindAt names what already holds a name, so a refusal to take it can say
// whether it is a file or a folder that is in the way.
func (s *Service) kindAt(ctx context.Context, dc *Context, path string) string {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		// Recorded and not counted: the refusal is on the screen either way, and
		// what holds the name is the detail rather than the answer.
		slog.DebugContext(ctx, "drive: what holds a name could not be read",
			"share", dc.handle(), "error", err)
		return "item"
	}
	if res.IsFolder {
		return TypeFolder
	}
	return TypeFile
}

// linkEditWindow is how long Proton lets somebody take back what they uploaded
// into a public link, matching the web client's NODE_EDIT_EXPIRACY.
const linkEditWindow = 59 * time.Minute

// ownership is what a public link says about changing something in it.
type ownership int

const (
	yours ownership = iota
	somebodyElses
	tooOld
)

// ownership judges whether an item is this account's to change.
//
// Only a public link asks the question. Your own files are yours, and a share
// you are a member of was opened as yourself; a link is opened as a stranger,
// who may take back what they put there themselves and only while it is new.
func (s *Service) ownership(ctx context.Context, dc *Context, link *Link) (ownership, error) {
	if !dc.Public() {
		return yours, nil
	}
	u, err := s.keys(ctx)
	if err != nil {
		return somebodyElses, err
	}
	if link.SignatureEmail == "" || !hasAddress(u.Addresses, link.SignatureEmail) {
		return somebodyElses, nil
	}
	if time.Since(time.Unix(link.CreateTime, 0)) > linkEditWindow {
		return tooOld, nil
	}
	return yours, nil
}

func hasAddress(addresses []keys.Address, email string) bool {
	for _, a := range addresses {
		if strings.EqualFold(a.Email, email) {
			return true
		}
	}
	return false
}

// refuses is what a link answers about an item that is not this account's to
// change, and nothing at all about one that is.
func (o ownership) refuses(name, verb string) *errs.Problem {
	about, instead := o.words(verb)
	if about == "" {
		return nil
	}
	return errs.Problemf("%s %s", name, about).Hint(instead)
}

// words is the refusal in two pieces: what is true of the item, and what to do
// about it. A warning is shown with nothing beside it, so a bulk verb says both
// in one breath where an error puts the second under Try.
func (o ownership) words(verb string) (about, instead string) {
	switch o {
	case somebodyElses:
		return fmt.Sprintf("is not yours to %s here.", verb),
			fmt.Sprintf("%s Ask whoever sent the link to %s it.", linkChangeRule(verb), verb)
	case tooOld:
		return fmt.Sprintf("was uploaded more than an hour ago, so it is not yours to %s any more.", verb),
			fmt.Sprintf("Ask whoever sent the link to %s it.", verb)
	}
	return "", ""
}

// linkChangeRule is what a link permits whoever holds it, which is the same
// sentence whichever verb was refused.
func linkChangeRule(verb string) string {
	return fmt.Sprintf("In a link you can %s only what you uploaded yourself, within an hour of uploading it.", verb)
}

// rename writes a new name for an item whose place among its siblings the caller
// has already worked out.
//
// A name is unique within its folder, so Proton is told both hashes: the one the
// new name takes and the one it releases. The root of a tree has no folder to be
// unique in, which is the whole of the difference between renaming a file and
// renaming the computer or the share it sits at the top of.
func (s *Service) rename(ctx context.Context, dc *Context, res *Resolved, newName, newHash, oldHash string) error {
	by, err := s.author(ctx, dc)
	if err != nil {
		return err
	}
	encName, err := encryptName(newName, res.ParentKR, by.key(res.ParentKR))
	if err != nil {
		return err
	}
	var signedBy any
	if by.email != "" {
		signedBy = by.email
	}
	return s.C.Decode(ctx, renameRequest(res.dc, res.LinkID, map[string]any{
		"Name": encName, "Hash": newHash, "OriginalHash": oldHash, "NameSignatureEmail": signedBy,
	}), nil)
}

// renameRequest writes a new name, of whichever endpoint serves the tree the
// item is in.
func renameRequest(dc *Context, linkID string, body map[string]any) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "PUT", Body: body,
			Path: fmt.Sprintf("/drive/unauth/v2/volumes/%s/links/%s/rename", dc.VolumeID, linkID),
		}
	}
	return proton.Request{
		Method: "PUT", Body: body,
		Path: fmt.Sprintf("/drive/shares/%s/links/%s/rename", dc.ShareID, linkID),
	}
}

// ResolveFolder resolves a path that has to be a folder, which is what a
// destination is.
//
// A destination is resolved once for the whole of a move or a copy rather than
// per item: it is the same folder every time, and a preview that has not looked
// for it would promise work into a folder that is not there.
func (s *Service) ResolveFolder(ctx context.Context, dc *Context, path string) (*Resolved, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if !res.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", path)
	}
	return res, nil
}

// Move puts an item into a folder that has already been resolved.
func (s *Service) Move(ctx context.Context, dc *Context, sourcePath string, dst *Resolved) error {
	src, err := s.ResolvePath(ctx, dc, sourcePath)
	if err != nil {
		return err
	}
	hk, err := hashKeyOf(dst.Link, dst.NodeKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(strings.ToLower(src.Name), hk)
	if err != nil {
		return err
	}
	encName, err := reEncryptName(src.Link.Name, src.Name, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	newPass, _, err := reEncryptNodePassphrase(src.Link, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return fmt.Errorf("re-encrypt passphrase: %w", err)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/links/%s/move", src.dc.ShareID, src.LinkID),
		Body: map[string]any{
			"Name": encName, "Hash": newHash, "ParentLinkID": dst.LinkID,
			"NodePassphrase": newPass, "NameSignatureEmail": dc.AddrEmail,
		},
	}, nil)
}

// Copy duplicates a file into an already-resolved folder. The node passphrase
// and name are re-encrypted to that folder's node key (the content is copied
// server-side); the source is left in place.
func (s *Service) Copy(ctx context.Context, dc *Context, sourcePath string, dst *Resolved) error {
	src, err := s.ResolvePath(ctx, dc, sourcePath)
	if err != nil {
		return err
	}
	hk, err := hashKeyOf(dst.Link, dst.NodeKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(strings.ToLower(src.Name), hk)
	if err != nil {
		return err
	}
	encName, err := reEncryptName(src.Link.Name, src.Name, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	newPass, _, err := reEncryptNodePassphrase(src.Link, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return fmt.Errorf("re-encrypt passphrase: %w", err)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/volumes/%s/links/%s/copy", dc.VolumeID, src.LinkID),
		Body: map[string]any{
			"Name": encName, "Hash": newHash, "NodePassphrase": newPass,
			"TargetVolumeID": dc.VolumeID, "TargetParentLinkID": dst.LinkID,
			"NameSignatureEmail": dc.AddrEmail,
		},
	}, nil)
}

// Trash moves items to the trash, from where they can be restored.
//
// It takes the IDs a selection already resolved rather than paths: acting on
// what was chosen is what makes the count in a confirmation true, and resolving
// a path a second time is a chance for it to mean something else by then.
func (s *Service) Trash(ctx context.Context, dc *Context, linkIDs []string) ([]Refused, error) {
	return s.linkBatches(ctx, linkIDs, func(batch []string) proton.Request {
		return proton.Request{
			Method: "POST", Path: "/drive/v2/volumes/" + dc.VolumeID + "/trash_multiple",
			Body: map[string]any{"LinkIDs": batch},
		}
	})
}

// Removal is what deleting a selection will do, worked out before any of it is
// done: which links the requests will name, in which order, and what the tree
// refuses before anything is asked of it.
type Removal struct {
	// Refused is what will not be deleted, named the way the caller named it.
	Refused []Refused

	// deep are the links the requests name, by how deep each one sits.
	deep levels
	// chosen marks the links the caller named. A refusal about anything else is
	// covered by the refusal of the folder holding it, which is on the screen.
	chosen map[string]bool
}

// levels holds links by how deep each one sits, the shallowest first.
//
// A public link gives back only an empty folder, and answers a whole request
// against the tree as it stood when the request arrived - so a folder and what
// is inside it cannot be named in one breath, however they are ordered within
// it. The depths are what a removal is sent in, one round each, from the bottom.
type levels [][]string

// merged folds another set of links into this one, offset levels deeper.
func (l levels) merged(other levels, offset int) levels {
	for i, level := range other {
		for len(l) <= i+offset {
			l = append(l, nil)
		}
		l[i+offset] = append(l[i+offset], level...)
	}
	return l
}

// all is every link the plan names, which is what a tree of your own takes in
// one go: its trash accepts a folder with everything still inside it.
func (l levels) all() []string {
	var out []string
	for _, level := range l {
		out = append(out, level...)
	}
	return out
}

// removing is what deleting these links will do where there is nothing to work
// out: a tree of your own takes back whatever it is handed, whole.
func removing(linkIDs []string) *Removal {
	plan := &Removal{chosen: make(map[string]bool, len(linkIDs))}
	if len(linkIDs) > 0 {
		plan.deep = levels{linkIDs}
	}
	for _, id := range linkIDs {
		plan.chosen[id] = true
	}
	return plan
}

// PlanDelete works out what deleting these items will send.
//
// Your own files go into the trash and out of it whole, so there is nothing to
// work out: the IDs the selection already resolved are what the requests name. A
// public link gives back only what this account put there, and only an empty
// folder, so each item is judged and each folder is read to the bottom first -
// which is what makes the count in the question true, and its refusals sentences
// somebody reads before answering rather than after.
func (s *Service) PlanDelete(ctx context.Context, dc *Context, rows []Child) (*Removal, error) {
	chosen := make([]string, 0, len(rows))
	for _, row := range rows {
		chosen = append(chosen, row.LinkID)
	}
	plan := removing(chosen)
	if !dc.Public() {
		return plan, nil
	}
	// In a link the requests name what each chosen item takes with it, at the
	// depths Proton will part with them, rather than the chosen items alone.
	plan.deep = nil
	for _, row := range rows {
		res, err := s.ResolvePath(ctx, dc, row.Path)
		if err != nil {
			return nil, err
		}
		inside, refusal, err := s.removable(ctx, dc, res)
		if err != nil {
			return nil, err
		}
		if refusal != "" {
			plan.Refused = append(plan.Refused, Refused{Name: row.Path, LinkID: row.LinkID, Reason: refusal})
			continue
		}
		plan.deep = plan.deep.merged(inside, 0)
	}
	return plan, nil
}

// removable is everything one chosen item takes with it, by depth, and why a
// link will not let this account take it.
//
// A folder means its contents everywhere else in this CLI and it means them
// here, so what is inside one is a level deeper and goes first. Anything in
// there this account did not put there makes the whole folder somebody else's -
// said once, about the thing that was chosen, rather than about a file its owner
// never named.
func (s *Service) removable(ctx context.Context, dc *Context, res *Resolved) (levels, string, error) {
	owned, err := s.ownership(ctx, dc, res.Link)
	if err != nil {
		return nil, "", err
	}
	if about, instead := owned.words("delete"); about != "" {
		return nil, about + " " + instead, nil
	}
	taken := levels{{res.LinkID}}
	if !res.IsFolder {
		return taken, "", nil
	}
	children, err := s.listRawChildren(ctx, dc, res.LinkID)
	if err != nil {
		return nil, "", err
	}
	for _, child := range children {
		childKR, err := unlockNode(&child, res.NodeKR, dc.AddrKR)
		if err != nil {
			return nil, "", fmt.Errorf("unlock %s: %w", child.LinkID, err)
		}
		below, refusal, err := s.removable(ctx, dc, &Resolved{
			dc: dc, Parent: res, LinkID: child.LinkID, ParentKR: res.NodeKR, NodeKR: childKR,
			IsFolder: child.Type == protonFolder, Link: &child,
		})
		if err != nil {
			return nil, "", err
		}
		if refusal != "" {
			return nil, "holds something that is not yours to delete. " + linkChangeRule("delete"), nil
		}
		taken = taken.merged(below, 1)
	}
	return taken, "", nil
}

// Delete removes items for good.
//
// Your own files go through the trash, because Proton deletes out of it - which
// is why a permanent delete there is two rounds of requests rather than one, and
// an item the first round refused is not offered to the second. A public link
// has no trash: what was uploaded into one is handed straight back, a depth at a
// time from the bottom.
func (s *Service) Delete(ctx context.Context, dc *Context, plan *Removal) ([]Refused, error) {
	if dc.Public() {
		var refused []Refused
		for depth := len(plan.deep) - 1; depth >= 0; depth-- {
			gone, err := s.linkBatches(ctx, plan.deep[depth], func(batch []string) proton.Request {
				return proton.Request{
					Method: "POST", Path: fmt.Sprintf("/drive/unauth/v2/volumes/%s/remove-mine", dc.VolumeID),
					Body: map[string]any{"LinkIDs": batch},
				}
			})
			refused = append(refused, gone...)
			if err != nil {
				return plan.onlyChosen(ctx, refused), err
			}
		}
		return plan.onlyChosen(ctx, refused), nil
	}
	all := plan.deep.all()
	refused, err := s.Trash(ctx, dc, all)
	if err != nil {
		return refused, err
	}
	trashed := make([]string, 0, len(all))
	for _, id := range all {
		if !refusedIn(refused, id) {
			trashed = append(trashed, id)
		}
	}
	gone, err := s.linkBatches(ctx, trashed, func(batch []string) proton.Request {
		return proton.Request{
			Method: "POST", Path: "/drive/v2/volumes/" + dc.VolumeID + "/trash/delete_multiple",
			Body: map[string]any{"LinkIDs": batch},
		}
	})
	return append(refused, gone...), err
}

// onlyChosen keeps the refusals about what the caller chose.
//
// A refusal about something inside a folder they chose is not theirs to count:
// the folder it is in is refused with it, and that refusal is on the screen.
func (r *Removal) onlyChosen(ctx context.Context, refused []Refused) []Refused {
	out := make([]Refused, 0, len(refused))
	for _, one := range refused {
		if r.chosen[one.LinkID] {
			out = append(out, one)
			continue
		}
		slog.DebugContext(ctx, "drive: something inside a chosen folder was refused, so the folder is refused with it",
			"link", one.LinkID, "error", one.Reason)
	}
	return out
}

func refusedIn(refused []Refused, linkID string) bool {
	for _, r := range refused {
		if r.LinkID == linkID {
			return true
		}
	}
	return false
}
