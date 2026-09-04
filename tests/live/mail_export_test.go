package live

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Export and import: what `export` writes, `--eml` reads back.
//
// The assertions parse the output as real mail rather than grepping it, because
// the point of an export is that another client can open it.

// Export writes messages back out as RFC 822, so the assertions here parse the
// output as real mail rather than grepping it, and the round trip proves the
// export and the --eml import agree with each other.

func TestMailMessagesExportToStdout(t *testing.T) {
	msgID, _, subject := plainMail(t)

	stdout := runOK(t, "mail", "messages", "export", "--dest", "-", "--", msgID)
	msg, err := mail.ReadMessage(strings.NewReader(stdout))
	if err != nil {
		t.Fatalf("exported document is not parseable mail: %v\n%s", err, truncateOutput(stdout))
	}
	if got := msg.Header.Get("Subject"); !strings.Contains(got, subject) {
		t.Errorf("Subject header = %q, want it to contain %q", got, subject)
	}
	for _, h := range []string{"From", "To", "Date", "Content-Type"} {
		if msg.Header.Get(h) == "" {
			t.Errorf("exported document has no %s header", h)
		}
	}
	if !strings.Contains(msg.Header.Get("Content-Type"), "text/") &&
		!strings.Contains(msg.Header.Get("Content-Type"), "multipart/") {
		t.Errorf("Content-Type = %q", msg.Header.Get("Content-Type"))
	}
}

func TestMailMessagesExportToFile(t *testing.T) {
	msgID, _, subject := plainMail(t)
	dest := filepath.Join(t.TempDir(), "message.eml")

	runOK(t, "mail", "messages", "export", "--dest", dest, "--", msgID)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("export wrote nothing to %s: %v", dest, err)
	}
	if _, err := mail.ReadMessage(strings.NewReader(string(data))); err != nil {
		t.Fatalf("exported file is not parseable mail: %v", err)
	}
	if !strings.Contains(string(data), subject) {
		t.Error("the exported file does not contain the subject")
	}
}

func TestMailMessagesExportFolderToDirectory(t *testing.T) {
	plainMail(t) // make sure the inbox has something to export
	dir := t.TempDir()

	runOK(t, "mail", "messages", "export",
		"--folder", "inbox", "--limit", "3", "--all",
		"--no-attachments", "--dest-dir", dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("a bulk export wrote no files")
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".eml") {
			t.Errorf("exported file %q should end in .eml", e.Name())
		}
		// Named "<date> <subject>.eml", so it sorts chronologically.
		if !strings.HasPrefix(e.Name(), "20") {
			t.Errorf("exported file %q should start with its date", e.Name())
		}
	}
}

func TestMailMessagesExportMbox(t *testing.T) {
	plainMail(t)
	dest := filepath.Join(t.TempDir(), "inbox.mbox")

	runOK(t, "mail", "messages", "export", "--format", "mbox",
		"--folder", "inbox", "--limit", "2", "--all",
		"--no-attachments", "--dest", dest)

	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("mbox export wrote nothing: %v", err)
	}
	if !strings.HasPrefix(string(data), "From ") {
		t.Errorf("an mbox must start with a From_ separator, got: %s", truncateOutput(string(data)))
	}
}

func TestMailMessagesExportRejectsAmbiguousOutput(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "one.eml")
	_, stderr, code := run(t, "mail", "messages", "export",
		"--folder", "inbox", "--limit", "5", "--all", "--dest", dest)
	if code == 0 {
		t.Error("--output with many messages should be refused")
	}
	assertContains(t, stderr, "--dest-dir")
}

func TestMailMessagesExportDryRun(t *testing.T) {
	msgID, _, _ := plainMail(t)
	dir := t.TempDir()
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "export",
		"--dest-dir", dir, "--", msgID)
	assertContains(t, stderr, "Dry run")
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Error("--dry-run wrote files")
	}
}

func TestMailConversationsExportWholeThread(t *testing.T) {
	_, convID, subject := plainMail(t)
	dest := filepath.Join(t.TempDir(), "thread.mbox")

	runOK(t, "mail", "conversations", "export", "--dest", dest, "--", convID)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("thread export wrote nothing: %v", err)
	}
	if !strings.HasPrefix(string(data), "From ") {
		t.Error("a thread export should be an mbox")
	}
	if !strings.Contains(string(data), subject) {
		t.Error("the exported thread does not contain the subject")
	}
}

// The point of having both halves: what export writes, --eml reads back.
func TestMailExportImportRoundTrip(t *testing.T) {
	msgID, _, attID, attName := attachedMail(t)
	_ = attID

	dest := filepath.Join(t.TempDir(), "round-trip.eml")
	runOK(t, "mail", "messages", "export", "--dest", dest, "--", msgID)

	subject := testID() + "-roundtrip"
	id := strings.TrimSpace(runOK(t, "mail", "drafts", "create",
		"--eml", dest, "--to", selfEmail(), "--subject", subject))
	cleanupRun(t, "Delete round-trip draft: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	// The flags win over the file, and the file supplies the rest.
	read := runOK(t, "mail", "messages", "get", "--", id)
	assertField(t, read, "Subject:", subject)
	assertContains(t, runOK(t, "mail", "messages", "attachments", "list", id), attName)
}

func TestMailSendFromEMLFile(t *testing.T) {
	subject := testID() + "-eml-send"
	path := filepath.Join(t.TempDir(), "message.eml")
	doc := "From: " + selfEmail() + "\r\n" +
		"To: " + selfEmail() + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Body straight from a file.\r\n"
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	id := strings.TrimSpace(runOK(t, "mail", "messages", "send", "--eml", path))
	cleanupRun(t, "Delete sent mail: proton mail messages delete "+id,
		"mail", "messages", "delete", "--", id)

	read := runOK(t, "mail", "messages", "get", "--", id)
	assertField(t, read, "Subject:", subject)
	assertContains(t, read, "Body straight from a file.")

	if inbox := findMessage(t, "inbox", subject); inbox != "" && inbox != id {
		cleanupRun(t, "Delete inbox copy: proton mail messages delete "+inbox,
			"mail", "messages", "delete", "--", inbox)
	}
}
