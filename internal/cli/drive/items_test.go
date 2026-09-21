package drive

import (
	"testing"

	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
)

// What a listing puts under NAME is what the next command takes.
//
// A row that came out of the tree is somewhere, and two files of the same name
// in different folders are one row each: a listing of names would show the
// same word twice and answer neither question. A listing of one folder is
// already somewhere, so the folder is not repeated down the column.
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
