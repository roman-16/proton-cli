package live

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// The response contract, across every app: how a record is rendered, which
// stream it goes to, and what --dry-run does and does not do. One file, because
// it is one contract, and the tests here are the ones that hold for every
// command rather than for one collection.
//
// The other half of the contract is asserted where a command is exercised: the
// exit codes by the tests that provoke them, and the stdout=ID convention by
// assertBareID where a thing is created.

// ── --output yaml ──
//
// The keys are the json tags, so one consumer reads either format.

func TestOutputYAMLSnakeCase(t *testing.T) {
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "1", "--output", "yaml")
	// Non-omitempty keys only; from_name drops out when the sender has no display name.
	for _, want := range []string{"from_address", "num_attachments"} {
		if !strings.Contains(stdout, want+":") {
			t.Errorf("expected YAML key %q, got:\n%s", want, truncateOutput(stdout))
		}
	}
	// And NOT the Go-field lowercased alternatives
	for _, bad := range []string{"fromaddress:", "fromname:", "numattachments:"} {
		if strings.Contains(stdout, bad) {
			t.Errorf("unexpected YAML key %q (indicates yaml lib ignored json tags)", bad)
		}
	}
}

// The raw escape hatch keeps a number a number: a Code of 1000 must not come
// back as 1000.0 because it passed through a float on the way.

func TestOutputYAMLRawAPIKeepsIntegers(t *testing.T) {
	stdout := runOK(t, "--output", "yaml", "api", "GET", "/core/v4/users")
	// Code: 1000 (int) rather than 1000.0
	intRe := regexp.MustCompile(`(?m)^Code:\s+\d+$`)
	floatRe := regexp.MustCompile(`(?m)^Code:\s+\d+\.\d+`)
	if !intRe.MatchString(stdout) {
		t.Errorf("expected integer Code in YAML output, got:\n%s", truncateOutput(stdout))
	}
	if floatRe.MatchString(stdout) {
		t.Error("Code rendered as float; json.Number conversion regressed")
	}
}

// ── --output text, the default ──

func TestOutputTextIsDefault(t *testing.T) {
	stdout := runOK(t, "mail", "messages", "list", "--page-size", "1")
	// Table output has a separator line with ─ chars
	if !strings.Contains(stdout, "─") {
		t.Error("expected table output by default")
	}
	// And NOT a JSON brace
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Error("default output looks like JSON")
	}
}

// Every listing is valid JSON, whichever app it belongs to.

func TestOutputJSONParsesEverywhere(t *testing.T) {
	cases := [][]string{
		{"mail", "messages", "list", "--page-size", "1"},
		{"mail", "settings", "labels", "list"},
		{"mail", "settings", "addresses", "list"},
		{"contacts", "list"},
		{"calendar", "settings", "calendars", "list"},
		{"pass", "vaults", "list"},
	}
	for _, args := range cases {
		stdout := runOK(t, append(args, "--output", "json")...)
		var v any
		if err := json.Unmarshal([]byte(stdout), &v); err != nil {
			t.Errorf("%v: not valid JSON: %v", args, err)
		}
	}
}

// ── --dry-run ──
//
// It says what would happen and does none of it. The flag is structural rather
// than remembered - every mutation goes through kit.Mutate or kit.Create - so
// what these check is the promise at the surface, one shape of mutation each.

func TestDryRunLabelCreate(t *testing.T) {
	name := testID() + "-dryrun"
	_, stderr := runOKStderr(t, "--dry-run", "mail", "settings", "labels", "create",
		"--name", name, "--color", "#8080FF")
	assertContains(t, stderr, "Dry run")

	list := runOK(t, "mail", "settings", "labels", "list")
	if strings.Contains(list, name) {
		t.Errorf("dry-run created a label: %q appears in list", name)
	}
}

func TestDryRunFolderCreate(t *testing.T) {
	path := "/" + testID() + "-dryrun"
	_, stderr := runOKStderr(t, "--dry-run", "drive", "items", "create", path)
	assertContains(t, stderr, "Dry run")

	list := runOK(t, "drive", "items", "list")
	name := strings.TrimPrefix(path, "/")
	if strings.Contains(list, name) {
		t.Errorf("dry-run created a folder: %q appears in listing", name)
	}
}

func TestDryRunContactsCreate(t *testing.T) {
	name := testID() + "-dryrun-contact"
	_, stderr := runOKStderr(t, "--dry-run", "contacts", "create",
		"--name", name, "--email", "t@x.invalid")
	assertContains(t, stderr, "Dry run")

	_, _, code := run(t, "contacts", "get", name)
	if code != 3 {
		t.Error("dry-run should not create the contact")
	}
}

// ── consent ──
//
// `run` passes the command line through untouched, so these see what a cron job
// sees: no terminal, and therefore a question that has to become an error rather
// than a wait. The helpers that demand success add --yes, which is why the guard
// needs checking on purpose here rather than incidentally everywhere else.

// A permanent deletion refuses to happen unattended, and the thing it was asked
// to delete is still there afterwards.
func TestDeleteWithoutConsentRefusesAndChangesNothing(t *testing.T) {
	name := testID() + "-consent"
	runOK(t, "mail", "settings", "labels", "create", "--name", name, "--color", "#8080FF")
	cleanupRun(t, "Delete: proton mail settings labels delete "+name,
		"mail", "settings", "labels", "delete", name)

	_, stderr, code := run(t, "mail", "settings", "labels", "delete", name)
	if code != 1 {
		t.Fatalf("want exit 1 for a refused deletion, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "cannot be undone")
	assertContains(t, stderr, "--yes")
	assertContains(t, stderr, "--dry-run")
	// The refusal names the label: a question about "1 label" is one nobody can
	// actually answer.
	assertContains(t, stderr, name)

	assertContains(t, runOK(t, "mail", "settings", "labels", "list"), name)
}

// Trashing something named by hand is not worth a question: it is reversible,
// and the user typed the reference.
func TestTrashOfANamedReferenceNeedsNoConsent(t *testing.T) {
	path := "/" + testID() + "-consent-trash"
	runOK(t, "drive", "items", "create", path)
	cleanupRun(t, "Delete: proton drive items delete "+path,
		"drive", "items", "delete", path)
	// Taken before the trash, because a trashed item has no path any more - and
	// its name arrives encrypted, so the ID is the only way back to this exact
	// folder rather than to whatever else the suite has left in there.
	linkID, _ := runJSON(t, "drive", "items", "get", path)["link_id"].(string)
	if linkID == "" {
		t.Fatal("drive items get should report the folder's link ID")
	}

	if _, stderr, code := run(t, "drive", "items", "trash", path); code != 0 {
		t.Fatalf("trashing a named path should not ask, got exit %d: %s", code, stderr)
	}

	// Put it back, so the cleanup registered above can find it by path.
	runOK(t, "drive", "trash", "restore", "--", linkID)
}

// Trashing what a filter found is, because the filter chose them and nobody has
// read the list.
func TestTrashOfAFilteredSelectionNeedsConsent(t *testing.T) {
	_, stderr, code := run(t, "mail", "messages", "trash", "--unread", "--limit", "1")
	if code == 0 {
		// Nothing matched, so there was nothing to ask about.
		assertContains(t, stderr, "Nothing to move")
		return
	}
	if code != 1 {
		t.Fatalf("want exit 1 for a refused filtered trash, got %d (stderr: %s)", code, stderr)
	}
	assertContains(t, stderr, "--yes")
	// A trash is recoverable, so the refusal must not claim otherwise.
	assertNotContains(t, stderr, "cannot be undone")
}

// --dry-run answers the question in a safer form, so it never has to ask it.
func TestDryRunNeedsNoConsent(t *testing.T) {
	_, stderr, code := run(t, "--dry-run", "mail", "messages", "delete", "--unread", "--limit", "1")
	if code != 0 {
		t.Fatalf("a dry run should never need consent, got exit %d: %s", code, stderr)
	}
	assertContains(t, stderr, "Dry run")
}
