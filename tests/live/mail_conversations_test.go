package live

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Conversations: a thread is not the sum of its messages.
//
// Proton has its own verbs for one, and marking a thread read is a different
// request from marking each message in it read - so the thread-level verbs are
// exercised on a thread.

func TestMailConversationsListFromZeroResultsHint(t *testing.T) {
	needle := "no-such-sender-" + testID()
	_, stderr := runOKStderr(t, "mail", "conversations", "list", "--folder", "all", "--from", needle)
	if !strings.Contains(stderr, "--from matches the address only") {
		t.Errorf("expected the --from hint, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--keyword "+needle) {
		t.Errorf("expected stderr to contain '--keyword %s', got: %s", needle, stderr)
	}
}

func TestMailConversationsList(t *testing.T) {
	stdout := runOK(t, "mail", "conversations", "list", "--page-size", "5")
	assertContains(t, stdout, "SUBJECT")
}

func TestMailConversationsListJSONShape(t *testing.T) {
	data := runJSON(t, "mail", "conversations", "list", "--page-size", "3")
	if _, ok := data["total"]; !ok {
		t.Error("expected 'total' key")
	}
	convs, ok := data["conversations"].([]interface{})
	if !ok {
		t.Fatal("expected 'conversations' array")
	}
	if len(convs) > 0 {
		c := convs[0].(map[string]interface{})
		for _, field := range []string{"id", "subject", "num_messages", "time"} {
			if _, has := c[field]; !has {
				t.Errorf("expected snake_case field %q, got keys: %v", field, keysOf(c))
			}
		}
	}
}

// findConversationFor is the thread a message belongs to.
//
// A message always has one, so a miss is a failure rather than a reason to skip:
// a test that quietly did nothing is a worse answer than a red one.
func findConversationFor(t *testing.T, msgID string) string {
	t.Helper()
	convID := conversationIDOf(msgID)
	if convID == "" {
		t.Fatalf("message %s reports no conversation", msgID)
	}
	return convID
}

func TestMailConversationsRead(t *testing.T) {
	_, convID, subject := plainMail(t)

	stdout := runOK(t, "mail", "conversations", "get", "--", convID)
	assertContains(t, stdout, subject)
	assertContains(t, stdout, "Subject:")
	assertContains(t, stdout, "Messages:")
	assertContains(t, stdout, "Messages:")
}

func TestMailConversationsReadMsgIDRedirects(t *testing.T) {
	msgID, _, _ := plainMail(t)

	_, stderr, code := run(t, "mail", "conversations", "get", "--", msgID)
	if code != 3 {
		t.Errorf("expected exit 3, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "is a message, not a conversation")
	assertContains(t, stderr, "proton mail messages get")
	assertContains(t, stderr, msgID)
}

func TestMailConversationsBulkDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "mail", "conversations", "trash",
		"--unread", "--limit", "3")
	assertContains(t, stderr, "Dry run")
}

func TestMailConversationsTrashRoundTrip(t *testing.T) {
	convID := findConversationFor(t, mutableMail(t))

	runOK(t, "mail", "conversations", "trash", "--", convID)
	data := runJSON(t, "mail", "conversations", "list", "--page-size", "50")
	convs := data["conversations"].([]interface{})
	for _, c := range convs {
		if c.(map[string]interface{})["id"].(string) == convID {
			t.Error("trashed conversation should not appear in inbox list")
		}
	}
	runOK(t, "mail", "conversations", "move", "--into", "inbox", "--", convID)
}

// A thread is not the sum of its messages: Proton has its own verbs for one, and
// marking a thread read is a different request from marking each message in it
// read. So the thread-level verbs are exercised on a thread.
func TestMailConversationsMarkAndStar(t *testing.T) {
	convID := findConversationFor(t, mutableMail(t))

	runOK(t, "mail", "conversations", "mark", "unread", "--", convID)
	if !conversationListed(t, convID, "--unread") {
		t.Error("the thread should be among the unread ones after being marked unread")
	}
	runOK(t, "mail", "conversations", "mark", "read", "--", convID)
	if conversationListed(t, convID, "--unread") {
		t.Error("the thread should not be among the unread ones after being marked read")
	}

	runOK(t, "mail", "conversations", "star", "--", convID)
	if !conversationListed(t, convID, "--folder", "starred") {
		t.Error("the thread should be starred")
	}
	runOK(t, "mail", "conversations", "unstar", "--", convID)
	if conversationListed(t, convID, "--folder", "starred") {
		t.Error("the star should be gone")
	}
}

// conversationListed reports whether a thread is in the listing the filters name.
func conversationListed(t *testing.T, convID string, filters ...string) bool {
	t.Helper()
	args := append([]string{"mail", "conversations", "list", "--page-size", "50"}, filters...)
	for _, row := range runJSONArray(t, args...) {
		if row.(map[string]interface{})["id"] == convID {
			return true
		}
	}
	return false
}

// Deleting a thread is permanent and takes the whole thread, so it is tried on
// one nothing else reads: a draft is a thread of its own, and making one sends
// nothing.
func TestMailConversationsDeleteTakesTheWholeThread(t *testing.T) {
	subject := testID() + "-conv-delete"
	id := strings.TrimSpace(runOK(t, "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject, "--body", "a thread of one"))
	cleanupRun(t, "Delete draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	convID := findConversationFor(t, id)

	runOK(t, "mail", "conversations", "delete", "--", convID)

	if _, _, code := run(t, "mail", "messages", "get", "--", id); code != 3 {
		t.Errorf("the message is still there after its thread was deleted (exit %d)", code)
	}
}

func TestMailConversationsReadSummary(t *testing.T) {
	_, convID, _ := plainMail(t)

	data := runJSON(t, "--full-ids", "mail", "conversations", "get", convID)
	// Use the JSON shape to determine the expected message count.
	msgs := data["messages"].([]interface{})
	wantCount := len(msgs)
	if wantCount == 0 {
		t.Fatal("a thread holds at least the message it was found by")
	}

	// One line per message is a table, so the rows sit under a header and a rule.
	stdout := runOK(t, "mail", "conversations", "get", "--summary", convID)
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != wantCount+2 {
		t.Fatalf("expected a header, a rule and %d rows, got %d lines:\n%s", wantCount, len(lines), stdout)
	}
	for _, want := range []string{"#", "DATE", "FROM", "PREVIEW"} {
		assertContains(t, lines[0], want)
	}

	// Each row must match: <N>/<M>  <YYYY-MM-DD HH:MM>  <addr>  <preview>
	re := regexp.MustCompile(`^\d+/\d+\s+\d{4}-\d{2}-\d{2} \d{2}:\d{2}\s+\S`)
	for i, line := range lines[2:] {
		if !re.MatchString(line) {
			t.Errorf("row %d does not match summary shape: %q", i, line)
		}
	}
}

func TestMailConversationsReadSummaryAttachmentTag(t *testing.T) {
	_, convID, _, _ := attachedMail(t)
	stdout := runOK(t, "mail", "conversations", "get", "--summary", convID)
	// A row carrying attachments says how many, in the FLAGS column.
	assertContains(t, stdout, "FLAGS")
	if !regexp.MustCompile(`(?m)\s[1-9]\d*$`).MatchString(stdout) {
		t.Errorf("expected a row flagged with an attachment count:\n%s", truncateOutput(stdout))
	}
}

func TestMailConversationsReadStripQuotesKeepsLayout(t *testing.T) {
	_, convID, _ := plainMail(t)

	stdout := runOK(t, "mail", "conversations", "get", "--strip-quotes", convID)
	// Layout should still have per-message dividers + headers.
	if !strings.Contains(stdout, "\u2500\u2500\u2500 1/") {
		t.Errorf("--strip-quotes (without --summary) should keep per-message dividers; stdout:\n%s", truncateOutput(stdout))
	}
	if !strings.Contains(stdout, "From: ") {
		t.Errorf("--strip-quotes should keep per-message headers; stdout:\n%s", truncateOutput(stdout))
	}
}

func TestMailConversationsReadBodyOnly(t *testing.T) {
	_, convID, subject := plainMail(t)

	stdout := runOK(t, "mail", "conversations", "get", "--body-only", convID)
	for _, marker := range []string{
		"Subject:      ",
		"Conversation: ",
		"Messages:     ",
		"─── 1/",
		"\nFrom: ",
		"\nDate: ",
		"---",
		"Attachments (",
	} {
		if strings.Contains(stdout, marker) {
			t.Errorf("--body-only conv read should not contain %q; got:\n%s", marker, truncateOutput(stdout))
		}
	}
	// At least one body should reach stdout (this self-mail's body).
	if !strings.Contains(stdout, subject) {
		t.Errorf("--body-only conv read missing body containing %q", subject)
	}
}

func TestMailConversationsReadShowsAttachmentsPerMessage(t *testing.T) {
	_, convID, _, _ := attachedMail(t)
	stdout := runOK(t, "mail", "conversations", "get", convID)
	assertContains(t, stdout, "Attachments")
	assertContains(t, stdout, "NAME")
}

func TestMailConversationsReadIncludeInline(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	default1 := runOK(t, "mail", "conversations", "get", convID)
	if strings.Contains(default1, "DISPOSITION") {
		t.Errorf("default conv read should not show the DISPOSITION column:\n%s", truncateOutput(default1))
	}

	incl := runOK(t, "mail", "conversations", "get", "--include-inline", convID)
	if !strings.Contains(incl, "DISPOSITION") || !strings.Contains(incl, "inline") {
		t.Errorf("--include-inline conv read should show an inline attachment:\n%s", truncateOutput(incl))
	}
}

// A thread leaves the inbox as a whole and returns as a whole, which is why
// snooze is on threads and not on messages.
func TestMailConversationsSnoozeAndBringBack(t *testing.T) {
	convID := findConversationFor(t, mutableMail(t))

	runOK(t, "mail", "conversations", "snooze", "--until", "3d", "--", convID)
	cleanupRun(t, fmt.Sprintf("Unsnooze: proton mail conversations unsnooze -- %s", convID),
		"mail", "conversations", "unsnooze", "--", convID)

	if !waitFor(30*time.Second, 2*time.Second, func() bool {
		return conversationHasLabel(convID, "16") // SNOOZED
	}) {
		t.Error("a snoozed thread should carry the snoozed label")
	}

	runOK(t, "mail", "conversations", "unsnooze", "--", convID)
	if !waitFor(30*time.Second, 2*time.Second, func() bool {
		return !conversationHasLabel(convID, "16")
	}) {
		t.Error("after unsnoozing, the thread should be back in the inbox")
	}
}

func conversationHasLabel(convID, labelID string) bool {
	stdout, _, code, _ := runArgs(nil, "--output", "json", "api", "GET", "/mail/v4/conversations/"+convID)
	if code != 0 {
		return false
	}
	return strings.Contains(stdout, `"`+labelID+`"`)
}
