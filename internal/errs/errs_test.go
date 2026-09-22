package errs

import (
	"errors"
	"fmt"
	"testing"
)

// The two readers get two different sentences: the person whose vault it is
// needs to know which item failed, and the log they may later hand to somebody
// else has no business knowing.
func TestANamedFailureIsWithheldFromEverybodyButItsOwner(t *testing.T) {
	err := Naming("Bank login", errors.New("unsupported item type"))

	if got, want := err.Error(), "Bank login: unsupported item type"; got != want {
		t.Errorf("the reader is not told which item failed:\n got %q\nwant %q", got, want)
	}
	if got, want := Withheld(err), "<name>: unsupported item type"; got != want {
		t.Errorf("the log kept the name:\n got %q\nwant %q", got, want)
	}
}

// A sentence that already reads as a sentence keeps it, and the name still
// travels beside it. "GitHub: GitHub carries no two-factor secret." is what
// naming it twice would say, which is why a message holding the name is left as
// its author wrote it.
func TestANameWrittenIntoTheSentenceIsNotSaidTwice(t *testing.T) {
	err := Naming("GitHub", Problemf("%s carries no two-factor secret.", "GitHub"))

	if got, want := err.Error(), "GitHub carries no two-factor secret."; got != want {
		t.Errorf("the reader is told twice:\n got %q\nwant %q", got, want)
	}
	if got, want := Withheld(err), "<name> carries no two-factor secret."; got != want {
		t.Errorf("the log kept the name:\n got %q\nwant %q", got, want)
	}
}

// The name is taken out of the finished sentence, so it goes whether or not
// whatever wrapped it kept the shape it was attached in.
func TestANameIsWithheldFromUnderAWrapping(t *testing.T) {
	err := fmt.Errorf("could not export: %w", Naming("Bank login", errors.New("no key")))

	if got := Withheld(err); got != "could not export: <name>: no key" {
		t.Errorf("the log kept the name: %q", got)
	}
}

// A name is spelled the way its owner spelled it. Everything else opening a
// message is a sentence and is capitalised to read as one.
func TestAShownFailureSpellsANameTheWayItIsWritten(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{Naming("yaasa", Problemf("a reorder sets the order of folders that sit in the same folder.")),
			"yaasa: A reorder sets the order of folders that sit in the same folder."},
		{Naming("bank login", errors.New("unsupported item type")),
			"bank login: Unsupported item type."},
		{Naming("GitHub", Problemf("%s carries no two-factor secret.", "GitHub")),
			"GitHub carries no two-factor secret."},
		{errors.New("no key"), "No key."},
		{nil, ""},
	} {
		if got := Shown(tc.err); got != tc.want {
			t.Errorf("the reader is shown %q, want %q", got, tc.want)
		}
	}
}

func TestWithheldLeavesAnOrdinaryFailureAlone(t *testing.T) {
	if got := Withheld(errors.New("no key")); got != "no key" {
		t.Errorf("rewrote a failure carrying no name: %q", got)
	}
	if got := Withheld(nil); got != "" {
		t.Errorf("wrote something about nothing: %q", got)
	}
}

// A lookup the reader chose the extent of says what that extent was, because
// "no attachment matched" reads the same whether one message came up empty or a
// whole thread did.
func TestAMissingThingNamesWhatWasSearched(t *testing.T) {
	for _, c := range []struct {
		err  *NotFound
		want string
	}{
		{&NotFound{Kind: "message", Ref: "x"}, `No message matching "x".`},
		{&NotFound{Kind: "vault"}, "No vault found."},
		{&NotFound{Kind: "attachment", Where: "in that thread", Ref: "x"},
			`No attachment in that thread matching "x".`},
		{&NotFound{Kind: "attachment", Where: "on that draft"}, "No attachment on that draft."},
	} {
		if got := c.err.Error(); got != c.want {
			t.Errorf("\n got %q\nwant %q", got, c.want)
		}
	}
}
