package offline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExportingIntoADirectoryOfOtherFilesIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	refuses(t, 1, []string{"account", "export", "--dest-dir", dir},
		"holds other files and is not a copy of an account", "account export")
}

func TestExportingIntoAFileIsRefused(t *testing.T) {
	path := write(t, "copy", "not a directory")
	refuses(t, 1, []string{"account", "export", "--dest-dir", path}, "is not a directory")
}

func TestExportingSomethingThatIsNotAnAppIsRefused(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "copy")
	refuses(t, 1, []string{"account", "export", "photos", "--dest-dir", dest}, `"photos"`, "part of drive")
	refuses(t, 1, []string{"account", "export", "notes", "--dest-dir", dest}, "calendar, contacts, drive, mail and pass")
}

func TestLockingThePassArchiveNeedsPassInTheExport(t *testing.T) {
	passphrase := write(t, "passphrase", "correct horse")
	dest := filepath.Join(t.TempDir(), "copy")
	refuses(t, 1, []string{"account", "export", "mail", "--passphrase-file", passphrase, "--dest-dir", dest},
		"--passphrase-file locks the Pass archive")
}

func TestExportingTheAccountNeedsADirectory(t *testing.T) {
	refuses(t, 1, []string{"account", "export"}, "dest-dir")
}

func TestAWholeCalendarHasNoDays(t *testing.T) {
	refuses(t, 1, []string{"calendar", "events", "export", "--all", "--after", "2026-01-01"}, "--all", "--after")
	refuses(t, 1, []string{"calendar", "events", "export", "--all", "--before", "2026-12-31"}, "--all", "--before")
}

func TestDownloadingPhotosNeedsThemNamed(t *testing.T) {
	refuses(t, 1, []string{"drive", "photos", "download", "--dest-dir", t.TempDir()},
		"Nothing selected", "--album", "--all")
}
