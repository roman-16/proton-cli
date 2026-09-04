package offline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A file the command line names is read, and judged, before anything is sent.
//
// Nothing about "this file holds no events" or "this is not a message" needs an
// account: the refusal is about the bytes on this machine. Each of these once
// cost a round trip to Proton and a set of credentials to reach.

func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// A calendar file with nothing importable in it is refused, and the refusal
// names the file rather than leaving somebody to guess which one.
func TestImportingACalendarFileWithNoEventsIsRefused(t *testing.T) {
	empty := write(t, "empty.ics", "BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n")
	refuses(t, 1, []string{"calendar", "events", "import", empty}, "holds no events", empty)
}

// A file that is not mail cannot be sent as mail.
func TestSendingAFileThatIsNotMailIsRefused(t *testing.T) {
	path := write(t, "not-mail.eml", "this is not a message")
	refuses(t, 1, []string{"mail", "messages", "send", "--eml", path}, path, "parse message")
}

// A moment in the past is not a moment to wake up at.
func TestSnoozingUntilThePastIsRefused(t *testing.T) {
	refuses(t, 1, []string{"mail", "conversations", "snooze", "--until", "2020-01-01T09:00", "any-ref"},
		"in the past")
}

// An update that changes nothing is a command line whose author did not check
// it, and the refusal says which flags would have changed something.
func TestUpdatingAnAddressWithNothingToChangeIsRefused(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "addresses", "update", "someone@example.com"},
		"Nothing to change", "--signature")
}

// Favouriting is a verb, not a tag to be set. Proton's own client offers no way
// to add or remove a tag, so neither does this - and a `tags` collection would
// invite a second way to say what `favorite` already says.
func TestPhotosOfferFavouritingRatherThanTags(t *testing.T) {
	stdout, stderr, code := run(t, "drive", "photos", "--help")
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	for _, want := range []string{"favorite", "unfavorite"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("photos does not offer %q:\n%s", want, truncate(stdout))
		}
	}
	for _, line := range strings.Split(stdout, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == "tags" {
			t.Errorf("photos should not expose a 'tags' subcommand:\n%s", truncate(stdout))
		}
	}
}
