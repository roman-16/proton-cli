package drive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/localfile"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
)

func TestAListingNamesAFileByWhereItIs(t *testing.T) {
	name := nameCell(t)
	if got := name(drivesvc.Child{Name: "scratch.tmp", Path: "/Build/obj/scratch.tmp"}); got != "/Build/obj/scratch.tmp" {
		t.Errorf("a row from the tree is %q, want the path it sits at", got)
	}
	if got := name(drivesvc.Child{Name: "scratch.tmp"}); got != "scratch.tmp" {
		t.Errorf("a row of one folder's children is %q, want the name alone", got)
	}
}

func nameCell(t *testing.T) func(drivesvc.Child) string {
	t.Helper()
	for _, column := range childColumns() {
		if column.Header == "NAME" {
			return column.Cell
		}
	}
	t.Fatal("a listing of items has no NAME column")
	return func(drivesvc.Child) string { return "" }
}

func TestAFolderDownloadSortsWhatIsAlreadyThere(t *testing.T) {
	top := filepath.Join(t.TempDir(), "Documents")
	when := time.Date(2026, 3, 12, 9, 41, 0, 0, time.UTC)
	if err := localfile.Publish(filepath.Join(top, "held.txt"), []byte("held"), when); err != nil {
		t.Fatal(err)
	}
	files := []drivesvc.PlannedFile{
		{Path: "/held.txt", Plain: 4, Modified: when},
		{Path: "/sub/new.txt", Plain: 3, Modified: when},
	}
	owed, held, err := sortTree(top, files, false)
	if err != nil {
		t.Fatal(err)
	}
	if held != 1 || len(owed) != 1 || owed[0].Path != "/sub/new.txt" {
		t.Errorf("held %d, owed %+v; want the file already there kept and the new one owed", held, owed)
	}

	if err := os.WriteFile(filepath.Join(top, "held.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = sortTree(top, files, false)
	if err == nil || !strings.Contains(err.Error(), "held.txt") || !strings.Contains(err.Error(), "differ") {
		t.Errorf("a file that differs = %v, want a refusal naming it", err)
	}
	owed, held, err = sortTree(top, files, true)
	if err != nil || held != 0 || len(owed) != 2 {
		t.Errorf("--force owes %d and keeps %d (%v), want everything downloaded again", len(owed), held, err)
	}
}
