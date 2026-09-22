package live

import (
	"os"
	"path/filepath"
	"testing"
)

// Imports of mail from another provider, which Proton calls Easy Switch.
//
// Proton connects to the other mailbox itself, so the half of this that starts
// an import cannot be reached by any run: no Proton account speaks IMAP without
// Bridge, and a server this machine could stand up is not one Proton's machines
// can reach. What is here is everything a run can answer - the listings, the
// server lookup a create does first, and a dry run, which is the whole command
// bar the three requests that move mail. The mapping those would send is pinned
// offline, in internal/service/mail/imports_test.go.

// imapPassword is a file for the command to read a password out of. Nothing
// sends it: every test here stops before the request that would.
func imapPassword(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "imap-password")
	if err := os.WriteFile(path, []byte("proton-cli-test-not-a-real-password"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The listing answers whatever the account holds, including nothing, and says
// so in the shape every other listing uses.
func TestMailImportsList(t *testing.T) {
	stdout, stderr := runOKStderr(t, "mail", "settings", "imports", "list")
	if stdout == "" {
		assertContains(t, stderr, "No imports")
	} else {
		assertContains(t, stdout, "ACCOUNT")
		assertContains(t, stdout, "STATE")
	}

	for _, row := range listAll(t, "mail", "settings", "imports", "list") {
		m, ok := row.(map[string]interface{})
		if !ok {
			t.Fatalf("a row of the listing is not an object: %v", row)
		}
		for _, field := range []string{"id", "account", "provider", "state"} {
			if s, _ := m[field].(string); s == "" {
				t.Errorf("an import carries no %s: %v", field, m)
			}
		}
		// A consumer iterating the folders gets a list whatever the import is
		// doing, never null.
		if _, ok := m["folders"].([]interface{}); !ok {
			t.Errorf("an import's folders are not a list: %v", m["folders"])
		}
	}
}

// A dry run says what it would do and connects to nothing: it is the one way to
// see which server an import would reach before handing Proton a password.
func TestMailImportsCreateDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "settings", "imports", "create",
		"--imap-password-file", imapPassword(t),
		"--server", "imap.example.com", "--port", "993",
		"--dry-run", "proton-cli-test@example.com")

	assertContains(t, stderr, "Dry run")
	assertContains(t, stderr, "proton-cli-test@example.com")

	// The listing is the proof that nothing was made: a dry run that had
	// connected would leave an importer behind.
	for _, row := range listAll(t, "mail", "settings", "imports", "list") {
		m, _ := row.(map[string]interface{})
		if m["account"] == "proton-cli-test@example.com" {
			t.Fatal("a dry run made an import")
		}
	}
}

// Where Proton knows the provider, the server is looked up rather than typed.
// Yahoo is one Easy Switch imports over IMAP, so Proton answers for it.
func TestMailImportsFindsTheServerForAKnownProvider(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "settings", "imports", "create",
		"--imap-password-file", imapPassword(t),
		"--dry-run", "proton-cli-test@yahoo.com")

	assertContains(t, stderr, "Dry run")
}

// A reference naming no import is not found, rather than a request built around
// an ID nobody issued.
func TestMailImportsRefusesAnImportNobodyHas(t *testing.T) {
	for _, verb := range []string{"get", "cancel", "resume", "undo", "delete"} {
		_, stderr, code := run(t, "mail", "settings", "imports", verb, "proton-cli-test@example.com")
		if code != 3 {
			t.Errorf("%s: exit %d, want 3\nstderr: %s", verb, code, truncateOutput(stderr))
		}
	}
}
