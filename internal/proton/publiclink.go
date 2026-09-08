package proton

import (
	"context"
	"fmt"
)

// A public link is a share nobody had to be invited to, opened by proving the
// password that is in its URL.
//
// The exchange is the one SRP code path everything else uses, so the server's
// proof is verified here as it is at sign-in. What differs is only where the
// parameters come from and what comes back: a signed-in caller's session is
// granted access to the link, so the answer carries no tokens and every request
// that follows is an ordinary one. Mirrors SharingPublicLinkSession in Proton's
// Drive SDK (client/js/src/internal/sharingPublic/session).

// Public link flags, as Proton numbers them. A link made today carries a
// generated password in its URL and, when its owner set one, a second password
// concatenated after it.
const (
	PublicLinkCustomPassword    = 1
	PublicLinkGeneratedPassword = 2
)

// Public link vendors. A link can point at a Docs or Sheets document, which is
// a different product behind the same URL shape.
const (
	PublicLinkDrive  = 0
	PublicLinkDoc    = 1
	PublicLinkSheet  = 2
	vendorTypeUnread = -1
)

// PublicLinkInfo is what a link says about itself before anything is proved:
// which passwords open it, what it points at, and the SRP parameters to prove
// them against.
type PublicLinkInfo struct {
	Flags      int
	VendorType int

	auth authInfo
}

// NeedsCustomPassword reports whether the URL's own password is not enough.
func (i *PublicLinkInfo) NeedsCustomPassword() bool {
	return i.Flags&PublicLinkCustomPassword != 0
}

// Legacy reports a link made before the password moved into the URL. Its owner
// gave the whole password to whoever they sent it to, so there is nothing in the
// URL to open it with.
func (i *PublicLinkInfo) Legacy() bool {
	return i.Flags&PublicLinkGeneratedPassword == 0
}

// PublicLinkShare is what proving the password opens: the share holding the
// link's tree, and where its root is.
type PublicLinkShare struct {
	ShareKey          string
	SharePassphrase   string
	SharePasswordSalt string
	VolumeID          string
	LinkID            string
	PublicPermissions int
}

// PublicLinkInfo starts the handshake for a link.
func (c *Client) PublicLinkInfo(ctx context.Context, token string) (*PublicLinkInfo, error) {
	var r struct {
		Modulus         string
		ServerEphemeral string
		UrlPasswordSalt string
		SRPSession      string
		Version         int
		Flags           int
		VendorType      int
	}
	// VendorType is absent from an older link's answer, where the only vendor
	// there was is Drive. Reading it as the zero value would be right by
	// accident; saying so is what keeps a Docs link from being read as one.
	r.VendorType = vendorTypeUnread
	if err := c.Decode(ctx, Request{Method: "GET", Path: "/drive/urls/" + token + "/info"}, &r); err != nil {
		return nil, err
	}
	if r.VendorType == vendorTypeUnread {
		r.VendorType = PublicLinkDrive
	}
	return &PublicLinkInfo{
		Flags: r.Flags, VendorType: r.VendorType,
		auth: authInfo{
			Modulus:         r.Modulus,
			ServerEphemeral: r.ServerEphemeral,
			Version:         r.Version,
			Salt:            r.UrlPasswordSalt,
			SRPSession:      r.SRPSession,
		},
	}, nil
}

// PublicLinkAuth proves a link's password and takes up what it opens.
//
// info is the handshake the caller already has, which the first attempt proves
// against; a further attempt asks for a fresh one, because the SRPSession a
// handshake carries is spent by the attempt that used it.
//
// A session that is signed in sends its own UID with the proof, which is how
// Proton is told who is opening the link and what grants it access. The answer
// then carries no tokens of its own, so nothing here has a second session to
// keep: every request about the link that follows is an ordinary one.
func (c *Client) PublicLinkAuth(ctx context.Context, token string, info *PublicLinkInfo, password string) (*PublicLinkShare, error) {
	held := info
	resp, err := c.exchange(ctx, srpExchange{
		parameters: func(ctx context.Context) (*authInfo, error) {
			if held != nil {
				used := held
				held = nil
				return &used.auth, nil
			}
			fresh, err := c.PublicLinkInfo(ctx, token)
			if err != nil {
				return nil, err
			}
			return &fresh.auth, nil
		},
		method: "POST", path: "/drive/urls/" + token + "/auth",
		password: []byte(password),
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		AccessToken string
		Share       PublicLinkShare
	}
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	if r.AccessToken != "" {
		c.log.DebugContext(ctx, "the public link opened a session of its own rather than using this one",
			"share", token)
	}
	if r.Share.LinkID == "" {
		return nil, fmt.Errorf("the public link opened, but Proton named no root for it")
	}
	return &r.Share, nil
}
