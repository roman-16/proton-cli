package offline

import (
	"strings"
	"testing"
)

// Replacing or deleting this binary needs no Proton account, so previewing it
// asks for none either. A dry run asserts an account exists everywhere else
// because it sends no request and would otherwise be the one path answering as
// though it had one - and that is exactly why the two commands that change the
// disk rather than the account have to say so.
func TestPreviewingAnInstallChangeNeedsNoAccount(t *testing.T) {
	for _, args := range [][]string{
		{"update", "--dry-run", "2.4.1"},
		{"uninstall", "--dry-run"},
	} {
		_, stderr, code := run(t, args...)
		if code != 0 {
			t.Errorf("%v: exit %d, want 0\nstderr: %s", args, code, truncate(stderr))
		}
		if strings.Contains(stderr, "not signed in") {
			t.Errorf("%v: asked for an account to preview a change to this machine\nstderr: %s",
				args, truncate(stderr))
		}
		if !strings.Contains(stderr, "Dry run") {
			t.Errorf("%v: did not report what it would do\nstderr: %s", args, truncate(stderr))
		}
	}
}

// The changelog's own arguments are judged before the fetch: a bound that is not
// a version, and a version asked for alongside a range, are both answerable from
// the command line alone.
func TestChangelogArgumentsAreJudgedBeforeTheFetch(t *testing.T) {
	refuses(t, 1, []string{"changelog", "latest"}, `"latest" is not a version`, "changelog 2.4.1")
	refuses(t, 1, []string{"changelog", "2.4"}, `"2.4" is not a version`, "changelog 2.4.1")
	refuses(t, 1, []string{"changelog", "--since", "yesterday"},
		`"yesterday" is not a version`, "changelog --since 2.4.1")
	refuses(t, 1, []string{"changelog", "--until", "2"},
		`"2" is not a version`, "changelog --until 2.4.1")
	refuses(t, 1, []string{"changelog", "2.4.1", "--since", "2.3.0"},
		"ask for different things", "changelog 2.4.1", "changelog --since 2.4.1")
	refuses(t, 1, []string{"changelog", "2.4.1", "2.3.0"}, "takes VERSION, but 2 arguments were given")
}
