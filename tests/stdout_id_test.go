package tests

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The stdout=ID convention: every create command writes just the new ID on
// stdout (one line, no JSON) and a "✓ …" message on stderr. This lets scripts
// do ID=$(proton-cli foo create ...).

func assertBareID(t *testing.T, stdout, where string) string {
	t.Helper()
	id := strings.TrimSpace(stdout)
	// Exactly one non-empty line
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	if len(lines) != 1 {
		t.Fatalf("%s: expected 1 line on stdout, got %d:\n%s", where, len(lines), stdout)
	}
	if !looksLikeID(id) {
		t.Fatalf("%s: stdout is not a Proton ID: %q", where, id)
	}
	return id
}

func TestStdoutIDMailLabelCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-label"
	stdout, stderr := runOKStderr(t, "mail", "labels", "create",
		"--name", name, "--color", "#8080FF")
	id := assertBareID(t, stdout, "labels create")
	cleanupRun(t, fmt.Sprintf("Delete label: proton-cli mail labels delete -- %s", id),
		"mail", "labels", "delete", "--", id)
	if !strings.Contains(stderr, "✓") {
		t.Errorf("expected ✓ on stderr, got: %q", stderr)
	}
}

func TestStdoutIDMailFilterCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-filter"
	stdout, _ := runOKStderr(t, "mail", "filters", "create",
		"--name", name,
		"--sieve", `require ["fileinto"]; if header :contains "Subject" "nope-`+testID()+`" { fileinto "Archive"; }`)
	id := assertBareID(t, stdout, "filters create")
	cleanupRun(t, fmt.Sprintf("Delete filter: proton-cli mail filters delete -- %s", id),
		"mail", "filters", "delete", "--", id)
}

func TestStdoutIDCalendarCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-cal"
	stdout, _ := runOKStderr(t, "calendar", "calendars", "create",
		"--name", name, "--color", "#8080FF")
	id := assertBareID(t, stdout, "calendars create")
	cleanupRun(t, fmt.Sprintf("Delete calendar: proton-cli calendar calendars delete -- %s", id),
		"calendar", "calendars", "delete", "--", id)
}

func TestStdoutIDContactCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-contact"
	stdout, _ := runOKStderr(t, "contacts", "create",
		"--name", name, "--email", "t@x.invalid")
	id := assertBareID(t, stdout, "contacts create")
	cleanupRun(t, fmt.Sprintf("Delete contact: proton-cli contacts delete -- %s", id),
		"contacts", "delete", "--", id)
}

func TestStdoutIDVaultCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-vault"
	stdout, _ := runOKStderr(t, "pass", "vaults", "create", "--name", name)
	id := assertBareID(t, stdout, "vaults create")
	cleanupRun(t, fmt.Sprintf("Delete vault: proton-cli pass vaults delete -- %s", id),
		"pass", "vaults", "delete", "--", id)
}

func TestStdoutIDPassItemCreate(t *testing.T) {
	skipIfNoCredentials(t)
	name := testID() + "-stid-item"
	stdout, _ := runOKStderr(t, "pass", "items", "create",
		"--type", "note", "--name", name, "--note", "x")
	id := assertBareID(t, stdout, "pass items create")
	_ = id
	cleanupRun(t, fmt.Sprintf("Delete pass item: proton-cli pass items delete %s", name),
		"pass", "items", "delete", name)
}

func TestStdoutIDCalendarEventCreate(t *testing.T) {
	skipIfNoCredentials(t)
	title := testID() + "-stid-event"
	start := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	stdout, _ := runOKStderr(t, "calendar", "events", "create",
		"--calendar", "Default",
		"--title", title,
		"--start", start,
		"--duration", "30m")
	_ = assertBareID(t, stdout, "events create")
	cleanupRun(t, fmt.Sprintf("Delete event by title: proton-cli calendar events delete %q", title),
		"calendar", "events", "delete", title)
}
