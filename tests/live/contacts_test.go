package live

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Contacts: the fields one carries, the references that find it, and the round
// trip through a vCard file.

func TestContactsList(t *testing.T) {
	stdout := runOK(t, "contacts", "list")
	assertContains(t, stdout, "NAME")
}

func TestContactsCRUD(t *testing.T) {
	name := testID() + "-contact"
	email := "test+" + name + "@example.invalid"

	stdout := runOK(t, "contacts", "create",
		"--name", name,
		"--email", email,
		"--phone", "+1234567890")
	id := assertBareID(t, stdout, "contacts create")
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete -- %s", id),
		"contacts", "delete", "--", id)

	// Get by explicit ID
	got := runOK(t, "contacts", "get", "--", id)
	assertField(t, got, "Name:", name)
	assertField(t, got, "Email:", email)
	// Signature: a contact we just created is signed with our own user key.
	assertField(t, got, "Signature:", "verified")
	assertField(t, got, "Phone:", "+1234567890")

	// Update phone
	runOK(t, "contacts", "update", "--phone", "+9999999999", "--", id)
	got2 := runOK(t, "contacts", "get", "--", id)
	assertField(t, got2, "Phone:", "+9999999999")
	// name/email unchanged
	assertField(t, got2, "Name:", name)
}

func TestContactsGetByNameRef(t *testing.T) {
	name := testID() + "-refname"
	stdout := runOK(t, "contacts", "create", "--name", name, "--email", "t@x.invalid")
	id := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete -- %s", id),
		"contacts", "delete", "--", id)

	got := runOK(t, "contacts", "get", name)
	assertField(t, got, "Name:", name)
}

func TestContactsGetByEmailRef(t *testing.T) {
	name := testID() + "-refmail"
	email := "t+" + name + "@x.invalid"
	stdout := runOK(t, "contacts", "create", "--name", name, "--email", email)
	id := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete -- %s", id),
		"contacts", "delete", "--", id)

	got := runOK(t, "contacts", "get", email)
	assertField(t, got, "Email:", email)
}

func TestContactsDeleteByRef(t *testing.T) {
	name := testID() + "-refdel"
	runOK(t, "contacts", "create", "--name", name, "--email", "t@x.invalid")

	runOK(t, "contacts", "delete", name)
	_, _, code := run(t, "contacts", "get", name)
	if code != 3 {
		t.Errorf("expected exit 3 after delete, got %d", code)
	}
}

func TestContactsNotFound(t *testing.T) {
	_, _, code := run(t, "contacts", "get", "no-such-contact-"+testID())
	if code != 3 {
		t.Errorf("expected exit 3 for unknown contact, got %d", code)
	}
}

func TestContactsAmbiguous(t *testing.T) {
	prefix := testID() + "-ambig"
	for i := 0; i < 2; i++ {
		stdout := runOK(t, "contacts", "create",
			"--name", fmt.Sprintf("%s-%d", prefix, i),
			"--email", fmt.Sprintf("a%d@x.invalid", i))
		id := strings.TrimSpace(stdout)
		cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete -- %s", id),
			"contacts", "delete", "--", id)
	}
	_, _, code := run(t, "contacts", "get", prefix)
	if code != 4 {
		t.Errorf("expected exit 4 for ambiguous match, got %d", code)
	}
}

func TestContactsMultiValue(t *testing.T) {
	name := testID() + "-mv"
	e1 := testID() + "-1@example.com"
	e2 := testID() + "-2@example.com"
	cid := strings.TrimSpace(runOK(t, "contacts", "create", "--name", name,
		"--email", e1, "--email", e2, "--phone", "+1234567890",
		"--job-title", "CTO", "--birthday", "1990-01-31", "--address", "Vienna", "--website", "https://x.example"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", cid),
		"contacts", "delete", "--", cid)

	got := runOK(t, "contacts", "get", "--", cid)
	assertContains(t, got, e1)
	assertContains(t, got, e2)
	assertContains(t, got, "CTO")
	assertContains(t, got, "Vienna")
}

// Export and import are each other's inverse, which is the only thing that makes
// an export a backup.
func TestContactsExportAndImportRoundTrip(t *testing.T) {
	name := testID() + "-vcf"
	email := testID() + "@example.com"
	id := strings.TrimSpace(runOK(t, "contacts", "create",
		"--name", name, "--email", email, "--phone", "+43 1 234567", "--note", "Likes tea"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	// Out as one stream: a .vcf file is cards one after another.
	out := runOK(t, "contacts", "export", "--dest", "-", "--", id)
	for _, want := range []string{"BEGIN:VCARD", "END:VCARD", name, email} {
		if !strings.Contains(out, want) {
			t.Errorf("the export is missing %q", want)
		}
	}
	// Both cards merged into one: the address is signed, the phone and note are
	// encrypted, and a file has to carry all of them.
	if !strings.Contains(out, "+43 1 234567") || !strings.Contains(out, "Likes tea") {
		t.Error("the export should merge the signed and encrypted cards into one vCard")
	}
	if got := strings.Count(out, "BEGIN:VCARD"); got != 1 {
		t.Errorf("one contact exported as %d cards", got)
	}

	// And back in. A card carries the UID of the contact it is, so reading the
	// file back changes that contact rather than making a second one - which is
	// the difference between a backup and a way to fill an address book with
	// duplicates.
	edited := strings.Replace(out, "Likes tea", "Likes coffee", 1)
	file := filepath.Join(t.TempDir(), "one.vcf")
	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runOK(t, "contacts", "import", file)

	var copies []string
	for _, row := range runJSONArray(t, "contacts", "list", "--keyword", name) {
		m, _ := row.(map[string]interface{})
		if cid, _ := m["id"].(string); cid != "" {
			copies = append(copies, cid)
		}
	}
	for _, cid := range copies {
		if cid != id {
			cleanupRun(t, fmt.Sprintf("Delete imported contact: proton contacts delete %s", cid),
				"contacts", "delete", "--", cid)
		}
	}
	if len(copies) != 1 {
		t.Fatalf("re-importing an export made %d contacts out of one", len(copies))
	}
	if got := runJSON(t, "contacts", "get", "--", id); !strings.Contains(fmt.Sprint(got["note"]), "coffee") {
		t.Errorf("the edited note did not come back: %v", got["note"])
	}
}

// A property this tool has no flag for still has to survive the trip, since the
// stored card goes out and in whole.
func TestContactsImportKeepsAPropertyTheCLICannotSet(t *testing.T) {
	name := testID() + "-anniversary"
	card := strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0",
		"FN:" + name,
		"EMAIL:" + testID() + "@example.com",
		"ANNIVERSARY:2015-06-20",
		"END:VCARD",
	}, "\r\n")
	file := filepath.Join(t.TempDir(), "extra.vcf")
	if err := os.WriteFile(file, []byte(card), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	runOK(t, "contacts", "import", file)

	var id string
	for _, row := range runJSONArray(t, "contacts", "list", "--keyword", name) {
		m, _ := row.(map[string]interface{})
		id, _ = m["id"].(string)
	}
	if id == "" {
		t.Fatal("the imported contact is not in the address book")
	}
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	if out := runOK(t, "contacts", "export", "--dest", "-", "--", id); !strings.Contains(out, "ANNIVERSARY:2015-06-20") {
		t.Errorf("a property the CLI has no flag for was dropped:\n%s", out)
	}
}

// A card with nothing to file it under is skipped and named, and the rest of the
// file still lands.
func TestContactsImportSkipsWhatItCannotFile(t *testing.T) {
	name := testID() + "-partial"
	file := filepath.Join(t.TempDir(), "partial.vcf")
	content := strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0", "NOTE:no name and no address", "END:VCARD",
		"BEGIN:VCARD", "VERSION:4.0", "FN:" + name, "EMAIL:" + testID() + "@example.com", "END:VCARD",
	}, "\r\n")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, stderr := runOKStderr(t, "contacts", "import", file)
	if !strings.Contains(stderr, "Skipped") {
		t.Errorf("the unusable card should be reported, got: %s", stderr)
	}

	rows := runJSONArray(t, "contacts", "list", "--keyword", name)
	if len(rows) != 1 {
		t.Fatalf("the usable card should still have landed; found %d", len(rows))
	}
	m, _ := rows[0].(map[string]interface{})
	id, _ := m["id"].(string)
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)
}

// Every field the CLI can set comes back the way it was written, kinds included.
func TestContactsCarryEveryFieldTheyAreGiven(t *testing.T) {
	name := testID() + "-fields"
	id := strings.TrimSpace(runOK(t, "contacts", "create",
		"--name", name,
		"--first-name", "Jane", "--last-name", "Roe", "--nickname", "Janey",
		"--email", "work:"+testID()+"@example.com",
		"--phone", "cell:+43 1 234567",
		"--address", "home:1 Example St",
		"--website", "work:https://example.com",
		"--organization", "Acme", "--job-title", "Engineer", "--role", "Team lead",
		"--birthday", "1990-01-31", "--anniversary", "2015-06-20",
		"--gender", "female", "--language", "de-AT", "--timezone", "Europe/Vienna",
		"--note", "Likes tea"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	got := runJSON(t, "contacts", "get", "--", id)
	for field, want := range map[string]string{
		"first_name": "Jane", "last_name": "Roe", "nickname": "Janey",
		"org": "Acme", "title": "Engineer", "role": "Team lead",
		"birthday": "1990-01-31", "anniversary": "2015-06-20",
		"gender": "female", "language": "de-AT", "timezone": "Europe/Vienna",
		"note": "Likes tea",
	} {
		if v, _ := got[field].(string); v != want {
			t.Errorf("%s = %q, want %q", field, v, want)
		}
	}
	// A kind is stored and printed back the way --phone accepts it.
	for field, want := range map[string]string{
		"phones": "cell:+43 1 234567", "addresses": "home:1 Example St",
		"urls": "work:https://example.com",
	} {
		list, _ := got[field].([]interface{})
		if len(list) != 1 {
			t.Errorf("%s = %v, want one value", field, list)
			continue
		}
		if v, _ := list[0].(string); v != want {
			t.Errorf("%s = %q, want %q", field, v, want)
		}
	}
}

// Editing one field must not drop the rest - including properties this tool has
// no flag for, which are read off the stored card and written back.
func TestContactsUpdateKeepsWhatItDoesNotMention(t *testing.T) {
	name := testID() + "-keep"
	id := strings.TrimSpace(runOK(t, "contacts", "create",
		"--name", name, "--email", testID()+"@example.com",
		"--organization", "Acme", "--note", "Original", "--birthday", "1990-01-31"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", id),
		"contacts", "delete", "--", id)

	runOK(t, "contacts", "update", "--note", "Changed", "--", id)

	got := runJSON(t, "contacts", "get", "--", id)
	if v, _ := got["note"].(string); v != "Changed" {
		t.Errorf("note = %q, want Changed", v)
	}
	for field, want := range map[string]string{"org": "Acme", "birthday": "1990-01-31"} {
		if v, _ := got[field].(string); v != want {
			t.Errorf("editing the note dropped %s: got %q, want %q", field, v, want)
		}
	}
}

// A merge folds duplicates into the oldest, which keeps its identity so anything
// referring to it still does.
func TestContactsMergeFoldsDuplicatesIntoTheKeptOne(t *testing.T) {
	shared := testID() + "@example.com"
	keepName := testID() + "-keep"
	first := strings.TrimSpace(runOK(t, "contacts", "create",
		"--name", keepName, "--email", shared, "--note", "Original note"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", first),
		"contacts", "delete", "--", first)
	second := strings.TrimSpace(runOK(t, "contacts", "create",
		"--name", testID()+"-dupe", "--email", shared,
		"--note", "Different note", "--organization", "Acme"))
	cleanupRun(t, fmt.Sprintf("Delete contact: proton contacts delete %s", second),
		"contacts", "delete", "--", second)

	runOK(t, "contacts", "merge", "--yes")

	got := runJSON(t, "contacts", "get", "--", first)
	if v, _ := got["note"].(string); v != "Original note" {
		t.Errorf("note = %q; the kept contact's value must win", v)
	}
	if v, _ := got["org"].(string); v != "Acme" {
		t.Errorf("org = %q; a field only the other had should be folded in", v)
	}
	// The folded-in contact is gone, so its cleanup would fail; that is expected
	// and the deletion above is what proves it.
	if _, _, code := run(t, "contacts", "get", "--", second); code == 0 {
		t.Error("the folded-in contact should have been removed")
	}
}
