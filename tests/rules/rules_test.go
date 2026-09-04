// Package rules holds the rules the live suite is held to, checked by reading
// its source.
//
// They live apart from the suite because they answer without Proton: what a test
// may do is a property of the code, so demanding three sign-ins and a photograph
// of somebody's real account to find out would be paying for an answer that was
// already on disk. These run in `just test-fast`, on every push.
package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/cli"
	"github.com/roman-16/proton-cli/tests/argv"
	"github.com/roman-16/proton-cli/tests/paid"
)

// liveDir is the suite these rules are about.
const liveDir = "../live"

// The paid account is somebody's, so nothing fills it with test data.
//
// The seed exists to bring an account to the state the suite and the README
// panel expect: it sends mail, makes calendars and leaves them there between
// runs. That is right for an account kept for the purpose and wrong for one
// somebody pays for, so the seed asks for the accounts declared free and never
// reaches for another.
func TestNothingSeedsThePaidAccount(t *testing.T) {
	src := read(t, "../../scripts/seed/main.go")
	for _, reach := range []string{"account.All()", "account.Get(", "account.Paid"} {
		if strings.Contains(src, reach) {
			t.Errorf("scripts/seed reaches for %s, which can name the paid account;"+
				" it fills accounts kept for the suite, and a paid one is not that", reach)
		}
	}
	if !strings.Contains(src, "account.Free()") {
		t.Error("scripts/seed no longer says which accounts it may act on;" +
			" it takes them from account.Free()")
	}
}

// Every restriction names a command that exists.
//
// A restriction guarding a command nobody can type refuses nothing, and makes
// the list look more protective than it is. Three of them once named
// `mail addresses delete`, `mail addresses disable` and `account delete`, none
// of which this CLI has - and the check that was supposed to prove the list
// worked fed each rule into the matcher it came from, so it could never notice.
// This asks the command tree instead.
func TestEveryPaidRestrictionNamesARealCommand(t *testing.T) {
	root := cli.Root()
	for _, r := range append(paid.Restrictions(), paid.FixtureOnly()...) {
		cmd, _, err := root.Find(r.Command)
		if err != nil || cmd == nil {
			t.Errorf("%q is a rule about the paid account and is not a command", strings.Join(r.Command, " "))
			continue
		}
		if got := commandPath(cmd); got != strings.Join(r.Command, " ") {
			t.Errorf("%q resolves to %q, so the restriction guards something other than what it names",
				strings.Join(r.Command, " "), got)
		}
		if cmd.RunE == nil && cmd.Run == nil {
			t.Errorf("%q is a group rather than a command, so it never acts and cannot be refused",
				strings.Join(r.Command, " "))
		}
	}
}

// A restriction narrowed to one argument names a value that command accepts.
//
// The two that are narrowed both guard a setting key, and a key Proton no longer
// has - or one this CLI renamed - would leave the setting writable with a
// restriction still claiming otherwise. The command is asked rather than its
// help text read: `settings set` judges its key before anything reaches the
// network, so this is the same answer a person would get.
func TestEveryNarrowedRestrictionNamesARealValue(t *testing.T) {
	root := cli.Root()
	for _, r := range paid.Restrictions() {
		if r.Value == "" {
			continue
		}
		cmd, _, err := root.Find(r.Command)
		if err != nil || cmd == nil {
			continue // the test above reports this
		}
		if cmd.Args == nil {
			t.Errorf("%q takes no arguments, so narrowing it to %q says nothing",
				strings.Join(r.Command, " "), r.Value)
			continue
		}
		// An impossible value, so what is left to complain about is the key.
		err = cmd.Args(cmd, []string{r.Value, "\x00"})
		if err != nil && strings.Contains(err.Error(), "There is no") {
			t.Errorf("%q is refused for %q, and the command says: %v",
				strings.Join(r.Command, " "), r.Value, err)
		}
	}
}

// Only the fixture mints what cannot be un-minted.
//
// paid.FixtureOnly names the commands whose effect on the paid account outlives
// the run: an alias address cannot be given back, so one made for the life of
// the account is a cost worth paying and one made per run is not. The fixture
// brings itself about through fixture.Ensure, so a test naming one of these
// directly is a test minting its own - which is what this catches, and the only
// place it can be caught: the run-time check sees a command line, and what makes
// this wrong is whose command line it is.
func TestOnlyTheFixtureMintsWhatCannotBeUnminted(t *testing.T) {
	paidCall := regexp.MustCompile(`run(?:OK)?(?:Stderr)?(?:JSON)?(?:Array)?Paid\(t,\s*((?:"[^"]*"(?:,\s*)?)+)`)
	word := regexp.MustCompile(`"([^"]*)"`)
	for _, name := range goFiles(t, liveDir) {
		for _, call := range paidCall.FindAllStringSubmatch(read(t, filepath.Join(liveDir, name)), -1) {
			var args []string
			for _, w := range word.FindAllStringSubmatch(call[1], -1) {
				args = append(args, w[1])
			}
			for _, r := range paid.FixtureOnly() {
				if argv.Has(args, r.Command...) {
					t.Errorf("%s runs %q as the paid account: %s",
						name, strings.Join(r.Command, " "), r.Why)
				}
			}
		}
	}
}

// Nothing in the live suite starts the binary itself.
//
// runAs is the one place that chooses which account a command acts as and builds
// the environment it runs in. A test that starts the process itself inherits
// whatever the developer has exported and acts as whatever profile that names -
// which is how a stdin upload once landed in a personal Drive instead of a test
// account's, and reported an empty folder rather than a failure.
func TestEveryInvocationGoesThroughTheRunner(t *testing.T) {
	for _, name := range goFiles(t, liveDir) {
		if name == "run_test.go" || name == "watch_test.go" || name == "main_test.go" {
			continue
		}
		if strings.Contains(read(t, filepath.Join(liveDir, name)), "exec.Command") {
			t.Errorf("%s starts the binary itself; go through run, runOK or runAs so the account"+
				" is chosen in one place", name)
		}
	}
}

// A profile is chosen by the runner, never by a flag.
//
// `--profile` on a command the primary runner started acts as another account
// with the primary's environment around it, which is the same mistake in a form
// that reads as though it were deliberate.
func TestNoTestChoosesItsAccountWithAFlag(t *testing.T) {
	for _, name := range goFiles(t, liveDir) {
		src := read(t, filepath.Join(liveDir, name))
		for _, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, `"--profile"`) || strings.Contains(line, "//") {
				continue
			}
			// The tests whose subject is the flag itself say so by naming the
			// variable the CLI reads, or by running with a stated environment.
			if strings.Contains(src, "PROTON_PROFILE") {
				continue
			}
			t.Errorf("%s picks an account with --profile; use runAs or one of the named runners:\n\t%s",
				name, strings.TrimSpace(line))
		}
	}
}

// The suite runs one test at a time, so nothing in it asks to overlap.
//
// It is bound by waiting for Proton, so overlapping would be faster - but what
// gives out first is what the free plan meters, and rate limiting is what a run
// hits before it saves any time. So the suite is serial, and saying otherwise in
// one test would be a claim the rest of it does not support: no lease, no
// vocabulary of shared state, and nothing to make two tests writing to the same
// calendar take turns.
func TestNoTestAsksToRunInParallel(t *testing.T) {
	for _, name := range goFiles(t, liveDir) {
		if strings.Contains(read(t, filepath.Join(liveDir, name)), "t.Parallel()") {
			t.Errorf("%s calls t.Parallel(); the suite runs one test at a time, and nothing in it"+
				" says what two tests cannot both have", name)
		}
	}
}

// Nothing in the suite declines to test something for want of a plan.
//
// A plan is a property of an account, not of a question: the tests that need one
// act as the paid account, which every run signs in. A skip in its place is a
// run that reports success for having done nothing, and the two it used to leave
// behind had the same feature tested twice - once in a test that always skipped
// and once in a test nobody ran.
func TestNoTestSkipsForWantOfAPlan(t *testing.T) {
	plan := regexp.MustCompile(`(?i)t\.Skipf?\([^)]*(paid|plan|subscription|upgrade)`)
	for _, name := range goFiles(t, liveDir) {
		src := read(t, filepath.Join(liveDir, name))
		if loc := plan.FindString(src); loc != "" {
			t.Errorf("%s skips for want of a plan: %s\n\tthe paid account is signed in on every run;"+
				" act as it instead", name, strings.TrimSpace(loc))
		}
	}
}

// ── reading the suite ──

func read(t *testing.T, path string) string {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(src)
}

func goFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), "_test.go") {
			out = append(out, e.Name())
		}
	}
	if len(out) == 0 {
		t.Fatalf("%s holds no tests to check", dir)
	}
	slices.Sort(out)
	return out
}

// commandPath is a command's own words, without the program name.
func commandPath(cmd interface{ CommandPath() string }) string {
	_, rest, _ := strings.Cut(cmd.CommandPath(), " ")
	return rest
}
