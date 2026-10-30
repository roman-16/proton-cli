package export

import (
	"os"
	"path/filepath"
	"testing"
)

func TestADirectoryThatIsNotACopyIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := Check(filepath.Join(dir, "new")); err != nil {
		t.Errorf("a new directory: %v", err)
	}
	if err := Check(dir); err != nil {
		t.Errorf("an empty directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Check(dir); err != ErrNotACopy {
		t.Errorf("a directory of other files: %v", err)
	}
	if err := Check(filepath.Join(dir, "x")); err != ErrNotDirectory {
		t.Errorf("a file: %v", err)
	}
}

func TestACopyBelongsToOneAccount(t *testing.T) {
	root := filepath.Join(t.TempDir(), "copy")
	c, err := Open(root, "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if err := Check(root); err != nil {
		t.Errorf("a copy is refused as other files: %v", err)
	}
	_, err = Open(root, "user-2", "work@example.com")
	other, ok := err.(*OtherAccount)
	if !ok || other.Email != "you@proton.me" {
		t.Errorf("Open as another account = %v", err)
	}
}

func TestOneRunAtATimeIntoACopy(t *testing.T) {
	root := filepath.Join(t.TempDir(), "copy")
	c, err := Open(root, "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := Open(root, "user-1", "you@proton.me"); err == nil {
		t.Error("a second run opened the copy while the first held it")
	}
}

func TestARunKilledBeforeItNamedTheAccountLeavesACopy(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Check(root); err != nil {
		t.Errorf("a directory holding only the copy's own state = %v", err)
	}
}
