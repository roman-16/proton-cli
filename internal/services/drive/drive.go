// Package drive provides Proton Drive operations.
package drive

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/keys"
)

// Service is the Drive domain service.
type Service struct{ C *api.Client }

// New constructs a drive service.
func New(c *api.Client) *Service { return &Service{C: c} }

// Context holds the default share + address keys for a session.
type Context struct {
	ShareID    string
	ShareKR    *pgp.KeyRing
	AddrKR     *pgp.KeyRing
	AddrID     string
	AddrEmail  string
	VolumeID   string
	RootLinkID string
}

// Resolve returns the default share + key context, unlocking on first call.
func (s *Service) Resolve(ctx context.Context, u *keys.Unlocked) (*Context, error) {
	var r struct {
		Volumes []struct {
			VolumeID string
			Share    struct{ ShareID, LinkID string }
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/drive/volumes"}, &r); err != nil {
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
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/drive/shares/" + shareID}, &sh); err != nil {
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

// Link is a decrypted view of a Proton Drive link (file or folder).
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
	CreateTime              int64
	ModifyTime              int64
	FolderProperties        *struct{ NodeHashKey string }
	FileProperties          *struct {
		ContentKeyPacket string
		ActiveRevision   struct{ ID string }
	}
}

// Resolved is the outcome of resolving a path.
type Resolved struct {
	ShareID  string
	LinkID   string
	ParentKR *pgp.KeyRing
	NodeKR   *pgp.KeyRing
	Name     string
	IsFolder bool
}

// ResolvePath walks /a/b/c from the root, decrypting names as it goes.
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
			return nil, fmt.Errorf("not found: %s", part)
		}
	}
	_ = parentKR
	return nil, fmt.Errorf("path resolution failed")
}

// Child describes an item in a folder listing.
type Child struct {
	LinkID     string `json:"link_id"`
	Name       string `json:"name"`
	Path       string `json:"path,omitempty"` // full decrypted path (populated by Walk)
	Type       int    `json:"type"`           // 1=folder, 2=file
	Size       int64  `json:"size"`
	CreateTime int64  `json:"create_time,omitempty"`
	ModifyTime int64  `json:"modify_time,omitempty"`
}

// List returns decrypted child entries of a folder.
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
		out = append(out, Child{LinkID: r.LinkID, Name: name, Type: r.Type, Size: r.Size, CreateTime: r.CreateTime, ModifyTime: r.ModifyTime})
	}
	return out, nil
}

// Walk recursively lists all descendants of path. Entries are returned with
// their full decrypted path in the Path field. Folders are included as well
// as files; order is depth-first, folders before their contents.
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
		out = append(out, Child{LinkID: r.LinkID, Name: name, Path: full, Type: r.Type, Size: r.Size, CreateTime: r.CreateTime, ModifyTime: r.ModifyTime})
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

// CreateFolder creates a new folder at the given path (parent must exist).
func (s *Service) CreateFolder(ctx context.Context, dc *Context, fullPath string) error {
	parent := dirOf(fullPath)
	name := baseOf(fullPath)

	p, err := s.ResolvePath(ctx, dc, parent)
	if err != nil {
		return fmt.Errorf("parent not found: %w", err)
	}
	parentLink, err := s.getLink(ctx, p.ShareID, p.LinkID)
	if err != nil {
		return err
	}
	hashKey, err := hashKeyOf(parentLink, p.NodeKR)
	if err != nil {
		return err
	}
	hash, err := lookupHash(strings.ToLower(name), hashKey)
	if err != nil {
		return err
	}
	encName, err := encryptName(name, p.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	nodeKey, nodePass, nodePassSig, nodePriv, err := genNodeKeys(p.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	nodeKR, err := pgp.NewKeyRing(nodePriv)
	if err != nil {
		return err
	}
	hashKeyEnc, err := genNodeHashKey(nodeKR, nodeKR)
	if err != nil {
		return err
	}
	body := map[string]any{
		"Name":                    encName,
		"Hash":                    hash,
		"ParentLinkID":            p.LinkID,
		"NodePassphrase":          nodePass,
		"NodePassphraseSignature": nodePassSig,
		"SignatureAddress":        dc.AddrEmail,
		"NodeKey":                 nodeKey,
		"NodeHashKey":             hashKeyEnc,
	}
	return s.C.Send(ctx, api.Request{Method: "POST", Path: "/drive/shares/" + p.ShareID + "/folders", Body: body}, nil)
}

// Rename renames a file or folder in place.
func (s *Service) Rename(ctx context.Context, dc *Context, path, newName string) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	resLink, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return err
	}
	parentLink, err := s.getLink(ctx, res.ShareID, resLink.ParentLinkID)
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
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/links/%s/rename", res.ShareID, res.LinkID),
		Body: map[string]any{
			"Name": encName, "Hash": newHash, "OriginalHash": oldHash,
			"NameSignatureEmail": dc.AddrEmail,
		},
	}, nil)
}

// Move relocates a file/folder to a different parent folder.
func (s *Service) Move(ctx context.Context, dc *Context, sourcePath, destPath string) error {
	src, err := s.ResolvePath(ctx, dc, sourcePath)
	if err != nil {
		return fmt.Errorf("source not found: %w", err)
	}
	dst, err := s.ResolvePath(ctx, dc, destPath)
	if err != nil {
		return fmt.Errorf("destination not found: %w", err)
	}
	if !dst.IsFolder {
		return fmt.Errorf("%s is not a folder", destPath)
	}
	srcLink, err := s.getLink(ctx, src.ShareID, src.LinkID)
	if err != nil {
		return err
	}
	dstLink, err := s.getLink(ctx, dst.ShareID, dst.LinkID)
	if err != nil {
		return err
	}
	hk, err := hashKeyOf(dstLink, dst.NodeKR)
	if err != nil {
		return err
	}
	newHash, err := lookupHash(strings.ToLower(src.Name), hk)
	if err != nil {
		return err
	}
	encName, err := reEncryptName(srcLink.Name, src.Name, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return err
	}
	newPass, _, err := reEncryptNodePassphrase(srcLink, src.ParentKR, dst.NodeKR, dc.AddrKR)
	if err != nil {
		return fmt.Errorf("re-encrypt passphrase: %w", err)
	}
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/links/%s/move", src.ShareID, src.LinkID),
		Body: map[string]any{
			"Name":               encName,
			"Hash":               newHash,
			"ParentLinkID":       dst.LinkID,
			"NodePassphrase":     newPass,
			"NameSignatureEmail": dc.AddrEmail,
		},
	}, nil)
}

// Delete moves an item to trash (or permanently deletes when permanent=true).
func (s *Service) Delete(ctx context.Context, dc *Context, path string, permanent bool) error {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return err
	}
	if err := s.C.Send(ctx, api.Request{
		Method: "POST", Path: "/drive/v2/volumes/" + dc.VolumeID + "/trash_multiple",
		Body: map[string]any{"LinkIDs": []string{res.LinkID}},
	}, nil); err != nil {
		return err
	}
	if permanent {
		return s.C.Send(ctx, api.Request{
			Method: "POST", Path: "/drive/v2/volumes/" + dc.VolumeID + "/trash/delete_multiple",
			Body: map[string]any{"LinkIDs": []string{res.LinkID}},
		}, nil)
	}
	return nil
}

// TrashEntry is a trashed link.
type TrashEntry struct {
	ShareID string `json:"share_id"`
	LinkID  string `json:"link_id"`
	Type    int    `json:"type"`
	Size    int64  `json:"size"`
	Trashed int64  `json:"trashed"`
}

// TrashList returns trashed link IDs grouped by share.
func (s *Service) TrashList(ctx context.Context, dc *Context) ([]TrashEntry, error) {
	var r struct {
		Trash []struct {
			ShareID string
			LinkIDs []string
		}
	}
	if err := s.C.Send(ctx, api.Request{
		Method: "GET", Path: "/drive/volumes/" + dc.VolumeID + "/trash",
		Query: keys.Query("Page", "0", "PageSize", "150"),
	}, &r); err != nil {
		return nil, err
	}
	var out []TrashEntry
	for _, t := range r.Trash {
		for _, id := range t.LinkIDs {
			l, err := s.getLink(ctx, t.ShareID, id)
			if err != nil {
				out = append(out, TrashEntry{ShareID: t.ShareID, LinkID: id})
				continue
			}
			out = append(out, TrashEntry{ShareID: t.ShareID, LinkID: id, Type: l.Type, Size: l.Size})
		}
	}
	return out, nil
}

// TrashRestore restores items from trash.
func (s *Service) TrashRestore(ctx context.Context, dc *Context, linkIDs []string) error {
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/drive/v2/volumes/" + dc.VolumeID + "/trash/restore_multiple",
		Body: map[string]any{"LinkIDs": linkIDs},
	}, nil)
}

// TrashEmpty empties the trash.
func (s *Service) TrashEmpty(ctx context.Context, dc *Context) error {
	return s.C.Send(ctx, api.Request{
		Method: "DELETE", Path: "/drive/volumes/" + dc.VolumeID + "/trash",
	}, nil)
}

func (s *Service) getLink(ctx context.Context, shareID, linkID string) (*Link, error) {
	var r struct{ Link Link }
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: fmt.Sprintf("/drive/shares/%s/links/%s", shareID, linkID)}, &r); err != nil {
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
		if err := s.C.Send(ctx, api.Request{
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

func unlockNode(l *Link, parentKR, addrKR *pgp.KeyRing) (*pgp.KeyRing, error) {
	enc, err := pgp.NewPGPMessageFromArmored(l.NodePassphrase)
	if err != nil {
		return nil, err
	}
	dec, err := parentKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return nil, fmt.Errorf("decrypt node passphrase: %w", err)
	}
	if l.NodePassphraseSignature != "" && addrKR != nil {
		if sig, err := pgp.NewPGPSignatureFromArmored(l.NodePassphraseSignature); err == nil {
			_ = addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime())
		}
	}
	locked, err := pgp.NewKeyFromArmored(l.NodeKey)
	if err != nil {
		return nil, err
	}
	unlocked, err := locked.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("unlock node key: %w", err)
	}
	return pgp.NewKeyRing(unlocked)
}

func decryptName(encName string, parentKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(encName)
	if err != nil {
		return "", err
	}
	dec, err := parentKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", err
	}
	return dec.GetString(), nil
}

func encryptName(name string, parentKR, addrKR *pgp.KeyRing) (string, error) {
	pub, err := parentKR.GetKey(0)
	if err != nil {
		return "", err
	}
	pubKR, err := pgp.NewKeyRing(pub)
	if err != nil {
		return "", err
	}
	enc, err := pubKR.Encrypt(pgp.NewPlainMessageFromString(name), addrKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
}

func reEncryptName(encryptedName, plainName string, oldKR, newKR, addrKR *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(encryptedName)
	if err != nil {
		return "", err
	}
	split, err := msg.SplitMessage()
	if err != nil {
		return "", err
	}
	sk, err := oldKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", err
	}
	newKP, err := newKR.EncryptSessionKey(sk)
	if err != nil {
		return "", err
	}
	dataPacket, err := sk.EncryptAndSign(pgp.NewPlainMessageFromString(plainName), addrKR)
	if err != nil {
		return "", err
	}
	return pgp.NewPGPSplitMessage(newKP, dataPacket).GetPGPMessage().GetArmored()
}

func reEncryptNodePassphrase(l *Link, oldKR, newKR, addrKR *pgp.KeyRing) (string, string, error) {
	enc, err := pgp.NewPGPMessageFromArmored(l.NodePassphrase)
	if err != nil {
		return "", "", err
	}
	split, err := enc.SplitMessage()
	if err != nil {
		return "", "", err
	}
	sk, err := oldKR.DecryptSessionKey(split.GetBinaryKeyPacket())
	if err != nil {
		return "", "", err
	}
	dec, err := oldKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return "", "", err
	}
	newKP, err := newKR.EncryptSessionKey(sk)
	if err != nil {
		return "", "", err
	}
	dataPacket, err := sk.Encrypt(dec)
	if err != nil {
		return "", "", err
	}
	newPass, err := pgp.NewPGPSplitMessage(newKP, dataPacket).GetPGPMessage().GetArmored()
	if err != nil {
		return "", "", err
	}
	sig, err := addrKR.SignDetached(dec)
	if err != nil {
		return "", "", err
	}
	newSig, err := sig.GetArmored()
	if err != nil {
		return "", "", err
	}
	return newPass, newSig, nil
}

func hashKeyOf(l *Link, nodeKR *pgp.KeyRing) ([]byte, error) {
	if l.FolderProperties == nil || l.FolderProperties.NodeHashKey == "" {
		return nil, fmt.Errorf("link has no hash key")
	}
	msg, err := pgp.NewPGPMessageFromArmored(l.FolderProperties.NodeHashKey)
	if err != nil {
		return nil, err
	}
	dec, err := nodeKR.Decrypt(msg, nodeKR, pgp.GetUnixTime())
	if err != nil {
		return nil, err
	}
	return dec.GetBinary(), nil
}

func lookupHash(name string, hashKey []byte) (string, error) {
	mac := hmac.New(sha256.New, hashKey)
	mac.Write([]byte(name))
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func genNodeKeys(parentKR, addrKR *pgp.KeyRing) (nodeKey, passphrase, passSig string, priv *pgp.Key, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", "", nil, err
	}
	phrase := base64.StdEncoding.EncodeToString(raw)
	key, err := pgp.GenerateKey("Drive key", "", "x25519", 0)
	if err != nil {
		return "", "", "", nil, err
	}
	locked, err := key.Lock([]byte(phrase))
	if err != nil {
		return "", "", "", nil, err
	}
	armKey, err := locked.Armor()
	if err != nil {
		return "", "", "", nil, err
	}
	msg := pgp.NewPlainMessageFromString(phrase)
	enc, err := parentKR.Encrypt(msg, nil)
	if err != nil {
		return "", "", "", nil, err
	}
	armPass, err := enc.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}
	sig, err := addrKR.SignDetached(msg)
	if err != nil {
		return "", "", "", nil, err
	}
	armSig, err := sig.GetArmored()
	if err != nil {
		return "", "", "", nil, err
	}
	return armKey, armPass, armSig, key, nil
}

func genNodeHashKey(nodeKR, signingKR *pgp.KeyRing) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	s := base64.StdEncoding.EncodeToString(raw)
	enc, err := nodeKR.Encrypt(pgp.NewPlainMessageFromString(s), signingKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
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

// MarshalJSON for Link suppresses unexported fields; unused but keeps vet quiet.
var _ = json.Marshal
