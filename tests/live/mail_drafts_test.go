package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Composing: a draft, a reply, a forward, and every shape of send.
//
// They all go through one path, so these cover it end to end. Each distinct send
// shape keeps exactly one test, because that is what makes a change to Proton's
// send path fail something - and because the free plan meters what a run may
// send, which is what decides how often the suite can run at all.

func TestMailDraftsLifecycle(t *testing.T) {
	subject := testID() + "-draft"

	stdout := runOK(t, "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject, "--body", "first version")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("drafts create should print a bare ID, got %q", stdout)
	}
	cleanupRun(t, "Delete draft: proton mail drafts delete "+id,
		"mail", "messages", "delete", "--", id)

	// It shows up as a draft, listed by the dedicated command.
	list := runJSON(t, "mail", "drafts", "list", "--page-size", "50")
	drafts, _ := list["drafts"].([]interface{})
	found := false
	for _, d := range drafts {
		if d.(map[string]interface{})["id"] == id {
			found = true
		}
	}
	if !found {
		t.Error("the new draft did not appear in `mail drafts list`")
	}

	// Editing replaces only what was passed.
	runOK(t, "mail", "drafts", "update", "--body", "second version", "--", id)
	read := runOK(t, "mail", "messages", "get", "--", id)
	assertContains(t, read, "second version")
	assertNotContains(t, read, "first version")
	assertField(t, read, "Subject:", subject)

	runOK(t, "mail", "drafts", "update", "--subject", subject+"-renamed", "--", id)
	read = runOK(t, "mail", "messages", "get", "--", id)
	assertContains(t, read, subject+"-renamed")
	assertContains(t, read, "second version")

	// Sending it delivers the stored body.
	runOK(t, "mail", "drafts", "send", "--", id)
	inboxID := findMessage(t, "inbox", subject+"-renamed")
	if inboxID == "" {
		t.Fatal("the sent draft never arrived")
	}
	cleanupRun(t, "Delete inbox copy: proton mail messages delete "+inboxID,
		"mail", "messages", "delete", "--", inboxID)
	assertContains(t, runOK(t, "mail", "messages", "get", "--", inboxID), "second version")
}

func TestMailDraftsCreateDryRunCreatesNothing(t *testing.T) {
	subject := testID() + "-draft-dry"
	_, stderr := runOKStderr(t, "--dry-run", "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject, "--body", "x")
	assertContains(t, stderr, "Dry run")
	if messageIDInFolder("drafts", subject) != "" {
		t.Error("--dry-run created a draft")
	}
}

func TestMailDraftsAttachAndDetach(t *testing.T) {
	subject := testID() + "-draft-attach"
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(path, []byte("draft attachment"), 0o600); err != nil {
		t.Fatal(err)
	}

	id := strings.TrimSpace(runOK(t, "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject, "--body", "see attached", "--attach", path))
	cleanupRun(t, "Delete draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	assertContains(t, runOK(t, "mail", "messages", "attachments", "list", id), "note.txt")

	// --detach takes the file name, not just an ID.
	runOK(t, "mail", "drafts", "update", "--detach", "note.txt", "--", id)
	assertNotContains(t, runOK(t, "mail", "messages", "attachments", "list", id), "note.txt")
}

func TestMailDraftsEditRejectsASentMessage(t *testing.T) {
	_, _, subject := plainMail(t)
	// REF resolution for drafts is scoped to the Drafts folder, so a sent
	// message's subject must not resolve.
	_, stderr, code := run(t, "mail", "drafts", "update", "--body", "nope", subject)
	// Exit 4 means something in Drafts answered to this subject, and which
	// messages those were is the whole story - stderr names them.
	if code != 3 {
		t.Errorf("expected exit 3 for a subject that is not a draft, got %d\nstderr: %s",
			code, truncateOutput(stderr))
	}
}

func TestMailDraftsSendRequiresARecipient(t *testing.T) {
	subject := testID() + "-draft-norecipient"
	id := strings.TrimSpace(runOK(t, "mail", "drafts", "create", "--subject", subject, "--body", "x"))
	cleanupRun(t, "Delete draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	_, stderr, code := run(t, "mail", "drafts", "send", "--", id)
	if code == 0 {
		t.Error("sending a draft with no recipients should fail")
	}
	assertContains(t, stderr, "no recipients")
}

func TestMailMessagesReply(t *testing.T) {
	subject := testID() + "-reply-parent"
	parentID := sendTestMail(t, subject)

	stdout := runOK(t, "mail", "messages", "reply", "--body", "My answer here.", "--", parentID)
	replyID := strings.TrimSpace(stdout)
	if !looksLikeID(replyID) {
		t.Fatalf("reply should print a bare ID, got %q", stdout)
	}
	cleanupRun(t, "Delete reply: proton mail messages delete "+replyID,
		"mail", "messages", "delete", "--", replyID)

	body := runOK(t, "mail", "messages", "get", "--", replyID)
	assertField(t, body, "Subject:", "Re: "+subject)
	assertContains(t, body, "My answer here.")
	assertContains(t, body, "wrote:")
	assertContains(t, body, "> Integration test body")

	// --strip-quotes removes exactly the quote we just wrote.
	stripped := runOK(t, "mail", "messages", "get", "--strip-quotes", "--", replyID)
	assertContains(t, stripped, "My answer here.")
	assertNotContains(t, stripped, "> Integration test body")

	// The parent is flagged as replied to, which is what ParentID + Action buy.
	data := runJSON(t, "api", "GET", "/mail/v4/messages/"+parentID)
	msg, _ := data["Message"].(map[string]interface{})
	if replied, _ := msg["IsReplied"].(float64); replied != 1 {
		t.Errorf("parent IsReplied = %v, want 1", msg["IsReplied"])
	}

	// The inbox copy needs cleaning up too.
	if inbox := findMessage(t, "inbox", "Re: "+subject); inbox != "" && inbox != replyID {
		cleanupRun(t, "Delete reply inbox copy: proton mail messages delete "+inbox,
			"mail", "messages", "delete", "--", inbox)
	}
}

func TestMailMessagesReplyNoQuote(t *testing.T) {
	msgID, _, _ := plainMail(t)
	id := strings.TrimSpace(runOK(t, "mail", "messages", "reply",
		"--body", "Terse.", "--no-quote", "--", msgID))
	cleanupRun(t, "Delete reply: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	body := runOK(t, "mail", "messages", "get", "--", id)
	assertContains(t, body, "Terse.")
	assertNotContains(t, body, "wrote:")
}

func TestMailMessagesReplyAsDraft(t *testing.T) {
	msgID, _, subject := plainMail(t)
	stdout, stderr := runOKStderr(t, "mail", "messages", "reply",
		"--body", "Later.", "--draft", "--", msgID)
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("--draft should print the draft ID, got %q", stdout)
	}
	cleanupRun(t, "Delete reply draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	assertContains(t, stderr, "draft")

	// It is a draft, not a sent message.
	if messageIDInFolder("drafts", "Re: "+subject) == "" {
		t.Error("--draft did not leave the reply in Drafts")
	}
	assertContains(t, runOK(t, "mail", "messages", "get", "--", id), "Later.")
}

func TestMailMessagesReplyDryRun(t *testing.T) {
	msgID, _, _ := plainMail(t)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "reply",
		"--body", "x", "--", msgID)
	// A reply is a send, so it reports as one - naming the message it would put
	// on the wire rather than the verb that composed it.
	assertContains(t, stderr, "would send message")
	assertContains(t, stderr, "Re: ")
}

func TestMailMessagesForwardCarriesAttachmentsToTheAltAccount(t *testing.T) {
	msgID, _, _, attName := attachedMail(t)
	marker := testID() + "-forwarded"

	fwdID := strings.TrimSpace(runOK(t, "mail", "messages", "forward",
		"--to", secondaryEmail(), "--body", marker, "--", msgID))
	cleanupRun(t, "Delete forward: proton mail messages delete "+fwdID,
		"mail", "messages", "delete", "--", fwdID)

	// The sent copy carries the forwarded headers and the original's attachment.
	sent := runOK(t, "mail", "messages", "get", "--", fwdID)
	assertContains(t, sent, marker)
	assertContains(t, sent, "Forwarded Message")
	assertContains(t, sent, attName)

	// The second account receives it, attachment and all.
	var recvID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		recvID = secondaryMailContaining(t, selfEmail(), marker)
		return recvID != ""
	})
	if recvID == "" {
		t.Fatal("the second account never received the forward")
	}
	cleanupRunSecondary(t, "Delete received forward (secondary): proton --profile secondary mail messages delete "+recvID,
		"mail", "messages", "delete", recvID)

	atts := runOKSecondary(t, "mail", "messages", "attachments", "list", recvID)
	assertContains(t, atts, attName)

	// And the bytes survived the re-keying rather than the upload.
	dir := t.TempDir()
	runOKSecondary(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, recvID)
	got, err := os.ReadFile(filepath.Join(dir, attName))
	if err != nil {
		t.Fatalf("read forwarded attachment: %v", err)
	}
	if len(got) == 0 {
		t.Error("the forwarded attachment arrived empty")
	}
}

func TestMailMessagesForwardWithoutAttachments(t *testing.T) {
	msgID, _, _, attName := attachedMail(t)
	subject := testID() + "-fwd-noatt"

	id := strings.TrimSpace(runOK(t, "mail", "messages", "forward",
		"--to", selfEmail(), "--body", subject, "--no-attachments", "--", msgID))
	cleanupRun(t, "Delete forward: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	for _, row := range runJSONArray(t, "mail", "messages", "attachments", "list", id) {
		a, _ := row.(map[string]interface{})
		if n, _ := a["name"].(string); n == attName {
			t.Errorf("--no-attachments still carried %s", attName)
		}
	}
}

func TestMailMessagesForwardRequiresTo(t *testing.T) {
	msgID, _, _ := plainMail(t)
	_, stderr, code := run(t, "mail", "messages", "forward", "--body", "x", "--", msgID)
	if code == 0 {
		t.Error("forwarding without --to should fail")
	}
	assertContains(t, stderr, "--to is required")
}

func TestMailSendFromRejectsAnAddressYouDoNotOwn(t *testing.T) {
	_, stderr, code := run(t, "mail", "messages", "send",
		"--from", "someone@not-your-account.invalid",
		"--to", selfEmail(), "--subject", testID(), "--body", "x")
	if code != 3 {
		t.Errorf("expected exit 3 for an unknown --from, got %d", code)
	}
	assertContains(t, stderr, "can send mail")
	assertContains(t, stderr, selfEmail())
}

func TestMailSendFromAcceptsYourOwnAddress(t *testing.T) {
	subject := testID() + "-from"
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "send",
		"--from", selfEmail(), "--to", selfEmail(), "--subject", subject, "--body", "x")
	assertContains(t, stderr, "would send message")
	assertContains(t, stderr, subject)
}

// Signatures are applied automatically, matching the web client, so a message
// composed with one set carries it and --no-signature suppresses it.
func TestMailSignatureIsAppliedAndSuppressible(t *testing.T) {
	addrID := primaryAddressID(t)
	original := addressSignature(t, addrID)
	marker := testID() + "-sig"
	runOK(t, "mail", "settings", "addresses", "update", "--signature", marker, "--", addrID)
	cleanup(t, "Restore the address signature", func() error {
		args := []string{"mail", "settings", "addresses", "update", "--html", "--signature", original, "--", addrID}
		if strings.TrimSpace(original) == "" {
			args = []string{"mail", "settings", "addresses", "update", "--clear-signature", "--", addrID}
		}
		_, stderr, code := run(t, args...)
		if code != 0 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})

	subject := testID() + "-sig-send"
	id := strings.TrimSpace(runOK(t, "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject, "--body", "Body text."))
	cleanupRun(t, "Delete draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	assertContains(t, runOK(t, "mail", "messages", "get", "--", id), marker)

	bare := strings.TrimSpace(runOK(t, "mail", "drafts", "create",
		"--to", selfEmail(), "--subject", subject+"-bare", "--body", "Body text.", "--no-signature"))
	cleanupRun(t, "Delete draft: proton mail messages delete "+bare,
		"mail", "messages", "delete", "--", bare)
	assertNotContains(t, runOK(t, "mail", "messages", "get", "--", bare), marker)
}

// An inline image is a distinct thing to put on the wire: it is embedded in the
// HTML body by Content-ID, which is what makes Proton record the part as inline
// rather than as an attachment. The tests that tell the two dispositions apart
// read a seeded message, so the sending of one is asserted here.
func TestMailSendWithInlineImage(t *testing.T) {
	subject := testID() + "-inline"
	dir := t.TempDir()
	img := filepath.Join(dir, "pixel.png")
	writePNG(t, img)
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("regular attachment"), 0o600); err != nil {
		t.Fatal(err)
	}

	id := strings.TrimSpace(runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--html", "--body", "<p>see below</p>",
		"--attach", note, "--attach-inline", img))
	cleanupRun(t, "Delete sent mail: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	if inbox := findMessage(t, "inbox", subject); inbox != "" {
		cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inbox,
			"mail", "messages", "delete", "--", inbox)
	}

	// The image is recorded as inline and the note as an attachment, so the
	// default listing shows one of the two and --include-inline both.
	regular := runJSONArray(t, "mail", "messages", "attachments", "list", id)
	if len(regular) != 1 {
		t.Fatalf("the default listing shows %d attachments, want only the regular one", len(regular))
	}
	if name, _ := regular[0].(map[string]interface{})["name"].(string); name != "note.txt" {
		t.Errorf("the regular attachment is %q, want note.txt", name)
	}
	dispositions := map[string]bool{}
	for _, row := range runJSONArray(t, "mail", "messages", "attachments", "list", "--include-inline", id) {
		if d, ok := row.(map[string]interface{})["disposition"].(string); ok {
			dispositions[d] = true
		}
	}
	if !dispositions["inline"] || !dispositions["attachment"] {
		t.Errorf("the message carries dispositions %v, want both inline and attachment", dispositions)
	}
}

func TestMailSendWithAttachment(t *testing.T) {
	subject := testID() + "-attach"
	dir := t.TempDir()
	path := filepath.Join(dir, "note.txt")
	content := "attachment body for " + subject
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "see attached", "--attach", path)

	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, fmt.Sprintf("Delete sent mail: proton mail messages delete %s", sentID),
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatal("attachment mail did not arrive in inbox")
	}
	cleanupRun(t, fmt.Sprintf("Delete inbox mail: proton mail messages delete %s", inboxID),
		"mail", "messages", "delete", "--", inboxID)

	// The attachment must be listed and decrypt back to the original bytes.
	atts := runOK(t, "mail", "messages", "attachments", "list", inboxID)
	assertContains(t, atts, "note.txt")

	dlDir := filepath.Join(dir, "dl")
	if err := os.MkdirAll(dlDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dlDir, inboxID)
	got, err := os.ReadFile(filepath.Join(dlDir, "note.txt"))
	if err != nil {
		t.Fatalf("read downloaded attachment: %v", err)
	}
	if string(got) != content {
		t.Errorf("attachment round-trip mismatch: got %q want %q", got, content)
	}
}

func TestMailSendHTMLSetsHTMLMimeType(t *testing.T) {
	subject := testID() + "-html"
	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "<p>Hello <b>world</b></p>", "--html")

	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, fmt.Sprintf("Delete sent mail: proton mail messages delete %s", sentID),
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatal("HTML mail did not arrive in inbox")
	}
	cleanupRun(t, fmt.Sprintf("Delete inbox mail: proton mail messages delete %s", inboxID),
		"mail", "messages", "delete", "--", inboxID)

	data := runJSON(t, "api", "GET", "/mail/v4/messages/"+inboxID)
	msg, ok := data["Message"].(map[string]interface{})
	if !ok {
		t.Fatalf("no Message in response: %v", data)
	}
	if mt, _ := msg["MIMEType"].(string); mt != "text/html" {
		t.Errorf("received MIMEType = %q, want text/html", mt)
	}
}

func TestMailSendPrintsMessageID(t *testing.T) {
	subject := testID() + "-sendid"
	stdout, stderr := runOKStderr(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", "id on stdout")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("send stdout should be a bare message ID, got %q", stdout)
	}
	cleanupRun(t, "Delete sent mail: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	assertContains(t, stderr, "Sent")
	// The returned ID resolves via read.
	assertContains(t, runOK(t, "mail", "messages", "get", "--", id), subject)
	// Clean the inbox copy too (distinct ID).
	if inbox := findMessage(t, "inbox", subject); inbox != "" && inbox != id {
		cleanupRun(t, "Delete inbox mail: proton mail messages delete "+inbox,
			"mail", "messages", "delete", "--", inbox)
	}
}

// A scheduled send is one behaviour, so it is one test: the message is queued
// rather than sent, it answers with the ID it was queued under, it carries the
// delivery time it was given, and it can be taken back out of the queue.
//
// Sending it once rather than three times is not only faster - a free plan meters
// what the suite may send, and that is what decides how often it can be run.
func TestMailMessagesScheduledSendAndUnschedule(t *testing.T) {
	subject := testID() + "-scheduled"
	sendAt := time.Now().Add(3 * time.Hour)
	stdout, stderr := runOKStderr(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "scheduled via --send-at", "--send-at", sendAt.Format("2006-01-02T15:04"))

	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("a scheduled send should print a bare message ID, got %q", stdout)
	}
	// cancel_send keeps the same message ID, so this covers it in either state.
	cleanupRun(t, "Delete scheduled mail: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)
	assertContains(t, stderr, "Scheduled")

	// It is queued rather than sent, under exactly the ID that was reported.
	var schedID string
	waitFor(30*time.Second, 1*time.Second, func() bool {
		schedID = messageIDInFolder("scheduled", subject)
		return schedID != ""
	})
	if schedID != id {
		t.Errorf("the scheduled folder holds %q, want the ID the send reported, %q", schedID, id)
	}

	// It carries its future delivery time; an immediate send would be about now.
	data := runJSON(t, "api", "GET", "/mail/v4/messages/"+id)
	msg, _ := data["Message"].(map[string]interface{})
	msgTime, _ := msg["Time"].(float64)
	if int64(msgTime) <= time.Now().Add(time.Hour).Unix() {
		t.Errorf("scheduled Time = %d, want a delivery time near %d", int64(msgTime), sendAt.Unix())
	}

	// A dry run does not take it out of the queue.
	_, stderr = runOKStderr(t, "--dry-run", "mail", "messages", "unschedule", "--", id)
	assertContains(t, stderr, "Dry run")
	if messageIDInFolder("scheduled", subject) == "" {
		t.Error("--dry-run unscheduled the message")
	}

	// Taking it back out returns it to Drafts.
	runOK(t, "mail", "messages", "unschedule", "--", id)
	var draftID string
	waitFor(30*time.Second, 1*time.Second, func() bool {
		draftID = messageIDInFolder("drafts", subject)
		return draftID != ""
	})
	if draftID == "" {
		t.Error("an unscheduled message should appear in Drafts")
	}
	if messageIDInFolder("scheduled", subject) != "" {
		t.Error("the message is still in Scheduled after being unscheduled")
	}
}

func TestMailSendScheduledDryRunEchoesTime(t *testing.T) {
	subject := testID() + "-scheddry"
	sendAt := time.Now().Add(5 * time.Hour)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "x", "--send-at", sendAt.Format("2006-01-02T15:04"))
	assertContains(t, stderr, "would schedule")
	assertContains(t, stderr, sendAt.Format("2006-01-02"))
	if messageIDInFolder("scheduled", subject) != "" {
		t.Error("dry-run should not create a scheduled message")
	}
}

func TestMailMessagesUnscheduleByAllDryRun(t *testing.T) {
	// --all with --dry-run is safe: it previews without touching the queue.
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "unschedule", "--all")
	assertContains(t, stderr, "Dry run")
}

func TestMailSendExpiringHasExpirationTime(t *testing.T) {
	subject := testID() + "-expires"
	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "self-destructs via --expires", "--expires", "1d")

	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, fmt.Sprintf("Delete sent mail: proton mail messages delete %s", sentID),
			"mail", "messages", "delete", "--", sentID)
	}
	inboxID := findMessage(t, "inbox", subject)
	if inboxID == "" {
		t.Fatal("expiring message not delivered")
	}
	cleanupRun(t, fmt.Sprintf("Delete inbox mail: proton mail messages delete %s", inboxID),
		"mail", "messages", "delete", "--", inboxID)

	data := runJSON(t, "api", "GET", "/mail/v4/messages/"+inboxID)
	msg, _ := data["Message"].(map[string]interface{})
	exp, _ := msg["ExpirationTime"].(float64)
	now := time.Now().Unix()
	if int64(exp) <= now {
		t.Errorf("ExpirationTime = %d, expected a future expiry near %d", int64(exp), now+86400)
	}
}

// eoPasswordFile is the password a recipient outside Proton types, delivered the
// way every secret is: a file only its owner can read, never a flag value.
func eoPasswordFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "eo-password")
	if err := os.WriteFile(path, []byte("hunter2hunter2"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMailSendEncryptedForOutsideDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "send",
		"--to", externalRecipient(t), "--subject", testID()+"-eo-dry",
		"--body", "secret", "--eo-password-file", eoPasswordFile(t), "--eo-password-hint", "the usual")
	assertContains(t, stderr, "Dry run")
}

// TestMailSendEncryptedForOutside exercises the encrypted-for-outside (password)
// send path end to end. It delivers to a real external (non-Proton) mailbox -
// an external mailbox, per tests/AGENTS.md - so a non-zero exit means either an
// EO-packaging regression or a server-side address policy. (Sending to a fake
// @example.com address instead would bounce with a MAILER-DAEMON return.)
func TestMailSendEncryptedForOutside(t *testing.T) {
	subject := testID() + "-eo-real"

	runOK(t, "mail", "messages", "send",
		"--to", externalRecipient(t), "--subject", subject, "--body", "encrypted outside body",
		"--eo-password-file", eoPasswordFile(t), "--eo-password-hint", "the usual")

	sentID := findMessage(t, "sent", subject)
	if sentID == "" {
		t.Fatal("EO message did not appear in Sent")
	}
	cleanupRun(t, fmt.Sprintf("Delete sent EO mail: proton mail messages delete %s", sentID),
		"mail", "messages", "delete", "--", sentID)

	// EO always attaches an expiration (defaults to 28 days).
	data := runJSON(t, "api", "GET", "/mail/v4/messages/"+sentID)
	msg, _ := data["Message"].(map[string]interface{})
	exp, _ := msg["ExpirationTime"].(float64)
	if int64(exp) <= time.Now().Unix() {
		t.Errorf("EO ExpirationTime = %d, expected a future expiry (~28 days)", int64(exp))
	}
}

func TestMailCrossAccountDelivery(t *testing.T) {
	subject := testID() + "-x2acct"
	body := "cross-account e2ee body for " + subject
	runOK(t, "mail", "messages", "send", "--to", secondaryEmail(), "--subject", subject, "--body", body)

	if sentID := findMessage(t, "sent", subject); sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete "+sentID,
			"mail", "messages", "delete", sentID)
	}

	// The second account receives it, decrypts the body, and the signature verifies
	// (internal Proton-to-Proton mail is signed with the sender's address key).
	var recvID string
	waitFor(45*time.Second, 3*time.Second, func() bool {
		recvID = secondaryMailContaining(t, selfEmail(), body)
		return recvID != ""
	})
	if recvID == "" {
		t.Fatal("the second account did not receive the cross-account mail")
	}
	cleanupRunSecondary(t, "Delete received mail (secondary): proton --profile secondary mail messages delete "+recvID,
		"mail", "messages", "delete", recvID)

	read := runOKSecondary(t, "mail", "messages", "get", recvID)
	assertContains(t, read, body)
	assertField(t, read, "Signature:", "verified")
}
