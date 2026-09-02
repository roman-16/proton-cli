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

type Service struct {
	C    proton.Doer
	keys keys.Get
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

type Context struct {
	ShareID    string
	ShareKR    *pgp.KeyRing
	AddrKR     *pgp.KeyRing
	AddrID     string
	AddrEmail  string
	VolumeID   string
	RootLinkID string

	// rootLink is the share's root folder, fetched while the share itself was
	// being fetched. Everything addressed by path starts from it, so resolving the
	// share without it would only mean asking for it a moment later, alone.
	rootLink *Link
}

// RootKR is the key ring of the share's root folder.
//
// It is not the share key. The share key opens the root link's passphrase; the
// root's own node key is what the names and passphrases of everything directly
// inside it are sealed to, and reading one with the other yields nothing rather
// than an error - so this is derived in one place instead of at each of them.
func (dc *Context) RootKR() (*pgp.KeyRing, error) {
	kr, err := unlockNode(dc.rootLink, dc.ShareKR, dc.AddrKR)
	if err != nil {
		return nil, fmt.Errorf("unlock the root of share %s: %w", dc.ShareID, err)
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
	return &Context{
		ShareID: shareID, ShareKR: shareKR,
		AddrKR: addrKR, AddrID: sh.AddressID, AddrEmail: addrEmail,
		VolumeID: volumeID, RootLinkID: rootLinkID, rootLink: rootLink,
	}, nil
}

type Link struct {
	LinkID                  string
	ParentLinkID            string
	Type                    int // 1=folder, 2=file
	Size                    int64
	Name                    string
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
	FolderProperties *struct{ NodeHashKey string }
	AlbumProperties  *struct{ NodeHashKey string }
	PhotoProperties  *struct {
		Albums []struct{ AlbumLinkID string }
		Tags   []int
	}
	FileProperties *struct {
		ContentKeyPacket string
		ActiveRevision   struct {
			ID    string
			Photo struct {
				ContentHash          string
				RelatedPhotosLinkIDs []string
			}
		}
	}
}

type Resolved struct {
	ShareID  string
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

func (s *Service) ResolvePath(ctx context.Context, dc *Context, path string) (*Resolved, error) {
	st, err := s.resolveTo(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	if len(st.missing) == 0 {
		return st.at, nil
	}
	if !st.at.IsFolder {
		return nil, fmt.Errorf("%s is not a folder", st.at.Name)
	}
	return nil, &errs.NotFound{Kind: "path", Ref: st.missing[0]}
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
	st := &stopped{
		at: &Resolved{
			ShareID: dc.ShareID, LinkID: dc.RootLinkID, ParentKR: dc.ShareKR,
			NodeKR: rootKR, IsFolder: true, Link: dc.rootLink,
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
	children, err := s.listRawChildren(ctx, parent.ShareID, parent.LinkID)
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
			ShareID: parent.ShareID, LinkID: child.LinkID,
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

// Refused is one item Proton would not act on, and why.
//
// A count is a promise, so a bulk verb that was refused part of what it asked
// for names the part rather than reporting the number it hoped for.
type Refused struct {
	LinkID string `json:"link_id"`
	Reason string `json:"reason"`
}

// String names what was refused. A trashed item has no path to name it by, so
// the ID is what a reader has to go on.
func (r Refused) String() string { return fmt.Sprintf("Refused %s: %s", r.LinkID, r.Reason) }

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

func (s *Service) listRawChildren(ctx context.Context, shareID, linkID string) ([]Link, error) {
	var all []Link
	for page := 0; ; page++ {
		q := url.Values{}
		q.Set("Page", fmt.Sprintf("%d", page))
		q.Set("PageSize", "150")
		var r struct{ Links []Link }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/folders/%s/children", shareID, linkID), Query: q,
		}, &r); err != nil {
			return nil, err
		}
		if len(r.Links) == 0 {
			break
		}
		all = append(all, r.Links...)
		if len(r.Links) < 150 {
			break
		}
	}
	return all, nil
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
