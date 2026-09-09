package drive

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	srp "github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A public link is a tree like any other, reached by a URL instead of by being
// a member of the share behind it.
//
// The password that opens it is in the URL, after the #, and a link whose owner
// set one of their own needs that too. Together they prove the link over SRP and
// then open the share key, which is the same crypto `share link` writes, read
// the other way round: bcrypt the password under the share's salt, keep the last
// 31 bytes, and unwrap the passphrase the share key is locked with.

// Where Proton publishes a link, and the path segment its token follows. Both
// are needed to write one out: Proton stores the token alone, so a saved link's
// URL is put back together from it.
const (
	linkURLHost    = "https://drive.proton.me"
	linkURLSegment = "urls"
)

// ParseLink reads a public link: the token that names it, and the password its
// URL carries.
//
// A link with no password in it is one made before the password moved into the
// URL, and is opened with the one its owner sent separately.
func ParseLink(raw string) (token, password string, err error) {
	parsed, perr := url.Parse(strings.TrimSpace(raw))
	if perr != nil || !parsed.IsAbs() || parsed.Host == "" {
		return "", "", errs.Problemf("That is not a Proton Drive link.")
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if part == linkURLSegment && i+1 < len(parts) && parts[i+1] != "" {
			return parts[i+1], parsed.Fragment, nil
		}
	}
	return "", "", errs.Problemf("That is not a Proton Drive link.")
}

// LinkURL is the link a token and its URL password make, which is the address
// somebody was sent.
func LinkURL(token, password string) string {
	link := linkURLHost + "/" + linkURLSegment + "/" + token
	if password == "" {
		return link
	}
	return link + "#" + password
}

// OpenLink opens the tree behind a public link, so a path can start from it.
//
// The link itself is the top of that tree, which is why it is `/` there: a
// shared folder holds the paths below it, and a shared file is that path itself.
func (s *Service) OpenLink(ctx context.Context, rawURL, customPassword string) (*Context, error) {
	token, urlPassword, err := ParseLink(rawURL)
	if err != nil {
		return nil, err
	}
	return s.openLink(ctx, token, urlPassword, customPassword)
}

func (s *Service) openLink(ctx context.Context, token, urlPassword, customPassword string) (*Context, error) {
	info, err := s.C.PublicLinkInfo(ctx, token)
	if err != nil {
		return nil, linkFailure(err)
	}
	switch info.VendorType {
	case proton.PublicLinkDoc:
		return nil, otherProduct("Proton Docs document")
	case proton.PublicLinkSheet:
		return nil, otherProduct("Proton Sheets spreadsheet")
	}
	password, err := composeLinkPassword(info, urlPassword, customPassword)
	if err != nil {
		return nil, err
	}
	share, err := s.C.PublicLinkAuth(ctx, token, info, password)
	if err != nil {
		return nil, linkFailure(err)
	}
	shareKR, err := unlockLinkShare(share, password)
	if err != nil {
		return nil, err
	}
	root, err := s.linkRoot(ctx, token)
	if err != nil {
		return nil, linkFailure(err)
	}
	dc := &Context{
		Token: token, URL: LinkURL(token, urlPassword), LinkPassword: customPassword,
		ShareKR: shareKR, VolumeID: share.VolumeID, RootLinkID: root.LinkID, rootLink: root,
		Permissions: share.PublicPermissions, Anonymous: share.Anonymous,
		Type: shareTypeStandard,
	}
	dc.RootName = rootName(ctx, token, dc.Type, root, shareKR)
	return dc, nil
}

// composeLinkPassword builds what the link is proved with.
//
// A link made today carries a generated password in its URL, and one whose owner
// set a password of their own wants both, in that order. A legacy link carries
// nothing in its URL: the whole of its password was sent to the reader.
func composeLinkPassword(info *proton.PublicLinkInfo, urlPassword, customPassword string) (string, error) {
	if info.NeedsCustomPassword() && customPassword == "" {
		return "", errs.Problemf("This link has a password.").
			Hint("--link-password-file FILE, or --link-password-stdin")
	}
	if info.Legacy() {
		return customPassword, nil
	}
	if urlPassword == "" {
		return "", errs.Problemf("That link carries no password, so it cannot be opened.").
			Hint("Ask whoever sent it for the whole link, including everything after the #.")
	}
	return urlPassword + customPassword, nil
}

// otherProduct refuses a link to something behind another Proton product, which
// shares the URL shape and nothing else.
func otherProduct(what string) error {
	return errs.Problemf("This link opens a %s.", what).Hint("Open it in a browser")
}

// linkFailure phrases what a link answered, where the answer is about the link
// rather than about the request.
func linkFailure(err error) error {
	switch {
	case proton.WrongLinkPassword(err):
		return errs.Problemf("That is not this link's password.")
	case proton.DoesNotExist(err):
		return errs.Problemf("The link does not exist any more, or has expired.").Exit(3)
	}
	return err
}

// unlockLinkShare opens the share key a link's password locks.
func unlockLinkShare(share *proton.PublicLinkShare, password string) (*pgp.KeyRing, error) {
	salt, err := base64.StdEncoding.DecodeString(share.SharePasswordSalt)
	if err != nil {
		return nil, fmt.Errorf("decode the link's salt: %w", err)
	}
	hashed, err := srp.MailboxPassword([]byte(password), salt)
	if err != nil {
		return nil, err
	}
	enc, err := pgp.NewPGPMessageFromArmored(share.SharePassphrase)
	if err != nil {
		return nil, err
	}
	// Proton's key password is the last 31 bytes of the bcrypt hash, not the
	// whole thing.
	dec, err := pgp.DecryptMessageWithPassword(enc, hashed[len(hashed)-31:])
	if err != nil {
		return nil, fmt.Errorf("decrypt the link's share passphrase: %w", err)
	}
	locked, err := pgp.NewKeyFromArmored(share.ShareKey)
	if err != nil {
		return nil, err
	}
	unlocked, err := locked.Unlock(dec.GetBinary())
	if err != nil {
		return nil, fmt.Errorf("unlock the link's share key: %w", err)
	}
	return pgp.NewKeyRing(unlocked)
}

// linkRoot reads what a link points at.
//
// A link names one item and everything under it, so its root arrives whole
// rather than being looked up by ID: the answer carries the node's keys, its
// name sealed to the share, and for a file the key packet its content is under.
func (s *Service) linkRoot(ctx context.Context, token string) (*Link, error) {
	var r struct {
		Token struct {
			LinkID                  string
			LinkType                int
			MIMEType                string
			Name                    string
			NodeKey                 string
			NodeHashKey             string
			NodePassphrase          string
			NodePassphraseSignature string
			SignatureEmail          string
			ContentKeyPacket        string
			Size                    int64
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/drive/urls/" + token}, &r); err != nil {
		return nil, err
	}
	t := r.Token
	root := &Link{
		LinkID: t.LinkID, Type: t.LinkType, Size: t.Size, Name: t.Name,
		MIMEType: t.MIMEType, NodeKey: t.NodeKey, NodePassphrase: t.NodePassphrase,
		NodePassphraseSignature: t.NodePassphraseSignature, SignatureEmail: t.SignatureEmail,
	}
	if t.LinkType == protonFolder {
		root.FolderProperties = &FolderProperties{NodeHashKey: t.NodeHashKey}
		return root, nil
	}
	root.FileProperties = &FileProperties{ContentKeyPacket: t.ContentKeyPacket}
	return root, nil
}
