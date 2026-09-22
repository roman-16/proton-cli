package mail

import (
	"strings"
	"testing"
)

func laid(rows []Label) string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Name)
	}
	return strings.Join(out, " ")
}

// A listing shows each folder followed by what is inside it, however the
// account's folders arrive.
func TestTreePutsEachFolderBeforeWhatIsInsideIt(t *testing.T) {
	got := Tree([]Label{
		{ID: "work", Name: "work"},
		{ID: "apartment", Name: "apartment"},
		{ID: "vacations", Name: "vacations"},
		{ID: "2026", Name: "2026", Parent: "work"},
		{ID: "cruise", Name: "cruise", Parent: "vacations"},
		{ID: "june", Name: "june", Parent: "2026"},
	})
	if want := "work 2026 june apartment vacations cruise"; laid(got) != want {
		t.Errorf("the tree reads %q, want %q", laid(got), want)
	}
}

// Siblings keep the order they arrive in, so whatever sorted the slice decides
// their sequence.
func TestTreeKeepsTheOrderSiblingsArriveIn(t *testing.T) {
	got := Tree([]Label{
		{ID: "b", Name: "b"},
		{ID: "a", Name: "a"},
		{ID: "b2", Name: "b2", Parent: "b"},
		{ID: "b1", Name: "b1", Parent: "b"},
	})
	if want := "b b2 b1 a"; laid(got) != want {
		t.Errorf("the tree reads %q, want %q", laid(got), want)
	}
}

// Labels do not nest, so the list is the answer.
func TestTreeLeavesAFlatListAlone(t *testing.T) {
	got := Tree([]Label{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}})
	if want := "a b"; laid(got) != want {
		t.Errorf("the list reads %q, want %q", laid(got), want)
	}
}

// A folder whose parent is not in the list is still one of the account's, so it
// is kept rather than dropped.
func TestTreeKeepsAFolderItCannotPlace(t *testing.T) {
	got := Tree([]Label{
		{ID: "a", Name: "a"},
		{ID: "stray", Name: "stray", Parent: "gone"},
	})
	if want := "a stray"; laid(got) != want {
		t.Errorf("the tree reads %q, want %q", laid(got), want)
	}
}
