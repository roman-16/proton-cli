package passfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func backupAt(t *testing.T, passphrase string) string {
	t.Helper()
	doc := NewDocument("user-1")
	doc.Vaults["share-1"] = &ExportedVault{Name: "Personal", Items: []ExportedItem{{
		ItemID: "item-1", ShareID: "share-1", Files: []string{"note.txt"},
	}}}
	path := filepath.Join(t.TempDir(), "backup.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	files := []ArchiveFile{{Entry: "note.txt", Write: func(w io.Writer) error {
		_, err := io.WriteString(w, "attached")
		return err
	}}}
	if err := WriteArchive(out, doc, files, passphrase); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenBackupReadsTheDocumentAndItsFiles(t *testing.T) {
	for _, passphrase := range []string{"", "correct horse"} {
		b, err := OpenBackup(backupAt(t, passphrase), passphrase)
		if err != nil {
			t.Fatalf("passphrase %q: %v", passphrase, err)
		}
		if b.Doc.Encrypted != (passphrase != "") || b.Doc.Vaults["share-1"].Items[0].ItemID != "item-1" {
			t.Errorf("passphrase %q: read %+v", passphrase, b.Doc)
		}
		f, ok := b.File("note.txt")
		if !ok {
			t.Fatal("the attachment is not in the backup")
		}
		var got strings.Builder
		if err := f.Write(&got); err != nil || got.String() != "attached" {
			t.Errorf("the attachment reads %q, %v", got.String(), err)
		}
		if _, ok := b.File("other.txt"); ok {
			t.Error("a file the archive does not hold was found")
		}
		if err := b.Close(); err != nil {
			t.Error(err)
		}
	}
}

func TestOpenBackupNamesWhyItWillNotOpen(t *testing.T) {
	locked := backupAt(t, "correct horse")
	if _, err := OpenBackup(locked, ""); !errors.Is(err, ErrBackupLocked) {
		t.Errorf("no passphrase: %v", err)
	}
	if _, err := OpenBackup(locked, "battery staple"); !errors.Is(err, ErrBackupPassphrase) {
		t.Errorf("another passphrase: %v", err)
	}
	damaged := filepath.Join(t.TempDir(), "damaged.zip")
	if err := os.WriteFile(damaged, []byte("not a zip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenBackup(damaged, ""); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Errorf("a damaged archive: %v", err)
	}
	if _, err := OpenBackup(filepath.Join(t.TempDir(), "none.zip"), ""); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no archive: %v", err)
	}
}
