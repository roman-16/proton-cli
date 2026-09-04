package live

import (
	"strings"
	"testing"
	"time"
)

// Messages: listing them, finding one, reading it, and moving it about.

func TestMailMessagesList(t *testing.T) {
	stdout := runOK(t, "mail", "messages", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "FROM")
	assertContains(t, stdout, "SUBJECT")
}

func TestMailMessagesListSent(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--folder", "sent")
}

func TestMailMessagesListJSONFieldNames(t *testing.T) {
	data := runJSON(t, "mail", "messages", "list", "--page-size", "1")
	msgs, ok := data["messages"].([]interface{})
	if !ok {
		t.Fatal("expected messages array")
	}
	if len(msgs) > 0 {
		m := msgs[0].(map[string]interface{})
		for _, field := range []string{"id", "subject", "from_address", "num_attachments", "time"} {
			if _, has := m[field]; !has {
				t.Errorf("expected json field %q (snake_case), got keys: %v", field, keysOf(m))
			}
		}
	}
}

func TestMailMessagesListPageSize(t *testing.T) {
	data := runJSON(t, "mail", "messages", "list", "--page-size", "3")
	msgs := data["messages"].([]interface{})
	if len(msgs) > 3 {
		t.Errorf("expected at most 3 messages, got %d", len(msgs))
	}
}

func TestMailMessagesListUnreadFlag(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--unread")
}

func TestMailMessagesListFooterSinglePage(t *testing.T) {
	// Use 150 (Proton's documented max for messages list); 500 is
	// rejected as "Invalid page size parameter".
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--page-size", "150")
	last := lastNonEmpty(stderr)
	// One page holds everything, so the footer is a plain count: no "of", no
	// next-page instruction, and never a page number the reader did not ask for.
	if !strings.HasSuffix(last, "messages.") && !strings.HasSuffix(last, "message.") {
		t.Errorf("expected a plain count footer, got: %q", last)
	}
	if strings.Contains(last, "--page") || strings.Contains(last, " of ") {
		t.Errorf("a single page should not offer another: %q", last)
	}
}

func TestMailMessagesListFooterMidPagination(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--page-size", "1")
	last := lastNonEmpty(stderr)
	// Either mid-pagination ("Pass --page 1") or last/single-page if the
	// account has ≤ 1 messages. Pin the substring that's present in the
	// common case.
	if !strings.Contains(last, "--page 1") && !strings.Contains(last, "single page") && !strings.Contains(last, "last page") {
		t.Errorf("expected pagination footer, got: %q", last)
	}
}

func TestMailMessagesListJSONPaginationFields(t *testing.T) {
	data := runJSON(t, "mail", "messages", "list", "--page-size", "1")
	for _, key := range []string{"total", "page", "page_size", "has_more", "messages"} {
		if _, ok := data[key]; !ok {
			t.Errorf("expected JSON field %q, got keys: %v", key, keysOf(data))
		}
	}
}

// A filtered listing still pages, so its footer offers the next page rather than
// reporting a total that was never asked for.
func TestMailMessagesListFilteredFooterPages(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "messages", "list",
		"--folder", "all", "--keyword", "proton", "--page-size", "5")
	last := lastNonEmpty(stderr)
	if strings.Contains(last, "page 0") {
		t.Errorf("a footer should never name the page it is on: %q", last)
	}
	// Either there is another page, or the count stands on its own.
	if !strings.HasSuffix(last, "messages.") && !strings.Contains(last, "Next page:") {
		t.Errorf("expected a listing footer, got: %q", last)
	}
}

func TestMailMessagesListEmptyFilterFooter(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--folder", "all",
		"--keyword", "xyz-no-match-"+testID())
	// A search that matched nothing says so. "No messages." would read as an
	// empty mailbox, which is a different and more alarming fact.
	last := lastNonEmpty(stderr)
	if last != "No messages match." {
		t.Errorf("expected 'No messages match.' on an empty search, got: %q", last)
	}
}

func TestMailMessagesListKeyword(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--folder", "all", "--keyword", "proton")
}

func TestMailMessagesListFrom(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--folder", "all", "--from", selfEmail())
}

func TestMailMessagesListDateRange(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--folder", "all", "--after", "2020-01-01", "--before", "2099-12-31")
}

func TestMailMessagesListEmptyFilter(t *testing.T) {
	_, _, code := run(t, "mail", "messages", "list", "--folder", "all", "--keyword", "xyz-nothing-xxxyyy-"+testID())
	if code != 0 {
		t.Fatalf("search with no results should exit 0, got %d", code)
	}
}

func TestMailListFromZeroResultsHint(t *testing.T) {
	needle := "no-such-sender-" + testID()
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--folder", "all", "--from", needle)
	if !strings.Contains(stderr, "--from matches the address only") {
		t.Errorf("expected the --from hint, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--keyword "+needle) {
		t.Errorf("expected stderr to contain '--keyword %s', got: %s", needle, stderr)
	}
}

func TestMailListToZeroResultsHint(t *testing.T) {
	needle := "no-such-rcpt-" + testID()
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--folder", "all", "--to", needle)
	if !strings.Contains(stderr, "--to matches the address only") {
		t.Errorf("expected the --to hint, got: %s", stderr)
	}
	if !strings.Contains(stderr, "--keyword "+needle) {
		t.Errorf("expected stderr to contain '--keyword %s', got: %s", needle, stderr)
	}
}

func TestMailListFromKeywordSuppressesHint(t *testing.T) {
	needle := "impossible-" + testID()
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--folder", "all",
		"--from", needle, "--keyword", "alsoimpossible-"+testID())
	if strings.Contains(stderr, "matches the address only") {
		t.Errorf("hint should be suppressed when --keyword is set; got: %s", stderr)
	}
}

func TestMailListFromHitsNoHint(t *testing.T) {
	plainMail(t) // ensure a delivered self-mail exists and is indexed
	// --from selfEmail() should match. May take a beat to index.
	var stderr string
	for attempt := 0; attempt < 8; attempt++ {
		_, s := runOKStderr(t, "mail", "messages", "list", "--folder", "all",
			"--from", selfEmail(), "--page-size", "5")
		stderr = s
		if !strings.Contains(s, "matches the address only") {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Errorf("hint fired on a successful --from query; stderr: %s", stderr)
}

func TestMailListFromQuietSuppressesHint(t *testing.T) {
	_, stderr := runOKStderr(t, "--quiet", "mail", "messages", "list", "--folder", "all",
		"--from", "no-such-sender-"+testID())
	if strings.Contains(stderr, "matches the address only") {
		t.Errorf("--quiet should suppress the hint; got: %s", stderr)
	}
}

func TestMailMessagesSendAndReadText(t *testing.T) {
	msgID, _, subject := plainMail(t)

	// Default --render text: human-readable, fields on stderr-safe stdout
	stdout := runOK(t, "mail", "messages", "get", "--", msgID)
	assertContains(t, stdout, subject)
	assertContains(t, stdout, selfEmail())
	assertField(t, stdout, "Subject:", subject)
	// Signature: mail we sent ourselves is signed by our own key.
	assertField(t, stdout, "Signature:", "verified")
}

func TestMailMessagesReadByRef(t *testing.T) {
	_, _, subject := plainMail(t)

	// Proton's search index is populated asynchronously, so the message may
	// show up in list (used by sendTestMail) a few seconds before it shows up
	// in the keyword-search endpoint that REF resolution uses. Retry with
	// backoff instead of hard-failing on the first attempt.
	var stdout, lastStderr string
	var lastCode int
	for attempt := 0; attempt < 8; attempt++ {
		out, stderr, code := run(t, "mail", "messages", "get", subject)
		if code == 0 {
			stdout = out
			break
		}
		lastStderr = stderr
		lastCode = code
		time.Sleep(3 * time.Second)
	}
	if stdout == "" {
		t.Fatalf("REF resolution did not index within timeout (exit %d): %s", lastCode, lastStderr)
	}
	assertContains(t, stdout, subject)
}

func TestMailMessagesReadNotFound(t *testing.T) {
	_, _, code := run(t, "mail", "messages", "get", "no-such-msg-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3 (not-found), got %d", code)
	}
}

func TestMailMessagesMarkReadUnread(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "mark", "unread", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--unread", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	found := false
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should be in --unread list after mark unread")
	}

	runOK(t, "mail", "messages", "mark", "read", "--", msgID)
	data = runJSON(t, "mail", "messages", "list", "--unread", "--page-size", "50")
	msgs = data["messages"].([]interface{})
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			t.Error("message should NOT be in --unread list after mark read")
		}
	}
}

func TestMailMessagesStarUnstar(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "star", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--folder", "starred", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	found := false
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should appear in starred folder after star")
	}

	runOK(t, "mail", "messages", "unstar", "--", msgID)
}

func TestMailMessagesMoveDest(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "move", "--into", "archive", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--folder", "archive", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	found := false
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should appear in archive after --dest archive")
	}

	runOK(t, "mail", "messages", "move", "--into", "inbox", "--", msgID)
}

func TestMailMessagesTrash(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "trash", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			t.Error("trashed message should not appear in inbox")
		}
	}
	// put it back so cleanup can delete
	runOK(t, "mail", "messages", "move", "--into", "inbox", "--", msgID)
}

func TestMailBatchTrashDryRunUnread(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "trash", "--unread", "--limit", "5")
	assertContains(t, stderr, "Dry run")
}

func TestMailBatchTrashDryRunOlderThan(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "trash", "--older-than", "365d", "--from", "noreply", "--limit", "5")
	assertContains(t, stderr, "Dry run")
}

func TestMailMessagesReadConvIDRedirects(t *testing.T) {
	_, convID, _ := plainMail(t)

	_, stderr, code := run(t, "mail", "messages", "get", "--", convID)
	if code != 3 {
		t.Errorf("expected exit 3, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "is a conversation, not a message")
	assertContains(t, stderr, "proton mail conversations get")
	assertContains(t, stderr, convID)
}

func TestMailMessagesReadStripQuotesPlaintext(t *testing.T) {
	msgID, _ := quotedMail(t)

	default1 := runOK(t, "mail", "messages", "get", msgID)
	if !strings.Contains(default1, "ancient quoted text") {
		t.Errorf("default mode should preserve the quote; stdout:\n%s", truncateOutput(default1))
	}

	stripped := runOK(t, "mail", "messages", "get", "--strip-quotes", msgID)
	if strings.Contains(stripped, "ancient quoted text") {
		t.Errorf("--strip-quotes should remove the quote; stdout:\n%s", truncateOutput(stripped))
	}
	if !strings.Contains(stripped, "My new note") {
		t.Errorf("--strip-quotes should preserve new content; stdout:\n%s", truncateOutput(stripped))
	}
}

func TestMailMessagesReadStripQuotesNoFalsePositive(t *testing.T) {
	msgID, _, _ := plainMail(t)

	default1 := runOK(t, "mail", "messages", "get", msgID)
	stripped := runOK(t, "mail", "messages", "get", "--strip-quotes", msgID)
	// On a body with no canonical reply marker, --strip-quotes is a no-op.
	if default1 != stripped {
		t.Errorf("--strip-quotes should be a no-op on bodies without quote markers")
	}
}

func TestMailMessagesReadBodyOnly(t *testing.T) {
	msgID, _, subject := plainMail(t)
	stdout := runOK(t, "mail", "messages", "get", "--body-only", msgID)
	for _, marker := range []string{"Subject:", "From:", "To:", "ID:", "---", "Attachments ("} {
		if strings.Contains(stdout, marker) {
			t.Errorf("--body-only output should not contain %q; got:\n%s", marker, truncateOutput(stdout))
		}
	}
	// The body itself contains the subject (sendTestMail's body template).
	if !strings.Contains(stdout, subject) {
		t.Errorf("--body-only stripped the body too aggressively; subject %q missing", subject)
	}
}

func TestMailMessagesReadFormatHTMLNoHeader(t *testing.T) {
	msgID, _, _ := plainMail(t)
	stdout := runOK(t, "mail", "messages", "get", "--render", "html", msgID)
	if strings.HasPrefix(strings.TrimSpace(stdout), "Subject:") {
		t.Errorf("--render html must not start with 'Subject:' header; got:\n%s", truncateOutput(stdout))
	}
	for _, marker := range []string{"\nSubject: ", "\nFrom:    ", "\nTo:      ", "\nID:      "} {
		if strings.Contains(stdout, marker) {
			t.Errorf("--render html output should not contain header marker %q", marker)
		}
	}
}

func TestMailMessagesReadFormatRawNoHeader(t *testing.T) {
	msgID, _, _ := plainMail(t)
	stdout := runOK(t, "mail", "messages", "get", "--render", "raw", msgID)
	if strings.HasPrefix(strings.TrimSpace(stdout), "Subject:") {
		t.Errorf("--render raw must not start with 'Subject:' header; got:\n%s", truncateOutput(stdout))
	}
}

func TestMailMessagesReadDefaultStillHasHeader(t *testing.T) {
	msgID, _, _ := plainMail(t)
	stdout := runOK(t, "mail", "messages", "get", msgID)
	assertContains(t, stdout, "Subject:")
	assertContains(t, stdout, "From:")
}

func TestMailMessagesReadShowsAttachments(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)
	stdout := runOK(t, "mail", "messages", "get", msgID)
	assertContains(t, stdout, "Attachments")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "SIZE")
}

func TestMailMessagesReadNoAttachmentsNoFooter(t *testing.T) {
	msgID, _, _ := plainMail(t)
	stdout := runOK(t, "mail", "messages", "get", msgID)
	if strings.Contains(stdout, "---") {
		t.Errorf("unexpected '---' separator on no-attachments message:\n%s", truncateOutput(stdout))
	}
	if strings.Contains(stdout, "Attachments (") {
		t.Errorf("unexpected attachments footer on no-attachments message:\n%s", truncateOutput(stdout))
	}
}

func TestMailMessagesReadFormatHTMLNoFooter(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)
	stdout := runOK(t, "mail", "messages", "get", "--render", "html", msgID)
	if strings.Contains(stdout, "Attachments (") {
		t.Errorf("--render html must not append the footer:\n%s", truncateOutput(stdout))
	}
}

func TestMailMessagesReadFormatRawNoFooter(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)
	stdout := runOK(t, "mail", "messages", "get", "--render", "raw", msgID)
	if strings.Contains(stdout, "Attachments (") {
		t.Errorf("--render raw must not append the footer:\n%s", truncateOutput(stdout))
	}
}

func TestMailMessagesReadIncludeInlineTags(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)

	// The attachments trailer is the same table the list verb draws, so an inline
	// attachment shows up as a DISPOSITION column rather than a marker.
	default1 := runOK(t, "mail", "messages", "get", msgID)
	if strings.Contains(default1, "DISPOSITION") {
		t.Errorf("default footer should not show the DISPOSITION column:\n%s", truncateOutput(default1))
	}

	incl := runOK(t, "mail", "messages", "get", "--include-inline", msgID)
	if !strings.Contains(incl, "DISPOSITION") || !strings.Contains(incl, "inline") {
		t.Errorf("--include-inline footer should show an inline attachment:\n%s", truncateOutput(incl))
	}
}

// Emptying a folder is not a filtered delete: nothing is enumerated, Proton
// clears it, and that is why it always asks.
func TestMailMessagesEmptyClearsAFolder(t *testing.T) {
	subject := testID() + "-empty"
	msgID := sendTestMail(t, subject)
	if !waitFor(60*time.Second, 3*time.Second, func() bool {
		return messageIDInFolder("inbox", subject) != ""
	}) {
		t.Fatalf("the message %q never arrived, so there was nothing to empty", subject)
	}
	inbox := messageIDInFolder("inbox", subject)
	runOK(t, "mail", "messages", "trash", "--", inbox)
	_ = msgID

	if !waitFor(30*time.Second, 2*time.Second, func() bool {
		return messageIDInFolder("trash", subject) != ""
	}) {
		t.Fatalf("the message %q never reached the trash", subject)
	}

	// Another test moving something into the trash locks the label, and Proton
	// says so rather than queueing, so this waits for it to be free.
	runOKUntilFree(t, "mail", "messages", "empty", "--folder", "trash", "--yes")

	if !waitFor(60*time.Second, 3*time.Second, func() bool {
		return messageIDInFolder("trash", subject) == ""
	}) {
		t.Error("after emptying, the trash should not hold the message")
	}
}
