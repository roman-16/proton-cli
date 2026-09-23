package live

import (
	"strings"
	"testing"
)

// When other people are busy.
//
// Proton's calendar shows busy times only on a plan with room for several
// people, and Proton itself answers anybody who asks - so the CLI reads the
// account's plan before it asks anything. None of the three accounts is on such
// a plan: two are free and the paid one is on Unlimited. What a run can prove is
// that each kind is refused, having read the plan the way Proton reports it,
// and that nothing was asked about anybody on the way.

// A free account has no organization at all, and Unlimited has one with room
// for one: both are told which plans they would need.
func TestCalendarBusyTimesNeedAPlanForSeveralPeople(t *testing.T) {
	for _, tc := range []struct {
		account string
		run     func(*testing.T, ...string) (string, string, int)
	}{
		{"the free account", run},
		{"the Unlimited account", runPaid},
	} {
		stdout, stderr, code := tc.run(t, "calendar", "busy-times", "list", secondaryEmail())
		if code != 1 {
			t.Errorf("%s: exit %d, want 1\nstderr: %s", tc.account, code, truncateOutput(stderr))
		}
		if !strings.Contains(stderr, "needs a Duo, Family, Visionary or business plan") {
			t.Errorf("%s was not told which plans it would need: %s", tc.account, truncateOutput(stderr))
		}
		if stdout != "" {
			t.Errorf("%s: a refusal wrote to stdout: %s", tc.account, truncateOutput(stdout))
		}
	}
}
