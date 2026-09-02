package drive

import (
	"context"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
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
		return nil, fmt.Errorf("%s is not a folder", path)
	}
	raw, err := s.listRawChildren(ctx, res.ShareID, res.LinkID)
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
		return nil, fmt.Errorf("%s is not a folder", path)
	}
	return s.walk(ctx, res.ShareID, res.LinkID, res.NodeKR, strings.TrimRight(path, "/"))
}

func (s *Service) walk(ctx context.Context, shareID, linkID string, parentKR *pgp.KeyRing, prefix string) ([]Child, error) {
	raw, err := s.listRawChildren(ctx, shareID, linkID)
	if err != nil {
		return nil, err
	}
	var out []Child
	for _, r := range raw {
		name, err := decryptName(r.Name, parentKR)
		if err != nil {
			name = "(decrypt failed)"
		}
		full := prefix + "/" + name
		out = append(out, Child{LinkID: r.LinkID, Name: name, Path: full, Type: linkType(r.Type), Size: r.Size, CreateTime: r.CreateTime, ModifyTime: r.ModifyTime})
		if r.Type == 1 {
			childKR, err := unlockNode(&r, parentKR, nil)
			if err != nil {
				continue
			}
			nested, err := s.walk(ctx, shareID, r.LinkID, childKR, full)
			if err != nil {
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
		child, err := s.createFolder(ctx, dc, parent, baseOf(path))
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
	shareID string
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
	return &folder{
		shareID: res.ShareID, linkID: res.LinkID, path: path,
		nodeKR: res.NodeKR, hashKey: hashKey,
	}, nil
}

func (s *Service) createFolder(ctx context.Context, dc *Context, parent *folder, name string) (*folder, error) {
	hash, err := lookupHash(strings.ToLower(name), parent.hashKey)
	if err != nil {
		return nil, err
	}
	encName, err := encryptName(name, parent.nodeKR, dc.AddrKR)
	if err != nil {
		return nil, err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(parent.nodeKR, dc.AddrKR)
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
	var r struct{ Folder struct{ ID string } }
	err = s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/shares/" + parent.shareID + "/folders",
		Body: map[string]any{
			"Name":                    encName,
			"Hash":                    hash,
			"ParentLinkID":            parent.linkID,
			"NodePassphrase":          nodePass,
			"NodePassphraseSignature": nodePassSig,
			"SignatureAddress":        dc.AddrEmail,
			"NodeKey":                 nodeKey,
			"NodeHashKey":             hashKeyEnc,
		},
	}, &r)
	if proton.AlreadyExists(err) {
		return nil, &errs.Exists{Kind: TypeFolder, Name: name, Where: parent.path}
	}
	if err != nil {
		return nil, err
	}
	return &folder{
		shareID: parent.shareID, linkID: r.Folder.ID, path: join(parent.path, name),
		nodeKR: nodeKR, hashKey: hashKey,
	}, nil
}

func (s *Service) Rename(ctx context.Context, dc *Context, path, newName string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	parentLink, err := s.getLink(ctx, res.ShareID, res.Link.ParentLinkID)
	if err != nil {
		return err
	}
	hk, err := hashKeyOf(parentLink, res.ParentKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(strings.ToLower(newName), hk)
	if err != nil {
		return err
	}
	oldHash, _ := lookupHash(strings.ToLower(res.Name), hk)
	encName, err := encryptName(newName, res.ParentKR, dc.AddrKR)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/links/%s/rename", res.ShareID, res.LinkID),
		Body: map[string]any{"Name": encName, "Hash": newHash, "OriginalHash": oldHash, "NameSignatureEmail": dc.AddrEmail},
	}, nil)
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
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/links/%s/move", src.ShareID, src.LinkID),
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

// Delete removes items for good.
//
// Proton deletes out of the trash, so anything still in the tree is put there
// first - which is why a permanent delete is two rounds of requests rather than
// one. An item the first round refused is not offered to the second.
func (s *Service) Delete(ctx context.Context, dc *Context, linkIDs []string) ([]Refused, error) {
	refused, err := s.Trash(ctx, dc, linkIDs)
	if err != nil {
		return refused, err
	}
	trashed := make([]string, 0, len(linkIDs))
	for _, id := range linkIDs {
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

func refusedIn(refused []Refused, linkID string) bool {
	for _, r := range refused {
		if r.LinkID == linkID {
			return true
		}
	}
	return false
}
