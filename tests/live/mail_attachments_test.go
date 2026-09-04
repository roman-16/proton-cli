package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Attachments, on a message and across a thread.
//
// An inline image is embedded in the HTML body by Content-ID, which is what has
// Proton record the part as inline rather than as an attachment. A listing
// leaves those out unless asked, and says which is which when it is.

func TestMailAttachmentsListAndDownload(t *testing.T) {
	msgID, _, attID, attName := attachedMail(t)

	// List
	stdout := runOK(t, "mail", "messages", "attachments", "list", msgID)
	assertContains(t, stdout, "NAME")

	// Download to tempdir
	out := filepath.Join(t.TempDir(), "att")
	runOK(t, "mail", "messages", "attachments", "download", "--dest", out, msgID, attID)

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("attachment not saved: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("attachment %q is empty", attName)
	}
}

func TestMailAttachmentsDownloadCollisionAutoSuffix(t *testing.T) {
	msgID, _, attID, attName := attachedMail(t)

	dir := t.TempDir()
	// Pre-create the canonical destination so the download collides.
	placeholder := filepath.Join(dir, attName)
	if err := os.WriteFile(placeholder, []byte("placeholder"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, stderr := runOKStderr(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID, attID)

	// Suffixed file must exist; placeholder must be untouched.
	stem := strings.TrimSuffix(attName, filepath.Ext(attName))
	ext := filepath.Ext(attName)
	suffixed := filepath.Join(dir, stem+" (2)"+ext)
	if _, err := os.Stat(suffixed); err != nil {
		t.Errorf("expected auto-suffixed file at %s, got: %v\nstderr: %s", suffixed, err, stderr)
	}
	if data, _ := os.ReadFile(placeholder); string(data) != "placeholder" {
		t.Errorf("placeholder was overwritten: %q", string(data))
	}
}

func TestMailAttachmentsDownloadCollisionExplicitErrors(t *testing.T) {
	msgID, _, attID, _ := attachedMail(t)

	dest := filepath.Join(t.TempDir(), "existing.bin")
	if err := os.WriteFile(dest, []byte("x"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	_, stderr, code := run(t, "mail", "messages", "attachments", "download", "--dest", dest, msgID, attID)
	if code == 0 {
		t.Errorf("expected non-zero exit on collision, got 0")
	}
	if !strings.Contains(stderr, "exists") {
		t.Errorf("expected stderr to mention 'exists', got: %s", stderr)
	}
}

func TestMailAttachmentsDownloadForce(t *testing.T) {
	msgID, _, attID, _ := attachedMail(t)

	dest := filepath.Join(t.TempDir(), "existing.bin")
	if err := os.WriteFile(dest, []byte("placeholder"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	runOK(t, "mail", "messages", "attachments", "download", "--dest", dest, "--force", msgID, attID)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read after force: %v", err)
	}
	if string(data) == "placeholder" {
		t.Error("--force did not overwrite")
	}
}

func TestMailAttachmentsDownloadAll(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)

	dir := t.TempDir()
	runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("--all wrote no files")
	}
}

func TestMailAttachmentsDownloadOutputDirAutoCreates(t *testing.T) {
	msgID, _, attID, _ := attachedMail(t)

	dir := filepath.Join(t.TempDir(), "new", "deep", "nested")
	runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID, attID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("--dest-dir was not created: %v", err)
	}
	if len(entries) == 0 {
		t.Error("no file written into auto-created --dest-dir")
	}
}

func TestMailAttachmentsListFiltersInline(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)

	// Default: filtered. Text mode must NOT have a DISPOSITION header.
	stdout := runOK(t, "mail", "messages", "attachments", "list", msgID)
	if strings.Contains(stdout, "DISPOSITION") {
		t.Error("default text-mode list should not show DISPOSITION column")
	}

	// JSON: filtered, but each entry still has the disposition field.
	defaultAtts := runJSONArray(t, "mail", "messages", "attachments", "list", msgID)
	for _, row := range defaultAtts {
		a, _ := row.(map[string]interface{})
		if d, _ := a["disposition"].(string); d == "inline" {
			t.Errorf("default list contains inline attachment: %v", a)
		}
		if _, ok := a["disposition"]; !ok {
			t.Errorf("default JSON missing disposition field on %v", a)
		}
	}

	// --include-inline: text mode shows DISPOSITION column.
	stdoutAll := runOK(t, "mail", "messages", "attachments", "list", "--include-inline", msgID)
	if !strings.Contains(stdoutAll, "DISPOSITION") {
		t.Error("--include-inline text-mode list should show DISPOSITION column")
	}

	// --include-inline JSON has both kinds.
	allAtts := runJSONArray(t, "mail", "messages", "attachments", "list", "--include-inline", msgID)
	dispositions := map[string]bool{}
	for _, row := range allAtts {
		a, _ := row.(map[string]interface{})
		if d, ok := a["disposition"].(string); ok {
			dispositions[d] = true
		}
	}
	if !dispositions["inline"] {
		t.Error("--include-inline list should include at least one inline")
	}
	if !dispositions["attachment"] {
		t.Error("--include-inline list should include at least one attachment")
	}
	if len(allAtts) <= len(defaultAtts) {
		t.Errorf("--include-inline (%d) should yield more entries than default (%d)",
			len(allAtts), len(defaultAtts))
	}
}

func TestMailAttachmentsListJSONHasDisposition(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)

	atts := runJSONArray(t, "mail", "messages", "attachments", "list", msgID)
	if len(atts) == 0 {
		t.Fatal("the fixture message carries an attachment and the listing is empty")
	}
	for _, row := range atts {
		a, _ := row.(map[string]interface{})
		d, ok := a["disposition"].(string)
		if !ok {
			t.Errorf("attachment missing 'disposition' field: %v", a)
		}
		// May be "" on legacy messages; that's still a string.
		_ = d
	}
}

func TestMailAttachmentsDownloadAllSkipsInline(t *testing.T) {
	msgID, _, _, _ := attachedMail(t)

	dir := t.TempDir()
	runOK(t, "mail", "messages", "attachments", "download", "--dest-dir", dir, msgID)

	written, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	numWritten := len(written)

	// --include-inline should yield strictly more files.
	dir2 := t.TempDir()
	runOK(t, "mail", "messages", "attachments", "download", "--include-inline", "--dest-dir", dir2, msgID)
	written2, err := os.ReadDir(dir2)
	if err != nil {
		t.Fatalf("readdir2: %v", err)
	}
	if len(written2) <= numWritten {
		t.Errorf("--include-inline (%d files) should write more than default (%d files)",
			len(written2), numWritten)
	}
}

func TestMailConversationsAttachmentsList(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	// Default: filtered, includes a MESSAGE_ID column.
	stdout := runOK(t, "mail", "conversations", "attachments", "list", convID)
	assertContains(t, stdout, "MESSAGE_ID")
	if strings.Contains(stdout, "DISPOSITION") {
		t.Error("default text-mode list must not show DISPOSITION column")
	}

	// JSON: each entry carries message_id + disposition.
	atts := runJSONArray(t, "mail", "conversations", "attachments", "list", convID)
	if len(atts) == 0 {
		t.Fatal("the fixture message carries an attachment and its thread lists none")
	}
	for _, row := range atts {
		a, _ := row.(map[string]interface{})
		if _, ok := a["message_id"].(string); !ok {
			t.Errorf("attachment missing message_id: %v", a)
		}
		if _, ok := a["disposition"]; !ok {
			t.Errorf("attachment missing disposition: %v", a)
		}
	}
}

func TestMailConversationsAttachmentsListIncludeInline(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	stdout := runOK(t, "mail", "conversations", "attachments", "list",
		"--include-inline", convID)
	assertContains(t, stdout, "DISPOSITION")
	assertContains(t, stdout, "inline")
}

func TestMailConversationsAttachmentsDownloadAll(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	dir := t.TempDir()
	runOK(t, "mail", "conversations", "attachments", "download", "--dest-dir", dir, convID)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("downloading every attachment wrote no files")
	}
}

func TestMailConversationsAttachmentsDownloadAllSkipsInline(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	dir := t.TempDir()
	runOK(t, "mail", "conversations", "attachments", "download", "--dest-dir", dir, convID)
	default1, _ := os.ReadDir(dir)

	dir2 := t.TempDir()
	runOK(t, "mail", "conversations", "attachments", "download", "--include-inline", "--dest-dir", dir2, convID)
	inclAll, _ := os.ReadDir(dir2)

	if len(inclAll) <= len(default1) {
		t.Errorf("--include-inline (%d files) should write more than default (%d files)",
			len(inclAll), len(default1))
	}
}

func TestMailConversationsAttachmentsDownloadOneByID(t *testing.T) {
	msgID, _, attID, attName := attachedMail(t)
	convID := findConversationFor(t, msgID)

	dir := t.TempDir()
	runOK(t, "mail", "conversations", "attachments", "download", "--dest-dir", dir, convID, attID)

	dest := filepath.Join(dir, attName)
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatalf("single download not at %s: %v", dest, err)
	}
	if info.Size() == 0 {
		t.Errorf("downloaded file is empty")
	}
}

func TestMailConversationsAttachmentsDownloadUnknownID(t *testing.T) {
	_, convID, _, _ := attachedMail(t)

	_, stderr, code := run(t, "mail", "conversations", "attachments", "download",
		convID, "fake-attachment-id-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3 for unknown attachment, got %d", code)
	}
	assertContains(t, stderr, "in that thread")
}
