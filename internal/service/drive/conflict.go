package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// OnConflict is what to do when the folder already holds the name being written.
//
// The three answers are the ones Proton's own client offers when it finds a
// duplicate: replace what is there, keep both, or leave it alone
// (TransferConflictStrategy, applications/drive UploadConflictModal).
type OnConflict string

const (
	// ConflictRefuse is the absence of an answer. Nothing is checked and nothing
	// is guessed: Proton refuses the write and the caller says which name was
	// taken, which is the CLI's version of the question the web asks.
	ConflictRefuse OnConflict = ""
	// ConflictRename keeps both by adding a number to the name being written,
	// leaving what is already there untouched.
	ConflictRename OnConflict = "rename"
	// ConflictReplace writes the bytes as a new revision of the file already
	// there, so the file keeps its identity and its history.
	ConflictReplace OnConflict = "replace"
	// ConflictSkip leaves what is there alone and writes nothing.
	ConflictSkip OnConflict = "skip"
)

// UploadPlan is what an upload will do, worked out before it does it: which
// folder receives the bytes, which name they land under, and whether they become
// a new file, a new revision of the one already there, or nothing at all.
//
// Planning is a step of its own because a command has to report what it did and a
// dry run has to promise what it would do, and both are sentences about the
// answer to the duplicate question rather than about the request.
type UploadPlan struct {
	// Name is what the file will be called, which is not what was asked for when
	// both are being kept.
	Name string
	// Dest is the folder that receives it, as the caller named it.
	Dest string
	// Revision reports that the bytes become a new revision of the file already
	// there rather than a new file.
	Revision bool
	// Nothing reports that the file is already there and is being left alone.
	Nothing bool

	parent *Resolved
	hash   string
	// target is the file a new revision is being written to, with the revision
	// that is current now: Proton wants to be told which one is being succeeded.
	target *Resolved
	from   string
}

// PlanUpload decides what uploading name into destPath will do.
//
// Without an answer to the duplicate question there is nothing to look up: the
// upload goes ahead and Proton's own refusal is the answer, so the common case
// costs no extra request. With one, the names in the way are checked first,
// which is the same order the web client works in - it asks what to do only
// once it knows there is a duplicate.
func (s *Service) PlanUpload(ctx context.Context, dc *Context, destPath, name string, on OnConflict) (*UploadPlan, error) {
	parent, err := s.ResolvePath(ctx, dc, destPath)
	if err != nil {
		return nil, fmt.Errorf("target folder not found: %w", err)
	}
	if !parent.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", parent.Describe(destPath))
	}
	hashKey, err := hashKeyOf(parent.Link, parent.NodeKR)
	if err != nil {
		return nil, err
	}
	hash, err := lookupHash(strings.ToLower(name), hashKey)
	if err != nil {
		return nil, err
	}
	plan := &UploadPlan{Name: name, Dest: destPath, parent: parent, hash: hash}
	if on == ConflictRefuse {
		return plan, nil
	}

	free, err := s.freeNames(ctx, parent, hashKey, name)
	if err != nil {
		return nil, err
	}
	if free.taken == "" {
		return plan, nil
	}
	switch on {
	case ConflictSkip:
		plan.Nothing = true
	case ConflictRename:
		plan.Name, plan.hash = free.name, free.hash
	case ConflictReplace:
		target, err := s.ResolvePath(ctx, dc, strings.TrimSuffix(destPath, "/")+"/"+name)
		if err != nil {
			return nil, err
		}
		// A folder in the way is not something a new revision can be written to,
		// and trashing it would be a deletion this command never announced.
		if target.IsFolder {
			return nil, &errs.Exists{Kind: TypeFolder, Name: name, Where: destPath}
		}
		if target.Link.FileProperties == nil {
			return nil, fmt.Errorf("%s: no file properties", name)
		}
		plan.Revision, plan.target, plan.from = true, target, target.Link.FileProperties.ActiveRevision.ID
	}
	return plan, nil
}

// TreeItem is one thing a tree upload means to write, named by where it goes.
type TreeItem struct {
	Path  string
	IsDir bool
}

// TreePlan is what uploading a directory will do, worked out before any of it is
// done.
//
// A tree is one thing with one name, so the answer to a name already taken is
// given about the tree's own folder, the way Proton's own client gives it: kept
// both puts the whole tree beside what is there under a numbered name, skipped
// writes none of it, and replaced lands it in the folder already there, file by
// file.
type TreePlan struct {
	// Top is where the tree lands, which is not where it was asked to land when
	// its name was taken and both are being kept.
	Top string
	// Nothing reports that the tree is already there and is being left alone.
	Nothing bool
	// Folders are the folders to make, parents before children. A folder already
	// there is not among them: it is what the files go into.
	Folders []string
	// Files are the files to write.
	Files []TreeFile
}

// TreeFile is one file a tree upload will write.
type TreeFile struct {
	Path string
	// Replaces says a file of that name is there already, so this one becomes its
	// next revision rather than a second file.
	Replaces bool
}

// PlanTree decides what uploading a directory into top will do.
//
// Nothing is written and nothing is half-decided: a folder standing where a file
// goes, or a file standing where a folder goes, is refused here rather than
// found part way through, because neither can become the other and an upload is
// not a command that removes things.
func (s *Service) PlanTree(ctx context.Context, dc *Context, top string, items []TreeItem, on OnConflict) (*TreePlan, error) {
	st, err := s.resolveTo(ctx, dc, top)
	if err != nil {
		return nil, err
	}
	if !st.at.IsFolder {
		return nil, &errs.Exists{Kind: TypeFile, Name: st.at.Name, Where: dirOf(st.path)}
	}
	// A tree makes its own folders, but not the one it was told to land in: a
	// destination that is not there is a mistyped destination.
	if len(st.missing) > 1 {
		return nil, fmt.Errorf("target folder not found: %w", &errs.NotFound{Kind: "path", Ref: st.missing[0]})
	}
	if len(st.missing) == 1 {
		return freshTree(top, top, items), nil
	}

	switch on {
	case ConflictRefuse:
		return nil, &errs.Exists{Kind: TypeFolder, Name: baseOf(top), Where: dirOf(top)}
	case ConflictSkip:
		return &TreePlan{Top: top, Nothing: true}, nil
	case ConflictRename:
		parent, err := s.ResolvePath(ctx, dc, dirOf(top))
		if err != nil {
			return nil, err
		}
		hashKey, err := hashKeyOf(parent.Link, parent.NodeKR)
		if err != nil {
			return nil, err
		}
		free, err := s.freeNames(ctx, parent, hashKey, baseOf(top))
		if err != nil {
			return nil, err
		}
		return freshTree(top, join(dirOf(top), free.name), items), nil
	}

	holds, err := s.holdings(ctx, dc, top, items)
	if err != nil {
		return nil, err
	}
	plan := &TreePlan{Top: top}
	for _, it := range items {
		kind, taken := holds[it.Path]
		switch {
		case it.IsDir && !taken:
			plan.Folders = append(plan.Folders, it.Path)
		case it.IsDir && kind == TypeFolder:
		case it.IsDir:
			return nil, &errs.Exists{Kind: TypeFile, Name: baseOf(it.Path), Where: dirOf(it.Path)}
		case !taken:
			plan.Files = append(plan.Files, TreeFile{Path: it.Path})
		case kind == TypeFile:
			plan.Files = append(plan.Files, TreeFile{Path: it.Path, Replaces: true})
		default:
			return nil, &errs.Exists{Kind: TypeFolder, Name: baseOf(it.Path), Where: dirOf(it.Path)}
		}
	}
	return plan, nil
}

// freshTree is the plan for a tree that lands somewhere nothing is in the way,
// which is every path under a folder that did not exist or has just been given a
// name of its own: there is nothing left to ask about any of it.
func freshTree(asked, top string, items []TreeItem) *TreePlan {
	plan := &TreePlan{Top: top, Folders: []string{top}}
	for _, it := range items {
		path := top + strings.TrimPrefix(it.Path, asked)
		if it.IsDir {
			plan.Folders = append(plan.Folders, path)
			continue
		}
		plan.Files = append(plan.Files, TreeFile{Path: path})
	}
	return plan
}

// holdings reports what the destination already holds at each of the tree's
// paths.
//
// Only folders that exist and that the tree wants are read, so the cost is the
// overlap between the two trees rather than the size of either: a folder that is
// not there cannot hold anything, and one the tree does not enter is nobody's
// business.
func (s *Service) holdings(ctx context.Context, dc *Context, top string, items []TreeItem) (map[string]string, error) {
	wanted := map[string]bool{}
	for _, it := range items {
		if it.IsDir {
			wanted[it.Path] = true
		}
	}
	holds := map[string]string{}
	for queue := []string{top}; len(queue) > 0; queue = queue[1:] {
		children, err := s.List(ctx, dc, queue[0])
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			path := join(queue[0], child.Name)
			holds[path] = child.Type
			if child.Type == TypeFolder && wanted[path] {
				queue = append(queue, path)
			}
		}
	}
	return holds, nil
}

func join(dir, name string) string { return strings.TrimSuffix(dir, "/") + "/" + name }

// available is the answer to "is this name free, and if not, what is": the name
// asked for when it is free, and otherwise the first numbered variant that is.
type available struct {
	// taken is the name that is in the way, empty when nothing is.
	taken string
	name  string
	hash  string
}

// freeNames asks Proton which of a batch of candidate names are free.
//
// The candidates are the name itself and then the numbered variants of it, ten
// at a time, exactly as the web client does: it is one small request for the
// question "is this taken" and the question "what should it be called instead",
// which are the same question asked of different names.
//
// Only names Proton reports as available are treated as free. A name held by
// somebody's unfinished upload is left alone, because this client has no way to
// tell its own abandoned draft from another's.
func (s *Service) freeNames(ctx context.Context, parent *Resolved, hashKey []byte, name string) (available, error) {
	base, ext := splitName(name)
	for start := 0; start < 100; start += hashBatch {
		candidates := make([]available, 0, hashBatch)
		hashes := make([]string, 0, hashBatch)
		for i := start; i < start+hashBatch; i++ {
			candidate := numberedName(i, base, ext)
			hash, err := lookupHash(strings.ToLower(candidate), hashKey)
			if err != nil {
				return available{}, err
			}
			candidates = append(candidates, available{name: candidate, hash: hash})
			hashes = append(hashes, hash)
		}
		var r struct{ AvailableHashes []string }
		if err := s.C.Decode(ctx, hashesRequest(parent.dc, parent.LinkID, hashes), &r); err != nil {
			return available{}, err
		}
		isFree := map[string]bool{}
		for _, h := range r.AvailableHashes {
			isFree[h] = true
		}
		for i, c := range candidates {
			if !isFree[c.hash] {
				continue
			}
			if start == 0 && i == 0 {
				return available{}, nil
			}
			return available{taken: name, name: c.name, hash: c.hash}, nil
		}
	}
	return available{}, fmt.Errorf("no free name for %q in %s", name, parent.Name)
}

// hashBatch is how many candidate names are checked per request, matching the
// web client's HASH_CHECK_AMOUNT.
const hashBatch = 10

// hashesRequest asks a folder which of a batch of names are free, of whichever
// endpoint serves the tree it is in.
//
// Reads: it is a POST because the batch of hashes is a body rather than a query,
// and it changes nothing - so a dry run may ask it, which is what lets a preview
// promise the name a file would really land under.
func hashesRequest(dc *Context, parentLinkID string, hashes []string) proton.Request {
	body := map[string]any{"Hashes": hashes}
	if dc.Public() {
		return proton.Request{
			Method: "POST", Reads: true, Body: body,
			Path: fmt.Sprintf("/drive/urls/%s/files/%s/checkAvailableHashes", dc.Token, parentLinkID),
		}
	}
	return proton.Request{
		Method: "POST", Reads: true, Body: body,
		Path: fmt.Sprintf("/drive/shares/%s/links/%s/checkAvailableHashes", dc.ShareID, parentLinkID),
	}
}

// fileRequest opens a file draft, of whichever endpoint serves the tree it is
// going into.
func fileRequest(dc *Context, body map[string]any) proton.Request {
	if dc.Public() {
		return proton.Request{
			Method: "POST", Path: fmt.Sprintf("/drive/urls/%s/files", dc.Token), Body: body,
		}
	}
	return proton.Request{
		Method: "POST", Path: fmt.Sprintf("/drive/shares/%s/files", dc.ShareID), Body: body,
	}
}

// numberedName is the name a numbered copy takes, the same string the web client
// builds (adjustName, packages/drive-store/store/_links/link.ts): the number goes
// before the extension, in brackets, after a space.
func numberedName(i int, base, ext string) string {
	if i == 0 {
		if ext == "" {
			return base
		}
		return base + "." + ext
	}
	if base == "" {
		return fmt.Sprintf(".%s (%d)", ext, i)
	}
	if ext == "" {
		return fmt.Sprintf("%s (%d)", base, i)
	}
	return fmt.Sprintf("%s (%d).%s", base, i, ext)
}

// splitName splits a name into the part a number is added to and the extension,
// which a trailing dot and a leading dot both belong to the name itself
// (splitLinkName, the same file).
func splitName(name string) (base, ext string) {
	if strings.HasSuffix(name, ".") {
		return name, ""
	}
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i+1:]
}

// startRevision opens the revision the bytes will be written to and hands back
// the keys they are encrypted with.
//
// A new file brings its own keys. A new revision of a file that already exists
// uses that file's keys, because a revision is another version of the same
// encrypted thing rather than a new one.
func (s *Service) startRevision(ctx context.Context, dc *Context, plan *UploadPlan, by author, mimeType string) (
	linkID, revisionID string, sessionKey *pgp.SessionKey, nodeKR *pgp.KeyRing, err error,
) {
	if plan.Revision {
		link := plan.target
		kp, err := base64.StdEncoding.DecodeString(link.Link.FileProperties.ContentKeyPacket)
		if err != nil {
			return "", "", nil, nil, err
		}
		sessionKey, err := link.NodeKR.DecryptSessionKey(kp)
		if err != nil {
			return "", "", nil, nil, fmt.Errorf("get file session key: %w", err)
		}
		var r struct{ Revision struct{ ID string } }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST",
			Path:   fmt.Sprintf("/drive/shares/%s/files/%s/revisions", link.dc.ShareID, link.LinkID),
			Body:   map[string]any{"CurrentRevisionID": plan.from},
		}, &r); err != nil {
			return "", "", nil, nil, err
		}
		return link.LinkID, r.Revision.ID, sessionKey, link.NodeKR, nil
	}

	parent := plan.parent
	signKR := by.key(parent.NodeKR)
	encName, err := encryptName(plan.Name, parent.NodeKR, signKR)
	if err != nil {
		return "", "", nil, nil, err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(parent.NodeKR, signKR)
	if err != nil {
		return "", "", nil, nil, err
	}
	nodeKR, err = pgp.NewKeyRing(nodePriv)
	if err != nil {
		return "", "", nil, nil, err
	}
	sessionKey, contentKP, contentKPSig, err := genFileKeys(nodeKR)
	if err != nil {
		return "", "", nil, nil, err
	}
	body := map[string]any{
		"Name": encName, "Hash": plan.hash,
		"ParentLinkID":   parent.LinkID,
		"NodePassphrase": nodePass, "NodePassphraseSignature": nodePassSig,
		"NodeKey":                   nodeKey,
		"MIMEType":                  mimeType,
		"ContentKeyPacket":          contentKP,
		"ContentKeyPacketSignature": contentKPSig,
	}
	by.attribute(body, dc)
	var created struct {
		File struct{ ID, RevisionID string }
	}
	if err := s.C.Decode(ctx, fileRequest(dc, body), &created); err != nil {
		// Proton is the one that knows the name is taken when nobody asked it to
		// look, so this is where that answer gets its words.
		if proton.AlreadyExists(err) {
			return "", "", nil, nil, &errs.Exists{Kind: TypeFile, Name: plan.Name, Where: plan.Dest}
		}
		return "", "", nil, nil, err
	}
	return created.File.ID, created.File.RevisionID, sessionKey, nodeKR, nil
}
