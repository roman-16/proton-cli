package live

import (
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// Watching for mail.
//
// A watch reports what arrives while it is watching, so the subject is the
// watching itself: a message sent while it runs appears, and one that was
// already there does not.

// A watch reports what arrives while it is watching, so the test's subject is
// the watching itself: a message sent while it runs appears, and one that was
// already there does not. That means one real send, which is what the arrival
// path is.
func TestMailMessagesWatchReportsAnArrival(t *testing.T) {
	subject := testID() + "-watched"
	w, err := watchAs(account.Primary, "mail", "messages", "watch")
	if err != nil {
		t.Fatalf("start watch: %v", err)
	}
	defer w.stop(t)

	// Only act once the watch has signed in and begun, so the send is not racing
	// its first poll.
	w.waitReady(t, 10*time.Second)

	// A watch starts at its own cursor, so a message sent after this point is
	// the first thing it should tell us about.
	sendTestMailSecondary(t, subject)

	line := w.waitForLine(t, 90*time.Second, func(l string) bool {
		return strings.Contains(l, subject)
	})
	if !strings.Contains(line, subject) {
		t.Fatalf("watch line %q does not name the message %q", line, subject)
	}
}

// A watch that starts after a message exists never re-opens that message: the
// first thing it reports is what arrives after it began.
func TestMailMessagesWatchDoesNotReplayThePast(t *testing.T) {
	// A message that exists before any watcher starts.
	early := testID() + "-early"
	sendTestMailSecondary(t, early)

	w, err := watchAs(account.Primary, "mail", "messages", "watch")
	if err != nil {
		t.Fatalf("start watch: %v", err)
	}
	defer w.stop(t)
	w.waitReady(t, 10*time.Second)

	// Now a message arrives while it is watching. The first thing it reports is
	// this one, not the earlier one.
	late := testID() + "-late"
	sendTestMailSecondary(t, late)

	line := w.waitForLine(t, 90*time.Second, func(l string) bool {
		return strings.Contains(l, late)
	})
	if strings.Contains(line, early) {
		t.Fatalf("watch reopened the earlier message %q before reporting %q", early, line)
	}
}
