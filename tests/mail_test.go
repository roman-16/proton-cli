package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// ── mail list ──

func TestMailList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "FROM")
	assertContains(t, stdout, "SUBJECT")
}

func TestMailListSent(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "list", "--folder", "sent")
	assertContains(t, stdout, "ID")
}

func TestMailListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "mail", "list")
	if _, ok := data["Messages"]; !ok {
		t.Error("expected Messages in JSON output")
	}
}

func TestMailListPageSize(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "mail", "list", "--page-size", "5")
	messages := data["Messages"].([]interface{})
	if len(messages) > 5 {
		t.Errorf("expected at most 5 messages, got %d", len(messages))
	}
}

// ── mail search ──

func TestMailSearch(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "search", "--keyword", "proton")
	assertContains(t, stdout, "ID")
}

func TestMailSearchFrom(t *testing.T) {
	skipIfNoCredentials(t)
	// Search for something that likely exists
	runOK(t, "mail", "search", "--from", selfEmail())
}

func TestMailSearchDateRange(t *testing.T) {
	skipIfNoCredentials(t)
	runOK(t, "mail", "search", "--after", "2020-01-01", "--before", "2099-12-31")
}

func TestMailSearchEmpty(t *testing.T) {
	skipIfNoCredentials(t)
	stdout, _, code := run(t, "mail", "search", "--keyword", "xyznonexistent99999")
	if code != 0 {
		t.Fatalf("search should not fail on empty results, exit %d", code)
	}
	_ = stdout // empty results are fine
}

// ── mail send + read ──

func TestMailSendAndRead(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-send-read"
	msgID := sendTestMail(t, subject)

	// Read it
	stdout := runOK(t, "mail", "read", msgID)
	assertContains(t, stdout, subject)
	assertContains(t, stdout, "DecryptedBody")
}

// ── mail mark ──

func TestMailMarkUnreadRead(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-mark"
	msgID := sendTestMail(t, subject)

	// Mark unread
	runOK(t, "mail", "mark", "--unread", "--", msgID)

	// Verify appears in unread list
	data := runJSON(t, "mail", "list", "--unread")
	messages := data["Messages"].([]interface{})
	found := false
	for _, m := range messages {
		if m.(map[string]interface{})["ID"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should appear in unread list after marking unread")
	}

	// Mark read
	runOK(t, "mail", "mark", "--read", "--", msgID)

	// Verify gone from unread
	data2 := runJSON(t, "mail", "list", "--unread")
	messages2 := data2["Messages"].([]interface{})
	for _, m := range messages2 {
		if m.(map[string]interface{})["ID"].(string) == msgID {
			t.Error("message should not appear in unread list after marking read")
		}
	}
}

func TestMailMarkStarUnstar(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-star"
	msgID := sendTestMail(t, subject)

	// Star
	runOK(t, "mail", "mark", "--starred", "--", msgID)

	// Verify in starred
	data := runJSON(t, "mail", "list", "--folder", "starred")
	messages := data["Messages"].([]interface{})
	found := false
	for _, m := range messages {
		if m.(map[string]interface{})["ID"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should appear in starred after starring")
	}

	// Unstar
	runOK(t, "mail", "mark", "--unstar", "--", msgID)
}

// ── mail move ──

func TestMailMoveArchiveAndBack(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-move"
	msgID := sendTestMail(t, subject)

	// Move to archive
	runOK(t, "mail", "move", "--folder", "archive", "--", msgID)

	// Verify in archive
	data := runJSON(t, "mail", "list", "--folder", "archive")
	messages := data["Messages"].([]interface{})
	found := false
	for _, m := range messages {
		if m.(map[string]interface{})["ID"].(string) == msgID {
			found = true
			break
		}
	}
	if !found {
		t.Error("message should appear in archive after moving")
	}

	// Move back to inbox
	runOK(t, "mail", "move", "--folder", "inbox", "--", msgID)
}

// ── mail trash ──

func TestMailTrash(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-trash"
	msgID := sendTestMail(t, subject)

	// Trash it
	runOK(t, "mail", "trash", "--", msgID)

	// Verify not in inbox
	data := runJSON(t, "mail", "list")
	messages := data["Messages"].([]interface{})
	for _, m := range messages {
		if m.(map[string]interface{})["ID"].(string) == msgID {
			t.Error("trashed message should not appear in inbox")
		}
	}

	// Move back (cleanup already handles permanent delete)
	runOK(t, "mail", "move", "--folder", "inbox", "--", msgID)
}

// ── mail attachments ──

func TestMailAttachmentsList(t *testing.T) {
	skipIfNoCredentials(t)

	// Find a message with attachments
	data := runJSON(t, "mail", "list")
	messages := data["Messages"].([]interface{})
	var msgID string
	for _, m := range messages {
		msg := m.(map[string]interface{})
		if num, ok := msg["NumAttachments"].(float64); ok && num > 0 {
			msgID = msg["ID"].(string)
			break
		}
	}
	if msgID == "" {
		t.Skip("no messages with attachments found")
	}

	stdout := runOK(t, "mail", "attachments", "list", msgID)
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "NAME")
}

func TestMailAttachmentsDownload(t *testing.T) {
	skipIfNoCredentials(t)

	// Find a message with attachments
	data := runJSON(t, "mail", "list")
	messages := data["Messages"].([]interface{})
	var msgID string
	for _, m := range messages {
		msg := m.(map[string]interface{})
		if num, ok := msg["NumAttachments"].(float64); ok && num > 0 {
			msgID = msg["ID"].(string)
			break
		}
	}
	if msgID == "" {
		t.Skip("no messages with attachments found")
	}

	// Get first attachment ID
	attOut := runOK(t, "mail", "attachments", "list", msgID, "--json")
	var atts []map[string]interface{}
	if err := json.Unmarshal([]byte(attOut), &atts); err != nil {
		t.Fatalf("failed to parse attachments JSON: %v", err)
	}
	if len(atts) == 0 {
		t.Skip("message has no parseable attachments")
	}
	attID := atts[0]["ID"].(string)

	// Download
	outPath := filepath.Join(t.TempDir(), "test-attachment")
	runOK(t, "mail", "attachments", "download", msgID, attID, outPath)

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("downloaded file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Error("downloaded attachment is empty")
	}
}

// ── mail labels ──

func TestMailLabelsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "labels", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "TYPE")
}

func TestMailLabelsCreateDeleteLabel(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-label"

	stdout := runOK(t, "mail", "labels", "create", "--name", name, "--color", "#8080FF")

	// Extract label ID
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	labelID := jsonField(result, "Label", "ID")
	if labelID == "" {
		t.Fatal("no Label.ID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete label: proton-cli mail labels delete %s", labelID),
		"mail", "labels", "delete", labelID)

	// Verify it exists
	listOut := runOK(t, "mail", "labels", "list")
	assertContains(t, listOut, name)
}

func TestMailLabelsCreateDeleteFolder(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-folder"

	stdout := runOK(t, "mail", "labels", "create", "--name", name, "--folder", "--color", "#8080FF")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	labelID := jsonField(result, "Label", "ID")
	if labelID == "" {
		t.Fatal("no Label.ID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete folder: proton-cli mail labels delete %s", labelID),
		"mail", "labels", "delete", labelID)

	listOut := runOK(t, "mail", "labels", "list")
	assertContains(t, listOut, name)
}

// ── mail addresses ──

func TestMailAddressesList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "addresses", "list")
	assertContains(t, stdout, "EMAIL")
	assertContains(t, stdout, selfEmail())
}

// ── mail filters ──

func TestMailFiltersCRUD(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-filter"

	// Create
	stdout := runOK(t, "mail", "filters", "create",
		"--name", name,
		"--sieve", `require ["fileinto"]; if header :contains "Subject" "xyztest" { fileinto "Archive"; }`)

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	filterID := jsonField(result, "Filter", "ID")
	if filterID == "" {
		t.Fatal("no Filter.ID in create response")
	}

	cleanupRun(t, fmt.Sprintf("Delete filter: proton-cli mail filters delete %s", filterID),
		"mail", "filters", "delete", filterID)

	// List
	listOut := runOK(t, "mail", "filters", "list")
	assertContains(t, listOut, name)

	// Disable
	runOK(t, "mail", "filters", "disable", filterID)

	// Enable
	runOK(t, "mail", "filters", "enable", filterID)
}
