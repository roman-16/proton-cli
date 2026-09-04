package live

import (
	"fmt"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// Clearing up after a test.
//
// Every test that creates data registers its cleanup immediately after
// creation, before any assertion that might fail - including the tests about
// deletion, which might fail before reaching the delete step. t.Cleanup runs on
// failure too, which is the whole point.
//
// A fixture is never cleaned up. The account holds it between runs on purpose;
// deleting it makes the next run make it again and spend the allowance it exists
// to protect.

// cleanup registers a cleanup that says loudly what a person has to do by hand
// if it could not do it itself.
func cleanup(t *testing.T, description string, fn func() error) {
	t.Helper()
	t.Cleanup(func() {
		if err := fn(); err != nil {
			t.Logf("\n"+
				"╔══════════════════════════════════════════════════════════════╗\n"+
				"║  ⚠️  CLEANUP FAILED - MANUAL ACTION REQUIRED                ║\n"+
				"╠══════════════════════════════════════════════════════════════╣\n"+
				"║  %s\n"+
				"║  Error: %s\n"+
				"╚══════════════════════════════════════════════════════════════╝",
				description, err)
		}
	})
}

// cleanupRun registers a cleanup that invokes the CLI as the primary account.
//
// A cleanup's job is that nothing is left behind, so finding the thing already
// gone - exit 3 - is the job done. A test whose subject is deletion would
// otherwise raise the alarm every time it worked.
func cleanupRun(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanupAs(t, account.Primary, description, args...)
}

// cleanupRunSecondary is cleanupRun for something the second account owns.
func cleanupRunSecondary(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanupAs(t, account.Secondary, description, args...)
}

// cleanupRunPaid removes something a paid test made. What is left behind here is
// on somebody's real account, and the photograph either side of the run names it
// whether this succeeded or not.
func cleanupRunPaid(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanupAs(t, account.Paid, description, args...)
}

func cleanupAs(t *testing.T, profile, description string, args ...string) {
	t.Helper()
	cleanup(t, description, func() error {
		_, stderr, code := runProfile(t, profile, consenting(args)...)
		if code != 0 && code != 3 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})
}
