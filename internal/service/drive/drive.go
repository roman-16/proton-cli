// Package drive provides Proton Drive operations.
package drive

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

type Service struct{ C proton.Doer }

func New(c proton.Doer) *Service { return &Service{C: c} }

type Context struct {
	ShareID    string
	ShareKR    *pgp.KeyRing
	AddrKR     *pgp.KeyRing
	AddrID     string
	AddrEmail  string
	VolumeID   string
	RootLinkID string
}

func (s *Service) Resolve(ctx context.Context, u *keys.Unlocked) (*Context, error) {
	var r struct {
		Volumes []struct {
			VolumeID string
			Share    struct{ ShareID, LinkID string }
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/volumes"}, &r); err != nil {
		return nil, err
	}
	if len(r.Volumes) == 0 {
		return nil, fmt.Errorf("no volumes found")
	}
	shareID := r.Volumes[0].Share.ShareID
	rootLink := r.Volumes[0].Share.LinkID
	volumeID := r.Volumes[0].VolumeID

	var sh struct {
		AddressID           string
		Key                 string
		Passphrase          string
		PassphraseSignature string
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + shareID}, &sh); err != nil {
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
	if sig, err := pgp.NewPGPSignatureFromArmored(sh.PassphraseSignature); err == nil {
		_ = addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime())
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
		VolumeID: volumeID, RootLinkID: rootLink,
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
	XAttr                   string
	ShareIDs                []string
	ShareUrls               []struct{ ShareURLID string }
	FolderProperties        *struct{ NodeHashKey string }
	FileProperties          *struct {
		ContentKeyPacket string
		ActiveRevision   struct{ ID string }
	}
}

type Resolved struct {
	ShareID  string
	LinkID   string
	ParentKR *pgp.KeyRing
	NodeKR   *pgp.KeyRing
	Name     string
	IsFolder bool
}

func (s *Service) ResolvePath(ctx context.Context, dc *Context, path string) (*Resolved, error) {
	path = strings.Trim(path, "/")
	rootLink, err := s.getLink(ctx, dc.ShareID, dc.RootLinkID)
	if err != nil {
		return nil, err
	}
	rootKR, err := unlockNode(rootLink, dc.ShareKR, dc.AddrKR)
	if err != nil {
		return nil, fmt.Errorf("unlock root: %w", err)
	}
	if path == "" || path == "." {
		return &Resolved{ShareID: dc.ShareID, LinkID: dc.RootLinkID, ParentKR: dc.ShareKR, NodeKR: rootKR, IsFolder: true}, nil
	}
	parts := strings.Split(path, "/")
	currentID := dc.RootLinkID
	parentKR := dc.ShareKR
	currentKR := rootKR
	for i, part := range parts {
		isLast := i == len(parts)-1
		children, err := s.listRawChildren(ctx, dc.ShareID, currentID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, ch := range children {
			name, err := decryptName(ch.Name, currentKR)
			if err != nil {
				continue
			}
			if name == part {
				found = true
				prevKR := currentKR
				childKR, err := unlockNode(&ch, currentKR, dc.AddrKR)
				if err != nil {
					return nil, fmt.Errorf("unlock %s: %w", name, err)
				}
				if isLast {
					return &Resolved{
						ShareID: dc.ShareID, LinkID: ch.LinkID,
						ParentKR: prevKR, NodeKR: childKR, Name: name,
						IsFolder: ch.Type == 1,
					}, nil
				}
				if ch.Type != 1 {
					return nil, fmt.Errorf("%s is not a folder", name)
				}
				parentKR = currentKR
				currentKR = childKR
				currentID = ch.LinkID
				break
			}
		}
		if !found {
			return nil, &errs.NotFound{Kind: "path", Ref: part}
		}
	}
	_ = parentKR
	return nil, fmt.Errorf("path resolution failed")
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
