package live

import (
	"bytes"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Files attached to an item.
//
// Attachments need a plan, so everything that puts one on an item acts as the
// paid account. The free account proves the one thing it can: the refusal, which
// is what anybody without a plan meets.

// attachable writes a file big enough to cross the four-megabyte boundary Proton
// stores files in pieces of, so a mistake at the seam between two chunks is a
// mistake this catches.
func attachable(t *testing.T, name string) (path string, contents []byte) {
	t.Helper()
	const size = 4*1024*1024 + 4096
	contents = make([]byte, size)
	if _, err := rand.Read(contents); err != nil {
		t.Fatalf("make the file's contents: %v", err)
	}
	// A PDF says what it is in its first bytes, so the type Proton stores is one
	// read from the contents rather than from the name.
	copy(contents, []byte("%PDF-1.7\n"))
	return writtenFile(t, name, contents), contents
}

func writtenFile(t *testing.T, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// attachedItem makes an item carrying one file and answers with its reference.
func attachedItem(t *testing.T, name, path string) string {
	t.Helper()
	stdout, stderr, code := runPaid(t, "--yes", "pass", "items", "create",
		"--type", "note", "--attach", path, "--name", name)
	if code != 0 {
		t.Fatalf("could not create an item with a file on it: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(stdout)
	cleanupRunPaid(t, "Delete item: proton pass items delete "+ref, "pass", "items", "delete", ref)
	assertContains(t, stderr, "1 attachment")
	return ref
}

// A file goes up with the item, is listed, comes back down byte for byte, is
// renamed, is taken off - and is still readable in the version that held it.
func TestPassAttachmentsTravelWithAnItem(t *testing.T) {
	name := testID() + "-attached"
	path, contents := attachable(t, "passport.pdf")
	ref := attachedItem(t, name, path)

	// The item says what it carries, how big it is, and what it turned out to be.
	shown := runJSONPaid(t, "pass", "items", "get", ref)
	files, _ := shown["attachments"].([]interface{})
	if len(files) != 1 {
		t.Fatalf("the item carries %d attachments, want 1: %v", len(files), shown["attachments"])
	}
	file, _ := files[0].(map[string]interface{})
	if got, _ := file["name"].(string); got != "passport.pdf" {
		t.Errorf("the attachment is called %q", got)
	}
	if got, _ := file["mime_type"].(string); got != "application/pdf" {
		t.Errorf("mime_type = %q, want application/pdf, which is what the contents say", got)
	}
	if size, _ := file["size"].(float64); int64(size) != int64(len(contents)) {
		t.Errorf("size = %v, want %d", file["size"], len(contents))
	}

	if rows := runJSONArrayPaid(t, "pass", "items", "attachments", "list", ref); len(rows) != 1 {
		t.Fatalf("the listing shows %d attachments, want 1", len(rows))
	}

	// What comes back down is what went up, across the seam between two chunks.
	back := filepath.Join(t.TempDir(), "downloaded.pdf")
	runOKPaid(t, "pass", "items", "attachments", "download", "--dest", back, ref, "passport.pdf")
	got, err := os.ReadFile(back)
	if err != nil {
		t.Fatalf("read what was downloaded: %v", err)
	}
	if !bytes.Equal(got, contents) {
		t.Errorf("the file came back as %d bytes, want %d", len(got), len(contents))
	}

	// A name is the file's own, so changing it is not an edit of the item.
	runOKPaid(t, "pass", "items", "attachments", "update",
		"--name", "passport-2031.pdf", ref, "passport.pdf")
	renamed := runJSONArrayPaid(t, "pass", "items", "attachments", "list", ref)
	if len(renamed) != 1 {
		t.Fatalf("after renaming, the listing shows %d attachments", len(renamed))
	}
	row, _ := renamed[0].(map[string]interface{})
	if got, _ := row["name"].(string); got != "passport-2031.pdf" {
		t.Errorf("the renamed attachment is called %q", got)
	}

	// Taking it off leaves the item without it, and Proton with it.
	runOKPaid(t, "--yes", "pass", "items", "update", "--detach", "passport-2031.pdf", ref)
	if rows := runJSONArrayPaid(t, "pass", "items", "attachments", "list", ref); len(rows) != 0 {
		t.Errorf("the item still carries %d attachments", len(rows))
	}
	gone := runJSONArrayPaid(t, "pass", "items", "attachments", "list", "--removed", ref)
	if len(gone) != 1 {
		t.Fatalf("%d attachments were taken off the item, want 1", len(gone))
	}
	was, _ := gone[0].(map[string]interface{})
	if got, _ := was["name"].(string); got != "passport-2031.pdf" {
		t.Errorf("the removed attachment is called %q", got)
	}
}

// A file taken off an item is kept, and comes back by name.
func TestPassAttachmentsComeBack(t *testing.T) {
	name := testID() + "-restored"
	path, _ := attachable(t, "contract.pdf")
	ref := attachedItem(t, name, path)

	runOKPaid(t, "--yes", "pass", "items", "update", "--detach", "contract.pdf", ref)
	runOKPaid(t, "--yes", "pass", "items", "attachments", "restore", ref, "contract.pdf")

	if rows := runJSONArrayPaid(t, "pass", "items", "attachments", "list", ref); len(rows) != 1 {
		t.Fatalf("after restoring, the item carries %d attachments, want 1", len(rows))
	}
	// What is back on the item is not also waiting to be restored.
	if rows := runJSONArrayPaid(t, "pass", "items", "attachments", "list", "--removed", ref); len(rows) != 0 {
		t.Errorf("a file that is back on the item is still listed as removed: %v", rows)
	}
}

// Without a plan there is nothing to attach a file to, and the refusal comes
// before anything is sent or created.
func TestPassAttachmentsNeedAPlan(t *testing.T) {
	name := testID() + "-unpaid"
	path := writtenFile(t, "receipt.pdf", []byte("%PDF-1.7\nreceipt"))

	stdout, stderr, code := run(t, "--yes", "pass", "items", "create",
		"--type", "note", "--attach", path, "--name", name)
	if code == 0 {
		ref := strings.TrimSpace(stdout)
		cleanupRun(t, "Delete item: proton pass items delete "+ref, "pass", "items", "delete", ref)
		t.Fatal("an account without file storage attached a file")
	}
	assertContains(t, stderr, "paid Pass plan")

	// Nothing was created on the way to being refused.
	for _, row := range runJSONArray(t, "pass", "items", "list") {
		m, _ := row.(map[string]interface{})
		if got, _ := m["name"].(string); got == name {
			t.Fatalf("the refused item was created anyway: %v", m)
		}
	}
}
