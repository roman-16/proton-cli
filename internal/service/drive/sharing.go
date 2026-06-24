package drive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"

	srp "github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

const (
	permView = 4 // SHARE_URL_PERMISSIONS.VIEWER (READ)
	permEdit = 6 // EDITOR (READ|WRITE)

	flagGeneratedPassword          = 2 // GeneratedPasswordIncluded
	flagCustomAndGeneratedPassword = 3 // CustomPassword | GeneratedPasswordIncluded
	flagCustomPasswordBit          = 1 // CustomPassword

	generatedPasswordLen = 12
	passwordCharset      = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
)

type ItemInfo struct {
	Name         string `json:"name"`
	Location     string `json:"location"`
	Type         string `json:"type"`
	MIMEType     string `json:"mime_type,omitempty"`
	CreatedBy    string `json:"created_by,omitempty"`
	Signature    string `json:"signature"`
	Uploaded     int64  `json:"uploaded"`
	Modified     int64  `json:"modified,omitempty"`
	Size         int64  `json:"size_bytes"`
	OriginalSize int64  `json:"original_size_bytes,omitempty"`
	SHA1         string `json:"sha1,omitempty"`
	Shared       bool   `json:"shared"`
	LinkID       string `json:"link_id"`
	ShareID      string `json:"share_id"`
	VolumeID     string `json:"volume_id"`
}

type ShareLink struct {
	ShareURLID     string `json:"share_url_id"`
	ShareID        string `json:"share_id"`
	Token          string `json:"token"`
	URL            string `json:"url"`
	CanEdit        bool   `json:"can_edit"`
	CustomPassword string `json:"custom_password,omitempty"`
	CreateTime     int64  `json:"create_time"`
	ExpireTime     *int64 `json:"expire_time,omitempty"`
	NumAccesses    int    `json:"num_accesses"`
}

type ShareStatus struct {
	Path     string          `json:"path"`
	Type     string          `json:"type"`
	Links    []ShareLink     `json:"public_links"`
	Members  []Member        `json:"members"`
	Invitees []PendingInvite `json:"pending_invitations"`
}

// LinkOptions Set* fields record which options the caller explicitly provided,
// so an existing link is only modified on demand.
type LinkOptions struct {
	CanEdit        bool
	SetEdit        bool
	ExpireSeconds  int
	SetExpiry      bool
	CustomPassword string
	SetPassword    bool
}

func (o LinkOptions) modifies() bool { return o.SetEdit || o.SetExpiry || o.SetPassword }

type shareURLResp struct {
	ShareURLID     string
	ShareID        string
	Token          string
	PublicUrl      string
	Password       string
	Permissions    int
	Flags          int
	CreateTime     int64
	ExpirationTime *int64
	NumAccesses    int
}

func (u shareURLResp) toShareLink(generated string) ShareLink {
	url := u.PublicUrl
	if generated != "" {
		url = u.PublicUrl + "#" + generated
	}
	return ShareLink{
		ShareURLID:  u.ShareURLID,
		ShareID:     u.ShareID,
		Token:       u.Token,
		URL:         url,
		CanEdit:     u.Permissions&2 != 0,
		CreateTime:  u.CreateTime,
		ExpireTime:  u.ExpirationTime,
		NumAccesses: u.NumAccesses,
	}
}

func (s *Service) Info(ctx context.Context, dc *Context, path string) (*ItemInfo, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return nil, err
	}
	name := res.Name
	if name == "" {
		name = "/"
	}
	typeLabel := "file"
	if link.Type == 1 {
		typeLabel = "folder"
	}
	modified := link.RealModifyTime
	if modified == 0 {
		modified = link.ModifyTime
	}
	info := &ItemInfo{
		Name:      name,
		Location:  dirOf("/" + strings.Trim(path, "/")),
		Type:      typeLabel,
		CreatedBy: link.SignatureEmail,
		Uploaded:  link.CreateTime,
		Modified:  modified,
		Size:      link.Size,
		Shared:    len(link.ShareUrls) > 0,
		LinkID:    res.LinkID,
		ShareID:   res.ShareID,
		VolumeID:  dc.VolumeID,
	}
	if link.Type != 1 {
		info.MIMEType = link.MIMEType
		if x, err := decryptXAttr(link.XAttr, res.NodeKR); err == nil {
			info.OriginalSize = x.Common.Size
			info.SHA1 = x.Common.Digests.SHA1
		}
	}
	info.Signature = s.verifyCreator(ctx, dc, res, link)
	return info, nil
}

func (s *Service) verifyCreator(ctx context.Context, dc *Context, res *Resolved, link *Link) string {
	if link.SignatureEmail == "" {
		return "anonymous"
	}
	enc, err := pgp.NewPGPMessageFromArmored(link.NodePassphrase)
	if err != nil {
		return "unknown"
	}
	dec, err := res.ParentKR.Decrypt(enc, nil, pgp.GetUnixTime())
	if err != nil {
		return "unknown"
	}
	sig, err := pgp.NewPGPSignatureFromArmored(link.NodePassphraseSignature)
	if err != nil {
		return "unknown"
	}
	verKR := dc.AddrKR
	if link.SignatureEmail != dc.AddrEmail {
		kr, err := s.addressKeyRing(ctx, link.SignatureEmail)
		if err != nil {
			return "unknown"
		}
		verKR = kr
	}
	if verKR.VerifyDetached(dec, sig, pgp.GetUnixTime()) == nil {
		return "verified"
	}
	return "unverified"
}

func (s *Service) EnsureLink(ctx context.Context, dc *Context, path string, opts LinkOptions) (*ShareLink, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	linkShareID, sk, err := s.shareForLink(ctx, dc, res)
	if err != nil {
		return nil, err
	}
	raws, err := s.fetchShareURLs(ctx, linkShareID)
	if err != nil {
		return nil, err
	}
	if len(raws) > 0 {
		u := raws[0]
		generated, custom := s.decryptURLPassword(dc, u)
		if !opts.modifies() {
			link := u.toShareLink(generated)
			link.CustomPassword = custom
			return &link, nil
		}
		return s.updateShareURL(ctx, dc, linkShareID, u, sk, generated, opts)
	}
	return s.createShareURL(ctx, dc, linkShareID, sk, opts)
}

func (s *Service) createShareURL(ctx context.Context, dc *Context, linkShareID string, sk *pgp.SessionKey, opts LinkOptions) (*ShareLink, error) {
	generated, err := randomPassword(generatedPasswordLen)
	if err != nil {
		return nil, err
	}
	full, flags, custom := composePassword(generated, opts)
	pw, err := s.buildPasswordFields(ctx, dc, sk, full)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"Flags":                    flags,
		"Permissions":              permFor(opts.CanEdit),
		"MaxAccesses":              0,
		"CreatorEmail":             dc.AddrEmail,
		"SharePassphraseKeyPacket": pw.SharePassphraseKeyPacket,
		"SharePasswordSalt":        pw.SharePasswordSalt,
		"Password":                 pw.Password,
		"SRPModulusID":             pw.SRPModulusID,
		"SRPVerifier":              pw.SRPVerifier,
		"UrlPasswordSalt":          pw.UrlPasswordSalt,
		"ExpirationDuration":       expirationDuration(opts),
	}
	var r struct{ ShareURL shareURLResp }
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/drive/shares/" + linkShareID + "/urls", Body: body}, &r); err != nil {
		return nil, err
	}
	link := r.ShareURL.toShareLink(generated)
	link.CustomPassword = custom
	return &link, nil
}

func (s *Service) updateShareURL(ctx context.Context, dc *Context, linkShareID string, u shareURLResp, sk *pgp.SessionKey, generated string, opts LinkOptions) (*ShareLink, error) {
	body := map[string]any{}
	if opts.SetEdit {
		body["Permissions"] = permFor(opts.CanEdit)
	}
	if opts.SetExpiry {
		body["ExpirationDuration"] = expirationDuration(opts)
	}
	custom := ""
	if opts.SetPassword {
		newGenerated, err := randomPassword(generatedPasswordLen)
		if err != nil {
			return nil, err
		}
		var full string
		var flags int
		full, flags, custom = composePassword(newGenerated, opts)
		pw, err := s.buildPasswordFields(ctx, dc, sk, full)
		if err != nil {
			return nil, err
		}
		generated = newGenerated
		body["Flags"] = flags
		body["Password"] = pw.Password
		body["SharePassphraseKeyPacket"] = pw.SharePassphraseKeyPacket
		body["SharePasswordSalt"] = pw.SharePasswordSalt
		body["SRPModulusID"] = pw.SRPModulusID
		body["SRPVerifier"] = pw.SRPVerifier
		body["UrlPasswordSalt"] = pw.UrlPasswordSalt
	}
	var r struct{ ShareURL shareURLResp }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/drive/shares/%s/urls/%s", linkShareID, u.ShareURLID), Body: body,
	}, &r); err != nil {
		return nil, err
	}
	if r.ShareURL.PublicUrl == "" {
		r.ShareURL = u
	}
	link := r.ShareURL.toShareLink(generated)
	link.CustomPassword = custom
	return &link, nil
}

func (s *Service) RemoveLinks(ctx context.Context, dc *Context, path string) (int, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return 0, err
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, sid := range link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		raws, err := s.fetchShareURLs(ctx, sid)
		if err != nil {
			return removed, err
		}
		for _, u := range raws {
			if err := s.C.Decode(ctx, proton.Request{
				Method: "DELETE", Path: fmt.Sprintf("/drive/shares/%s/urls/%s", u.ShareID, u.ShareURLID),
			}, nil); err != nil {
				return removed, fmt.Errorf("delete %s: %w", u.ShareURLID, err)
			}
			removed++
		}
	}
	return removed, nil
}

func (s *Service) CountLinks(ctx context.Context, dc *Context, path string) (int, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return 0, err
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, sid := range link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		raws, err := s.fetchShareURLs(ctx, sid)
		if err != nil {
			return n, err
		}
		n += len(raws)
	}
	return n, nil
}

func (s *Service) ShareStatusOf(ctx context.Context, dc *Context, path string) (*ShareStatus, error) {
	res, err := s.ResolvePath(ctx, dc, path)
	if err != nil {
		return nil, err
	}
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return nil, err
	}
	st := &ShareStatus{Path: "/" + strings.Trim(path, "/"), Type: "file"}
	if link.Type == 1 {
		st.Type = "folder"
	}
	for _, sid := range link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		raws, err := s.fetchShareURLs(ctx, sid)
		if err == nil {
			for _, u := range raws {
				gen, _ := s.decryptURLPassword(dc, u)
				st.Links = append(st.Links, u.toShareLink(gen))
			}
		}
		if members, err := s.ListMembers(ctx, sid); err == nil {
			st.Members = append(st.Members, members...)
		}
		if invites, err := s.ListOutgoingInvites(ctx, sid); err == nil {
			st.Invitees = append(st.Invitees, invites...)
		}
	}
	return st, nil
}

func (s *Service) shareForLink(ctx context.Context, dc *Context, res *Resolved) (string, *pgp.SessionKey, error) {
	link, err := s.getLink(ctx, res.ShareID, res.LinkID)
	if err != nil {
		return "", nil, err
	}
	for _, sid := range link.ShareIDs {
		if sid == dc.ShareID {
			continue
		}
		sk, err := s.shareSessionKey(ctx, dc, sid, res)
		if err != nil {
			return "", nil, err
		}
		return sid, sk, nil
	}
	return s.createShare(ctx, dc, res, link)
}

func (s *Service) createShare(ctx context.Context, dc *Context, res *Resolved, link *Link) (string, *pgp.SessionKey, error) {
	shareKey, sharePass, sharePassSig, sharePriv, shareSessionKey, err := genShareKeys(res.NodeKR, dc.AddrKR)
	if err != nil {
		return "", nil, err
	}
	shareKR, err := pgp.NewKeyRing(sharePriv)
	if err != nil {
		return "", nil, err
	}
	passphraseKP, err := reEncryptSessionKeyTo(link.NodePassphrase, res.ParentKR, shareKR)
	if err != nil {
		return "", nil, fmt.Errorf("re-encrypt passphrase: %w", err)
	}
	nameKP, err := reEncryptSessionKeyTo(link.Name, res.ParentKR, shareKR)
	if err != nil {
		return "", nil, fmt.Errorf("re-encrypt name: %w", err)
	}
	var r struct{ Share struct{ ID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/volumes/" + dc.VolumeID + "/shares",
		Body: map[string]any{
			"AddressID":                dc.AddrID,
			"RootLinkID":               res.LinkID,
			"ShareKey":                 shareKey,
			"SharePassphrase":          sharePass,
			"SharePassphraseSignature": sharePassSig,
			"PassphraseKeyPacket":      passphraseKP,
			"NameKeyPacket":            nameKP,
		},
	}, &r); err != nil {
		return "", nil, err
	}
	return r.Share.ID, shareSessionKey, nil
}

func (s *Service) shareSessionKey(ctx context.Context, dc *Context, shareID string, res *Resolved) (*pgp.SessionKey, error) {
	var sh struct{ Passphrase string }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + shareID}, &sh); err != nil {
		return nil, err
	}
	enc, err := pgp.NewPGPMessageFromArmored(sh.Passphrase)
	if err != nil {
		return nil, err
	}
	split, err := enc.SplitMessage()
	if err != nil {
		return nil, err
	}
	kp := split.GetBinaryKeyPacket()
	// Modern shares wrap the passphrase to the link node key; legacy shares to
	// the address key.
	if sk, err := res.NodeKR.DecryptSessionKey(kp); err == nil {
		return sk, nil
	}
	return dc.AddrKR.DecryptSessionKey(kp)
}

func (s *Service) fetchShareURLs(ctx context.Context, shareID string) ([]shareURLResp, error) {
	var r struct{ ShareURLs []shareURLResp }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + shareID + "/urls"}, &r); err != nil {
		return nil, err
	}
	return r.ShareURLs, nil
}

func (s *Service) decryptURLPassword(dc *Context, u shareURLResp) (generated, custom string) {
	if u.Password == "" {
		return "", ""
	}
	msg, err := pgp.NewPGPMessageFromArmored(u.Password)
	if err != nil {
		return "", ""
	}
	dec, err := dc.AddrKR.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		return "", ""
	}
	full := dec.GetString()
	if u.Flags&flagGeneratedPassword != 0 && len(full) >= generatedPasswordLen {
		generated = full[:generatedPasswordLen]
		if u.Flags&flagCustomPasswordBit != 0 {
			custom = full[generatedPasswordLen:]
		}
		return generated, custom
	}
	return full, ""
}

type passwordFields struct {
	SharePassphraseKeyPacket string
	SharePasswordSalt        string
	Password                 string
	SRPModulusID             string
	SRPVerifier              string
	UrlPasswordSalt          string
}

func (s *Service) buildPasswordFields(ctx context.Context, dc *Context, sk *pgp.SessionKey, fullPassword string) (*passwordFields, error) {
	shareSalt := make([]byte, 16)
	if _, err := rand.Read(shareSalt); err != nil {
		return nil, err
	}
	hashed, err := srp.MailboxPassword([]byte(fullPassword), shareSalt)
	if err != nil {
		return nil, err
	}
	// Proton's key password is the last 31 bytes of the bcrypt hash, not the
	// whole thing.
	kp, err := pgp.EncryptSessionKeyWithPassword(sk, hashed[len(hashed)-31:])
	if err != nil {
		return nil, err
	}
	encPass, err := dc.AddrKR.Encrypt(pgp.NewPlainMessage([]byte(fullPassword)), nil)
	if err != nil {
		return nil, err
	}
	armPass, err := encPass.GetArmored()
	if err != nil {
		return nil, err
	}
	var mod struct{ Modulus, ModulusID string }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/auth/modulus"}, &mod); err != nil {
		return nil, err
	}
	// SRP salt is 10 bytes: hashPasswordVersion3 appends the 6-byte "proton"
	// suffix to fill bcrypt's 16-byte salt slot.
	urlSalt := make([]byte, 10)
	if _, err := rand.Read(urlSalt); err != nil {
		return nil, err
	}
	auth, err := srp.NewAuthForVerifier([]byte(fullPassword), mod.Modulus, urlSalt)
	if err != nil {
		return nil, err
	}
	verifier, err := auth.GenerateVerifier(2048)
	if err != nil {
		return nil, err
	}
	return &passwordFields{
		SharePassphraseKeyPacket: base64.StdEncoding.EncodeToString(kp),
		SharePasswordSalt:        base64.StdEncoding.EncodeToString(shareSalt),
		Password:                 armPass,
		SRPModulusID:             mod.ModulusID,
		SRPVerifier:              base64.StdEncoding.EncodeToString(verifier),
		UrlPasswordSalt:          base64.StdEncoding.EncodeToString(urlSalt),
	}, nil
}

func composePassword(generated string, opts LinkOptions) (full string, flags int, custom string) {
	if opts.SetPassword && opts.CustomPassword != "" {
		return generated + opts.CustomPassword, flagCustomAndGeneratedPassword, opts.CustomPassword
	}
	return generated, flagGeneratedPassword, ""
}

func permFor(canEdit bool) int {
	if canEdit {
		return permEdit
	}
	return permView
}

func expirationDuration(opts LinkOptions) any {
	if opts.SetExpiry && opts.ExpireSeconds > 0 {
		return opts.ExpireSeconds
	}
	return nil
}

func randomPassword(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = passwordCharset[int(b[i])%len(passwordCharset)]
	}
	return string(out), nil
}
