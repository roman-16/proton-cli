package drive

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	srp "github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
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
	// URL is the public link the item was reached through, and LinkPassword the
	// one its owner set on it. Both are empty for an item in your own files.
	URL          string `json:"url,omitempty"`
	LinkPassword string `json:"link_password,omitempty"`
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
	link := res.Link
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
		Name:         name,
		Location:     dirOf("/" + strings.Trim(path, "/")),
		Type:         typeLabel,
		CreatedBy:    link.SignatureEmail,
		Uploaded:     link.CreateTime,
		Modified:     modified,
		Size:         link.Size,
		Shared:       dc.Public() || len(link.ShareUrls) > 0,
		URL:          dc.URL,
		LinkPassword: dc.LinkPassword,
		LinkID:       res.LinkID,
		ShareID:      res.dc.ShareID,
		VolumeID:     dc.VolumeID,
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
	verKR := dc.AddrKR
	if link.SignatureEmail != dc.AddrEmail {
		kr, err := s.addressKeyRing(ctx, link.SignatureEmail)
		if err != nil {
			return "unknown"
		}
		verKR = kr
	}
	// Verify against the text form: the passphrase is signed as text at
	// creation, so the binary form would spuriously fail.
	norm := pgp.NewPlainMessageFromString(string(dec.GetBinary()))
	return string(pgphelper.VerifyDetachedStatus(verKR, norm, link.NodePassphraseSignature))
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
	link := res.Link
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
	link := res.Link
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
	link := res.Link
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
				gen, custom := s.decryptURLPassword(dc, u)
				link := u.toShareLink(gen)
				link.CustomPassword = custom
				st.Links = append(st.Links, link)
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
	link := res.Link
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

// ── what other people have shared with you ──

// Share types, as Proton numbers them. A share is how Drive grants access to a
// subtree, so the one you own and the one somebody granted you differ only in
// who created it.
const (
	shareTypeMain     = 1
	shareTypeStandard = 2
	shareTypeDevice   = 3
	shareTypePhotos   = 4
)

// SharedItem is a file or folder somebody else shared with you.
//
// It has no path, because it does not live in your tree: it is the root of a
// share of theirs that you were granted. So it is addressed by ID, the way
// trashed items and photos are.
type SharedItem struct {
	ShareID  string `json:"share_id,omitempty"`
	LinkID   string `json:"link_id"`
	VolumeID string `json:"volume_id,omitempty"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Size     int64  `json:"size,omitempty"`
	// SharedBy is the address that granted the access, and is empty for a public
	// link: a link says nothing about who sent it.
	SharedBy string `json:"shared_by,omitempty"`
	// Token names the public link this is, and URL is that link. Both are empty
	// for an item somebody shared with you directly.
	Token string `json:"token,omitempty"`
	URL   string `json:"url,omitempty"`
	// LinkPassword is the password the link's owner set on it. It is kept out of
	// every listing: what a listing is for is finding the thing, and `items get`
	// is where one item is looked at in full.
	LinkPassword string `json:"-"`
	Created      int64  `json:"create_time,omitempty"`
}

// IsLink reports whether this is a public link somebody sent you rather than a
// share you are a member of. The two are removed differently and only one of
// them can be written to, so nothing may assume.
func (i SharedItem) IsLink() bool { return i.Token != "" }

// Ref is what addresses the item on a command line. A link is named by its
// token, which is the only name Proton has for it and the one every request
// about it carries; everything else by the ID of the item shared.
func (i SharedItem) Ref() string {
	if i.IsLink() {
		return i.Token
	}
	return i.LinkID
}

// SharedWithMe lists what other people have granted you.
//
// Two things arrive that way and both belong in the one answer: a share you were
// invited into and accepted, and a public link you saved. They are addressed
// alike, by `--shared REF`, and differ in what may be done with them - a link
// can be read and not written - which each item says of itself.
//
// A share is somebody else's when its creator is not one of your own addresses.
// The main share, the photos share and the desktop client's device shares are
// yours by definition and are left out.
func (s *Service) SharedWithMe(ctx context.Context) ([]SharedItem, error) {
	shares, mine, err := s.listShares(ctx)
	if err != nil {
		return nil, err
	}
	saved, err := s.savedLinks(ctx)
	if err != nil {
		return nil, err
	}
	out := saved
	for _, sh := range shares {
		if sh.Locked || sh.Type != shareTypeStandard || mine[strings.ToLower(sh.Creator)] {
			continue
		}
		item, err := s.describeShare(ctx, sh)
		if err != nil {
			// A share whose key will not open is reported by its identity rather
			// than dropped: knowing it is there is what lets somebody act on it.
			slog.Debug("drive: could not read a share", "share", sh.ShareID, "error", err)
			out = append(out, SharedItem{
				ShareID: sh.ShareID, LinkID: sh.LinkID, VolumeID: sh.VolumeID,
				SharedBy: sh.Creator, Created: sh.CreateTime,
			})
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

// SharedByMe lists what you have shared, whether by link or with named people.
//
// `share get PATH` answers the question for one item; this answers the one a
// person actually has, which is "what have I left open".
func (s *Service) SharedByMe(ctx context.Context) ([]SharedItem, error) {
	shares, mine, err := s.listShares(ctx)
	if err != nil {
		return nil, err
	}
	var out []SharedItem
	for _, sh := range shares {
		if sh.Locked || sh.Type != shareTypeStandard || !mine[strings.ToLower(sh.Creator)] {
			continue
		}
		item, err := s.describeShare(ctx, sh)
		if err != nil {
			// This listing answers "what have I left open", so a share that will not
			// open is reported by its identity rather than dropped: leaving it out
			// would understate what is shared, which is the one direction this
			// question must not be wrong in.
			slog.Debug("drive: could not read a share of mine", "share", sh.ShareID, "error", err)
			out = append(out, SharedItem{
				ShareID: sh.ShareID, LinkID: sh.LinkID, VolumeID: sh.VolumeID,
				Created: sh.CreateTime,
			})
			continue
		}
		out = append(out, *item)
	}
	return out, nil
}

type rawShare struct {
	ShareID    string
	LinkID     string
	VolumeID   string
	AddressID  string
	Creator    string
	Type       int
	State      int
	Locked     bool
	CreateTime int64
}

// listShares reads every share the account can see, and the addresses that
// decide which of them are its own.
func (s *Service) listShares(ctx context.Context) ([]rawShare, map[string]bool, error) {
	var r struct{ Shares []rawShare }
	q := url.Values{}
	q.Set("ShowAll", "1")
	var u *keys.Unlocked
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares", Query: q}, &r)
		},
		func(ctx context.Context) error {
			var err error
			u, err = s.keys(ctx)
			return err
		},
	); err != nil {
		return nil, nil, err
	}
	mine := make(map[string]bool, len(u.Addresses))
	for _, a := range u.Addresses {
		mine[strings.ToLower(a.Email)] = true
	}
	return r.Shares, mine, nil
}

// describeShare opens a share and reads what it grants.
func (s *Service) describeShare(ctx context.Context, sh rawShare) (*SharedItem, error) {
	dc, err := s.unlockShare(ctx, sh.ShareID, sh.LinkID, sh.VolumeID)
	if err != nil {
		return nil, err
	}
	root := dc.rootLink
	if root == nil {
		return nil, fmt.Errorf("share %s has no root", sh.ShareID)
	}
	return &SharedItem{
		ShareID: sh.ShareID, LinkID: sh.LinkID, VolumeID: sh.VolumeID,
		Name: dc.RootName, Type: linkType(root.Type), Size: root.Size,
		SharedBy: sh.Creator, Created: sh.CreateTime,
	}, nil
}

// OpenShared opens what somebody shared with you, so a path can start from it.
//
// The item is the top of the tree rather than something in it, which is why it
// is `/` there: a shared folder holds the paths below it, and a shared file is
// that path itself.
func (s *Service) OpenShared(ctx context.Context, item SharedItem) (*Context, error) {
	if item.IsLink() {
		_, urlPassword, err := ParseLink(item.URL)
		if err != nil {
			return nil, err
		}
		return s.openLink(ctx, item.Token, urlPassword, item.LinkPassword)
	}
	return s.unlockShare(ctx, item.ShareID, item.LinkID, item.VolumeID)
}

// ── public links you saved ──

// A saved link is a link somebody sent you, kept in the account so opening it
// again needs neither the URL nor the password.
//
// What is stored is the password, encrypted to your own address key and signed
// with it; Proton keeps the token beside it and serves the link's own share
// alongside, which is why a listing can name what each one points at without
// opening anything.

// savedLinks reads the links this account has saved.
func (s *Service) savedLinks(ctx context.Context) ([]SharedItem, error) {
	var r struct {
		Bookmarks []struct {
			EncryptedUrlPassword string
			CreateTime           int64
			Token                struct {
				Token             string
				LinkID            string
				LinkType          int
				Name              string
				Size              int64
				ShareKey          string
				SharePassphrase   string
				SharePasswordSalt string
			}
		}
	}
	var u *keys.Unlocked
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/v2/shared-bookmarks"}, &r)
		},
		func(ctx context.Context) error {
			var err error
			u, err = s.keys(ctx)
			return err
		},
	); err != nil {
		return nil, err
	}
	out := make([]SharedItem, 0, len(r.Bookmarks))
	for _, b := range r.Bookmarks {
		item := SharedItem{
			Token: b.Token.Token, LinkID: b.Token.LinkID,
			Type: linkType(b.Token.LinkType), Size: b.Token.Size, Created: b.CreateTime,
		}
		password, err := decryptSavedPassword(u, b.EncryptedUrlPassword)
		if err != nil {
			// A link whose password will not open is listed by its token rather than
			// dropped, the way a share that will not open is: knowing it is saved is
			// what lets somebody remove it, and the empty name says as much.
			slog.DebugContext(ctx, "drive: a saved link's password could not be decrypted",
				"share", b.Token.Token, "error", err)
			out = append(out, item)
			continue
		}
		urlPassword, custom := splitLinkPassword(password)
		item.URL, item.LinkPassword = LinkURL(b.Token.Token, urlPassword), custom
		shareKR, err := unlockLinkShare(&proton.PublicLinkShare{
			ShareKey: b.Token.ShareKey, SharePassphrase: b.Token.SharePassphrase,
			SharePasswordSalt: b.Token.SharePasswordSalt,
		}, password)
		if err != nil {
			slog.DebugContext(ctx, "drive: a saved link's share key could not be opened",
				"share", b.Token.Token, "error", err)
			out = append(out, item)
			continue
		}
		if name, err := decryptName(b.Token.Name, shareKR); err == nil {
			item.Name = name
		} else {
			slog.DebugContext(ctx, "drive: a saved link's name could not be decrypted",
				"share", b.Token.Token, "error", err)
		}
		out = append(out, item)
	}
	return out, nil
}

// splitLinkPassword takes the two halves of a saved password apart: what the URL
// carried, and what the link's owner set after it.
func splitLinkPassword(password string) (urlPassword, custom string) {
	if len(password) <= generatedPasswordLen {
		return password, ""
	}
	return password[:generatedPasswordLen], password[generatedPasswordLen:]
}

// decryptSavedPassword opens a saved link's password with whichever of the
// account's addresses it was written to.
func decryptSavedPassword(u *keys.Unlocked, armored string) (string, error) {
	if armored == "" {
		return "", fmt.Errorf("the saved link carries no password")
	}
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return "", err
	}
	for _, addr := range u.Addresses {
		kr, ok := u.AddrKR(addr.ID)
		if !ok {
			continue
		}
		if dec, err := kr.Decrypt(msg, nil, pgp.GetUnixTime()); err == nil {
			return dec.GetString(), nil
		}
	}
	return "", fmt.Errorf("no address key opens it")
}

// SaveLink keeps an open link, so it can be opened again by name alone.
func (s *Service) SaveLink(ctx context.Context, dc *Context) (string, error) {
	if !dc.Public() {
		return "", fmt.Errorf("only a public link can be saved")
	}
	_, urlPassword, err := ParseLink(dc.URL)
	if err != nil {
		return "", err
	}
	if urlPassword == "" {
		return "", errs.Problemf("This link is too old to be saved.").
			Hint("Open it with --link each time, or ask whoever sent it for a new link.")
	}
	addrID, keyID, addrKR, err := s.savingAddress(ctx)
	if err != nil {
		return "", err
	}
	sealed, err := sealLinkPassword(urlPassword+dc.LinkPassword, addrKR)
	if err != nil {
		return "", err
	}
	body := map[string]any{"BookmarkShareURL": map[string]any{
		"EncryptedUrlPassword": sealed,
		"AddressID":            addrID,
		"AddressKeyID":         keyID,
	}}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/drive/v2/urls/" + dc.Token + "/bookmark", Body: body,
	}, nil); err != nil {
		return "", err
	}
	return dc.Token, nil
}

// ForgetLink takes a saved link out of the account. The link itself is not
// touched: it is its owner's, and it goes on working for everyone they sent it
// to.
func (s *Service) ForgetLink(ctx context.Context, item SharedItem) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/drive/v2/urls/" + item.Token + "/bookmark",
	}, nil)
}

// LeaveShare gives up membership of something somebody shared with you.
//
// Only its owner can put it back, which is what makes this the one thing in this
// collection worth stopping for.
func (s *Service) LeaveShare(ctx context.Context, item SharedItem) error {
	var sh struct {
		Memberships []struct{ MemberID string }
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/shares/" + item.ShareID}, &sh); err != nil {
		return err
	}
	if len(sh.Memberships) == 0 {
		return errs.Problemf("You are not a member of that share, so there is nothing to leave.")
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE",
		Path:   fmt.Sprintf("/drive/v2/shares/%s/members/%s", item.ShareID, sh.Memberships[0].MemberID),
	}, nil)
}

// savingAddress is the address a saved link is written to: the one your own
// files hang from, and its primary key.
func (s *Service) savingAddress(ctx context.Context) (addrID, keyID string, kr *pgp.KeyRing, err error) {
	dc, err := s.Resolve(ctx)
	if err != nil {
		return "", "", nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return "", "", nil, err
	}
	for _, addr := range u.Addresses {
		if addr.ID != dc.AddrID {
			continue
		}
		for _, key := range addr.Keys {
			if key.Primary == 1 {
				return addr.ID, key.ID, dc.AddrKR, nil
			}
		}
	}
	return "", "", nil, fmt.Errorf("no primary key for address %s", dc.AddrID)
}

// sealLinkPassword encrypts a link's password to your own address key and signs
// it with the same, which is what makes a saved link yours to read back.
func sealLinkPassword(password string, addrKR *pgp.KeyRing) (string, error) {
	enc, err := addrKR.Encrypt(pgp.NewPlainMessageFromString(password), addrKR)
	if err != nil {
		return "", err
	}
	return enc.GetArmored()
}
