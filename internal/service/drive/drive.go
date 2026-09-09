// Package drive provides Proton Drive operations.
package drive

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Client is what Drive asks of the transport: every request, and the handshake
// that opens a public link, which is SRP and so lives beside signing in.
type Client interface {
	proton.Doer
	PublicLinkInfo(ctx context.Context, token string) (*proton.PublicLinkInfo, error)
	PublicLinkAuth(ctx context.Context, token string, info *proton.PublicLinkInfo, password string) (*proton.PublicLinkShare, error)
}

type Service struct {
	C    Client
	keys keys.Get
}

func New(c Client, k keys.Get) *Service { return &Service{C: c, keys: k} }

type Context struct {
	ShareID string
	// Token names the public link a tree hangs from, and is empty for a share.
	// It is the other way a tree can be reached, so it is what decides where a
	// request about this tree goes.
	Token string
	// URL is the link this tree was opened with, and LinkPassword the one its
	// owner set on it. Both are empty for a share, and the password is empty for a
	// link that has none.
	URL          string
	LinkPassword string
	ShareKR      *pgp.KeyRing
	AddrKR       *pgp.KeyRing
	AddrID       string
	AddrEmail    string
	VolumeID     string
	RootLinkID   string
	// Permissions is what the tree permits whoever opened it, as Proton numbers
	// them: what a link grants its readers, or what your membership in somebody
	// else's share grants you. A tree of your own is nobody's to grant and
	// carries none.
	Permissions int
	// Anonymous reports that the tree was opened without an account, which only a
	// public link can be. Nothing written there can be attributed to anybody, and
	// nothing about it can be signed with an address key there is none of.
	Anonymous bool
	// Type is the kind of share the tree hangs from, as Proton numbers them.
	Type int
	// RootName is what the tree is called: a computer's name, or the name of the
	// item somebody shared. It is empty for the volumes Proton's own clients
	// label rather than name, and for a root whose name will not decrypt.
	RootName string

	// rootLink is the share's root, fetched while the share itself was being
	// fetched. Everything addressed by path starts from it, so resolving the
	// share without it would only mean asking for it a moment later, alone.
	rootLink *Link
}

// Public reports whether this tree is a public link.
//
// Proton serves a link's tree under the token that names it, in place of the
// share ID nobody outside the share has, so the two are reached through
// endpoints of their own - for writing as much as for reading.
func (dc *Context) Public() bool { return dc.Token != "" }

// CanEdit reports whether things may be written into this tree.
//
// Your own files are yours to write to. A link and a share somebody shared with
// you are theirs, and each says what it grants when it opens - which is what
// lets an upload into one be refused before anything is planned.
func (dc *Context) CanEdit() bool {
	return dc.Permissions == 0 || dc.Permissions&permWrite != 0
}

// handle names the tree in a log record, whichever way it is reached.
func (dc *Context) handle() string {
	if dc.Public() {
		return dc.Token
	}
	return dc.ShareID
}

// RootKR is the key ring of the share's root.
//
// It is not the share key. The share key opens the root link's passphrase; the
// root's own node key is what the names and passphrases of everything directly
// inside it are sealed to, and reading one with the other yields nothing rather
// than an error - so this is derived in one place instead of at each of them.
func (dc *Context) RootKR() (*pgp.KeyRing, error) {
	kr, err := unlockNode(dc.rootLink, dc.ShareKR, dc.AddrKR)
	if err != nil {
		return nil, fmt.Errorf("unlock the root of share %s: %w", dc.handle(), err)
	}
	return kr, nil
}

func (s *Service) Resolve(ctx context.Context) (*Context, error) {
	var r struct {
		Volumes []struct {
			VolumeID string
			Share    struct{ ShareID, LinkID string }
		}
	}
	// The keys leave with the first request rather than after it: the volume has
	// to answer before the share can be named, and the share cannot be opened
	// without them.
	if _, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/volumes"}, &r)
	}); err != nil {
		return nil, err
	}
	if len(r.Volumes) == 0 {
		return nil, fmt.Errorf("no volumes found")
	}
	return s.unlockShare(ctx, r.Volumes[0].Share.ShareID, r.Volumes[0].Share.LinkID, r.Volumes[0].VolumeID)
}

// ResolvePhotos resolves the dedicated photos share (ShareType 4) and unwraps
// its keys, parallel to Resolve for the main volume.
func (s *Service) ResolvePhotos(ctx context.Context) (*Context, error) {
	var r struct {
		Shares []struct {
			ShareID  string
			LinkID   string
			VolumeID string
			Type     int
			State    int
			Locked   bool
		}
	}
	q := url.Values{}
	q.Set("ShowAll", "1")
	if _, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares", Query: q}, &r)
	}); err != nil {
		return nil, err
	}
	for _, sh := range r.Shares {
		if sh.Type == 4 && !sh.Locked {
			return s.unlockShare(ctx, sh.ShareID, sh.LinkID, sh.VolumeID)
		}
	}
	return nil, &errs.NotFound{Kind: "photos share"}
}

// unlockShare opens a share's key and its root folder.
//
// The share, the root folder and the account's own keys are asked for at the same
// time: the volume named the first two and the third depends on nothing, so only
// the unwrapping that follows has an order.
func (s *Service) unlockShare(ctx context.Context, shareID, rootLinkID, volumeID string) (*Context, error) {
	var sh struct {
		AddressID           string
		Key                 string
		Passphrase          string
		PassphraseSignature string
		Type                int
		// Memberships is your own standing in the share, which Proton sends for a
		// share somebody shared with you and leaves empty for one of your own.
		Memberships []struct{ Permissions int }
	}
	var rootLink *Link
	var u *keys.Unlocked
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + shareID}, &sh)
		},
		func(ctx context.Context) error {
			var err error
			rootLink, err = s.getLink(ctx, shareID, rootLinkID)
			return err
		},
		func(ctx context.Context) error {
			var err error
			u, err = s.keys(ctx)
			return err
		},
	); err != nil {
		return nil, err
	}
	addrKR, ok := u.AddrKR(sh.AddressID)
	if !ok {
		return nil, fmt.Errorf("no key ring for address %s", sh.AddressID)
	}
	var addrEmail string
	for _, a := range u.Addresses {
		if a.ID == sh.AddressID {
			addrEmail = a.Email
			break
		}
	}
	enc, err := pgp.NewPGPMessageFromArmored(sh.Passphrase)
	if err != nil {
		return nil, err
	}
	dec, err := addrKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("decrypt share passphrase: %w", err)
	}
	norm := pgp.NewPlainMessageFromString(string(dec.GetBinary()))
	if v := pgphelper.VerifyDetachedStatus(addrKR, norm, sh.PassphraseSignature); v != pgphelper.Verified {
		slog.Debug("drive: share passphrase signature not verified", "share", shareID, "result", string(v))
	}
	locked, err := pgp.NewKeyFromArmored(sh.Key)
	if err != nil {
		return nil, err
	}
	unlocked, err := locked.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("unlock share key: %w", err)
	}
	shareKR, err := pgp.NewKeyRing(unlocked)
	if err != nil {
		return nil, err
	}
	dc := &Context{
		ShareID: shareID, ShareKR: shareKR,
		AddrKR: addrKR, AddrID: sh.AddressID, AddrEmail: addrEmail,
		VolumeID: volumeID, RootLinkID: rootLinkID, rootLink: rootLink,
		Type: sh.Type, RootName: rootName(ctx, shareID, sh.Type, rootLink, shareKR),
	}
	if len(sh.Memberships) > 0 {
		dc.Permissions = sh.Memberships[0].Permissions
	}
	return dc, nil
}

// author is who a write is attributed to and signed by: one of the account's
// addresses, or nobody at all behind a link opened without an account.
type author struct {
	kr    *pgp.KeyRing
	email string
}

// key is what a write is signed with.
//
// A write nobody is behind is signed with the key it hangs from - the parent's
// for a name or a passphrase, the file's own for its content - which is what
// Proton's own clients sign an anonymous upload with, and what leaves the
// signature checkable by everyone who can read the item at all.
func (a author) key(fallback *pgp.KeyRing) *pgp.KeyRing {
	if a.kr == nil {
		return fallback
	}
	return a.kr
}

// attribute names the author in a request body, under whichever name the
// endpoints serving this tree call it. A write nobody is behind names nobody.
func (a author) attribute(body map[string]any, dc *Context) {
	if a.email == "" {
		return
	}
	if dc.Public() {
		body["SignatureEmail"] = a.email
		return
	}
	body["SignatureAddress"] = a.email
}

// author is who the run writes into this tree as.
//
// A share was opened through one of the account's addresses, and that is the one
// its writes are signed with. A link was opened through no address at all, so a
// signed-in caller writes as their primary address - the one Proton's own
// clients use there - and a caller with no account writes as nobody.
func (s *Service) author(ctx context.Context, dc *Context) (author, error) {
	if !dc.Public() {
		return author{kr: dc.AddrKR, email: dc.AddrEmail}, nil
	}
	if dc.Anonymous {
		return author{}, nil
	}
	u, err := s.keys(ctx)
	if err != nil {
		return author{}, err
	}
	kr, addr, err := u.PrimaryAddr()
	if err != nil {
		return author{}, err
	}
	return author{kr: kr, email: addr.Email}, nil
}

// rootName reads what a tree is called.
//
// A root has no parent whose key could hold its name, so the name is sealed to
// the share key instead. The main volume and the photo volume are labelled by
// every Proton client rather than named, so what is stored on their roots is not
// read. A name that will not open leaves the tree unnamed rather than failing:
// recorded and not counted, because nothing goes missing from an answer - the
// tree is still listed, still addressable by ID, and the gap is on the screen.
func rootName(ctx context.Context, shareID string, shareType int, root *Link, shareKR *pgp.KeyRing) string {
	if shareType == shareTypeMain || shareType == shareTypePhotos || root == nil {
		return ""
	}
	name, err := decryptName(root.Name, shareKR)
	if err != nil {
		slog.DebugContext(ctx, "drive: a tree's own name could not be decrypted",
			"share", shareID, "error", err)
		return ""
	}
	return name
}

type Link struct {
	LinkID       string
	ParentLinkID string
	Type         int // 1=folder, 2=file
	Size         int64
	Name         string
	// Hash is the name's lookup hash under the parent folder's hash key, which a
	// rename hands back so Proton can tell which name it is replacing. A root has
	// no parent to be hashed under and carries none.
	Hash                    string
	EncName                 string
	MIMEType                string
	NodeKey                 string
	NodePassphrase          string
	NodePassphraseSignature string
	SignatureEmail          string
	CreateTime              int64
	ModifyTime              int64
	RealModifyTime          int64
	// Trashed is when the item was moved to the trash, and is absent for one
	// that is not in it.
	Trashed          int64
	XAttr            string
	ShareIDs         []string
	ShareUrls        []struct{ ShareURLID string }
	FolderProperties *FolderProperties
	AlbumProperties  *AlbumProperties
	PhotoProperties  *struct {
		Albums []struct{ AlbumLinkID string }
		Tags   []int
	}
	FileProperties *FileProperties
}

// FolderProperties and AlbumProperties each carry the key the names of what is
// inside are hashed under. Proton names them apart, so they are named apart.
type (
	FolderProperties struct{ NodeHashKey string }
	AlbumProperties  struct{ NodeHashKey string }
)

// FileProperties is what a file has and a folder does not: the key packet its
// content is encrypted under, and the version it holds now.
type FileProperties struct {
	ContentKeyPacket string
	ActiveRevision   struct {
		ID    string
		Photo struct {
			ContentHash          string
			RelatedPhotosLinkIDs []string
		}
	}
}

type Resolved struct {
	// dc is the tree the path was resolved in. A resolved item means nothing
	// outside it - its keys came from that tree, and so does the address every
	// request about it is sent to.
	dc *Context
	// Parent is the folder the walk found it in, and is nothing at all for the top
	// of a tree. A rename hashes the new name under the parent's key, and the walk
	// that found the item was holding it.
	Parent   *Resolved
	LinkID   string
	ParentKR *pgp.KeyRing
	NodeKR   *pgp.KeyRing
	Name     string
	IsFolder bool
	// Link is the record resolving the path already read. A caller that needs the
	// item's size, type or shares has it here rather than by asking for the link it
	// was just handed.
	Link *Link
}

// Describe names what a path resolved to, for a sentence somebody reads: the
// path they typed, or the tree's own name where that path was the root.
func (r *Resolved) Describe(path string) string {
	if strings.Trim(path, "/") == "" && r.Name != "" {
		return r.Name
	}
	return path
}

// IsRoot reports whether this is the top of the tree it was resolved in, which
// is the one item with no parent to be named under.
func (r *Resolved) IsRoot() bool { return r.Link != nil && r.Link.ParentLinkID == "" }

func (s *Service) ResolvePath(ctx context.Context, dc *Context, path string) (*Resolved, error) {
	st, err := s.resolveTo(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if len(st.missing) == 0 {
		return st.at, nil
	}
	if !st.at.IsFolder {
		return nil, errs.Problemf("%s is not a folder.", st.at.Name)
	}
	return nil, &errs.NotFound{Kind: "path", Ref: st.missing[0]}
}

// ResolveFile resolves a path that has to be a file, which is what a download
// and every revision operation need and no folder can answer.
func (s *Service) ResolveFile(ctx context.Context, dc *Context, path string) (*Resolved, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if res.IsFolder {
		return nil, errs.Problemf("%s is a folder, not a file.", res.Describe(path))
	}
	return res, nil
}

// stopped is where a path ran out: the deepest link that is there, the path it
// sits at, and the components below it that are not.
type stopped struct {
	at      *Resolved
	path    string
	missing []string
}

// resolveTo walks a path as far as it goes.
//
// Where the walk stopped is what makes a folder creatable together with the
// folders above it, and what lets an upload tell a destination that is not there
// from a tree that is not there yet. Asking one ancestor at a time until one
// answers would be the same walk, repeated.
func (s *Service) resolveTo(ctx context.Context, dc *Context, path string) (*stopped, error) {
	rootKR, err := dc.RootKR()
	if err != nil {
		return nil, err
	}
	// The root is an item like any other: a computer's is a folder, and one
	// somebody shared may be a single file, so both come off the link rather than
	// being assumed.
	st := &stopped{
		at: &Resolved{
			dc: dc, LinkID: dc.RootLinkID, ParentKR: dc.ShareKR,
			NodeKR: rootKR, Name: dc.RootName, IsFolder: dc.rootLink.Type == protonFolder,
			Link: dc.rootLink,
		},
		path: "/",
	}
	parts := components(path)
	for i, part := range parts {
		if !st.at.IsFolder {
			st.missing = parts[i:]
			return st, nil
		}
		child, err := s.childNamed(ctx, dc, st.at, part)
		if err != nil {
			return nil, err
		}
		if child == nil {
			st.missing = parts[i:]
			return st, nil
		}
		st.at, st.path = child, join(st.path, part)
	}
	return st, nil
}

// childNamed finds one named child of a folder, or nothing when the folder has
// no such child. A name that will not decrypt is not the name being looked for.
func (s *Service) childNamed(ctx context.Context, dc *Context, parent *Resolved, name string) (*Resolved, error) {
	children, err := s.listRawChildren(ctx, dc, parent.LinkID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		if decrypted, err := decryptName(child.Name, parent.NodeKR); err != nil || decrypted != name {
			continue
		}
		childKR, err := unlockNode(&child, parent.NodeKR, dc.AddrKR)
		if err != nil {
			return nil, fmt.Errorf("unlock %s: %w", name, err)
		}
		return &Resolved{
			dc: dc, Parent: parent, LinkID: child.LinkID,
			ParentKR: parent.NodeKR, NodeKR: childKR, Name: name,
			IsFolder: child.Type == protonFolder, Link: &child,
		}, nil
	}
	return nil, nil
}

// components splits a path into the names along it. The root is no name at all.
func components(path string) []string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" || trimmed == "." {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// linkBatch is how many links one request may name, matching BATCH_REQUEST_SIZE
// in the web clients. Proton answers such a request with a code per link, so a
// batch that succeeded as a whole can still have refused some of what it named.
const linkBatch = 50

// Refused is one item that will not be acted on, and why.
//
// A count is a promise, so a bulk verb that was refused part of what it asked
// for names the part rather than reporting the number it hoped for.
type Refused struct {
	// Name is what the caller called it, and is set where the refusal came before
	// anything was sent and the item still had a path. Reason then finishes the
	// sentence Name starts, because a warning is shown with nothing beside it.
	Name   string `json:"name,omitempty"`
	LinkID string `json:"link_id"`
	Reason string `json:"reason"`
}

// String says what was refused. A refusal Proton made names the link ID: by then
// the item may have no path left to name it by.
func (r Refused) String() string {
	if r.Name != "" {
		return fmt.Sprintf("%s %s", r.Name, r.Reason)
	}
	return fmt.Sprintf("Refused %s: %s", r.LinkID, r.Reason)
}

// linkBatches acts on many links a batch at a time, collecting what Proton
// refused. request builds the call for one batch, because the endpoints differ
// in method and path but answer the same way.
func (s *Service) linkBatches(ctx context.Context, linkIDs []string,
	request func(batch []string) proton.Request) ([]Refused, error) {
	var refused []Refused
	for _, batch := range chunk(linkIDs, linkBatch) {
		var r struct {
			Responses []struct {
				LinkID   string
				Response struct {
					Code  int
					Error string
				}
			}
		}
		if err := s.C.Decode(ctx, request(batch), &r); err != nil {
			return refused, err
		}
		for i, answer := range r.Responses {
			if proton.Succeeded(answer.Response.Code) {
				continue
			}
			id := answer.LinkID
			if id == "" && i < len(batch) {
				id = batch[i]
			}
			reason := answer.Response.Error
			if reason == "" {
				reason = "Proton did not accept it"
			}
			refused = append(refused, Refused{LinkID: id, Reason: reason})
		}
	}
	return refused, nil
}

func (s *Service) getLink(ctx context.Context, shareID, linkID string) (*Link, error) {
	var r struct{ Link Link }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/links/%s", shareID, linkID)}, &r); err != nil {
		return nil, err
	}
	return &r.Link, nil
}

// childrenPageSize is how many links one listing of a folder asks for.
const childrenPageSize = 150

// listRawChildren reads what a folder holds, of whichever endpoints serve the
// tree it is in.
func (s *Service) listRawChildren(ctx context.Context, dc *Context, linkID string) ([]Link, error) {
	if dc.Public() {
		return s.linkChildren(ctx, dc, linkID)
	}
	return proton.All(ctx, func(ctx context.Context, page int) ([]Link, bool, error) {
		q := url.Values{}
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("PageSize", fmt.Sprintf("%d", childrenPageSize))
		var r struct{ Links []Link }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Query: q,
			Path: fmt.Sprintf("/drive/shares/%s/folders/%s/children", dc.ShareID, linkID),
		}, &r); err != nil {
			return nil, false, err
		}
		return r.Links, proton.Full(r.Links, childrenPageSize), nil
	})
}

// linkDetailsBatch is how many items one reading of a link's tree asks about,
// matching API_NODES_BATCH_SIZE in Proton's Drive SDK.
const linkDetailsBatch = 100

// linkChildren reads what a folder behind a public link holds.
//
// A link's tree is served under the volume rather than the share, and in two
// steps: the folder answers with the IDs of what is in it, and those are read
// back in batches. It is the second answer that carries the address of whoever
// uploaded each item, which is what makes a link's tree something its reader can
// be told about rather than a list of anonymous names.
func (s *Service) linkChildren(ctx context.Context, dc *Context, linkID string) ([]Link, error) {
	ids, err := s.linkChildIDs(ctx, dc, linkID)
	if err != nil {
		return nil, err
	}
	out := make([]Link, 0, len(ids))
	for _, batch := range chunk(ids, linkDetailsBatch) {
		var r struct{ Links []linkDetails }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "POST", Reads: true,
			Path: fmt.Sprintf("/drive/unauth/v2/volumes/%s/links", dc.VolumeID),
			Body: map[string]any{"LinkIDs": batch},
		}, &r); err != nil {
			return nil, err
		}
		for _, details := range r.Links {
			link, finished := details.link()
			if !finished {
				// Recorded and not counted: an upload nobody finished is not an item,
				// and Proton's own clients leave one out of a listing too.
				slog.DebugContext(ctx, "drive: an unfinished upload is not listed",
					"link", details.Link.LinkID, "parent", linkID)
				continue
			}
			out = append(out, link)
		}
	}
	return out, nil
}

// linkChildIDs names what is in a folder behind a public link, a page at a time.
func (s *Service) linkChildIDs(ctx context.Context, dc *Context, linkID string) ([]string, error) {
	// The endpoint hands back the anchor its next answer starts from.
	anchor := ""
	return proton.All(ctx, func(ctx context.Context, _ int) ([]string, bool, error) {
		req := proton.Request{
			Method: "GET",
			Path:   fmt.Sprintf("/drive/unauth/v2/volumes/%s/folders/%s/children", dc.VolumeID, linkID),
		}
		if anchor != "" {
			req.Query = proton.Query("AnchorID", anchor)
		}
		var r struct {
			LinkIDs  []string
			AnchorID string
			More     bool
		}
		if err := s.C.Decode(ctx, req, &r); err != nil {
			return nil, false, err
		}
		anchor = r.AnchorID
		return r.LinkIDs, r.More && r.AnchorID != "", nil
	})
}

// linkDetails is what one item behind a public link is answered as: the item,
// and whichever of the two shapes it takes.
type linkDetails struct {
	Link struct {
		LinkID                  string
		ParentLinkID            string
		Type                    int
		CreateTime              int64
		ModifyTime              int64
		TrashTime               int64
		Name                    string
		NameHash                string
		NodeKey                 string
		NodePassphrase          string
		NodePassphraseSignature string
		SignatureEmail          string
	}
	Folder *struct {
		NodeHashKey string
		XAttr       string
	}
	File *struct {
		ContentKeyPacket string
		MediaType        string
		ActiveRevision   *struct {
			RevisionID    string
			EncryptedSize int64
			XAttr         string
		}
	}
	Sharing *struct{ ShareURLID string }
}

// link is the record the rest of Drive reads, and nothing at all for a file no
// upload ever finished: a draft holds no version to read, name or count.
func (d linkDetails) link() (Link, bool) {
	l := Link{
		LinkID: d.Link.LinkID, ParentLinkID: d.Link.ParentLinkID, Type: d.Link.Type,
		Name: d.Link.Name, Hash: d.Link.NameHash, NodeKey: d.Link.NodeKey,
		NodePassphrase: d.Link.NodePassphrase, NodePassphraseSignature: d.Link.NodePassphraseSignature,
		SignatureEmail: d.Link.SignatureEmail, CreateTime: d.Link.CreateTime,
		ModifyTime: d.Link.ModifyTime, Trashed: d.Link.TrashTime,
	}
	if d.Sharing != nil && d.Sharing.ShareURLID != "" {
		l.ShareUrls = []struct{ ShareURLID string }{{ShareURLID: d.Sharing.ShareURLID}}
	}
	switch {
	case d.Folder != nil:
		l.FolderProperties = &FolderProperties{NodeHashKey: d.Folder.NodeHashKey}
		l.XAttr = d.Folder.XAttr
	case d.File == nil:
		// Anything else is listed as what it says it is. Nothing goes missing that
		// way, and what cannot be done with it is refused where it is asked for.
	case d.File.ActiveRevision == nil:
		return Link{}, false
	default:
		l.MIMEType = d.File.MediaType
		l.Size = d.File.ActiveRevision.EncryptedSize
		l.XAttr = d.File.ActiveRevision.XAttr
		l.FileProperties = &FileProperties{ContentKeyPacket: d.File.ContentKeyPacket}
		l.FileProperties.ActiveRevision.ID = d.File.ActiveRevision.RevisionID
	}
	return l, true
}

func dirOf(path string) string {
	p := strings.TrimRight(path, "/")
	i := strings.LastIndex(p, "/")
	if i <= 0 {
		return "/"
	}
	return p[:i]
}

func baseOf(path string) string {
	p := strings.TrimRight(path, "/")
	i := strings.LastIndex(p, "/")
	return p[i+1:]
}
