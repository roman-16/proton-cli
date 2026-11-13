package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/profile"
)

type revokeServer struct {
	*httptest.Server
	mu      sync.Mutex
	revoked []string
}

func newRevokeServer(t *testing.T, refuse map[string]int) *revokeServer {
	t.Helper()
	srv := &revokeServer{}
	srv.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid := r.Header.Get("x-pm-uid")
		if status, ok := refuse[uid]; ok {
			w.WriteHeader(status)
			return
		}
		if r.Method == http.MethodDelete && r.URL.Path == "/core/v4/auth" {
			srv.mu.Lock()
			srv.revoked = append(srv.revoked, uid)
			srv.mu.Unlock()
			_, _ = w.Write([]byte(`{"Code":1000}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestApp(t *testing.T, name, apiURL string) *App {
	t.Helper()
	a, err := New(Options{
		Resolved: config.Resolved{Profile: profile.MustParse(name), NoLog: true, NoInput: true},
		APIURL:   apiURL,
		Version:  "dev",
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func saveTestSession(t *testing.T, name, uid, base string) {
	t.Helper()
	err := session.Save(profile.MustParse(name), &session.Session{
		UID: uid, AccessToken: "access-" + uid, RefreshToken: "refresh-" + uid, BaseURL: base,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSigningOutRevokesEachProfileWithItsOwnSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newRevokeServer(t, nil)
	saveTestSession(t, "default", "uid-default", srv.URL)
	saveTestSession(t, "work", "uid-work", srv.URL)
	a := newTestApp(t, "default", srv.URL)

	for _, name := range []string{"default", "work"} {
		if err := a.SignOut(context.Background(), profile.MustParse(name), false); err != nil {
			t.Fatalf("SignOut(%s): %v", name, err)
		}
	}
	if strings.Join(srv.revoked, ",") != "uid-default,uid-work" {
		t.Errorf("revoked %v, want both sessions", srv.revoked)
	}
	for _, name := range []string{"default", "work"} {
		if s, err := session.Load(profile.MustParse(name)); s != nil || err != nil {
			t.Errorf("profile %s still holds %v, %v after signing out", name, s, err)
		}
	}
}

func TestASessionAlreadyEndedIsSignedOutWithoutComplaint(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newRevokeServer(t, map[string]int{"uid-work": http.StatusUnauthorized})
	saveTestSession(t, "work", "uid-work", srv.URL)
	a := newTestApp(t, "default", srv.URL)

	if err := a.SignOut(context.Background(), profile.MustParse("work"), false); err != nil {
		t.Fatalf("SignOut: %v", err)
	}
	if s, _ := session.Load(profile.MustParse("work")); s != nil {
		t.Error("the ended session was kept")
	}
}

func TestASessionThatCouldNotBeRevokedIsKept(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	saveTestSession(t, "work", "uid-work", "http://127.0.0.1:1")
	a := newTestApp(t, "default", "")

	err := a.SignOut(context.Background(), profile.MustParse("work"), false)
	var refused *RevokeError
	if !errors.As(err, &refused) || refused.Profile.String() != "work" {
		t.Fatalf("SignOut = %v, want the revocation reported for profile work", err)
	}
	if s, _ := session.Load(profile.MustParse("work")); s == nil {
		t.Error("a session still valid at Proton was discarded")
	}

	if err := a.SignOut(context.Background(), profile.MustParse("work"), true); err != nil {
		t.Fatalf("SignOut --local: %v", err)
	}
	if s, _ := session.Load(profile.MustParse("work")); s != nil {
		t.Error("--local kept the session")
	}
}

func TestASessionIsOnlySentToTheAPIItWasSignedInAt(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv := newRevokeServer(t, nil)
	saveTestSession(t, "default", "uid-default", "https://mail.proton.me/api/")
	a := newTestApp(t, "default", srv.URL)

	if a.SignedIn() {
		t.Fatal("a session signed in at another API was loaded")
	}
	err := a.Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "https://mail.proton.me/api/") {
		t.Errorf("Authenticate = %v, want it to name the API the session belongs to", err)
	}
	if err := a.Login(context.Background(), "someone@proton.me"); err == nil {
		t.Error("signing in over a session from another API was allowed")
	}

	same := newTestApp(t, "default", "HTTPS://Mail.Proton.Me/api")
	if !same.SignedIn() {
		t.Error("the same API written differently was taken for another")
	}
}

func TestADamagedOrUnreadableSessionSaysSo(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	name := profile.MustParse("work")
	p, err := session.Path(name)
	if err != nil {
		t.Fatal(err)
	}
	saveTestSession(t, "work", "uid-work", "https://mail.proton.me/api")
	if err := os.WriteFile(p, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}

	err = newTestApp(t, "work", "").Authenticate(context.Background())
	if err == nil || err.Error() != `The saved session of profile "work" is damaged.` {
		t.Errorf("Authenticate = %v, want the damage named", err)
	}
	var hinter errs.Hinter
	if !errors.As(err, &hinter) || len(hinter.Hints()) == 0 || hinter.Hints()[0] != name.SignIn() {
		t.Errorf("hints = %v, want how to sign in again", hinter)
	}

	if os.Geteuid() == 0 {
		return
	}
	if err := os.Chmod(p, 0); err != nil {
		t.Fatal(err)
	}
	err = newTestApp(t, "work", "").Authenticate(context.Background())
	if err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("Authenticate = %v, want the file named as unreadable", err)
	}
}

func TestDefaultUserAgent(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", "proton-cli/dev"},
		{"dev", "proton-cli/dev"},
		{"1.2.3", "proton-cli/1.2.3"},
		{"v1.4.2", "proton-cli/v1.4.2"},
	}
	for _, tc := range tests {
		if got := defaultUserAgent(tc.in); got != tc.want {
			t.Errorf("defaultUserAgent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
