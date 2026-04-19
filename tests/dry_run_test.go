package tests

import (
	"strings"
	"testing"
)

// --dry-run must never mutate state.

func TestDryRunLabelCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-dryrun"
	_, stderr := runOKStderr(t, "--dry-run", "mail", "labels", "create",
		"--name", name, "--color", "#8080FF")
	assertContains(t, stderr, "dry-run")

	list := runOK(t, "mail", "labels", "list")
	if strings.Contains(list, name) {
		t.Errorf("dry-run created a label: %q appears in list", name)
	}
}

func TestDryRunFolderCreate(t *testing.T) {
	skipIfNoCredentials(t)
	path := "/" + testID() + "-dryrun"
	_, stderr := runOKStderr(t, "--dry-run", "drive", "folders", "create", path)
	assertContains(t, stderr, "dry-run")

	list := runOK(t, "drive", "items", "list")
	name := strings.TrimPrefix(path, "/")
	if strings.Contains(list, name) {
		t.Errorf("dry-run created a folder: %q appears in listing", name)
	}
}

func TestDryRunMailTrashBatch(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "messages", "trash",
		"--unread", "--limit", "3")
	assertContains(t, stderr, "dry-run")
}

func TestDryRunPassTrashBatch(t *testing.T) {
	skipIfNoCredentials(t)
	_, stderr, code := run(t, "--dry-run", "pass", "items", "trash", "--type", "note")
	if code != 0 {
		t.Fatalf("dry-run should succeed, got exit %d", code)
	}
	assertContains(t, stderr, "dry-run")
}

func TestDryRunContactsCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-dryrun-contact"
	_, stderr := runOKStderr(t, "--dry-run", "contacts", "create",
		"--name", name, "--email", "t@x.invalid")
	assertContains(t, stderr, "dry-run")

	_, _, code := run(t, "contacts", "get", name)
	if code != 3 {
		t.Error("dry-run should not create the contact")
	}
}
