package drive

import (
	"bytes"
	stdctx "context"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
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

// What the tree has already refused is not counted in the question: a delete
// that names five things and can take three asks about three, and the two it
// will not touch were said before the question was put.
func TestARefusedItemLeavesTheSelectionBeforeTheQuestion(t *testing.T) {
	rows := []drivesvc.Child{
		{LinkID: "mine", Path: "/mine.txt", Type: drivesvc.TypeFile},
		{LinkID: "theirs", Path: "/theirs.txt", Type: drivesvc.TypeFile},
		{LinkID: "old", Path: "/old.txt", Type: drivesvc.TypeFile},
	}
	refused := []drivesvc.Refused{
		{Name: "/theirs.txt", LinkID: "theirs", Reason: "is not yours to delete here."},
		{Name: "/old.txt", LinkID: "old", Reason: "was uploaded more than an hour ago."},
	}
	got := withoutRefusedItems(selection(rows), refused)

	if len(got.Rows) != 1 || got.Rows[0].LinkID != "mine" {
		t.Errorf("kept %v, want only what will really go", paths(got))
	}
	if len(got.IDs) != 1 || got.IDs[0] != "mine" {
		t.Errorf("ids are %v, want the rows' own", got.IDs)
	}
	if same := withoutRefusedItems(selection(rows), nil); len(same.Rows) != 3 {
		t.Errorf("a selection nothing was refused from kept %v", paths(same))
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

// A keyword that read less than the contents of everything it listed says so,
// and the four ways it can be short are four different things to do about it.
//
// Nothing on the screen otherwise distinguishes "there is no such file" from
// "what the files say was never read", which is what makes the caveat the
// difference between an answer and a wrong answer.
func TestAKeywordSaysWhatItDidNotRead(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cover drivesvc.Coverage
		f     filters
		want  string
	}{
		{
			name:  "the texts are still downloading",
			cover: drivesvc.Coverage{Indexed: true, Texts: 1240, Read: 512},
			f:     filters{keyword: "parking permit"},
			want:  "Only 512 of 1240 files have their text indexed",
		},
		{
			name: "nothing is indexed on this machine",
			f:    filters{keyword: "parking permit"},
			want: "There is no drive index on this machine",
		},
		{
			name:  "somewhere an index does not cover",
			cover: drivesvc.Coverage{Foreign: true},
			f:     filters{keyword: "parking permit"},
			want:  "Only your own files have their text indexed",
		},
		{
			name:  "an index that does not answer for the tree yet",
			cover: drivesvc.Coverage{Unfinished: true},
			f:     filters{keyword: "parking permit"},
			want:  "The drive index is not complete",
		},
		{
			name:  "every text read",
			cover: drivesvc.Coverage{Indexed: true, Texts: 1240, Read: 1240},
			f:     filters{keyword: "parking permit"},
		},
		{
			name:  "a filter that is not about what a file says",
			cover: drivesvc.Coverage{Foreign: true},
			f:     filters{pattern: "*.tmp"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var errb bytes.Buffer
			c := &kit.Invocation{
				Ctx: stdctx.Background(),
				App: &app.App{UI: ui.New(ui.Options{Format: ui.FormatText, Out: &bytes.Buffer{}, Err: &errb})},
			}
			shortIndex(c, tc.cover, &tc.f)
			got := errb.String()
			switch {
			case tc.want == "" && got != "":
				t.Errorf("said %q, want nothing", got)
			case tc.want != "" && !strings.Contains(got, tc.want):
				t.Errorf("said %q, want it to carry %q", got, tc.want)
			}
		})
	}
}
