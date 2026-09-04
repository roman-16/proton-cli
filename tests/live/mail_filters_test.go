package live

import (
	"fmt"
	"strings"
	"testing"
)

// Filters: a rule described on the command line, or a script written by hand.
//
// Proton generates the script from the conditions, so what these really check is
// that what it generated says what was asked for - the one failure here that
// would otherwise be invisible.

func TestMailFiltersCRUD(t *testing.T) {
	name := testID() + "-filter"
	sieve := `require ["fileinto"]; if header :contains "Subject" "xyz-never-matches-` + testID() + `" { fileinto "Archive"; }`

	id := assertBareID(t, runOK(t, "mail", "settings", "filters", "create",
		"--name", name, "--sieve", sieve), "filters create")
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete -- %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	assertContains(t, runOK(t, "mail", "settings", "filters", "list"), name)
	if got := filterStatus(t, id); got != 1 {
		t.Errorf("a new filter should be on, got status %d", got)
	}

	runOK(t, "mail", "settings", "filters", "disable", "--", id)
	if got := filterStatus(t, id); got != 0 {
		t.Errorf("status after disable = %d, want 0", got)
	}

	runOK(t, "mail", "settings", "filters", "enable", "--", id)
	if got := filterStatus(t, id); got != 1 {
		t.Errorf("status after enable = %d, want 1", got)
	}
}

// A filter described rather than written. Proton generates the script from the
// conditions, so what this really checks is that what it generated says what was
// asked for - the one failure here that would be invisible.
func TestMailFiltersFromConditions(t *testing.T) {
	name := testID() + "-rule"

	id := strings.TrimSpace(runOK(t, "mail", "settings", "filters", "create", "--name", name,
		"--match", "any",
		"--if", "subject contains xyz-never-matches-"+testID(),
		"--if", "sender not is nobody@example.com",
		"--if", "attachments contains",
		"--move-to", "Archive", "--mark-read"))
	if !looksLikeID(id) {
		t.Fatalf("expected bare ID on stdout, got %q", id)
	}
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete -- %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	sieve := filterSieve(t, id)
	for _, want := range []string{
		"anyof",                     // --match any
		`fileinto "Archive"`,        // --move-to
		`addflag "\\Seen"`,          // --mark-read
		"not address",               // the negated sender
		`exists "X-Attached"`,       // the attachment condition
		"vnd.proton.spam-threshold", // never runs on spam
	} {
		if !strings.Contains(sieve, want) {
			t.Errorf("the generated script is missing %q:\n%s", want, sieve)
		}
	}
}

// A rule can be rewritten in place. That matters because order decides which
// filter wins, so the alternative - delete and recreate - moves the filter to
// the end and changes what happens to mail.
func TestMailFiltersRewriteARuleInPlace(t *testing.T) {
	name := testID() + "-rewrite"

	id := strings.TrimSpace(runOK(t, "mail", "settings", "filters", "create", "--name", name,
		"--if", "subject contains before-"+testID(), "--move-to", "Archive"))
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete -- %s", id),
		"mail", "settings", "filters", "delete", "--", id)
	before := filterPriority(t, id)

	// By name, because every other filter verb takes one.
	runOK(t, "mail", "settings", "filters", "update", "--if", "sender not is nobody@example.com",
		"--label", "Receipts", "--star", name)

	shown := runOK(t, "mail", "settings", "filters", "get", "--", id)
	for _, want := range []string{"sender not is nobody@example.com", "Receipts", "Stars"} {
		assertContains(t, shown, want)
	}
	assertNotContains(t, shown, "before-")
	if after := filterPriority(t, id); after != before {
		t.Errorf("the filter moved in the order: priority %v before, %v after", before, after)
	}
	if got := filterStatus(t, id); got != 1 {
		t.Errorf("a rewritten filter should still be on, got status %d", got)
	}
}

// A script nothing in the CLI's grammar can describe is shown as itself, rather
// than as words that would not rebuild it.
func TestMailFiltersShowAScriptItCannotDescribe(t *testing.T) {
	name := testID() + "-script"
	sieve := `require ["fileinto"];` + "\n" + `if header :contains "X-Custom-Header" "` + testID() + `" { fileinto "Archive"; }`

	id := strings.TrimSpace(runOK(t, "mail", "settings", "filters", "create", "--name", name, "--sieve", sieve))
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete -- %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	shown := runOK(t, "mail", "settings", "filters", "get", "--", id)
	assertContains(t, shown, "X-Custom-Header")
	assertNotContains(t, shown, "Matches:")
}

// filterPriority is where a filter sits in the order filters run in.
func filterPriority(t *testing.T, id string) float64 {
	t.Helper()
	shown := runJSON(t, "--output", "json", "api", "GET", "/mail/v4/filters/"+id)
	filter, _ := shown["Filter"].(map[string]interface{})
	priority, _ := filter["Priority"].(float64)
	return priority
}

// filterSieve reads back the script Proton wrote for a filter.
func filterSieve(t *testing.T, id string) string {
	t.Helper()
	shown := runJSON(t, "--output", "json", "api", "GET", "/mail/v4/filters/"+id)
	filter, _ := shown["Filter"].(map[string]interface{})
	sieve, _ := filter["Sieve"].(string)
	if sieve == "" {
		t.Fatalf("no script came back for filter %s", id)
	}
	return sieve
}

// filterStatus reads one filter's on/off state, which the table spells as a yes
// or a no under ENABLED.
func filterStatus(t *testing.T, id string) int {
	t.Helper()
	for _, row := range runJSONArray(t, "--full-ids", "mail", "settings", "filters", "list") {
		f, _ := row.(map[string]interface{})
		if f["id"] == id {
			status, _ := f["status"].(float64)
			return int(status)
		}
	}
	t.Fatalf("filter %s is not in the list", id)
	return -1
}

func TestMailFiltersUpdate(t *testing.T) {
	name := testID() + "-filter"
	sieve := `require ["fileinto"]; if header :contains "Subject" "` + name + `" { fileinto "Archive"; }`
	id := strings.TrimSpace(runOK(t, "mail", "settings", "filters", "create", "--name", name, "--sieve", sieve))
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	newName := name + "-renamed"
	runOK(t, "mail", "settings", "filters", "update", "--name", newName, id)
	assertContains(t, runOK(t, "mail", "settings", "filters", "list"), newName)
}

// A filter ordinarily acts once, as mail arrives. Running it again over what is
// already here is the catching-up.
func TestMailFiltersApplyToExistingMail(t *testing.T) {
	name := testID() + "-apply"
	sieve := fmt.Sprintf("require [\"fileinto\"];\n# %s\nif header :contains \"Subject\" \"%s\" {\n  fileinto \"Archive\";\n}\n", name, name)
	stdout, stderr, code := run(t, "mail", "settings", "filters", "create", "--name", name, "--sieve", sieve)
	if code != 0 {
		t.Fatalf("filter create failed (exit %d): %s", code, truncateOutput(stderr))
	}
	id := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	// One apply, not two. Proton runs this over the whole mailbox as a background
	// job, meters it hard, and refuses a second while the first is going - so
	// asking twice teaches nothing the endpoint has not already answered and
	// spends an allowance the rest of the suite needs.
	runOKUntilFree(t, "mail", "settings", "filters", "apply", "--", id)
}

// A half-stated order is one nobody can predict, so naming some of them is
// refused rather than leaving the rest wherever they fell.
func TestMailFiltersReorderNeedsEveryFilter(t *testing.T) {
	name := testID() + "-order"
	sieve := fmt.Sprintf("require [\"fileinto\"];\n# %s\nif header :contains \"Subject\" \"%s\" {\n  fileinto \"Archive\";\n}\n", name, name)
	stdout, stderr, code := run(t, "mail", "settings", "filters", "create", "--name", name, "--sieve", sieve)
	if code != 0 {
		t.Fatalf("filter create failed (exit %d): %s", code, truncateOutput(stderr))
	}
	id := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete filter: proton mail settings filters delete %s", id),
		"mail", "settings", "filters", "delete", "--", id)

	all := runJSONArray(t, "mail", "settings", "filters", "list")
	if len(all) < 2 {
		// One filter is already in the only order there is.
		runOK(t, "mail", "settings", "filters", "reorder", "--", id, id)
		return
	}
	var ids []string
	for _, row := range all {
		m, _ := row.(map[string]interface{})
		if fid, _ := m["id"].(string); fid != "" {
			ids = append(ids, fid)
		}
	}
	runOK(t, append([]string{"mail", "settings", "filters", "reorder", "--"}, ids...)...)

	_, stderr, code = run(t, "mail", "settings", "filters", "reorder", "--", ids[0], ids[0])
	if code != 1 {
		t.Errorf("naming fewer filters than exist should exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "name every filter") {
		t.Errorf("the refusal should say to name them all, got: %s", stderr)
	}
}
