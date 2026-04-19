package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ── mail messages list ──

func TestMailMessagesList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "messages", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "FROM")
	assertContains(t, stdout, "SUBJECT")
}

func TestMailMessagesListSent(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "messages", "list", "--folder", "sent")
}

func TestMailMessagesListJSONFieldNames(t *testing.T) {
	skipIfNoCredentials(t)
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
	skipIfNoCredentials(t)
	data := runJSON(t, "mail", "messages", "list", "--page-size", "3")
	msgs := data["messages"].([]interface{})
	if len(msgs) > 3 {
		t.Errorf("expected at most 3 messages, got %d", len(msgs))
	}
}

func TestMailMessagesListUnreadFlag(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "messages", "list", "--unread")
}

// ── mail messages search ──

func TestMailMessagesSearch(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "messages", "search", "--keyword", "proton")
}

func TestMailMessagesSearchFrom(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "messages", "search", "--from", selfEmail())
}

func TestMailMessagesSearchDateRange(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "messages", "search", "--after", "2020-01-01", "--before", "2099-12-31")
}

func TestMailMessagesSearchEmpty(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "mail", "messages", "search", "--keyword", "xyz-nothing-xxxyyy-"+testID())
	if code != 0 {
		t.Fatalf("search with no results should exit 0, got %d", code)
	}
}

// ── send / read / REF search ──

func TestMailMessagesSendAndReadText(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-send-read"
	msgID := sendTestMail(t, subject)

	// Default --format text: human-readable, fields on stderr-safe stdout
	stdout := runOK(t, "mail", "messages", "read", "--", msgID)
	assertContains(t, stdout, subject)
	assertContains(t, stdout, selfEmail())
	assertField(t, stdout, "Subject:", subject)
}

func TestMailMessagesReadByRef(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-ref"
	sendTestMail(t, subject)

	// Proton's search index is populated asynchronously, so the message may
	// show up in list (used by sendTestMail) a few seconds before it shows up
	// in the keyword-search endpoint that REF resolution uses. Retry with
	// backoff instead of hard-failing on the first attempt.
	var stdout, lastStderr string
	var lastCode int
	for attempt := 0; attempt < 8; attempt++ {
		out, stderr, code := run(t, "mail", "messages", "read", subject)
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

func TestMailMessagesReadFormatRaw(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-raw"
	msgID := sendTestMail(t, subject)

	stdout := runOK(t, "mail", "messages", "read", "--format", "raw", "--", msgID)
	assertContains(t, stdout, subject)
}

func TestMailMessagesReadFormatInvalid(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-badfmt"
	msgID := sendTestMail(t, subject)

	_, stderr, code := run(t, "mail", "messages", "read", "--format", "wut", "--", msgID)
	if code == 0 {
		t.Error("expected non-zero exit for unknown --format")
	}
	assertContains(t, stderr, "unknown --format")
}

func TestMailMessagesReadNotFound(t *testing.T) {
	skipIfNoCredentials(t)
	_, _, code := run(t, "mail", "messages", "read", "no-such-msg-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3 (not-found), got %d", code)
	}
}

// ── mark / star / unstar ──

func TestMailMessagesMarkReadUnread(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-mark"
	msgID := sendTestMail(t, subject)

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
	skipIfNoCredentials(t)
	subject := testID() + "-star"
	msgID := sendTestMail(t, subject)

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

// ── move / trash with --dest ──

func TestMailMessagesMoveDest(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-move"
	msgID := sendTestMail(t, subject)

	runOK(t, "mail", "messages", "move", "--dest", "archive", "--", msgID)
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

	runOK(t, "mail", "messages", "move", "--dest", "inbox", "--", msgID)
}

func TestMailMessagesTrash(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-trash"
	msgID := sendTestMail(t, subject)

	runOK(t, "mail", "messages", "trash", "--", msgID)
	data := runJSON(t, "mail", "messages", "list", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	for _, m := range msgs {
		if m.(map[string]interface{})["id"].(string) == msgID {
			t.Error("trashed message should not appear in inbox")
		}
	}
	// put it back so cleanup can delete
	runOK(t, "mail", "messages", "move", "--dest", "inbox", "--", msgID)
}

// ── batch filters (all dry-run so nothing is actually mutated) ──

func TestMailBatchTrashDryRunUnread(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "trash", "--unread", "--limit", "5")
	assertContains(t, stderr, "dry-run")
}

func TestMailBatchTrashDryRunOlderThan(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "trash", "--older-than", "365d", "--from", "noreply", "--limit", "5")
	assertContains(t, stderr, "dry-run")
}

func TestMailBatchRequiresInput(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "mail", "messages", "trash")
	if code == 0 {
		t.Error("expected error when no REF and no filter given")
	}
	assertContains(t, stderr, "no messages selected")
}

// ── attachments ──

func TestMailAttachmentsListAndDownload(t *testing.T) {
	skipIfNoCredentials(t)

	// Find any message with an attachment.
	data := runJSON(t, "mail", "messages", "list", "--page-size", "50")
	msgs := data["messages"].([]interface{})
	var msgID string
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if num, ok := msg["num_attachments"].(float64); ok && num > 0 {
			msgID = msg["id"].(string)
			break
		}
	}
	if msgID == "" {
		t.Skip("no messages with attachments found in last 50 inbox items")
	}

	// List
	stdout := runOK(t, "mail", "attachments", "list", msgID)
	assertContains(t, stdout, "NAME")

	// Download to tempdir
	attsRaw := runOK(t, "mail", "attachments", "list", msgID, "--output", "json")
	var atts []map[string]interface{}
	if err := json.Unmarshal([]byte(attsRaw), &atts); err != nil {
		t.Fatalf("failed to parse attachments JSON: %v", err)
	}
	if len(atts) == 0 {
		t.Skip("no attachments after decryption")
	}
	attID := atts[0]["id"].(string)
	attName, _ := atts[0]["name"].(string)
	out := filepath.Join(t.TempDir(), "att")
	runOK(t, "mail", "attachments", "download", msgID, attID, out)

	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("attachment not saved: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("attachment %q is empty", attName)
	}
}

// ── labels ──

func TestMailLabelsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "labels", "list")
	assertContains(t, stdout, "NAME")
}

func TestMailLabelsCreateDeleteLabel(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-label"

	// stdout = just the ID
	stdout := runOK(t, "mail", "labels", "create", "--name", name, "--color", "#8080FF")
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete label: proton-cli mail labels delete -- %s", id),
		"mail", "labels", "delete", "--", id)

	list := runOK(t, "mail", "labels", "list")
	assertContains(t, list, name)
	assertContains(t, list, "LABEL")
}

func TestMailLabelsCreateFolder(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-folder"
	stdout := runOK(t, "mail", "labels", "create", "--name", name, "--folder", "--color", "#8080FF")
	id := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli mail labels delete -- %s", id),
		"mail", "labels", "delete", "--", id)
	list := runOK(t, "mail", "labels", "list")
	assertContains(t, list, name)
	assertContains(t, list, "FOLDER")
}

// ── filters ──

func TestMailFiltersCRUD(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-filter"
	sieve := `require ["fileinto"]; if header :contains "Subject" "xyz-never-matches-` + testID() + `" { fileinto "Archive"; }`

	stdout := runOK(t, "mail", "filters", "create", "--name", name, "--sieve", sieve)
	id := strings.TrimSpace(stdout)
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", stdout)
	}
	cleanupRun(t, fmt.Sprintf("Delete filter: proton-cli mail filters delete -- %s", id),
		"mail", "filters", "delete", "--", id)

	list := runOK(t, "mail", "filters", "list")
	assertContains(t, list, name)
	assertContains(t, list, "enabled")

	runOK(t, "mail", "filters", "disable", "--", id)
	assertContains(t, runOK(t, "mail", "filters", "list"), "disabled")

	runOK(t, "mail", "filters", "enable", "--", id)
	assertContains(t, runOK(t, "mail", "filters", "list"), "enabled")
}

// ── addresses ──

func TestMailAddressesList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "addresses", "list")
	assertContains(t, stdout, "EMAIL")
	assertContains(t, stdout, selfEmail())
}

// ── helpers local to mail tests ──

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// looksLikeID matches a Proton base64 ID (~88 chars ending in ==).
func looksLikeID(s string) bool {
	return len(s) > 60 && strings.HasSuffix(s, "==")
}
