package proton

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ProtonMail/go-srp"
)

// A public link is the one thing here that can be reached with no account, so
// what these prove is which session each request goes out as: none while the
// link is being opened, the link's own afterwards, and the account's for
// anything that is not about the link.

const (
	linkToken    = "7X2K9M3N1P"
	linkPassword = "kQ81mDx4T9wL"
)

// linkServer is Proton for one public link: it proves the link over SRP for real
// and records who each request came as.
//
// grants says what proving it hands back. Empty is a signed-in caller, whose own
// session is granted access; a token is the session the link mints for a caller
// who has none.
type linkServer struct {
	*httptest.Server

	grants   string
	rejects  func(*http.Request) bool
	salt     []byte
	verifier []byte

	mu      sync.Mutex
	seen    []seenRequest
	proving *srp.Server
	proved  int
}

// seenRequest is one request as the server saw it: what was asked, and what it
// came as.
type seenRequest struct {
	path   string
	uid    string
	bearer string
}

func newLinkServer(t *testing.T, grants string) *linkServer {
	t.Helper()
	salt := make([]byte, 10)
	if _, err := rand.Read(salt); err != nil {
		t.Fatalf("salt: %v", err)
	}
	auth, err := srp.NewAuthForVerifier([]byte(linkPassword), signedTestModulus, salt)
	if err != nil {
		t.Fatalf("build the link's verifier: %v", err)
	}
	verifier, err := auth.GenerateVerifier(srpBits)
	if err != nil {
		t.Fatalf("generate the link's verifier: %v", err)
	}
	s := &linkServer{grants: grants, salt: salt, verifier: verifier}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *linkServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.seen = append(s.seen, seenRequest{
		path:   r.URL.Path,
		uid:    r.Header.Get("x-pm-uid"),
		bearer: strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "),
	})
	s.mu.Unlock()

	if s.rejects != nil && s.rejects(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/info"):
		s.info(w)
	case strings.HasSuffix(r.URL.Path, "/auth"):
		s.auth(w, r)
	default:
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}
}

func (s *linkServer) info(w http.ResponseWriter) {
	proving, err := srp.NewServerFromSigned(signedTestModulus, s.verifier, srpBits)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	challenge, err := proving.GenerateChallenge()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.mu.Lock()
	s.proving = proving
	s.proved++
	s.mu.Unlock()

	_ = json.NewEncoder(w).Encode(map[string]any{
		"Code":            1000,
		"Modulus":         signedTestModulus,
		"ServerEphemeral": base64.StdEncoding.EncodeToString(challenge),
		"UrlPasswordSalt": base64.StdEncoding.EncodeToString(s.salt),
		"SRPSession":      "srp-session",
		"Version":         4,
		"Flags":           PublicLinkGeneratedPassword,
		"VendorType":      PublicLinkDrive,
	})
}

func (s *linkServer) auth(w http.ResponseWriter, r *http.Request) {
	var body struct{ ClientProof, ClientEphemeral string }
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	proof, _ := base64.StdEncoding.DecodeString(body.ClientProof)
	ephemeral, _ := base64.StdEncoding.DecodeString(body.ClientEphemeral)

	s.mu.Lock()
	proving := s.proving
	s.mu.Unlock()
	serverProof, err := proving.VerifyProofs(ephemeral, proof)
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"Code":2026,"Error":"Wrong password"}`))
		return
	}

	answer := map[string]any{
		"Code":        1000,
		"ServerProof": base64.StdEncoding.EncodeToString(serverProof),
		"Share": map[string]any{
			"LinkID": "root-link", "VolumeID": "vol",
			"ShareKey": "key", "SharePassphrase": "passphrase", "SharePasswordSalt": "salt",
		},
	}
	if s.grants != "" {
		answer["UID"] = "link-uid"
		answer["AccessToken"] = s.grants
	}
	_ = json.NewEncoder(w).Encode(answer)
}

// requests is what the server was asked, in order.
func (s *linkServer) requests() []seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenRequest(nil), s.seen...)
}

// handshakes is how many times the link was proved from the beginning.
func (s *linkServer) handshakes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proved
}

// lastRequest is the most recent one, which is what a read that was retried
// finally went out as.
func (s *linkServer) lastRequest() seenRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.seen[len(s.seen)-1]
}

// openLink is what a command does with a URL: prove the link, then read what is
// behind it.
func openLink(t *testing.T, c *Client) {
	t.Helper()
	info, err := c.PublicLinkInfo(context.Background(), linkToken)
	if err != nil {
		t.Fatalf("PublicLinkInfo: %v", err)
	}
	if _, err := c.PublicLinkAuth(context.Background(), linkToken, info, linkPassword); err != nil {
		t.Fatalf("PublicLinkAuth: %v", err)
	}
}

// A link is for people who do not have an account, so the two requests that open
// one go out as nobody and the guard is never asked. A guard that ran here would
// answer "you are not signed in" to somebody holding everything they need.
func TestAPublicLinkIsOpenedWithoutAnAccount(t *testing.T) {
	srv := newLinkServer(t, "link-token")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetSessionGuard(func() error { return errors.New("the guard was asked about opening a link") })

	openLink(t, c)
	for _, req := range srv.requests() {
		if req.uid != "" || req.bearer != "" {
			t.Errorf("%s went out as %q/%q, want nobody", req.path, req.uid, req.bearer)
		}
	}
}

// The session a link mints is used for the rest of the run and written nowhere:
// it belongs to one link, Proton hands out nothing to renew it with, and a
// profile that opened a link is still a profile nobody signed in.
func TestALinkSessionIsUsedAndNeverBecomesTheAccounts(t *testing.T) {
	srv := newLinkServer(t, "link-token")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})

	openLink(t, c)
	if uid, acc, ref := c.Tokens(); uid != "" || acc != "" || ref != "" {
		t.Errorf("the account's tokens are %q/%q/%q, want a link to leave them alone", uid, acc, ref)
	}

	if _, err := c.Do(context.Background(), Request{
		Method: "GET", Path: "/drive/urls/" + linkToken,
	}); err != nil {
		t.Fatalf("read the link: %v", err)
	}
	if last := srv.lastRequest(); last.uid != "link-uid" || last.bearer != "link-token" {
		t.Errorf("the read went out as %q/%q, want the link's session", last.uid, last.bearer)
	}
}

// What a link grants is the link. A run that opened one still has no account, so
// anything about an account is answered the way it is answered to nobody, rather
// than asked for with a token Proton would refuse.
func TestALinkSessionIsNotAnAccount(t *testing.T) {
	srv := newLinkServer(t, "link-token")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetSessionGuard(func() error { return ErrUnauthorized })

	openLink(t, c)

	asked := len(srv.requests())
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/core/v4/addresses"})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("err = %v, want the account asked for", err)
	}
	if len(srv.requests()) != asked {
		t.Errorf("an account's endpoint was asked with a link's session: %v", srv.lastRequest())
	}
}

// A link session cannot be refreshed, so one Proton stops accepting is proved
// again from the URL - as nobody, like the first time, and without the request
// that provoked it being lost.
func TestALinkSessionThatExpiredIsProvedAgain(t *testing.T) {
	srv := newLinkServer(t, "first-token")
	srv.rejects = func(r *http.Request) bool {
		return r.Header.Get("Authorization") == "Bearer first-token"
	}
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})

	openLink(t, c)
	srv.grants = "second-token"

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/drive/urls/" + linkToken})
	if err != nil {
		t.Fatalf("read the link: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the read to go through on the new session", resp.Status)
	}
	if srv.handshakes() != 2 {
		t.Errorf("the link was proved %d times, want it proved again", srv.handshakes())
	}
	for _, req := range srv.requests() {
		if strings.HasSuffix(req.path, "/info") || strings.HasSuffix(req.path, "/auth") {
			if req.uid != "" || req.bearer != "" {
				t.Errorf("proving the link again went out as %q/%q, want nobody", req.uid, req.bearer)
			}
		}
	}
	if last := srv.lastRequest(); last.bearer != "second-token" {
		t.Errorf("the read was retried as %q, want the session just proved", last.bearer)
	}
}

// Signed in, the link is opened as the account: that is how Proton is told whose
// access grants it, and the answer carries no session of its own to take up.
func TestASignedInCallerOpensALinkAsItself(t *testing.T) {
	srv := newLinkServer(t, "")
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("account-uid", "account-token", "account-refresh")

	openLink(t, c)
	for _, req := range srv.requests() {
		if req.uid != "account-uid" || req.bearer != "account-token" {
			t.Errorf("%s went out as %q/%q, want the account", req.path, req.uid, req.bearer)
		}
	}

	if _, err := c.Do(context.Background(), Request{
		Method: "GET", Path: "/drive/urls/" + linkToken,
	}); err != nil {
		t.Fatalf("read the link: %v", err)
	}
	if last := srv.lastRequest(); last.uid != "account-uid" || last.bearer != "account-token" {
		t.Errorf("the read went out as %q/%q, want the account", last.uid, last.bearer)
	}
}
