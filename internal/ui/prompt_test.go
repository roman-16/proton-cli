package ui

import (
	"strings"
	"testing"
)

// A verification is done in another window on another machine, so the only
// thing to say about it is that it is done. Anything typed is that and nothing
// more.
func TestAwaitTakesAnyLineAsTheAnswer(t *testing.T) {
	for _, typed := range []string{"\n", "y\n", "done\n", "   \n"} {
		u, _, errb := fixture(t, Options{})
		u.In = strings.NewReader(typed)
		if err := u.Await("Press Enter once it says you are verified."); err != nil {
			t.Errorf("typed %q: %v", typed, err)
		}
		if !strings.Contains(errb.String(), "Press Enter") {
			t.Errorf("typed %q: the question went nowhere: %q", typed, errb.String())
		}
	}
}

// An input that ends without a line is nobody answering, which is a no. A job
// with its input closed has to fail rather than treat silence as consent.
func TestAwaitTreatsAnEndOfInputAsNo(t *testing.T) {
	u, _, _ := fixture(t, Options{})
	u.In = strings.NewReader("")
	if err := u.Await("Press Enter once it says you are verified."); err == nil {
		t.Error("an input that answered nothing should not read as an answer")
	}
}

func TestAwaitRefusesWhenNothingMayBeAsked(t *testing.T) {
	u, _, errb := fixture(t, Options{NoInput: true})
	u.In = strings.NewReader("\n")
	if err := u.Await("Press Enter once it says you are verified."); err == nil {
		t.Error("--no-input forbids waiting for an answer as much as asking for one")
	}
	if errb.Len() != 0 {
		t.Errorf("a question that may not be asked was written anyway: %q", errb.String())
	}
}

// The environment and the flag are two ways to say the same thing, so either
// alone is enough.
func TestCanPromptRespectsBothTheFlagAndTheEnvironment(t *testing.T) {
	t.Run("the flag alone", func(t *testing.T) {
		u, _, _ := fixture(t, Options{NoInput: true})
		if u.CanPrompt() {
			t.Error("--no-input should forbid prompting")
		}
	})
	t.Run("the environment alone", func(t *testing.T) {
		t.Setenv("PROTON_NO_INPUT", "")
		u, _, _ := fixture(t, Options{})
		if u.CanPrompt() {
			t.Error("PROTON_NO_INPUT should forbid prompting")
		}
	})
	t.Run("neither, but stdin is not a terminal", func(t *testing.T) {
		u, _, _ := fixture(t, Options{})
		// The test process has no terminal on stdin, so prompting is impossible
		// regardless - which is the property a cron job relies on.
		if u.CanPrompt() {
			t.Error("a non-terminal stdin cannot be asked a question")
		}
	})
}

// A question is written where the cursor already is, whatever else the block
// goes on to ask. Padding a label towards a question that may never come puts
// a gap under the cursor of every sign-in that skips it.
func TestAskWritesTheLabelAndOneSpace(t *testing.T) {
	u, _, errb := fixture(t, Options{In: strings.NewReader("you@proton.me\n123456\n")})
	p := u.Ask()
	if _, err := p.Line("Email"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Line("Two-factor code"); err != nil {
		t.Fatal(err)
	}
	want := "Email: Two-factor code: "
	if got := errb.String(); got != want {
		t.Errorf("prompts are not written as the label alone\n got %q\nwant %q", got, want)
	}
}

// One reader for the whole block is what keeps a value typed before its question
// was asked. A reader per question buffers ahead and then throws the buffer away.
func TestAskKeepsWhatWasTypedAhead(t *testing.T) {
	u, _, _ := fixture(t, Options{In: strings.NewReader("you@proton.me\n123456\n")})
	p := u.Ask()
	if got, _ := p.Line("Email"); got != "you@proton.me" {
		t.Fatalf("first answer = %q", got)
	}
	if got, _ := p.Line("Two-factor code"); got != "123456" {
		t.Errorf("second answer = %q, want the line typed ahead", got)
	}
}

// Refusing to prompt has to produce an error a reader can act on, naming both
// ways to supply the value.
func TestPromptRefusalIsActionable(t *testing.T) {
	t.Setenv("PROTON_NO_INPUT", "1")
	u, _, errb := fixture(t, Options{})
	if _, err := u.Ask().Secret("Password"); err == nil {
		t.Fatal("want a refusal")
	}
	if errb.Len() != 0 {
		t.Errorf("a refused prompt should print nothing, got %q", errb.String())
	}
}
