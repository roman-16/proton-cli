package kit

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/ui"
)

// What follows is the guard's specification. It has one job - to stand between a
// mistake and something that cannot be got back - and one way to fail that
// matters: letting the change through. So every case here checks not what was
// printed but whether the work ran.

// noTerminal is the answer a cron job gives: there is nobody there to ask, so a
// change that needs consent has to fail rather than wait for one.
const noTerminal = ""

// signedIn is an app with a session, which is what the guard's cases are about:
// whether a change is applied, not whether anyone is signed in. The one case about
// that says so.
func signedIn(a *app.App) *app.App {
	a.API = proton.New(proton.Options{})
	a.API.SetTokens("uid", "access", "refresh")
	return a
}

// mutation runs a spec against a prepared invocation and reports whether the
// change was applied.
func mutation(t *testing.T, a *app.App, answer string, spec ui.ResultSpec, computed bool) (applied bool, err error) {
	t.Helper()
	// Whether a question can be asked is decided here, not by whatever the
	// developer running the tests happens to have exported.
	prev, had := os.LookupEnv("PROTON_NO_INPUT")
	if err := os.Unsetenv("PROTON_NO_INPUT"); err != nil {
		t.Fatalf("unset PROTON_NO_INPUT: %v", err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("PROTON_NO_INPUT", prev)
		}
	})
	var out, errb bytes.Buffer
	a.UI = ui.New(ui.Options{
		Out: &out, Err: &errb,
		In:      strings.NewReader(answer),
		NoInput: answer == noTerminal,
	})
	c := &Invocation{Ctx: context.Background(), App: a, computed: computed}
	err = Mutate(c, spec, func() error { applied = true; return nil })
	return applied, err
}

func TestForeverIsRefusedWithNobodyToAsk(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "messages", Count: 112}
	applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, false)
	if applied {
		t.Fatal("a deletion ran without consent")
	}
	if err == nil {
		t.Fatal("want a refusal")
	}
	// The refusal has to name both ways out, or a script is stuck with a
	// message that says only that it may not proceed.
	if !strings.Contains(err.Error(), "cannot be undone") {
		t.Errorf("the refusal should say what is at stake: %v", err)
	}
	hints := strings.Join(err.(interface{ Hints() []string }).Hints(), " ")
	for _, want := range []string{"--yes", "--dry-run"} {
		if !strings.Contains(hints, want) {
			t.Errorf("the refusal should offer %s: %q", want, hints)
		}
	}
}

func TestForeverAsksEvenForOneNamedThing(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "labels", Count: 1, Name: "Work"}

	applied, err := mutation(t, signedIn(&app.App{}), "n\n", spec, false)
	if applied || err == nil {
		t.Errorf("a no must stop the change: applied=%v err=%v", applied, err)
	}

	applied, err = mutation(t, signedIn(&app.App{}), "y\n", spec, false)
	if !applied || err != nil {
		t.Errorf("a yes must let it through: applied=%v err=%v", applied, err)
	}
}

// A reference typed by hand carries no surprise, so a reversible removal of it
// runs. The same removal of things a filter chose does not.
func TestOutOfSightAsksOnlyForAComputedSelection(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Trashed, Kind: "messages", Count: 3, Detail: "to trash"}

	applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, false)
	if !applied || err != nil {
		t.Errorf("trashing what was named should not ask: applied=%v err=%v", applied, err)
	}

	applied, err = mutation(t, signedIn(&app.App{}), noTerminal, spec, true)
	if applied || err == nil {
		t.Errorf("trashing what a filter found should ask: applied=%v err=%v", applied, err)
	}
	if strings.Contains(err.Error(), "cannot be undone") {
		t.Errorf("a trash is recoverable and must not claim otherwise: %v", err)
	}
}

// Everything else runs, filtered or not. The prompt exists for removals; a
// question in front of a label or a move would only teach people to ignore it.
func TestOrdinaryChangesNeverAsk(t *testing.T) {
	for _, action := range []ui.Action{ui.Moved, ui.Labelled, ui.Updated, ui.Restored, ui.Copied} {
		for _, computed := range []bool{false, true} {
			spec := ui.ResultSpec{Action: action, Kind: "messages", Count: 40}
			applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, computed)
			if !applied || err != nil {
				t.Errorf("%s (computed=%v) should just run: applied=%v err=%v",
					action.Key, computed, applied, err)
			}
		}
	}
}

func TestYesIsTheAnswerGivenInAdvance(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "messages", Count: 112}
	applied, err := mutation(t, signedIn(&app.App{Yes: true}), noTerminal, spec, false)
	if !applied || err != nil {
		t.Errorf("--yes should let it through: applied=%v err=%v", applied, err)
	}
}

// A dry run destroys nothing, so it has nothing to ask about - and it must not
// apply the change on the way to saying so.
func TestDryRunNeitherAsksNorApplies(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "messages", Count: 112}
	applied, err := mutation(t, signedIn(&app.App{DryRun: true}), noTerminal, spec, false)
	if applied {
		t.Fatal("a dry run applied the change")
	}
	if err != nil {
		t.Fatalf("a dry run should not fail: %v", err)
	}
}

// A filter that matched nothing has nothing to confirm, and asking would make
// `delete --older-than 10y` a question on an account with no old mail.
//
// It has nothing to perform either. Proton answers a request to act on no IDs
// by complaining about the request - "The IDs is required" - which reads as a
// failure when the truth is that there was nothing to do.
func TestNothingSelectedIsNeitherAskedAboutNorApplied(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "messages", Count: 0}
	applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, true)
	if err != nil {
		t.Errorf("an empty change should not fail: %v", err)
	}
	if applied {
		t.Error("an empty change was sent to Proton")
	}
}

// A preview is a claim about what the command would do. Without an account it
// would not do it, so the dry run says so rather than describing a change that
// could never have happened - the one thing a dry run could otherwise answer as
// though it had reached Proton.
func TestDryRunWithoutAnAccountIsRefused(t *testing.T) {
	spec := ui.ResultSpec{Action: ui.Deleted, Kind: "messages", Count: 3}
	applied, err := mutation(t, &app.App{DryRun: true, API: proton.New(proton.Options{})}, noTerminal, spec, false)
	if applied {
		t.Fatal("a dry run applied the change")
	}
	if err == nil {
		t.Fatal("a dry run with no session should be refused")
	}
	var coder errs.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 2 {
		t.Errorf("err = %v, want one that exits 2 for a missing session", err)
	}
}

// What the run could not read is part of the account of the change, wherever
// the change is described: the confirmation, the preview, and the question a
// removal stops for. It is read off the tally by the guard, not declared by the
// command, so a change made on a short reading says so without anybody having
// to remember - and it is read again after the work, because a send consults a
// contact for every recipient while it runs.
func TestAChangeSaysWhatTheRunCouldNotRead(t *testing.T) {
	run := func(t *testing.T, a *app.App, answer string, spec ui.ResultSpec, during func(context.Context)) (string, error) {
		t.Helper()
		prev, had := os.LookupEnv("PROTON_NO_INPUT")
		if err := os.Unsetenv("PROTON_NO_INPUT"); err != nil {
			t.Fatalf("unset PROTON_NO_INPUT: %v", err)
		}
		t.Cleanup(func() {
			if had {
				_ = os.Setenv("PROTON_NO_INPUT", prev)
			}
		})
		var out, errb bytes.Buffer
		a.UI = ui.New(ui.Options{Format: ui.FormatText, Out: &out, Err: &errb, In: strings.NewReader(answer), NoInput: answer == noTerminal})
		ctx, tally := skip.With(context.Background())
		c := &Invocation{Ctx: ctx, App: a, tally: tally}
		err := Mutate(c, spec, func() error {
			if during != nil {
				during(ctx)
			}
			return nil
		})
		return errb.String(), err
	}
	before := func(ctx context.Context) { skip.Record(ctx, skip.KindFolder, "f1", skip.Unlockable, nil) }
	caveat := "1 folder could not be opened, so nothing inside it was included."

	t.Run("the confirmation carries it", func(t *testing.T) {
		spec := ui.ResultSpec{Action: ui.Trashed, Kind: "items", Count: 12}
		got, err := run(t, signedIn(&app.App{}), "y\n", spec, before)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "✓ Moved 12 items.\n! "+caveat) {
			t.Errorf("the confirmation does not carry the caveat:\n%s", got)
		}
	})
	t.Run("a skip during the work is said afterwards", func(t *testing.T) {
		spec := ui.ResultSpec{Action: ui.Sent, Kind: "messages", Count: 1}
		got, err := run(t, signedIn(&app.App{}), "y\n", spec, func(ctx context.Context) {
			skip.Record(ctx, skip.KindContact, "c1", skip.Unreadable, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "1 contact could not be read and was not included.") {
			t.Errorf("a skip during apply is not said:\n%s", got)
		}
	})
	t.Run("the question carries it", func(t *testing.T) {
		spec := ui.ResultSpec{Action: ui.Deleted, Kind: "items", Count: 12}
		ctx, tally := skip.With(context.Background())
		before(ctx)
		var errb bytes.Buffer
		a := signedIn(&app.App{})
		a.UI = ui.New(ui.Options{Format: ui.FormatText, Out: &bytes.Buffer{}, Err: &errb, In: strings.NewReader("n\n")})
		c := &Invocation{Ctx: ctx, App: a, tally: tally}
		if err := Mutate(c, spec, func() error { t.Fatal("no was not taken as no"); return nil }); err == nil {
			t.Fatal("a refused change should fail")
		}
		if i, j := strings.Index(errb.String(), caveat), strings.Index(errb.String(), "Continue?"); i < 0 || j < i {
			t.Errorf("the caveat should come before the question:\n%s", errb.String())
		}
	})
	t.Run("the preview carries it", func(t *testing.T) {
		spec := ui.ResultSpec{Action: ui.Trashed, Kind: "items", Count: 12}
		ctx, tally := skip.With(context.Background())
		before(ctx)
		var errb bytes.Buffer
		a := signedIn(&app.App{DryRun: true})
		a.UI = ui.New(ui.Options{Format: ui.FormatText, Out: &bytes.Buffer{}, Err: &errb})
		c := &Invocation{Ctx: ctx, App: a, tally: tally}
		if err := Mutate(c, spec, func() error { t.Fatal("a dry run applied the change"); return nil }); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(errb.String(), "Dry run - would move 12 items.\n! "+caveat) {
			t.Errorf("the preview does not carry the caveat:\n%s", errb.String())
		}
	})
}
