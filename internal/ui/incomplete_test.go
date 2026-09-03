package ui

import (
	"strings"
	"testing"
)

func TestAnIncompleteAnswerSaysWhatIsMissing(t *testing.T) {
	for _, c := range []struct {
		spec IncompleteSpec
		want string
	}{
		{IncompleteSpec{Count: 1, Kind: "item"},
			"1 item could not be decrypted and is not listed."},
		{IncompleteSpec{Count: 3, Kind: "item"},
			"3 items could not be decrypted and are not listed."},
		{IncompleteSpec{Count: 1, Kind: "contact"},
			"1 contact could not be decrypted and is not listed."},
		{IncompleteSpec{Count: 1, Kind: "folder", Hides: true},
			"1 folder could not be opened, so nothing inside it is listed."},
		{IncompleteSpec{Count: 2, Kind: "vault", Hides: true},
			"2 vaults could not be opened, so nothing inside them is listed."},
	} {
		if got := c.spec.Sentence(); got != c.want {
			t.Errorf("got  %q\nwant %q", got, c.want)
		}
	}
}

func TestACompleteAnswerSaysNothing(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.Incomplete(IncompleteSpec{})
	if out.Len() != 0 || errb.Len() != 0 {
		t.Errorf("a complete listing wrote a warning:\nout: %q\nerr: %q", out, errb)
	}
}

func TestAnIncompleteAnswerWarnsOnceWithItsRemedy(t *testing.T) {
	u, out, errb := fixture(t, Options{})
	u.Incomplete(IncompleteSpec{Count: 4, Kind: "item", Remedy: "run `proton report`"})

	if out.Len() != 0 {
		t.Errorf("the warning reached the answer stream: %q", out)
	}
	got := errb.String()
	for _, want := range []string{"4 items", "not listed", "run `proton report`"} {
		if !strings.Contains(got, want) {
			t.Errorf("the warning does not mention %q:\n%s", want, got)
		}
	}
	if strings.Count(got, "not listed") != 1 {
		t.Errorf("the warning is repeated:\n%s", got)
	}
}

func TestAQuietRunIsNotWarned(t *testing.T) {
	u, _, errb := fixture(t, Options{Quiet: true})
	u.Incomplete(IncompleteSpec{Count: 4, Kind: "item", Remedy: "run `proton report`"})
	if errb.Len() != 0 {
		t.Errorf("--quiet still warned: %q", errb)
	}
}
