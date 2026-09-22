package offline

import (
	"os"
	"path/filepath"
	"testing"
)

// Starting an import hands another mailbox's password to Proton and sets a job
// going that runs for hours, so everything about it that a command line settles
// is settled here, before any of that.

// The password is the whole point of the command and it is never a flag value,
// so a run without a file to read it from is refused rather than started.
func TestAnImportNeedsThePasswordAsAFile(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com"},
		"The other mailbox's password is required", "--imap-password-file")
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com",
		"--imap-password", "hunter2"}, "Unknown flag: --imap-password")
}

// A file that is not there, or that holds nothing, is a mistake rather than an
// empty password sent to somebody else's server.
func TestAnImportPasswordFileIsRead(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com",
		"--imap-password-file", filepath.Join(dir, "nope")}, "Could not read")
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com",
		"--imap-password-file", empty}, "is empty")
}

// The window an import brings over is two days, and both are judged the way
// every other day range in this CLI is.
func TestAnImportWindowIsJudgedBeforeTheNetwork(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pw")
	if err := os.WriteFile(file, []byte("hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com",
		"--imap-password-file", file, "--after", "last tuesday"}, "--after")
	refuses(t, 1, []string{"mail", "settings", "imports", "create", "jane@fastmail.com",
		"--imap-password-file", file, "--after", "2026-04-15", "--before", "2026-01-01"}, "--before")
}

// Every verb that acts on an import needs to be told which one.
func TestAnImportVerbNeedsAnImport(t *testing.T) {
	for _, verb := range []string{"get", "cancel", "resume", "undo", "delete"} {
		refuses(t, 1, []string{"mail", "settings", "imports", verb}, "REF")
	}
}

// A listing orders by keys this collection has, and says which when it is given
// one it has not.
func TestAnImportListingOrdersByItsOwnKeys(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "imports", "list", "--sort", "folders"},
		"--sort accepts", "date", "account", "state", "size")
}
