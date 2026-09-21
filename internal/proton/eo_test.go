package proton

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// eoRequests records what reached the server about a password-protected
// message: who it claimed to be, and what it carried instead.
type eoRequest struct {
	path, uid, authorization, eoUID string
}

func newEOServer(t *testing.T) (*httptest.Server, *[]eoRequest) {
	t.Helper()
	var seen []eoRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, eoRequest{
			path:          r.URL.Path,
			uid:           r.Header.Get("x-pm-uid"),
			authorization: r.Header.Get("Authorization"),
			eoUID:         r.Header.Get("x-eo-uid"),
		})
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Code":1000,"Token":"sealed"}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// Such a message is read by somebody Proton has no account for, so its requests
// go out as nobody and the guard is never asked. A guard that ran here would
// answer "you are not signed in" to somebody holding everything they need.
func TestAPasswordProtectedMessageIsReadWithoutAnAccount(t *testing.T) {
	srv, seen := newEOServer(t)
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetSessionGuard(func() error { return errors.New("the guard was asked about a message behind a password") })

	for _, req := range []Request{
		EOTokenRequest("msg-id"),
		EOMessageRequest("msg-id", "opened-token"),
		EOAttachmentRequest("msg-id", "opened-token", "att-id"),
		EOReplyRequest("msg-id", "opened-token", []byte("form"), "multipart/form-data; boundary=x"),
	} {
		if _, err := c.Do(context.Background(), req); err != nil {
			t.Fatalf("%s: %v", req.Path, err)
		}
	}
	if len(*seen) != 4 {
		t.Fatalf("sent %d requests, want 4", len(*seen))
	}
}

// The token a password unwrapped is the whole credential. An account signed in
// beside it has nothing to do with the message, so nothing of it goes on the
// request - a UID there would claim somebody else's mail for the account.
func TestAMessageBehindAPasswordCarriesItsTokenAndNotTheAccount(t *testing.T) {
	srv, seen := newEOServer(t)
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("account-uid", "account-token", "account-refresh")

	if _, err := c.Do(context.Background(), EOMessageRequest("msg-id", "opened-token")); err != nil {
		t.Fatalf("read the message: %v", err)
	}
	got := (*seen)[0]
	if got.uid != "" {
		t.Errorf("the request went out as %q, want nobody", got.uid)
	}
	if got.authorization != "opened-token" {
		t.Errorf("Authorization = %q, want the token alone and no bearer", got.authorization)
	}
	if got.eoUID != "msg-id" {
		t.Errorf("x-eo-uid = %q, want the message it opens", got.eoUID)
	}
}

// The first request is the public one: it hands out the token still sealed, so
// it names the message and identifies nobody at all.
func TestAskingForTheTokenIdentifiesNobody(t *testing.T) {
	srv, seen := newEOServer(t)
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("account-uid", "account-token", "account-refresh")

	if _, err := c.Do(context.Background(), EOTokenRequest("msg-id")); err != nil {
		t.Fatalf("ask for the token: %v", err)
	}
	got := (*seen)[0]
	if got.path != "/mail/v4/eo/token/msg-id" {
		t.Errorf("path = %q", got.path)
	}
	if got.uid != "" || got.authorization != "" || got.eoUID != "" {
		t.Errorf("the request carried %q/%q/%q, want nothing", got.uid, got.authorization, got.eoUID)
	}
}
