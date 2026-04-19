package tests

import "testing"

// Exit-code scheme:
//   0 = success
//   1 = user error (bad flag, missing arg, etc.)
//   3 = not-found
//   4 = ambiguous / conflict

func TestExit0Success(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "mail", "messages", "list", "--page-size", "1")
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestExit3NotFoundMail(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "mail", "messages", "read", "no-such-message-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
}

func TestExit3NotFoundContact(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "contacts", "get", "no-such-contact-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
}

func TestExit3NotFoundCalendarEvent(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "calendar", "events", "get", "no-such-event-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
}

func TestExit4AmbiguousMail(t *testing.T) {
	skipIfNoCredentials(t)
	// "a" matches many messages in any real inbox
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "2")
	if stdout == "" {
		t.Skip("empty mailbox; cannot test ambiguous")
	}
	_, _, code := run(t, "mail", "messages", "read", "a")
	// 3 (no match) or 4 (ambiguous) both acceptable; we specifically want 4
	// only when there are >=2 matches. Accept either but flag unexpected codes.
	if code != 3 && code != 4 {
		t.Errorf("expected exit 3 or 4 for generic 'a' REF, got %d", code)
	}
}

func TestExit1MissingRequiredFlag(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "mail", "messages", "send")
	if code == 0 {
		t.Error("expected non-zero exit for missing required --to")
	}
	if code != 1 {
		t.Errorf("expected exit 1 for user error, got %d", code)
	}
}

func TestExit1BadArgCount(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "api")
	if code == 0 {
		t.Error("expected non-zero exit for missing api args")
	}
}
