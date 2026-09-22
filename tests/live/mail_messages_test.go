package live

import (
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
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
	data := runJSON(t, "mail", "messages", "list", "--limit", "1")
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
	data := runJSON(t, "mail", "messages", "list", "--limit", "3")
	msgs := data["messages"].([]interface{})
	if len(msgs) > 3 {
		t.Errorf("expected at most 3 messages, got %d", len(msgs))
	}
}

func TestMailMessagesListUnreadFlag(t *testing.T) {
	runOK(t, "mail", "messages", "list", "--unread")
}

// A page size of zero is the whole folder in one answer, whatever it costs
// underneath. Nothing is left to page towards, so there is no page to report.
func TestMailMessagesListWholeCollection(t *testing.T) {
	data := runJSON(t, "mail", "messages", "list", "--folder", "all", "--limit", "0")
	msgs := data["messages"].([]interface{})
	if total, ok := data["total"].(float64); !ok || int(total) != len(msgs) {
		t.Errorf("total = %v with %d messages shown; the whole collection was asked for",
			data["total"], len(msgs))
	}
	for _, key := range []string{"page", "page_size", "has_more"} {
		if _, has := data[key]; has {
			t.Errorf("an unpaged listing reports %q", key)
		}
	}
}

// A page wider than Proton serves at once is composed from as many of its pages
// as it takes and cut to the number that was asked for, so --limit is the
// reader's number rather than the endpoint's.
func TestMailMessagesListWiderThanAPage(t *testing.T) {
	const want = 160 // one row past Proton's own page
	data := runJSON(t, "mail", "messages", "list", "--folder", "all", "--limit", "160")
	msgs := data["messages"].([]interface{})
	total, ok := data["total"].(float64)
	if !ok {
		t.Fatalf("no total in the answer: %v", keysOf(data))
	}
	if expected := min(want, int(total)); len(msgs) != expected {
		t.Errorf("got %d messages of %d in the folder, want %d", len(msgs), int(total), expected)
	}
	if int(total) <= 150 {
		t.Logf("the folder holds %d messages, so no page boundary was crossed", int(total))
	}
}

func TestMailMessagesListFooterSinglePage(t *testing.T) {
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--limit", "150")
	last := footerOf(stderr)
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
	_, stderr := runOKStderr(t, "mail", "messages", "list", "--limit", "1")
	last := footerOf(stderr)
	// Either mid-pagination ("Pass --page 1") or last/single-page if the
	// account has ≤ 1 messages. Pin the substring that's present in the
	// common case.
	if !strings.Contains(last, "--page 1") && !strings.Contains(last, "single page") && !strings.Contains(last, "last page") {
		t.Errorf("expected pagination footer, got: %q", last)
	}
}

func TestMailMessagesListJSONPaginationFields(t *testing.T) {
	data := runJSON(t, "mail", "messages", "list", "--limit", "1")
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
		"--folder", "all", "--keyword", "proton", "--limit", "5")
	last := footerOf(stderr)
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
	//
	// The footer is not the last line here: what the search did not look inside
	// is said after it, about it.
	if !strings.Contains(stderr, "No messages match.") {
		t.Errorf("expected 'No messages match.' on an empty search, got: %q", stderr)
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
			"--from", selfEmail(), "--limit", "5")
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
	data := runJSON(t, "mail", "messages", "list", "--unread", "--limit", "50")
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
	data = runJSON(t, "mail", "messages", "list", "--unread", "--limit", "50")
	msgs = data["messages"].([]interface{})
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			t.Error("message should NOT be in --unread list after mark read")
		}
	}
}

// Marking a message legitimate is the reader overruling Proton's own verdict, so
// what proves it is the verdict reading back off the message afterwards.
//
// The fixture is not a message Proton flagged - no account holds one to order -
// so what this establishes is that the endpoint takes a message the CLI names and
// that the flag it sets is the one `get` reports. The screen a real verdict
// produces is the half no test account can show.
func TestMailMessagesMarkLegitimate(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "mark", "legitimate", "--", msgID)

	msg := runJSON(t, "mail", "messages", "get", "--output", "json", "--", msgID)
	if legitimate, _ := msg["marked_legitimate"].(bool); !legitimate {
		t.Errorf("marked_legitimate = %v, want true after mark legitimate", msg["marked_legitimate"])
	}
}

// --starred is a filter, not a folder, so it narrows whatever else was asked
// for - and every row it answers with carries the star.
//
// The second half is what makes this worth a live test: a predicate the server
// quietly dropped would answer with the whole folder, and every row of it would
// look like a match.
func TestMailMessagesListStarredFilter(t *testing.T) {
	msgID := mutableMail(t)
	runOK(t, "mail", "messages", "star", "--", msgID)
	cleanupRun(t, "Unstar: proton mail messages unstar "+msgID,
		"mail", "messages", "unstar", "--", msgID)

	data := runJSON(t, "mail", "messages", "list", "--starred", "--folder", "all", "--limit", "50")
	msgs, _ := data["messages"].([]interface{})
	found := false
	for _, row := range msgs {
		m := row.(map[string]interface{})
		if m["id"] == msgID {
			found = true
		}
		if !starred(m) {
			t.Errorf("--starred answered with %v, which carries no star", m["id"])
		}
	}
	if !found {
		t.Error("--starred did not answer with the message that was just starred")
	}
}

func starred(m map[string]interface{}) bool {
	labels, _ := m["labels"].([]interface{})
	for _, label := range labels {
		if label == "10" {
			return true
		}
	}
	return false
}

// A folder of your own is named the way it is named on screen, and the listing
// answers for that folder rather than for a label nobody has.
func TestMailMessagesListACustomFolderByName(t *testing.T) {
	name, _ := pinned(t, account.Primary, "folder", "Projects")["name"].(string)
	if name == "" {
		t.Fatal("the folder fixture has no name")
	}
	msgID := mutableMail(t)
	runOK(t, "mail", "messages", "move", "--into", name, "--", msgID)
	cleanupRun(t, "Put the message back: proton mail messages move --into inbox "+msgID,
		"mail", "messages", "move", "--into", "inbox", "--", msgID)

	data := runJSON(t, "mail", "messages", "list", "--folder", name, "--limit", "50")
	msgs, _ := data["messages"].([]interface{})
	for _, row := range msgs {
		if row.(map[string]interface{})["id"] == msgID {
			return
		}
	}
	t.Errorf("--folder %q did not answer with the message moved into it", name)
}

func TestMailMessagesStarUnstar(t *testing.T) {
	msgID := mutableMail(t)

	runOK(t, "mail", "messages", "star", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--folder", "starred", "--limit", "50")
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
	data := runJSON(t, "mail", "messages", "list", "--folder", "archive", "--limit", "50")
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
	data := runJSON(t, "mail", "messages", "list", "--limit", "50")
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

// A selection that fills its cap says so, and --limit 0 lifts the cap: the
// preview then holds everything the filter matched rather than one page of it.
//
// Nothing is acted on - the point is what a selection reports about its own
// completeness, which only a dry run can be asked without consequences.
func TestMailMessagesTrashLimitZeroWalks(t *testing.T) {
	capped := runJSON(t, "--dry-run", "mail", "messages", "trash",
		"--folder", "all", "--limit", "2", "--output", "json")
	uncapped := runJSON(t, "--dry-run", "mail", "messages", "trash",
		"--folder", "all", "--limit", "0", "--output", "json")

	few, all := countOf(t, capped), countOf(t, uncapped)
	if few > 2 {
		t.Errorf("--limit 2 selected %d messages", few)
	}
	if all < few {
		t.Errorf("--limit 0 selected %d messages, fewer than the %d a cap of 2 found", all, few)
	}
	if all <= 2 {
		t.Logf("the account holds %d messages, so no cap was reached", all)
	}
}

// countOf reads the count out of a mutation's own answer.
func countOf(t *testing.T, data map[string]interface{}) int {
	t.Helper()
	count, ok := data["count"].(float64)
	if !ok {
		t.Fatalf("no count in the answer: %v", keysOf(data))
	}
	return int(count)
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

// A read receipt has two ends, and one account cannot play both: the asking is a
// flag on the message that goes out, and the answering is a header on the copy
// that arrives. So this is the cross-account one - the primary asks, the
// secondary is told it was asked, answers, and is refused a second answer.
//
// The receipt itself arrives as ordinary mail on the asking account, which is
// why the primary's inbox is swept of it afterwards.
func TestMailMessagesReadReceipt(t *testing.T) {
	subject := testID() + "-receipt"
	body := "read receipt requested for " + subject
	runOK(t, "mail", "messages", "send", "--to", secondaryEmail(),
		"--subject", subject, "--body", body, "--request-receipt")
	cleanupRun(t, "Delete the read receipt: proton mail messages delete --folder inbox --keyword "+
		subject+" --all --yes",
		"mail", "messages", "delete", "--folder", "inbox", "--keyword", subject, "--all")

	sentID := findMessage(t, "sent", subject)
	if sentID == "" {
		t.Fatal("the message that asked for a receipt never reached the sent folder")
	}
	cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
		"mail", "messages", "delete", "--", sentID)

	// The copy you sent carries the request you made, and there is nothing on it
	// to answer.
	sent := runJSON(t, "mail", "messages", "get", "--output", "json", "--", sentID)
	if requested, _ := sent["receipt_requested"].(bool); !requested {
		t.Errorf("receipt_requested = %v on the message you sent, want true", sent["receipt_requested"])
	}
	if due, _ := sent["receipt_due"].(bool); due {
		t.Error("receipt_due is true on a message you sent, which has nobody to answer")
	}
	if _, _, code := run(t, "mail", "messages", "receipt", sentID); code != 3 {
		t.Errorf("answering your own message exited %d, want 3", code)
	}

	var recvID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		recvID = secondaryMailContaining(t, selfEmail(), body)
		return recvID != ""
	})
	if recvID == "" {
		t.Fatal("the second account did not receive the message that asked for a receipt")
	}
	cleanupRunSecondary(t, "Delete received mail (secondary): proton --profile secondary mail messages delete "+recvID,
		"mail", "messages", "delete", "--", recvID)

	read := runOKSecondary(t, "mail", "messages", "get", "--", recvID)
	assertField(t, read, "Receipt:", "requested")
	received := runJSONSecondary(t, "mail", "messages", "get", "--output", "json", "--", recvID)
	if due, _ := received["receipt_due"].(bool); !due {
		t.Errorf("receipt_due = %v on a message that asked, want true", received["receipt_due"])
	}

	runOKSecondary(t, "mail", "messages", "receipt", "--", recvID)

	answered := runJSONSecondary(t, "mail", "messages", "get", "--output", "json", "--", recvID)
	if sent, _ := answered["receipt_sent"].(bool); !sent {
		t.Errorf("receipt_sent = %v after sending one, want true", answered["receipt_sent"])
	}
	if _, _, code := runSecondary(t, "mail", "messages", "receipt", recvID); code != 3 {
		t.Errorf("a second receipt for the same message exited %d, want 3", code)
	}
}

// A message nobody asked about is refused before anything is sent, which is what
// keeps the verb from telling a sender something they never asked to know.
func TestMailMessagesReceiptRefusesAMessageThatDidNotAsk(t *testing.T) {
	msgID, _, _ := plainMail(t)
	_, stderr, code := run(t, "mail", "messages", "receipt", "--", msgID)
	if code != 3 {
		t.Errorf("exit %d, want 3 for a message that asked for no receipt: %s", code, stderr)
	}
	assertContains(t, stderr, "did not ask for a read receipt")
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

	runOK(t, "mail", "messages", "empty", "--folder", "trash", "--yes")

	if !waitFor(60*time.Second, 3*time.Second, func() bool {
		return messageIDInFolder("trash", subject) == ""
	}) {
		t.Error("after emptying, the trash should not hold the message")
	}
}
