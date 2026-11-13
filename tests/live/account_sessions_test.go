package live

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/tests/account"
)

func TestAccountSessionsListMarksTheCurrentOne(t *testing.T) {
	sessions := runJSONArray(t, "account", "sessions", "list")
	if len(sessions) == 0 {
		t.Fatal("a signed-in account has at least this session")
	}
	current := 0
	for _, s := range sessions {
		row := s.(map[string]interface{})
		for _, key := range []string{"uid", "client_id", "create_time", "current"} {
			if _, ok := row[key]; !ok {
				t.Errorf("missing %q in %v", key, keysOf(row))
			}
		}
		if row["current"] == true {
			current++
		}
	}
	if current != 1 {
		t.Errorf("exactly one session is the current one, got %d", current)
	}
}

func TestAccountSessionsSignAnotherDeviceIn(t *testing.T) {
	code, stderr, saved, _ := signInAnotherDevice(t, t.TempDir())

	if !strings.HasPrefix(code, "0:") {
		t.Errorf("the code %q is not the shape a Proton app reads", code)
	}
	assertContains(t, stderr, "Signed in as "+selfEmail())
	if saved.Email != selfEmail() {
		t.Errorf("the device signed in as %q, want %q", saved.Email, selfEmail())
	}
	if saved.EncKeyBlob == "" {
		t.Error("the new session carries no key password, so nothing on that device can be decrypted")
	}

	sessions := runJSONArray(t, "account", "sessions", "list")
	for _, s := range sessions {
		if s.(map[string]interface{})["uid"] == saved.UID {
			return
		}
	}
	t.Errorf("the session the device was given is not among the %d Proton lists", len(sessions))
}

func signInAnotherDevice(t *testing.T, dir string) (code, stderr string, saved session.Session, path string) {
	t.Helper()
	device, err := signingIn(account.Primary, dir)
	if err != nil {
		t.Fatalf("start a sign-in: %v", err)
	}
	t.Cleanup(device.stop)

	code = device.waitForCode(t, time.Minute)
	runOK(t, "account", "sessions", "create", code)
	stderr = device.waitForSession(t, time.Minute)
	saved, path = signedInSession(t, dir)
	cleanupRun(t, "Revoke the session this test signed in: proton account sessions revoke "+saved.UID,
		"account", "sessions", "revoke", saved.UID)
	return code, stderr, saved, path
}

func signedInSession(t *testing.T, dir string) (session.Session, string) {
	t.Helper()
	var found string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(filepath.Dir(p)) == "sessions" && strings.HasSuffix(p, ".json") {
			found = p
		}
		return nil
	})
	if err != nil {
		t.Fatalf("look for the session: %v", err)
	}
	if found == "" {
		t.Fatal("the sign-in reported success and wrote no session")
	}
	raw, err := os.ReadFile(found)
	if err != nil {
		t.Fatalf("read %s: %v", found, err)
	}
	var s session.Session
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("read %s: %v", found, err)
	}
	return s, found
}
