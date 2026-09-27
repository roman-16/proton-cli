package live

import (
	"fmt"
	"strings"
	"testing"
)

// Making mail self-destruct, which a plan gates.
//
// Each test acts on mail the account sent to itself and takes the expiry off
// again before deleting it, so nothing of the account's own is ever counting
// down.

// Proton stores the moment, not the duration, so a message counting down reports
// when.
func TestMailMessagesExpireAndStop(t *testing.T) {
	subject := testID() + "-expire"
	msgID := sentToSelfPaid(t, subject)

	runOKPaid(t, "mail", "messages", "update", "--expires", "30d", "--", msgID)
	cleanupRunPaid(t, fmt.Sprintf("Stop expiry: proton mail messages update --expires never -- %s", msgID),
		"mail", "messages", "update", "--expires", "never", "--", msgID)

	if at := messageOf(t, msgID).expires; at <= 0 {
		t.Fatalf("ExpirationTime = %v, want a moment in the future", at)
	}

	runOKPaid(t, "mail", "messages", "update", "--expires", "never", "--", msgID)
	if at := messageOf(t, msgID).expires; at != 0 {
		t.Errorf("after --never, ExpirationTime = %v, want 0", at)
	}
}

func TestMailConversationsExpireAndStop(t *testing.T) {
	subject := testID() + "-thread-expire"
	msgID := sentToSelfPaid(t, subject)
	thread := messageOf(t, msgID).conversation

	runOKPaid(t, "mail", "messages", "trash", "--", msgID)
	trashed := messageOf(t, msgID).expires
	_, stderr := runOKStderrPaid(t, "mail", "conversations", "update", "--expires", "30d", "--", thread)
	for _, want := range []string{"1 message is in trash or spam", "Nothing to update."} {
		if !strings.Contains(stderr, want) {
			t.Errorf("updating a thread whose message is in the trash did not say %q:\n%s", want, stderr)
		}
	}
	if at := messageOf(t, msgID).expires; at != trashed {
		t.Errorf("the trashed message's ExpirationTime moved from %v to %v", trashed, at)
	}

	runOKPaid(t, "mail", "messages", "move", "--into", "inbox", "--", msgID)
	runOKPaid(t, "mail", "conversations", "update", "--expires", "30d", "--", thread)
	cleanupRunPaid(t, "Stop expiry: proton mail conversations update --expires never -- "+thread,
		"mail", "conversations", "update", "--expires", "never", "--", thread)
	if at := messageOf(t, msgID).expires; at <= 0 {
		t.Errorf("ExpirationTime = %v, want a moment in the future", at)
	}
	if at := threadRowExpiry(t, thread); at <= 0 {
		t.Errorf("conversations list carries expires %v for the thread, want a moment in the future", at)
	}

	runOKPaid(t, "mail", "conversations", "update", "--expires", "never", "--", thread)
	if at := messageOf(t, msgID).expires; at != 0 {
		t.Errorf("after --never, ExpirationTime = %v, want 0", at)
	}
}

func TestMailConversationsExpiryKeepsWhatTheSenderSet(t *testing.T) {
	subject := testID() + "-sender-expiry"
	inboxID := sentToSelfPaid(t, subject, "--expires", "7d")
	inbox := messageOf(t, inboxID)
	if !inbox.fixed() {
		t.Fatalf("a message sent with --expires arrived without the fixed-expiry flag (Flags %v)", inbox.flags)
	}

	runOKPaid(t, "mail", "conversations", "update", "--expires", "never", "--", inbox.conversation)
	if at := messageOf(t, inboxID).expires; at != inbox.expires {
		t.Errorf("the sender's expiry moved from %v to %v", inbox.expires, at)
	}

	_, stderr := runOKStderrPaid(t, "mail", "messages", "update", "--expires", "never", "--", inboxID)
	for _, want := range []string{"1 message keeps the expiry its sender set.", "Nothing to update."} {
		if !strings.Contains(stderr, want) {
			t.Errorf("messages update did not say %q:\n%s", want, stderr)
		}
	}
}

type liveMessage struct {
	conversation   string
	expires, flags float64
}

func (m liveMessage) fixed() bool { return int64(m.flags)&(1<<32) != 0 }

// messageOf reads a message straight from the API, so an assertion does not
// rest on how the CLI renders it.
func messageOf(t *testing.T, msgID string) liveMessage {
	t.Helper()
	data := runJSONPaid(t, "api", "GET", "/mail/v4/messages/"+msgID)
	msg, _ := data["Message"].(map[string]interface{})
	conversation, _ := msg["ConversationID"].(string)
	expires, _ := msg["ExpirationTime"].(float64)
	flags, _ := msg["Flags"].(float64)
	return liveMessage{conversation: conversation, expires: expires, flags: flags}
}

func threadRowExpiry(t *testing.T, thread string) float64 {
	t.Helper()
	data := runJSONPaid(t, "mail", "conversations", "list", "--folder", "all", "--limit", "50")
	rows, _ := data["conversations"].([]interface{})
	for _, row := range rows {
		r := row.(map[string]interface{})
		if r["id"] == thread {
			at, _ := r["expires"].(float64)
			return at
		}
	}
	t.Fatalf("conversations list --folder all did not show the thread %s", thread)
	return 0
}
