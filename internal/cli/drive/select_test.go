package drive

import (
	"testing"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
)

// Every verb this selection feeds acts on a folder as a whole, so a filter that
// matched a folder and what is inside it named the same work twice.
func TestASelectedFolderCoversWhatIsInsideIt(t *testing.T) {
	rows := []drivesvc.Child{
		{LinkID: "build", Path: "/Build", Type: drivesvc.TypeFolder},
		{LinkID: "tmp", Path: "/Build/tmp", Type: drivesvc.TypeFolder},
		{LinkID: "a", Path: "/Build/tmp/a.tmp", Type: drivesvc.TypeFile},
		{LinkID: "elsewhere", Path: "/Downloads/b.tmp", Type: drivesvc.TypeFile},
		{LinkID: "sibling", Path: "/Buildings/c.tmp", Type: drivesvc.TypeFile},
	}
	got := withoutCoveredItems(selection(rows))

	want := []string{"build", "elsewhere", "sibling"}
	if len(got.Rows) != len(want) {
		t.Fatalf("kept %d rows, want %d: %v", len(got.Rows), len(want), paths(got))
	}
	for i, id := range want {
		if got.Rows[i].LinkID != id {
			t.Errorf("row %d is %q, want %q (order is what the preview shows)", i, got.Rows[i].LinkID, id)
		}
		if got.IDs[i] != id {
			t.Errorf("id %d is %q, want %q", i, got.IDs[i], id)
		}
	}
}

// A name is not a path: /Buildings is not inside /Build, and the root covers
// everything under it.
func TestCoveredComparesWholePathSegments(t *testing.T) {
	for _, tt := range []struct {
		path    string
		folders []string
		want    bool
	}{
		{"/Build/tmp", []string{"/Build"}, true},
		{"/Buildings/tmp", []string{"/Build"}, false},
		{"/Build", []string{"/Build"}, false},
		{"/Documents/report.pdf", []string{"/"}, true},
		{"/", []string{"/"}, false},
		{"/Documents/report.pdf", []string{"/Build", "/Documents"}, true},
	} {
		if got := covered(tt.path, tt.folders); got != tt.want {
			t.Errorf("covered(%q, %v) = %v, want %v", tt.path, tt.folders, got, tt.want)
		}
	}
}

// A selection of files alone is left exactly as it was.
func TestASelectionWithNoFoldersIsUntouched(t *testing.T) {
	rows := []drivesvc.Child{
		{LinkID: "a", Path: "/a.tmp", Type: drivesvc.TypeFile},
		{LinkID: "b", Path: "/b.tmp", Type: drivesvc.TypeFile},
	}
	if got := withoutCoveredItems(selection(rows)); len(got.Rows) != 2 || len(got.IDs) != 2 {
		t.Errorf("kept %v, want both files", paths(got))
	}
}

func selection(rows []drivesvc.Child) kit.Selection[drivesvc.Child] {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.LinkID)
	}
	return kit.Selection[drivesvc.Child]{Rows: rows, IDs: ids}
}

func paths(sel kit.Selection[drivesvc.Child]) []string {
	out := make([]string, 0, len(sel.Rows))
	for _, row := range sel.Rows {
		out = append(out, row.Path)
	}
	return out
}
