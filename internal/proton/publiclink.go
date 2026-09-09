package proton

import (
	"context"
	"fmt"
	"strings"
)

// A public link is a share nobody had to be invited to, opened by proving the
// password that is in its URL.
//
// The exchange is the one SRP code path everything else uses, so the server's
// proof is verified here as it is at sign-in. What differs is where the
// parameters come from and what the answer carries. A signed-in caller sends its
// own UID with the proof, and that session is granted access: the answer carries
// no tokens and every request that follows is an ordinary one. A caller with no
// account proves the link as nobody, and Proton answers with a session of its
// own, good for that link and nothing else - which is what lets a link be read
// without an account. Mirrors SharingPublicLinkSession in Proton's Drive SDK
// (client/js/src/internal/sharingPublic/session).

// linkSession is what a public link hands a caller who had no account.
//
// It is never written anywhere and never mixed with the account's. Proton mints
// it for one link, gives it no refresh token, and stops accepting it in its own
// time - so what it holds beside the credential is what proved the link, which
// is all it takes to prove it again.
type linkSession struct {
	// uid and access are what Proton answered the proof with, and what every
	// request about the link carries.
	uid, access string
	// token names the link and password opens it.
	token, password string
}

// covers reports whether a request is about this link.
//
// Proton serves a link's tree two ways, and both are the link's rather than the
// account's: under the token that names it, in place of the share ID nobody
// outside the share has, and under the prefix that answers without an account at
// all. Everything else is about the account whether or not a link is open.
func (s linkSession) covers(req Request) bool {
	return strings.Contains(req.Path, "/urls/"+s.token) || strings.HasPrefix(req.Path, unauthenticated)
}

// unauthenticated is where Proton serves what a public link permits: the same
// endpoints as the account's, answered for whoever holds the link.
const unauthenticated = "/drive/unauth/"

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
// link's tree, where its root is, and what the link permits whoever opened it.
type PublicLinkShare struct {
	ShareKey          string
	SharePassphrase   string
	SharePasswordSalt string
	VolumeID          string
	LinkID            string
	PublicPermissions int

	// Anonymous reports that the link answered with a session of its own, which
	// Proton does only for a caller it could not recognise: nobody is behind
	// anything done in this tree, and nothing written there can be attributed.
	Anonymous bool
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
	if err := c.Decode(ctx, Request{
		Method: "GET", Path: "/drive/urls/" + token + "/info", opensLink: true,
	}, &r); err != nil {
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
//
// A caller with no account gets a session for the link instead, which is taken
// up for the rest of the run: it is what every request about the link then
// carries, and it is all Proton will answer.
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
		password: []byte(password), opensLink: true,
	})
	if err != nil {
		return nil, err
	}
	var r struct {
		UID         string
		AccessToken string
		Share       PublicLinkShare
	}
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	if r.Share.LinkID == "" {
		return nil, fmt.Errorf("the public link opened, but Proton named no root for it")
	}
	if r.AccessToken != "" {
		c.mu.Lock()
		c.link = &linkSession{uid: r.UID, access: r.AccessToken, token: token, password: password}
		c.mu.Unlock()
		r.Share.Anonymous = true
		c.log.DebugContext(ctx, "the public link opened a session of its own, so nobody is behind this tree",
			"share", token)
	}
	return &r.Share, nil
}

// linkHeld is the public-link session and whether there is one.
func (c *Client) linkHeld() (linkSession, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.link == nil {
		return linkSession{}, false
	}
	return *c.link, true
}

// reproveLink opens a public link again, for a session Proton has stopped
// accepting.
//
// A link session cannot be refreshed - Proton mints it from the URL and hands
// out nothing to renew it with - so the URL is what mints it again, and the
// whole URL is held. failed is the token whose refusal prompted this, so a
// caller that queued behind another's proof asks for nothing.
//
// What comes back opens the same share, and a link that is gone answers what it
// answers: passing that on is why a link revoked mid-run says so instead of
// telling somebody their session expired.
func (c *Client) reproveLink(ctx context.Context, failed credential) error {
	c.linkMu.Lock()
	defer c.linkMu.Unlock()

	held, ok := c.linkHeld()
	if !ok || held.access != failed.token {
		return nil
	}
	info, err := c.PublicLinkInfo(ctx, held.token)
	if err != nil {
		return err
	}
	_, err = c.PublicLinkAuth(ctx, held.token, info, held.password)
	return err
}
