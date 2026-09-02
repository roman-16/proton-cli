package tests

import (
	"os"
	"strings"
	"testing"
)

// What a plan gates, and what a test may do about it.
//
// Proton puts a good deal of what the web clients offer behind a subscription,
// and the suite's accounts are free ones. No seeding can change that, so a test
// that reaches a gated feature skips rather than fails.
//
// Which features those are is written down here rather than discovered by each
// test, for the same reason the leases are: a refusal recognised by whatever
// substring happened to be in one error message is a skip that stops working the
// day Proton rewords it, and the test then fails somewhere unrelated to what it
// was about.
//
// The refusals are Proton's, so they are matched the way Proton makes them:
// by code where there is one, and by the words it uses where there is not.

// gate is how Proton says no to an account whose plan does not include
// something.
type gate struct {
	// feature is what the skip message names.
	feature string
	// code is Proton's own error code, which is the reliable half.
	code string
	// words are the phrases it uses when there is no code to match. Any one of
	// them is enough.
	words []string
}

var (
	// contactGroups: Proton answers 2027, which is unambiguous.
	contactGroups = gate{feature: "contact groups", code: "2027"}

	// autoReply refuses with 9100, the same code it uses for a missing scope, so
	// the CLI elevates the session and only then hears the real reason. The
	// sentence is all that separates the two.
	autoReplySchedule = gate{
		feature: "the auto-reply",
		words:   []string{"upgrade", "paid", "subscription"},
	}

	// messageExpiry answers the same way: 9100, meaning the scope on the way out
	// and the subscription on the way back. A command reaching it therefore
	// carries the credential flags even though no password would have helped.
	messageExpiry = gate{
		feature: "self-destructing messages",
		words:   []string{"upgrade", "paid", "subscription"},
	}

	// aliasContacts: Proton answers 2011, which is unambiguous.
	aliasContacts = gate{feature: "writing as an alias", code: "2011"}
)

// refusedByPlan reports whether stderr is Proton declining for want of a plan.
func (g gate) refusedByPlan(stderr string) bool {
	if g.code != "" && strings.Contains(stderr, g.code) {
		return true
	}
	lower := strings.ToLower(stderr)
	for _, w := range g.words {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// skipIfPlanRefuses ends the test when Proton declined for want of a plan, and
// fails it for anything else.
//
// It takes the exit code as well as stderr so that a command which succeeded is
// never mistaken for one that was refused: a message that happens to contain the
// word "upgrade" is not a refusal, and this is the only place that judgement is
// made.
func skipIfPlanRefuses(t *testing.T, g gate, code int, stderr string) {
	t.Helper()
	if code == 0 {
		return
	}
	if g.refusedByPlan(stderr) {
		// What Proton said goes in the skip: a gate is matched on words, and a
		// skip that hides the sentence it matched is one nobody can check.
		t.Skipf("%s needs a paid plan and this account does not have one: %s",
			g.feature, truncateOutput(strings.TrimSpace(stderr)))
	}
	t.Fatalf("%s: exit %d: %s", g.feature, code, truncateOutput(stderr))
}

// ── what may not be done to the paid account ──

// The paid account is somebody's, and unlike the two free ones it is not
// disposable. So a handful of commands are refused against it outright, here
// rather than in each test, because the failure they guard against is a test
// nobody thought about reaching for the wrong profile.
//
// What earns a place on this list is a command that changes something an account
// has only one of and cannot be put back by making another: a setting whose
// previous value is not readable, or an address that cannot be recreated once
// removed. Everything else is allowed, because a test that creates a thing can
// delete it.
//
// runAs enforces this - the one place a target account is chosen is the one
// place the choice can be refused.
var offLimitsOnPaid = []struct {
	words []string
	why   string
}{
	{[]string{"mail", "settings", "autoreply", "set"},
		"an auto-reply answers real mail on somebody's behalf, and what it said before is not readable"},
	{[]string{"mail", "settings", "set", "auto-delete-spam-trash"},
		"it decides whether real mail is deleted after thirty days"},
	{[]string{"drive", "settings", "set", "version-history"},
		"lowering it discards revisions Proton has already kept, and nothing puts them back"},
	{[]string{"mail", "addresses", "delete"},
		"an address cannot be recreated, and mail sent to it stops arriving"},
	{[]string{"mail", "addresses", "disable"},
		"mail sent to a disabled address stops arriving"},
	{[]string{"mail", "messages", "empty"},
		"it deletes a whole folder with no listing of what was in it"},
	{[]string{"drive", "trash", "empty"},
		"it deletes everything in the trash, including whatever was there before the run"},
	{[]string{"account", "delete"},
		"it is the account"},
}

// offLimits reports why a command may not be run as the paid account, or "".
func offLimits(args []string) string {
	for _, rule := range offLimitsOnPaid {
		if indexOfRun(args, rule.words...) >= 0 {
			return rule.why
		}
	}
	return ""
}

// ── the guards ──

// Nothing seeds the paid account.
//
// The seed fills an account with the fixtures the suite reads, which is right
// for an account kept for that and wrong for one somebody pays for: it sends
// mail, makes calendars and leaves them there between runs. The list of accounts
// it knows about is therefore the two free ones, and this says so out loud so
// that adding a third there has to be a decision.
func TestNothingSeedsThePaidAccount(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../scripts/seed/main.go")
	if err != nil {
		t.Fatalf("read the seed: %v", err)
	}
	if strings.Contains(string(src), "PROTON_CLI_TEST_PAID") {
		t.Error("scripts/seed names the paid account; it fills accounts kept for the suite, " +
			"and a paid one is not that")
	}
}

// The off-limits list is enforced where the account is chosen, not remembered by
// each test.
//
// The refused half is driven from the declaration rather than restating it: a
// second copy of those command words here would be one more thing to keep in
// step, and the source-reading guards would read them as commands this test
// runs.
func TestThePaidAccountRefusesWhatCannotBePutBack(t *testing.T) {
	t.Parallel()
	for _, rule := range offLimitsOnPaid {
		if offLimits(rule.words) == "" {
			t.Errorf("%v is declared off limits but allowed", rule.words)
		}
		// A rule matches the command, whatever follows it: the flags a caller
		// adds must not be a way around it.
		withFlags := append(append([]string{"--yes"}, rule.words...), "--some-flag", "value")
		if offLimits(withFlags) == "" {
			t.Errorf("%v is allowed once flags are added", rule.words)
		}
	}
}

// Reading is always allowed, and so is anything a test can undo. A guard that
// refused everything would be safe and useless.
func TestThePaidAccountAllowsWhatCanBeUndone(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"mail", "addresses", "list"},
		{"mail", "settings", "autoreply", "get"},
		{"drive", "settings", "get"},
		{"calendar", "events", "list"},
		{"pass", "items", "list"},
	} {
		if why := offLimits(args); why != "" {
			t.Errorf("%v is refused against the paid account: %s", args, why)
		}
	}
}

// Every rule says why, because the message is what a person reads when a test
// they just wrote refuses to run.
func TestEveryPaidRestrictionSaysWhy(t *testing.T) {
	t.Parallel()
	for _, rule := range offLimitsOnPaid {
		if len(rule.words) == 0 {
			t.Error("a restriction matches nothing")
		}
		if rule.why == "" {
			t.Errorf("%v is refused without saying why", rule.words)
		}
	}
}
