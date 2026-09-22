package mail

import (
	"strings"
	"testing"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
)

// The folders of an account, as a listing hands them over: each one followed by
// what is inside it.
func folderTree() []mailsvc.Label {
	return []mailsvc.Label{
		{ID: "work", Name: "work"},
		{ID: "2026", Name: "2026", Parent: "work"},
		{ID: "2025", Name: "2025", Parent: "work"},
		{ID: "apartment", Name: "apartment"},
		{ID: "vacations", Name: "vacations"},
	}
}

func laidOut(rows []mailsvc.Label) string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Name)
	}
	return strings.Join(out, " ")
}

// Naming some of them moves those to the front and leaves the rest as they
// were, which is what makes one name mean "put this one first".
func TestReorderMovesTheNamedMailboxesToTheFront(t *testing.T) {
	all := folderTree()
	roots := mailboxSiblings(all, "")

	order := reorderedMailboxes(roots, []mailsvc.Label{all[4]})
	if got := laidOut(order); got != "vacations work apartment" {
		t.Errorf("one named folder left the order %q", got)
	}

	order = reorderedMailboxes(roots, []mailsvc.Label{all[3], all[4], all[0]})
	if got := laidOut(order); got != "apartment vacations work" {
		t.Errorf("a whole order came out as %q", got)
	}
}

// A folder is ordered among the folders it sits beside, so the siblings of one
// inside another are its parent's children and nothing else.
func TestSiblingsAreTheFoldersInOnePlace(t *testing.T) {
	all := folderTree()
	if got := laidOut(mailboxSiblings(all, "")); got != "work apartment vacations" {
		t.Errorf("the top level is %q", got)
	}
	if got := laidOut(mailboxSiblings(all, "work")); got != "2026 2025" {
		t.Errorf("what is inside work is %q", got)
	}
	if got := laidOut(mailboxSiblings(all, "apartment")); got != "" {
		t.Errorf("an empty folder holds %q", got)
	}
}

// Every list is written on its own, so a tree is cut into one per containing
// folder, each in the order it is already in.
func TestEveryListIsWrittenOnItsOwn(t *testing.T) {
	lists := mailboxLists(folderTree())
	if len(lists) != 2 {
		t.Fatalf("a tree of two levels cut into %d lists", len(lists))
	}
	if lists[0].parent != "" || laidOut(lists[0].rows) != "work apartment vacations" {
		t.Errorf("the top level is %q under %q", laidOut(lists[0].rows), lists[0].parent)
	}
	if lists[1].parent != "work" || laidOut(lists[1].rows) != "2026 2025" {
		t.Errorf("the second list is %q under %q", laidOut(lists[1].rows), lists[1].parent)
	}
}

// Labels are one list, which is the same shape with nothing nested.
func TestLabelsAreOneList(t *testing.T) {
	lists := mailboxLists([]mailsvc.Label{{ID: "a", Name: "a"}, {ID: "b", Name: "b"}})
	if len(lists) != 1 || lists[0].parent != "" || laidOut(lists[0].rows) != "a b" {
		t.Errorf("labels came out as %d lists", len(lists))
	}
}

// A command line that asks for both orders, or for neither, is wrong before
// anybody is signed in.
func TestAReorderAsksForOneOrderOrTheOther(t *testing.T) {
	if err := mailboxReorderTakes("folders", true, 2); err == nil ||
		!strings.Contains(err.Error(), "not both") {
		t.Errorf("naming folders beside --alphabetical = %v, want it refused", err)
	}
	if err := mailboxReorderTakes("folders", false, 0); err == nil ||
		!strings.Contains(err.Error(), "Nothing to reorder") {
		t.Errorf("naming nothing at all = %v, want it refused", err)
	}
	if err := mailboxReorderTakes("folders", false, 1); err != nil {
		t.Errorf("naming one folder was refused: %v", err)
	}
	if err := mailboxReorderTakes("labels", true, 0); err != nil {
		t.Errorf("--alphabetical on its own was refused: %v", err)
	}
}

// An order the account is already in is a change nobody made.
func TestAnOrderTheAccountIsAlreadyInIsTheSameOrder(t *testing.T) {
	all := folderTree()
	roots := mailboxSiblings(all, "")
	if !sameMailboxes(roots, reorderedMailboxes(roots, []mailsvc.Label{all[0]})) {
		t.Error("naming the folder that is already first read as a change")
	}
	if sameMailboxes(roots, reorderedMailboxes(roots, []mailsvc.Label{all[4]})) {
		t.Error("moving a folder to the front read as no change")
	}
}
