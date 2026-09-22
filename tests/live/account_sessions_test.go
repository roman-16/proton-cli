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

// The sessions Proton holds for this account, across every device - and the one
// way a device gets one without a password: a code shown where the sign-in is
// happening and approved where one already has.

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

// Both halves of signing in from another device, which is the only way to test
// either: a sign-in waits with a code, this run approves it as the account it is
// already signed in as, and the waiting one ends up with a session of its own.
//
// It signs in to a config directory of its own, so the session it gets is one
// this test made rather than the suite's, and it revokes it afterwards.
func TestAccountSessionsSignAnotherDeviceIn(t *testing.T) {
	dir := t.TempDir()
	device, err := signingIn(account.Primary, dir)
	if err != nil {
		t.Fatalf("start a sign-in: %v", err)
	}
	t.Cleanup(device.stop)

	code := device.waitForCode(t, time.Minute)
	if !strings.HasPrefix(code, "0:") {
		t.Errorf("the code %q is not the shape a Proton app reads", code)
	}

	runOK(t, "account", "sessions", "create", code)
	stderr := device.waitForSession(t, time.Minute)
	saved := signedInSession(t, dir)
	cleanupRun(t, "Revoke the session this test signed in: proton account sessions revoke "+saved.UID,
		"account", "sessions", "revoke", saved.UID)

	assertContains(t, stderr, "Signed in as "+selfEmail())
	if saved.Email != selfEmail() {
		t.Errorf("the device signed in as %q, want %q", saved.Email, selfEmail())
	}
	// The passphrase that opens the keys came over sealed to the code, so the
	// device reads the account without anybody having typed a password into it.
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

// signedInSession reads the session a sign-in wrote, wherever the platform put
// the config directory it was pointed at.
func signedInSession(t *testing.T, dir string) session.Session {
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
	return s
}
