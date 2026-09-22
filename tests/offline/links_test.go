package offline

import (
	"strings"
	"testing"
)

// What a calendar link is called is the account's own note about it, and how
// long it is, is a fact about the command line. Proton bounds what it stores,
// so the bound is kept here rather than found out after the link exists.

func TestACalendarLinksNameIsBoundedBeforeItIsPublished(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "links", "create",
		"--name", strings.Repeat("a", 501), "Work"}, "at most 500 characters")
	refuses(t, 1, []string{"calendar", "settings", "links", "update",
		"--name", strings.Repeat("a", 501), "5bH2mQxK"}, "at most 500 characters")
}

// Renaming with nothing to rename it to, and asking for a name and no name at
// once, are both settled by the flags alone.
func TestRenamingACalendarLinkNeedsOneAnswer(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "links", "update", "5bH2mQxK"},
		"Nothing to change.")
	refuses(t, 1, []string{"calendar", "settings", "links", "update",
		"--name", "Team feed", "--clear-name", "5bH2mQxK"}, "opposite things")
}

// How much a link shows is a declared domain, so a third answer is refused with
// the two that exist.
func TestACalendarLinkShowsOneOfTwoThings(t *testing.T) {
	refuses(t, 1, []string{"calendar", "settings", "links", "create",
		"--access", "editor", "Work"}, "--access accepts:", "limited", "full")
}
