package live

import (
	"fmt"
	"testing"
)

// Making a message self-destruct, which a plan gates.
//
// It acts on a message the account sent to itself and takes the expiry off again
// before deleting both copies, so nothing of the account's own is ever counting
// down.

// Making a message self-destruct, which a plan gates.
//
// It acts on a message the account sent to itself and takes the expiry off again
// before deleting both copies, so nothing of the account's own is ever counting
// down.

// Proton stores the moment, not the duration, so a message counting down reports
// when.
func TestMailMessagesExpireAndStop(t *testing.T) {
	subject := testID() + "-expire"
	msgID := sentToSelfPaid(t, subject)

	runOKPaid(t, "mail", "messages", "expire", "--in", "30d", "--", msgID)
	cleanupRunPaid(t, fmt.Sprintf("Stop expiry: proton mail messages expire --never -- %s", msgID),
		"mail", "messages", "expire", "--never", "--", msgID)

	at, _ := expirationOf(t, msgID)
	if at <= 0 {
		t.Fatalf("ExpirationTime = %v, want a moment in the future", at)
	}

	runOKPaid(t, "mail", "messages", "expire", "--never", "--", msgID)
	if at, _ := expirationOf(t, msgID); at != 0 {
		t.Errorf("after --never, ExpirationTime = %v, want 0", at)
	}
}

// expirationOf reads the moment Proton will delete a message, straight from the
// API, so the assertion does not rest on how the CLI renders it.
func expirationOf(t *testing.T, msgID string) (float64, bool) {
	t.Helper()
	data := runJSONPaid(t, "api", "GET", "/mail/v4/messages/"+msgID)
	msg, _ := data["Message"].(map[string]interface{})
	at, ok := msg["ExpirationTime"].(float64)
	return at, ok
}
