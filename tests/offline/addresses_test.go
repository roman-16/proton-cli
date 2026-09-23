package offline

import (
	"strings"
	"testing"
)

// An address is judged from the command line on every command that takes one,
// so a mistyped one is refused before anybody is signed in - and the refusal
// shows the command's own way of writing one.
func TestAnAddressIsJudgedBeforeTheNetwork(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		hints string
	}{
		{[]string{"calendar", "busy-times", "list", "jane"},
			"proton calendar busy-times list jane.roe@example.com"},
		{[]string{"calendar", "busy-times", "list", "jane.roe@example.com", "Jane <jane@example.com>"},
			"proton calendar busy-times list jane.roe@example.com"},
		{[]string{"calendar", "settings", "calendars", "share", "add", "Work", "jane"},
			"proton calendar settings calendars share add Work jane@proton.me"},
		{[]string{"account", "settings", "recovery-contacts", "add", "jane"},
			"proton account settings recovery-contacts add alex.roe@proton.me"},
	} {
		refuses(t, 1, tc.args, "is not an email address.", tc.hints)
	}
}

// A sender is an address or a whole domain, so the domain gets past the command
// line and only then finds nobody signed in.
func TestASenderIsAnAddressOrADomain(t *testing.T) {
	refuses(t, 1, []string{"mail", "settings", "senders", "block", "jane"},
		"neither an email address nor a domain written as @example.com")
	_, stderr, code := run(t, "mail", "settings", "senders", "block", "@example.com")
	if code != 2 || !strings.Contains(stderr, "not signed in") {
		t.Errorf("a domain: exit %d, want 2 for nobody signed in\nstderr: %s", code, truncate(stderr))
	}
}

// The word that removes a recovery address is taken where an address is, and
// only there.
func TestNoneRemovesARecoveryAddressAndIsNobodyElse(t *testing.T) {
	_, stderr, code := run(t, "account", "settings", "recovery-email", "set", "none")
	if code != 2 || !strings.Contains(stderr, "not signed in") {
		t.Errorf("recovery-email set none: exit %d, want 2 for nobody signed in\nstderr: %s", code, truncate(stderr))
	}
	refuses(t, 1, []string{"calendar", "settings", "calendars", "share", "add", "Work", "none"},
		"is not an email address.")
}

// Whom an event invites is judged before the calendar it goes into is looked up.
func TestAnAttendeeIsJudgedBeforeTheNetwork(t *testing.T) {
	event := []string{"calendar", "events", "create", "--title", "Review", "--start", "2026-05-04T10:00"}
	refuses(t, 1, append(event, "--attendee", "jane"), `"jane" is not an email address.`)
	refuses(t, 1, append(event, "--attendee", "jane@example.com:optinal"),
		"after the colon, write optional or required.", "--attendee jane@example.com:optional")
}

// What signs in to another mailbox is usually its address and need not be, so a
// login gets past the command line when the server is named.
func TestAnIMAPLoginNeedNotBeAnAddress(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "imports", "create", "jdoe",
		"--server", "imap.example.com", "--imap-password-file", secretFile(t, "hunter2"))
	if code != 2 || !strings.Contains(stderr, "not signed in") {
		t.Errorf("a login that is not an address: exit %d, want 2 for nobody signed in\nstderr: %s",
			code, truncate(stderr))
	}
}
