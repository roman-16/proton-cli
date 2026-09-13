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

// The name is taken out of the finished sentence, so it goes whether or not
// whatever wrapped it kept the shape it was attached in.
func TestANameIsWithheldFromUnderAWrapping(t *testing.T) {
	err := fmt.Errorf("could not export: %w", Naming("Bank login", errors.New("no key")))

	if got := Withheld(err); got != "could not export: <name>: no key" {
		t.Errorf("the log kept the name: %q", got)
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
