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

// An explicitly named ordinary change remains interruption-free; a computed
// selection asks because the filter, not the command line, chose its targets.
func TestOrdinaryChangesAskOnlyForComputedSelections(t *testing.T) {
	for _, action := range []ui.Action{ui.Moved, ui.Labelled, ui.Updated, ui.Restored, ui.Copied} {
		spec := ui.ResultSpec{Action: action, Kind: "messages", Count: 40}
		applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, false)
		if !applied || err != nil {
			t.Errorf("named %s should just run: applied=%v err=%v", action.Key, applied, err)
		}
		applied, err = mutation(t, signedIn(&app.App{}), noTerminal, spec, true)
		if applied || err == nil {
			t.Errorf("computed %s should ask: applied=%v err=%v", action.Key, applied, err)
		}
	}
}

func TestConsentActionsAlwaysAskWithoutClaimingIrreversibility(t *testing.T) {
	for _, action := range []ui.Action{
		ui.Sent, ui.Invited, ui.Revoked, ui.SignedOut, ui.Connected,
		ui.Pinned.WithConsent(),
	} {
		spec := ui.ResultSpec{Action: action, Kind: "things", Count: 1}
		applied, err := mutation(t, signedIn(&app.App{}), noTerminal, spec, false)
		if applied || err == nil {
			t.Errorf("%s ran without consent: applied=%v err=%v", action.Key, applied, err)
		}
		if strings.Contains(err.Error(), "cannot be undone") {
			t.Errorf("%s is reversible and claimed otherwise: %v", action.Key, err)
		}
	}
}

func TestCreateHonorsConsentWithoutBurdeningOrdinaryDrafts(t *testing.T) {
	var sent bool
	a := signedIn(&app.App{})
	a.UI = ui.New(ui.Options{In: strings.NewReader(""), NoInput: true})
	c := &Invocation{Ctx: context.Background(), App: a}
	err := Create(c, ui.ResultSpec{Action: ui.Sent, Kind: "messages"}, func() (string, error) {
		sent = true
		return "message-id", nil
	})
	if err == nil || sent {
		t.Fatalf("send without consent: applied=%v err=%v", sent, err)
	}

	var drafted bool
	err = Create(c, ui.ResultSpec{Action: ui.Created, Kind: "drafts"}, func() (string, error) {
		drafted = true
		return "draft-id", nil
	})
	if err != nil || !drafted {
		t.Fatalf("ordinary draft creation: applied=%v err=%v", drafted, err)
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
