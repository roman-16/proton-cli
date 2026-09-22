package proton

import (
	"context"
	"errors"
)

// Forking a session: how a device that is already signed in signs another one
// in, without the account's password ever reaching the second device.
//
// It mirrors the flow behind Proton's "Sign in with QR code"
// (packages/account/signInWithAnotherDevice/ and the three calls in
// packages/shared/lib/api/auth.ts). The device being signed in opens a fork and
// waits on it; the device that is already signed in approves the fork, leaving a
// payload only the first can open; the first collects a session of its own.

// Fork is a sign-in waiting to be approved: the name Proton files it under, and
// the one-time code that says which fork an approval belongs to.
//
// The two travel differently. The selector stays here, because it is what this
// machine collects the session with; the code goes to the other device, because
// it is what the approval names.
type Fork struct {
	Selector string
	UserCode string
}

// NewFork opens a fork for this machine to be signed in through.
func (c *Client) NewFork(ctx context.Context) (*Fork, error) {
	var r struct {
		Selector string
		UserCode string
	}
	if err := c.Decode(ctx, Request{Method: "GET", Path: "/auth/v4/sessions/forks"}, &r); err != nil {
		return nil, err
	}
	return &Fork{Selector: r.Selector, UserCode: r.UserCode}, nil
}

// ErrForkWaiting is a fork nobody has approved yet, which is the answer to
// nearly every time it is asked about. It is a value rather than a failure
// because waiting is what the caller is there to do.
var ErrForkWaiting = errors.New("nobody has approved this fork yet")

// ForkedSession is what an approved fork hands over: a session of this machine's
// own, and the payload the other device sealed for whoever holds the code.
type ForkedSession struct {
	UID          string
	AccessToken  string
	RefreshToken string
	Payload      string
}

// PullFork collects the session an approved fork holds, and reports
// ErrForkWaiting for as long as nobody has approved it.
func (c *Client) PullFork(ctx context.Context, selector string) (*ForkedSession, error) {
	var r struct {
		UID          string
		AccessToken  string
		RefreshToken string
		Payload      string
	}
	err := c.Decode(ctx, Request{Method: "GET", Path: "/auth/v4/sessions/forks/" + selector}, &r)
	if err != nil {
		// Proton answers an unclaimed fork with the status a conflict comes back
		// under, which is what the web clients read it as too. Telling it apart
		// from a real failure is the whole of what makes waiting possible.
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.HTTPStatus == 422 {
			return nil, ErrForkWaiting
		}
		return nil, err
	}
	return &ForkedSession{
		UID: r.UID, AccessToken: r.AccessToken, RefreshToken: r.RefreshToken,
		Payload: r.Payload,
	}, nil
}

// PushFork approves a fork somebody opened on another device, handing this
// account to it.
//
// The fork it makes is independent, so the device that approved it can sign out
// without taking the new session with it.
func (c *Client) PushFork(ctx context.Context, userCode, clientID, payload string) error {
	return c.Decode(ctx, Request{
		Method: "POST", Path: "/auth/v4/sessions/forks",
		Body: map[string]any{
			"ChildClientID": clientID,
			"Independent":   1,
			"Payload":       payload,
			"UserCode":      userCode,
		},
	}, nil)
}

// AnonymousSession gives the client a session with nobody behind it.
//
// A fork is opened before there is an account to open it as, and Proton answers
// the request for a session rather than for nobody - which is what a sign-in
// screen holds too. Whatever the client was carrying is replaced, so a profile
// whose saved session has expired is signed in through a fork exactly as an
// empty one is.
func (c *Client) AnonymousSession(ctx context.Context) error {
	sess, err := c.createSession(ctx)
	if err != nil {
		return err
	}
	c.AdoptSession(sess.UID, sess.AccessToken, sess.RefreshToken)
	return nil
}

// AdoptSession takes up a session that arrived whole rather than being signed in
// to.
//
// The sealed key password goes with the session it was wrapped for: Proton holds
// no client key for this one, so a blob left over from the session being replaced
// could never be opened again, and keeping it would have `account get` report a
// profile as unlocked on the strength of bytes nothing can read.
func (c *Client) AdoptSession(uid, access, refresh string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uid, c.acc, c.ref = uid, access, refresh
	c.encKeyBlob = ""
}
