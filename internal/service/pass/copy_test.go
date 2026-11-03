package pass

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/roman-16/proton-cli/internal/passfile"
)

func passDoc(vaults map[string][]string) *passfile.ExportDocument {
	doc := passfile.NewDocument("user-1")
	for shareID, items := range vaults {
		vault := &passfile.ExportedVault{Name: shareID, Items: []passfile.ExportedItem{}}
		for _, id := range items {
			vault.Items = append(vault.Items, passfile.ExportedItem{ItemID: id, ShareID: shareID, Files: []string{id + ".txt"}})
		}
		doc.Vaults[shareID] = vault
	}
	return doc
}

func openedBackup(t *testing.T, doc *passfile.ExportDocument) *passfile.Backup {
	t.Helper()
	path := filepath.Join(t.TempDir(), "backup.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	var files []passfile.ArchiveFile
	for _, vault := range doc.Vaults {
		for _, item := range vault.Items {
			for _, entry := range item.Files {
				files = append(files, passfile.ArchiveFile{Entry: entry, Write: func(w io.Writer) error {
					_, err := io.WriteString(w, entry)
					return err
				}})
			}
		}
	}
	if err := passfile.WriteArchive(out, doc, files, ""); err != nil {
		t.Fatal(err)
	}
	_ = out.Close()
	b, err := passfile.OpenBackup(path, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestChangedSinceCountsWhatProtonChanged(t *testing.T) {
	before := passDoc(map[string][]string{"v1": {"a", "b"}})
	now := passDoc(map[string][]string{"v1": {"a", "b", "c"}, "v2": {"d"}})
	now.Vaults["v1"].Items[1].State = 2
	if got := changedSince(before, now); got != 4 {
		t.Errorf("changedSince = %d, want b, c, the new vault and d", got)
	}
	if got := changedSince(now, now); got != 0 {
		t.Errorf("an unchanged document changed %d times", got)
	}
}

func TestKeepCarriesWhatProtonNoLongerHas(t *testing.T) {
	previous := openedBackup(t, passDoc(map[string][]string{"v1": {"a", "gone"}, "v-gone": {"x"}}))
	now := passDoc(map[string][]string{"v1": {"a"}})
	kept := keep(previous, now)

	if got := len(now.Vaults["v1"].Items); got != 2 || now.Vaults["v1"].Items[1].ItemID != "gone" {
		t.Errorf("the vault holds %+v", now.Vaults["v1"].Items)
	}
	if _, ok := now.Vaults["v-gone"]; !ok {
		t.Error("a vault Proton no longer has left the copy")
	}
	entries := map[string]bool{}
	for _, f := range kept {
		entries[f.Entry] = true
	}
	if len(kept) != 2 || !entries["gone.txt"] || !entries["x.txt"] {
		t.Errorf("carried %v, want the attachments of what was kept", entries)
	}
	if got := changedSince(previous.Doc, now); got != 0 {
		t.Errorf("keeping what was there changed %d", got)
	}
}
