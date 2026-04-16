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
	assertContains(t, stdout, "DATE")
}

func TestMailListSent(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "list", "--folder", "sent")
	assertContains(t, stdout, "ID")
}

func TestMailListJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "mail", "list")
	messages, ok := data["Messages"].([]interface{})
	if !ok {
		t.Fatal("expected Messages array in JSON output")
	}
	if _, ok := data["Total"]; !ok {
		t.Error("expected Total in JSON output")
	}
	if len(messages) > 0 {
		msg := messages[0].(map[string]interface{})
		if msg["ID"] == nil || msg["ID"] == "" {
			t.Error("message missing ID")
		}
		if msg["Subject"] == nil {
			t.Error("message missing Subject")
		}
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
	_ = stdout
}

// ── mail send + read ──

func TestMailSendAndRead(t *testing.T) {
	skipIfNoCredentials(t)
	subject := testID() + "-send-read"
	msgID := sendTestMail(t, subject)

	stdout := runOK(t, "mail", "read", msgID)
	assertContains(t, stdout, subject)
	assertContains(t, stdout, "DecryptedBody")
	assertContains(t, stdout, selfEmail())

	// JSON structure check
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("mail read output is not valid JSON: %v", err)
	}
	if result["ID"] != msgID {
		t.Errorf("ID: got %v, want %s", result["ID"], msgID)
	}
	if result["Subject"] != subject {
		t.Errorf("Subject: got %v, want %s", result["Subject"], subject)
	}
	if result["DecryptedBody"] == nil || result["DecryptedBody"] == "" {
		t.Error("DecryptedBody is empty")
	}
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
			if m.(map[string]interface{})["Unread"].(float64) != 1 {
				t.Error("message should have Unread=1")
			}
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
		msg := m.(map[string]interface{})
		if msg["ID"].(string) == msgID {
			found = true
			if msg["Subject"].(string) != subject {
				t.Errorf("starred message Subject: got %v, want %s", msg["Subject"], subject)
			}
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
		msg := m.(map[string]interface{})
		if msg["ID"].(string) == msgID {
			found = true
			if msg["Subject"].(string) != subject {
				t.Errorf("archived message Subject: got %v, want %s", msg["Subject"], subject)
			}
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

	// Move back
	runOK(t, "mail", "move", "--folder", "inbox", "--", msgID)
}

// ── mail attachments ──

func TestMailAttachmentsList(t *testing.T) {
	skipIfNoCredentials(t)

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
	assertContains(t, stdout, "SIZE")
	assertContains(t, stdout, "TYPE")
}

func TestMailAttachmentsDownload(t *testing.T) {
	skipIfNoCredentials(t)

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

	attOut := runOK(t, "mail", "attachments", "list", msgID, "--json")
	var atts []map[string]interface{}
	if err := json.Unmarshal([]byte(attOut), &atts); err != nil {
		t.Fatalf("failed to parse attachments JSON: %v", err)
	}
	if len(atts) == 0 {
		t.Skip("message has no parseable attachments")
	}
	attID := atts[0]["ID"].(string)
	attName := atts[0]["Name"].(string)

	outPath := filepath.Join(t.TempDir(), "test-attachment")
	runOK(t, "mail", "attachments", "download", msgID, attID, outPath)

	info, err := os.Stat(outPath)
	if err != nil {
		t.Fatalf("downloaded file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Errorf("downloaded attachment %q is empty", attName)
	}
}

// ── mail labels ──

func TestMailLabelsList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "labels", "list")
	assertContains(t, stdout, "ID")
	assertContains(t, stdout, "TYPE")
	assertContains(t, stdout, "NAME")
	assertContains(t, stdout, "COLOR")
}

func TestMailLabelsCreateDeleteLabel(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-label"

	stdout := runOK(t, "mail", "labels", "create", "--name", name, "--color", "#8080FF")

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse create output: %v", err)
	}
	labelID := jsonField(result, "Label", "ID")
	if labelID == "" {
		t.Fatal("no Label.ID in create response")
	}
	labelName := jsonField(result, "Label", "Name")
	if labelName != name {
		t.Errorf("Label.Name: got %q, want %q", labelName, name)
	}

	cleanupRun(t, fmt.Sprintf("Delete label: proton-cli mail labels delete %s", labelID),
		"mail", "labels", "delete", labelID)

	listOut := runOK(t, "mail", "labels", "list")
	assertContains(t, listOut, name)
	assertContains(t, listOut, labelID)
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
	assertContains(t, listOut, "FOLDER")
}

// ── mail addresses ──

func TestMailAddressesList(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "mail", "addresses", "list")
	assertContains(t, stdout, "EMAIL")
	assertContains(t, stdout, selfEmail())
	assertContains(t, stdout, "active")
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
	filterName := jsonField(result, "Filter", "Name")
	if filterName != name {
		t.Errorf("Filter.Name: got %q, want %q", filterName, name)
	}

	cleanupRun(t, fmt.Sprintf("Delete filter: proton-cli mail filters delete %s", filterID),
		"mail", "filters", "delete", filterID)

	// List — verify name and enabled status
	listOut := runOK(t, "mail", "filters", "list")
	assertContains(t, listOut, name)
	assertContains(t, listOut, "enabled")

	// Disable
	runOK(t, "mail", "filters", "disable", filterID)
	listOut2 := runOK(t, "mail", "filters", "list")
	assertContains(t, listOut2, "disabled")

	// Enable
	runOK(t, "mail", "filters", "enable", filterID)
}
