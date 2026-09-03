//go:build paid

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"
)

// Running against an account somebody depends on.
//
// The paid half of Proton's feature set cannot be reached from a free account,
// and buying a second subscription to test with is not a reasonable thing to
// ask. So these tests run against a real account, under rules that make the run
// reversible rather than merely careful:
//
//   - They are not part of `just test`. Without the `paid` build tag they are
//     not compiled, so an ordinary run cannot reach the account by accident.
//   - Nothing seeds it. The fixtures the rest of the suite reads are never
//     written here.
//   - A test acts only on what it made itself. It does not list the account's
//     own calendars, vaults, messages or files and act on what it finds.
//   - A handful of commands are refused outright - see offLimitsOnPaid - because
//     what they change cannot be read first and put back.
//   - The account is photographed before the run and compared after it. Anything
//     left behind, or missing, fails the run and is named.
//
// The last of those is the one that catches a mistake nobody predicted, which is
// why it exists: the other four are promises, and this one checks them.

const paidBuild = true

// ── the canary ──

// photograph is what the account held, as one line per thing.
type photograph map[string][]string

// paidBefore is the account as it was when the run started.
var paidBefore photograph

// collections are what the canary counts, each read with a command that only
// reads. Between them they cover everything a test here could plausibly leave
// behind: a calendar, a vault, an item, a label, a folder, a filter, an address,
// a contact, a file at the root of the drive.
//
// A collection nobody can list is not covered, which is the honest limit of
// this: it says what it saw, and a test that changes something invisible to
// every listing is a test that has to be argued for on its own.
var collections = []struct {
	name string
	args []string
	id   string
	// label is the field a row is named by in the report, so what turned up is
	// readable rather than an identifier nobody can place.
	label string
}{
	// The newest of the inbox, because a run can leave mail behind: sharing
	// something makes Proton write to you when the other side answers, and no
	// setting on this end stops it. Only the newest are photographed - the whole
	// inbox is somebody's real mail and not this test's business.
	{"inbox", []string{"mail", "messages", "list", "--folder", "inbox", "--page-size", "25"}, "id", "subject"},
	{"calendars", []string{"calendar", "settings", "calendars", "list"}, "id", "name"},
	{"vaults", []string{"pass", "vaults", "list"}, "share_id", "name"},
	{"pass items", []string{"pass", "items", "list"}, "item_id", "name"},
	{"labels", []string{"mail", "settings", "labels", "list"}, "id", "name"},
	{"folders", []string{"mail", "settings", "folders", "list"}, "id", "name"},
	{"filters", []string{"mail", "settings", "filters", "list"}, "id", "name"},
	{"addresses", []string{"mail", "settings", "addresses", "list"}, "id", "name"},
	{"contacts", []string{"contacts", "list"}, "id", "name"},
	{"alias mailboxes", []string{"pass", "settings", "mailboxes", "list"}, "id", "email"},
	{"drive root", []string{"drive", "items", "list", "/"}, "id", "name"},
}

// settingsPages are the values a run must leave exactly as it found them. They
// are compared whole, so a single key changed anywhere shows up.
var settingsPages = [][]string{
	{"account", "settings", "get"},
	{"mail", "settings", "get"},
	{"calendar", "settings", "get"},
	{"drive", "settings", "get"},
}

// runStarted is when the run began, so the sweep below can tell mail this run
// caused from mail that was already there.
var runStarted int64

// noticesProtonSends are the subjects Proton writes to you about, unprompted,
// when a test shares something and the other side answers. Nothing on this end
// can turn them off, so they are swept instead.
//
// Matched by sender and subject and bounded to this run, and moved to the trash
// rather than deleted, because it is somebody's real mail.
var noticesProtonSends = []string{
	"has accepted your invitation",
	"has declined your invitation",
	"shared a vault with you",
	"shared a calendar with you",
}

// sweepNotices trashes the mail this run caused Proton to send.
//
// It runs before the photograph below, so what it clears is not then reported as
// something the run left behind. Anything it cannot clear stays, and the
// photograph names it.
//
// Proton writes a moment after the other side answers rather than as it answers,
// so this waits for as many notices as the run caused - runAs counts them - and
// stops as soon as it has them. A run that caused none sweeps once and returns.
func sweepNotices() {
	want := int(noticesCaused.Load())
	var swept int
	waitFor(90*time.Second, 5*time.Second, func() bool {
		swept += sweepOnce()
		return swept >= want
	})
	if swept < want {
		fmt.Fprintf(os.Stderr,
			"Proton was told to write %d notices and %d arrived in time to be cleared\n", want, swept)
	}
}

// sweepOnce trashes the notices that have arrived so far, and says how many.
func sweepOnce() int {
	body, _, code, err := runAs(paid, nil, asJSON([]string{
		"mail", "messages", "list", "--folder", "all",
		"--from", "no-reply@proton.me", "--page-size", "50",
	})...)
	if err != nil || code != 0 {
		return 0
	}
	rows, ok := rowsOf(body)
	if !ok {
		return 0
	}
	var cleared int
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		at, _ := m["time"].(float64)
		if int64(at) < runStarted {
			continue
		}
		subject, _ := m["subject"].(string)
		var ours bool
		for _, notice := range noticesProtonSends {
			if strings.Contains(subject, notice) {
				ours = true
			}
		}
		if !ours {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		if _, _, code, err := runAs(paid, nil, "--yes", "mail", "messages", "trash", "--", id); err != nil || code != 0 {
			fmt.Fprintf(os.Stderr, "could not clear the notice %q Proton sent; it is in the inbox\n", subject)
			continue
		}
		cleared++
	}
	return cleared
}

// snapshotPaid photographs the account before any test runs.
func snapshotPaid() {
	runStarted = time.Now().Unix()
	paidBefore = takePhotograph()
	if paidBefore == nil {
		fmt.Fprintln(os.Stderr,
			"could not photograph the paid account, so a run could not be checked afterwards")
		os.Exit(1)
	}
}

// comparePaid photographs it again and reports whether nothing moved.
func comparePaid() bool {
	sweepNotices()
	after := takePhotograph()
	if after == nil {
		fmt.Fprintln(os.Stderr,
			"\ncould not photograph the paid account after the run; check it by hand")
		return false
	}

	var problems []string
	for _, key := range sortedKeys(paidBefore, after) {
		gone, added := difference(paidBefore[key], after[key])
		for _, v := range added {
			problems = append(problems, fmt.Sprintf("  %s: left behind  %s", key, v))
		}
		for _, v := range gone {
			problems = append(problems, fmt.Sprintf("  %s: missing      %s", key, v))
		}
	}
	if len(problems) == 0 {
		return true
	}
	sort.Strings(problems)
	fmt.Fprintf(os.Stderr, `
╔══════════════════════════════════════════════════════════════╗
║  ⚠️  THE PAID ACCOUNT DID NOT COME BACK AS IT WAS            ║
╠══════════════════════════════════════════════════════════════╣
%s
╚══════════════════════════════════════════════════════════════╝
`, strings.Join(problems, "\n"))
	return false
}

// takePhotograph reads every collection and settings page, or nil if it could
// not read one: a partial photograph would report a change that never happened.
func takePhotograph() photograph {
	out := photograph{}
	for _, c := range collections {
		rows, ok := readRows(c.args, c.id, c.label)
		if !ok {
			fmt.Fprintf(os.Stderr, "could not read %s from the paid account\n", c.name)
			return nil
		}
		out[c.name] = rows
	}
	for _, page := range settingsPages {
		body, _, code, err := runAs(paid, nil, asJSON(page)...)
		if err != nil || code != 0 {
			fmt.Fprintf(os.Stderr, "could not read %v from the paid account\n", page)
			return nil
		}
		out[strings.Join(page, " ")] = []string{strings.Join(strings.Fields(body), " ")}
	}
	return out
}

// readRows lists a collection and returns one line per row.
func readRows(args []string, idKey, labelKey string) ([]string, bool) {
	body, _, code, err := runAs(paid, nil, asJSON(args)...)
	if err != nil || code != 0 {
		return nil, false
	}
	rows, ok := rowsOf(body)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		id, _ := m[idKey].(string)
		if id == "" {
			id, _ = m["id"].(string)
		}
		label, _ := m[labelKey].(string)
		out = append(out, strings.TrimSpace(label+" "+id))
	}
	sort.Strings(out)
	return out, true
}

// rowsOf unwraps a collection envelope without a *testing.T, because the
// photographs are taken before any test starts and after the last one ends.
func rowsOf(stdout string) ([]interface{}, bool) {
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, false
	}
	for key, value := range env {
		if key == "count" {
			continue
		}
		if arr, ok := value.([]interface{}); ok {
			return arr, true
		}
	}
	// An empty collection is an envelope with no array in it, which is a
	// photograph of nothing rather than a failure to take one.
	if _, ok := env["count"]; ok {
		return nil, true
	}
	return nil, false
}

// difference reports what is in before but not after, and the other way round.
func difference(before, after []string) (gone, added []string) {
	had := map[string]int{}
	for _, v := range before {
		had[v]++
	}
	for _, v := range after {
		if had[v] > 0 {
			had[v]--
			continue
		}
		added = append(added, v)
	}
	for v, n := range had {
		for i := 0; i < n; i++ {
			gone = append(gone, v)
		}
	}
	return gone, added
}

func sortedKeys(a, b photograph) []string {
	seen := map[string]bool{}
	for k := range a {
		seen[k] = true
	}
	for k := range b {
		seen[k] = true
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── the runners ──

// runOKStderrSecondary is runOKStderr for the second account, for the answers
// that are on the other stream.
func runOKStderrSecondary(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := runSecondary(t, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed as the secondary account (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

// memberEmail is the address Proton reports for the owner of a share, or for
// somebody who is not.
//
// It is read out of the listing rather than assumed, because an account is a
// member under its primary Proton address whatever address it signs in as.
func memberEmail(t *testing.T, members any, owner bool) string {
	t.Helper()
	rows, _ := members.([]any)
	for _, row := range rows {
		m, _ := row.(map[string]any)
		if is, _ := m["owner"].(bool); is == owner {
			email, _ := m["email"].(string)
			return email
		}
	}
	return ""
}

func runPaid(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	out, errOut, code, err := runAs(paid, nil, args...)
	if err != nil {
		t.Fatalf("failed to run %v as the paid account: %v", args, err)
	}
	return out, errOut, code
}

func runOKPaid(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runPaid(t, append([]string{"--yes"}, args...)...)
	if code != 0 {
		t.Fatalf("command %v failed as the paid account (exit %d):\n\tstdout: %s\n\tstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout
}

// runOKStderrPaid is runOKPaid when what the command said matters too.
func runOKStderrPaid(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := runPaid(t, append([]string{"--yes"}, args...)...)
	if code != 0 {
		t.Fatalf("command %v failed as the paid account (exit %d):\n\tstdout: %s\n\tstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

func runJSONArrayPaid(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOKPaid(t, asJSON(args)...))
}

func runJSONPaid(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOKPaid(t, asJSON(args)...))
}

// cleanupRunPaid removes something a paid test made. A failure here is loud,
// because what is left behind is on somebody's real account.
func cleanupRunPaid(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanup(t, description, func() error {
		_, stderr, code, err := runAs(paid, nil, append([]string{"--yes"}, args...)...)
		if err != nil {
			return err
		}
		if code != 0 {
			return fmt.Errorf("exit %d: %s", code, stderr)
		}
		return nil
	})
}

// ── the tests ──

// calendarSharing is how Proton refuses sharing a calendar to an account without
// the plan for it.
var calendarSharing = gate{
	feature: "calendar sharing",
	words:   []string{"upgrade", "paid", "subscription"},
}

// vaultSharing is how Proton refuses sharing a vault to an account without the
// plan for it.
var vaultSharing = gate{
	feature: "vault sharing",
	words:   []string{"upgrade", "paid", "subscription"},
}

// itemSharing is how Proton refuses an item offered to somebody on a free plan.
// It is the invitee's plan being refused rather than this account's, and the
// account the suite shares with is a free one - so the round trip needs a second
// paid account, and there is one.
var itemSharing = gate{feature: "item sharing", code: "2011"}

// secureLinks is how Proton refuses a secure link to an account without the
// plan for it.
var secureLinks = gate{
	feature: "secure links",
	words:   []string{"upgrade", "paid", "subscription"},
}

// calendarSubscription is how Proton refuses a subscribed calendar to an account
// without the plan for it. It lives here rather than with the other gates
// because only a paid test names it, and a gate nothing names is dead weight.
var calendarSubscription = gate{
	feature: "subscribed calendars",
	words:   []string{"upgrade", "paid", "subscription"},
}

// Every test here says Paid in its name.
//
// `just test-paid` selects them with -run Paid, so one named anything else would
// never run - and would look like it had passed. This reads the file rather than
// trusting the convention.
func TestEveryPaidTestSaysSoInItsName(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("paid_on_test.go")
	if err != nil {
		t.Fatalf("read this file: %v", err)
	}
	for _, line := range strings.Split(string(src), "\n") {
		if !strings.HasPrefix(line, "func Test") {
			continue
		}
		name := strings.TrimPrefix(strings.SplitN(line, "(", 2)[0], "func ")
		if !strings.Contains(name, "Paid") {
			t.Errorf("%s does not say Paid, so `just test-paid` would not run it", name)
		}
	}
}

// The canary has to have actually photographed something.
//
// A photograph that came back empty would compare equal to another empty one
// and pass every run while checking nothing, which is the failure mode of every
// guard that reads the world rather than the code.
func TestPaidCanarySawTheAccount(t *testing.T) {
	t.Parallel()
	if len(paidBefore) == 0 {
		t.Fatal("nothing was photographed before the run")
	}
	for _, c := range collections {
		if _, ok := paidBefore[c.name]; !ok {
			t.Errorf("%s was not photographed", c.name)
		}
	}
	for _, page := range settingsPages {
		key := strings.Join(page, " ")
		if v := paidBefore[key]; len(v) == 0 || v[0] == "" {
			t.Errorf("%s was not photographed", key)
		}
	}
}

// And the comparison has to notice a change.
func TestPaidCanaryNoticesAChange(t *testing.T) {
	t.Parallel()
	gone, added := difference([]string{"a", "b"}, []string{"b", "c"})
	if len(gone) != 1 || gone[0] != "a" {
		t.Errorf("missing items came out as %v, want [a]", gone)
	}
	if len(added) != 1 || added[0] != "c" {
		t.Errorf("new items came out as %v, want [c]", added)
	}
	if g, a := difference([]string{"a"}, []string{"a"}); len(g) != 0 || len(a) != 0 {
		t.Errorf("an unchanged account reported %v / %v", g, a)
	}
	// Two of the same thing is not one of it.
	if _, a := difference([]string{"a"}, []string{"a", "a"}); len(a) != 1 {
		t.Errorf("a duplicate went unnoticed: %v", a)
	}
}

// An account in the paid slot has to actually be on a paid plan.
//
// Proton says so in the session's scopes, and this is the cheapest possible way
// to find out: without it, free credentials put in the paid variables would make
// every gated test fail with Proton's refusal, which reads exactly like the
// feature being broken.
func TestPaidAccountIsOnAPaidPlan(t *testing.T) {
	t.Parallel()

	scopes, _ := runJSONPaid(t, "account", "get")["scopes"].([]interface{})
	for _, s := range scopes {
		if s == "paid" {
			return
		}
	}
	t.Fatalf("the account in PROTON_CLI_TEST_PAID_USER is not on a paid plan; "+
		"its scopes are %v", scopes)
}

// Pass Monitor reports which of your addresses have turned up in somebody
// else's data breach. It is read-only: it says what has already happened.
func TestPaidPassBreachesAreListedAndRead(t *testing.T) {
	t.Parallel()

	rows := runJSONArrayPaid(t, "pass", "breaches", "list")
	if len(rows) == 0 {
		t.Skip("this account has no watched addresses to read")
	}

	// Worst first, so the reason to run it is answered by the first row.
	var last float64 = -1
	var withBreaches string
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		n, _ := m["breaches"].(float64)
		if last >= 0 && n > last {
			t.Errorf("addresses came back %v before %v; the worst should lead", last, n)
		}
		last = n
		email, _ := m["email"].(string)
		if email == "" {
			t.Error("a watched address came back with no address")
		}
		if n > 0 && withBreaches == "" {
			withBreaches = email
		}
	}

	if withBreaches == "" {
		t.Skip("no watched address has a breach to read")
	}
	// The record says which breaches, which is what tells somebody what to
	// change. The values are this account's, so only the shape is asserted.
	shown := runJSONPaid(t, "pass", "breaches", "get", withBreaches)
	list, _ := shown["breach_list"].([]interface{})
	if len(list) == 0 {
		t.Fatalf("%s reports breaches but names none", withBreaches)
	}
	first, _ := list[0].(map[string]interface{})
	if name, _ := first["name"].(string); name == "" {
		t.Error("a breach came back with no name")
	}
	severity, _ := first["severity"].(string)
	switch severity {
	case "low", "medium", "high":
	default:
		t.Errorf("severity came back as %q, want one of low, medium, high", severity)
	}
}

// A subscribed calendar is filled from an address Proton fetches rather than
// from events made here, which is a paid feature.
//
// It makes its own calendar and deletes it again, so the account comes back as
// it was - and the canary checks that rather than taking this test's word.
func TestPaidCalendarSubscription(t *testing.T) {
	t.Parallel()

	// An address Proton can actually fetch, so what is being tested is
	// subscribing rather than the address being unreachable. Checked by hand
	// against the validate endpoint, which answers 0 for it.
	const feed = "https://www.officeholidays.com/ics/austria"

	name := testID() + "-subscribed"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create",
		"--name", name, "--url", feed)
	skipIfPlanRefuses(t, calendarSubscription, code, stderr)
	ref := strings.TrimSpace(out)
	if ref == "" {
		t.Fatalf("create returned no ID: %s", truncateOutput(stderr))
	}
	cleanupRunPaid(t, "Delete the subscribed calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	shown := runJSONPaid(t, "calendar", "settings", "calendars", "get", ref)
	if kind, _ := shown["kind"].(string); kind != "subscribed" {
		t.Errorf("the calendar came back as %q, want subscribed", kind)
	}
}

// An address Proton cannot read is refused before the calendar is made, so
// nobody is left with an empty calendar that never fills.
func TestPaidCalendarSubscriptionRefusesAnAddressItCannotRead(t *testing.T) {
	t.Parallel()

	name := testID() + "-bad-feed"
	_, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create",
		"--name", name, "--url", "https://example.com/not-a-calendar.ics")
	if code == 0 {
		t.Fatal("an address holding no calendar was accepted")
	}
	// The refusal has to be about the address. A plan refusal here would mean
	// the account is not the one it claims to be, which its own test covers.
	if calendarSubscription.refusedByPlan(stderr) {
		t.Fatalf("refused for want of a plan, on an account that has one: %s", truncateOutput(stderr))
	}
	assertContains(t, stderr, "nothing at that address")
	// Proton's own account of it is more specific than any wording here, so it
	// is passed through rather than replaced.
	assertContains(t, stderr, "404")
	// Nothing was made, so there is nothing to clean up - which the canary will
	// confirm at the end of the run.
}

// A secure link shows one item to somebody with no Proton account. The item
// stays encrypted: the key that opens it travels in the URL fragment, which is
// why the whole URL is the secret.
//
// The link is made against an item this test creates and both are removed again.
func TestPaidPassSecureLink(t *testing.T) {
	t.Parallel()

	name := testID() + "-link"
	out, stderr, code := runPaid(t, "--yes", "pass", "items", "create",
		"--name", name, "--username", "jane",
		"--secret-file", secretFile(t, "password", "hunter2"))
	if code != 0 {
		t.Fatalf("could not make an item to link to: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete item: proton pass items delete "+ref, "pass", "items", "delete", ref)

	shown, stderr, code := runPaid(t, "--yes", "pass", "links", "create", ref, "--expires", "1h", "--views", "2")
	skipIfPlanRefuses(t, secureLinks, code, stderr)

	// The URL carries the key after a '#', which is what a browser never sends
	// to Proton - so a link without one would be a link nothing can open.
	if !strings.Contains(shown, "#") {
		t.Errorf("the link carries no key: %s", truncateOutput(shown))
	}
	// The warning belongs on stderr, so capturing the link does not capture it.
	assertContains(t, stderr, "Anyone with this link")

	rows := runJSONArrayPaid(t, "pass", "links", "list")
	var linkID string
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		if id, _ := m["item_id"].(string); !strings.Contains(ref, id) {
			continue
		}
		linkID, _ = m["link_id"].(string)
		// The URL carries the key that opens the item, so a listing does not.
		if url, _ := m["url"].(string); url != "" {
			t.Errorf("a link listing carries the URL that opens the item: %q", url)
		}
	}
	if linkID == "" {
		t.Fatal("the link this test made is not in the listing")
	}
	// Proton stores the link key sealed under the item's own, so the whole URL
	// can be put back together - which is what makes a mislaid link recoverable
	// rather than lost. It takes a command that says so.
	back, warned := runOKStderrPaid(t, "pass", "links", "get", linkID)
	if !strings.Contains(back, "#") {
		t.Errorf("`links get` could not rebuild the link: %q", back)
	}
	assertContains(t, warned, "Anyone with this link")
	// The same URL is part of how that item is shared.
	assertContains(t, runOKPaid(t, "pass", "items", "share", "get", ref), "#")
	cleanupRunPaid(t, "Revoke link: proton pass links revoke "+linkID, "pass", "links", "revoke", linkID)
}

// Sharing a calendar hands somebody the key that opens it, encrypted to theirs
// and signed with yours. This is the whole round trip: the paid account makes a
// calendar, gives it to the second free account, and takes it back.
//
// The calendar is this test's own and is deleted at the end, so nothing of the
// account's own is shared even for a moment.
func TestPaidCalendarSharing(t *testing.T) {
	t.Parallel()

	name := testID() + "-shared"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar to share: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the shared calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "calendar", "settings", "calendars", "share",
		"add", ref, secondaryEmail())
	skipIfPlanRefuses(t, calendarSharing, code, stderr)
	// Deleting the calendar takes the invitation with it, so the clean-up above
	// covers this even if the test stops before withdrawing it by hand.

	// Until it is answered the invitation is pending, and the other account sees
	// nothing.
	var invited bool
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); !strings.EqualFold(email, secondaryEmail()) {
			continue
		}
		invited = true
		if status, _ := m["status"].(string); status != "pending" {
			t.Errorf("a fresh invitation is %q, want pending", status)
		}
		if access, _ := m["access"].(string); access != "viewer" {
			t.Errorf("access is %q, want viewer", access)
		}
	}
	if !invited {
		t.Fatal("the second account is not listed on the calendar it was given")
	}

	// The other side takes it, which is the half that proves the key it was
	// handed actually opens the calendar.
	var invitationID string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "calendar", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n != name {
				continue
			}
			invitationID, _ = m["id"].(string)
			return invitationID != ""
		}
		return false
	})
	if invitationID == "" {
		t.Fatal("the invitation never reached the second account")
	}
	runOKSecondary(t, "calendar", "invitations", "accept", "--", invitationID)

	// Once accepted it is a calendar like any other on that account, which is
	// only true if the passphrase it was given opened.
	var got bool
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "calendar", "settings", "calendars", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n == name {
				got = true
				return true
			}
		}
		return false
	})
	if !got {
		t.Error("the calendar did not appear on the account that accepted it")
	}

	// And it is listed as a member rather than an invitation now, so ending it
	// goes through the other endpoint.
	var active bool
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); !strings.EqualFold(email, secondaryEmail()) {
			continue
		}
		if status, _ := m["status"].(string); status == "active" {
			active = true
		}
	}
	if !active {
		t.Error("after accepting, the second account is not an active member")
	}

	runOKPaid(t, "calendar", "settings", "calendars", "share", "remove", ref, secondaryEmail())
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); strings.EqualFold(email, secondaryEmail()) {
			t.Error("the second account still has the calendar after being removed")
		}
	}
}

// An offer can be taken back before it is answered, which is a different
// endpoint from ending a membership somebody is already using.
func TestPaidCalendarSharingWithdrawnBeforeAnswer(t *testing.T) {
	t.Parallel()

	name := testID() + "-withdrawn"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "calendar", "settings", "calendars", "share",
		"add", ref, secondaryEmail())
	skipIfPlanRefuses(t, calendarSharing, code, stderr)

	// Nobody has answered, so this withdraws the invitation rather than ending a
	// membership.
	runOKPaid(t, "calendar", "settings", "calendars", "share", "remove", ref, secondaryEmail())
	for _, row := range runJSONArrayPaid(t, "calendar", "settings", "calendars", "share", "list", ref) {
		m, _ := row.(map[string]interface{})
		if email, _ := m["email"].(string); strings.EqualFold(email, secondaryEmail()) {
			t.Error("the invitation is still listed after being withdrawn")
		}
	}
}

// An invitation can be turned down, which takes it out of the way without
// opening anything.
func TestPaidCalendarInvitationDeclined(t *testing.T) {
	t.Parallel()

	name := testID() + "-declined"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "calendar", "settings", "calendars", "share",
		"add", ref, secondaryEmail())
	skipIfPlanRefuses(t, calendarSharing, code, stderr)

	var invitationID string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "calendar", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n != name {
				continue
			}
			invitationID, _ = m["id"].(string)
			return invitationID != ""
		}
		return false
	})
	if invitationID == "" {
		t.Skip("the invitation did not arrive, so there is nothing to turn down")
	}
	runOKSecondary(t, "calendar", "invitations", "decline", "--", invitationID)

	// Turning it down does not put the calendar on that account.
	for _, row := range runJSONArraySecondary(t, "calendar", "settings", "calendars", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n == name {
			t.Error("a declined calendar turned up on the account anyway")
		}
	}
}

// A calendar can only be given to another Proton account: what is handed over is
// encrypted to the recipient's key, and an address Proton holds no keys for has
// none to encrypt to.
func TestPaidCalendarSharingNeedsAProtonAddress(t *testing.T) {
	t.Parallel()

	name := testID() + "-outside"
	out, stderr, code := runPaid(t, "--yes", "calendar", "settings", "calendars", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a calendar: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete the calendar: proton calendar settings calendars delete "+ref,
		"calendar", "settings", "calendars", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "calendar", "settings", "calendars", "share",
		"add", ref, "nobody@example.com")
	if code == 0 {
		t.Fatal("an address outside Proton was accepted")
	}
	// Proton answers the key lookup first for an address it does not hold, so
	// the refusal is its own sentence rather than this tool's. Either way it
	// says the address is the problem, which is what somebody needs to read.
	if !strings.Contains(stderr, "not a Proton address") &&
		!strings.Contains(stderr, "address does not exist") {
		t.Errorf("the refusal does not name the address as the problem: %s", truncateOutput(stderr))
	}
}

// Sharing a vault hands somebody every rotation of the key that opens it. This
// is the whole round trip, and the assertion that matters is the last one: an
// item in the vault reads on the other account, which is only true if the keys
// it was given actually open what is in there.
func TestPaidPassVaultSharing(t *testing.T) {
	t.Parallel()

	name := testID() + "-shared-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault to share: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	// Something in it, so the other side has more to open than an empty vault.
	secret := testID() + "-in-shared-vault"
	runOKPaid(t, "pass", "items", "create", "--vault", vault, "--name", secret,
		"--username", "jane", "--secret-file", secretFile(t, "password", "hunter2"))

	_, stderr, code = runPaid(t, "--yes", "pass", "vaults", "share", "add", vault, secondaryEmail())
	skipIfPlanRefuses(t, vaultSharing, code, stderr)

	// The other side sees the offer, and can read the vault's name before taking
	// it - that much travels with the invitation.
	var token string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if v, _ := m["vault"].(string); v != name {
				continue
			}
			token, _ = m["id"].(string)
			return token != ""
		}
		return false
	})
	if token == "" {
		t.Fatal("the offer never reached the second account, or its vault could not be named")
	}
	runOKSecondary(t, "pass", "invitations", "accept", "--", token)

	// The item reads on the other account, which is the proof the keys opened.
	var found bool
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "items", "list") {
			m, _ := row.(map[string]interface{})
			if n, _ := m["name"].(string); n == secret {
				found = true
				return true
			}
		}
		return false
	})
	if !found {
		t.Error("the shared vault's item did not read on the account that took it")
	}

	// Somebody who accepted is a member, which is a different thing from an
	// invitation and the only one whose access can be changed in place.
	shared := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if !strings.Contains(fmt.Sprintf("%v", shared["members"]), secondaryEmail()) {
		t.Fatalf("the second account accepted the vault and is not a member: %v", shared["members"])
	}
	if !strings.Contains(fmt.Sprintf("%v", shared["members"]), "owner") {
		t.Errorf("the owner is not among the members: %v", shared["members"])
	}

	// A backup is of what you own. Somebody else's vault is theirs to back up,
	// and an archive that leaves one out says how many.
	_, note := runOKStderrSecondary(t, "pass", "export", "--dest", "-")
	if !strings.Contains(note, "shared with you") {
		t.Errorf("the second account's export says nothing about the vault it does not own: %s", truncateOutput(note))
	}

	runOKPaid(t, "pass", "vaults", "share", "update", vault, secondaryEmail(), "--access", "manager")
	if !strings.Contains(fmt.Sprintf("%v", runJSONPaid(t, "pass", "vaults", "share", "get", vault)["members"]), "manager") {
		t.Error("the member's access did not change")
	}

	// Handing it over, and taking it back. Only an owner may transfer, so the
	// return trip is the other account's to make - and if it fails, the vault is
	// theirs and the clean-up says so loudly rather than leaving it unsaid.
	//
	// The address to hand it back to is the one Proton reports, which is the
	// account's primary Proton address and not necessarily the address it signs
	// in as.
	owner := memberEmail(t, shared["members"], true)
	if owner == "" {
		t.Fatalf("the vault has no owner among its members: %v", shared["members"])
	}
	runOKPaid(t, "pass", "vaults", "transfer", vault, secondaryEmail())
	cleanupRunSecondary(t, "Give the vault back: proton pass vaults transfer "+name+" "+owner,
		"pass", "vaults", "transfer", name, owner)
	if was := fmt.Sprint(runJSONPaid(t, "pass", "vaults", "get", vault)["owner"]); was != "false" {
		t.Errorf("after handing the vault over, owner = %s", was)
	}
	runOKSecondary(t, "pass", "vaults", "transfer", name, owner)
	if was := fmt.Sprint(runJSONPaid(t, "pass", "vaults", "get", vault)["owner"]); was != "true" {
		t.Errorf("after taking the vault back, owner = %s", was)
	}

	runOKPaid(t, "pass", "vaults", "share", "remove", vault, secondaryEmail())
	if left := runJSONPaid(t, "pass", "vaults", "share", "get", vault); strings.Contains(
		fmt.Sprintf("%v", left["members"]), secondaryEmail()) {
		t.Errorf("the member is still there after being removed: %v", left["members"])
	}
}

// One item, shared on its own: what travels is that item's key rather than the
// vault's, so the other account can open it and nothing around it.
//
// Proton allows an item invitation only to somebody on a paid plan, and the
// account the suite shares with is a free one, so this skips until there is a
// second paid account to answer it.
func TestPaidPassItemSharing(t *testing.T) {
	t.Parallel()

	name := testID() + "-shared-item"
	out, stderr, code := runPaid(t, "--yes", "pass", "items", "create", "--name", name,
		"--username", "jane", "--generate-password")
	if code != 0 {
		t.Fatalf("could not make an item to share: %s", truncateOutput(stderr))
	}
	ref := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete item: proton pass items delete "+ref, "pass", "items", "delete", ref)

	_, stderr, code = runPaid(t, "--yes", "pass", "items", "share", "add", ref, secondaryEmail())
	skipIfPlanRefuses(t, itemSharing, code, stderr)
	cleanupRunPaid(t, "Unshare item: proton pass items share remove "+ref+" "+secondaryEmail(),
		"pass", "items", "share", "remove", ref, secondaryEmail())

	// Offered, and on the record as offered.
	if !strings.Contains(fmt.Sprintf("%v", runJSONPaid(t, "pass", "items", "share", "get", ref)["invited"]),
		secondaryEmail()) {
		t.Fatal("the invitation is not in `items share get`")
	}
	var shared bool
	for _, row := range runJSONArrayPaid(t, "pass", "sharing", "list") {
		m, _ := row.(map[string]interface{})
		if fmt.Sprint(m["name"]) == name {
			shared = true
		}
	}
	if !shared {
		t.Error("an item this account has shared is not in its `sharing list`")
	}

	var token string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if fmt.Sprint(m["item_id"]) == "" || !strings.Contains(ref, fmt.Sprint(m["item_id"])) {
				continue
			}
			token, _ = m["id"].(string)
			return token != ""
		}
		return false
	})
	if token == "" {
		t.Fatal("the offer of one item never reached the second account")
	}
	runOKSecondary(t, "pass", "invitations", "accept", "--", token)

	// It is theirs to read, and in no vault of theirs - which is why it is listed
	// apart from the ones that are.
	var sharedRef string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "shared", "list") {
			m, _ := row.(map[string]interface{})
			if fmt.Sprint(m["name"]) != name {
				continue
			}
			sharedRef = fmt.Sprint(m["share_id"]) + "/" + fmt.Sprint(m["item_id"])
			return true
		}
		return false
	})
	if sharedRef == "" {
		t.Fatal("the item the second account accepted is not in its `shared list`")
	}
	assertContains(t, runOKSecondary(t, "pass", "items", "get", "--", sharedRef), "jane")
	for _, row := range runJSONArraySecondary(t, "pass", "items", "list") {
		if m, _ := row.(map[string]interface{}); fmt.Sprint(m["name"]) == name {
			t.Error("an item shared with the second account turned up in its `items list`")
		}
	}

	// A member now, whose access can change and be taken away.
	runOKPaid(t, "pass", "items", "share", "update", ref, secondaryEmail(), "--access", "editor")
	if !strings.Contains(fmt.Sprintf("%v", runJSONPaid(t, "pass", "items", "share", "get", ref)["members"]), "editor") {
		t.Error("the member's access to the item did not change")
	}
	runOKPaid(t, "pass", "items", "share", "remove", ref, secondaryEmail())
	if left := runJSONPaid(t, "pass", "items", "share", "get", ref); strings.Contains(
		fmt.Sprintf("%v", left["members"]), secondaryEmail()) {
		t.Errorf("the second account still holds the item: %v", left["members"])
	}
}

// An offer can be withdrawn before it is taken.
func TestPaidPassVaultShareWithdrawn(t *testing.T) {
	t.Parallel()

	name := testID() + "-withdrawn-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	_, stderr, code = runPaid(t, "--yes", "pass", "vaults", "share", "add", vault, secondaryEmail())
	skipIfPlanRefuses(t, vaultSharing, code, stderr)

	shared := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if !strings.Contains(fmt.Sprintf("%v", shared["invited"]), secondaryEmail()) {
		t.Fatalf("the offer is not on the vault it was made for: %v", shared["invited"])
	}
	runOKPaid(t, "pass", "vaults", "share", "remove", vault, secondaryEmail())
	left := runJSONPaid(t, "pass", "vaults", "share", "get", vault)
	if strings.Contains(fmt.Sprintf("%v", left["invited"]), secondaryEmail()) {
		t.Errorf("the offer is still there after being withdrawn: %v", left["invited"])
	}
}

// An offer can be turned down, which opens nothing.
func TestPaidPassVaultInviteDeclined(t *testing.T) {
	t.Parallel()

	name := testID() + "-declined-vault"
	out, stderr, code := runPaid(t, "--yes", "pass", "vaults", "create", "--name", name)
	if code != 0 {
		t.Fatalf("could not make a vault: %s", truncateOutput(stderr))
	}
	vault := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete vault: proton pass vaults delete "+vault,
		"pass", "vaults", "delete", vault)

	_, stderr, code = runPaid(t, "--yes", "pass", "vaults", "share", "add", vault, secondaryEmail())
	skipIfPlanRefuses(t, vaultSharing, code, stderr)

	var token string
	waitFor(60*time.Second, 3*time.Second, func() bool {
		for _, row := range runJSONArraySecondary(t, "pass", "invitations", "list") {
			m, _ := row.(map[string]interface{})
			if v, _ := m["vault"].(string); v != name {
				continue
			}
			token, _ = m["id"].(string)
			return token != ""
		}
		return false
	})
	if token == "" {
		t.Skip("the offer did not arrive, so there is nothing to turn down")
	}
	runOKSecondary(t, "pass", "invitations", "decline", "--", token)

	for _, row := range runJSONArraySecondary(t, "pass", "vaults", "list") {
		m, _ := row.(map[string]interface{})
		if n, _ := m["name"].(string); n == name {
			t.Error("a declined vault turned up on the account anyway")
		}
	}
}

// Contact groups, which a free plan does not include - which is why six built
// commands and five of Proton's endpoints had never been exercised by anything.
//
// Proton groups addresses rather than people, so this covers both ways of saying
// who is in one: the whole contact, and one of their addresses.
func TestPaidContactGroups(t *testing.T) {
	t.Parallel()

	gname := testID() + "-group"
	out, stderr, code := runPaid(t, "--yes", "contacts", "groups", "create",
		"--name", gname, "--color", "#8080FF")
	skipIfPlanRefuses(t, contactGroups, code, stderr)
	group := strings.TrimSpace(out)
	if group == "" {
		t.Fatalf("create returned no ID: %s", truncateOutput(stderr))
	}
	cleanupRunPaid(t, "Delete group: proton contacts groups delete "+group,
		"contacts", "groups", "delete", group)

	// A contact of this test's own, with two addresses, so the difference
	// between naming a person and naming an address is visible.
	work := testID() + "-work@example.com"
	home := testID() + "-home@example.com"
	cname := testID() + "-member"
	out = runOKPaid(t, "contacts", "create", "--name", cname,
		"--email", "work:"+work, "--email", "home:"+home)
	contact := strings.TrimSpace(out)
	cleanupRunPaid(t, "Delete contact: proton contacts delete "+contact,
		"contacts", "delete", contact)

	assertContains(t, runOKPaid(t, "contacts", "groups", "list"), gname)

	// Naming the contact takes all of their addresses in.
	runOKPaid(t, "contacts", "groups", "add", group, contact)
	if n := groupSize(t, group); n != 2 {
		t.Errorf("after adding the contact the group holds %d addresses, want 2", n)
	}
	runOKPaid(t, "contacts", "groups", "remove", group, contact)
	if n := groupSize(t, group); n != 0 {
		t.Errorf("after removing the contact the group holds %d addresses, want 0", n)
	}

	// --email narrows it to the ones named, which is the whole reason Proton
	// groups addresses rather than people.
	runOKPaid(t, "contacts", "groups", "add", group, contact, "--email", work)
	if n := groupSize(t, group); n != 1 {
		t.Errorf("after adding one address the group holds %d, want 1", n)
	}
	runOKPaid(t, "contacts", "groups", "remove", group, contact, "--email", work)
	if n := groupSize(t, group); n != 0 {
		t.Errorf("after removing that address the group holds %d, want 0", n)
	}

	// Renaming is the last thing a group does that nothing had tried.
	renamed := gname + "-renamed"
	runOKPaid(t, "contacts", "groups", "update", group, "--name", renamed)
	assertContains(t, runOKPaid(t, "contacts", "groups", "list"), renamed)
}

// groupSize is how many addresses a group holds. Membership lives on the
// address, so this is the group's own record rather than the listing.
func groupSize(t *testing.T, group string) int {
	t.Helper()
	shown := runJSONPaid(t, "contacts", "groups", "get", group)
	members, _ := shown["members"].([]interface{})
	return len(members)
}

// Writing as an alias, which a free plan will not do. The address Proton mints
// is the whole feature: without it a reply leaves from the real mailbox.
func TestPaidPassAliasContacts(t *testing.T) {
	t.Parallel()

	name := testID() + "-alias"
	prefix := fmt.Sprintf("pcli-%d", time.Now().UnixNano()%1_000_000_000)
	runOKPaid(t, "pass", "aliases", "create", "--prefix", prefix, "--name", name)
	cleanupRunPaid(t, "Delete alias: proton pass items delete "+name, "pass", "items", "delete", name)
	alias := name

	email := fmt.Sprintf("pcli-%d@example.com", time.Now().UnixNano()%1_000_000_000)
	stdout, stderr := runOKStderrPaid(t, "pass", "aliases", "contacts", "create", "--name", "Seller", alias, email)
	id := strings.TrimSpace(stdout)
	cleanupRunPaid(t, "Delete alias contact: proton pass aliases contacts delete "+alias+" "+id,
		"pass", "aliases", "contacts", "delete", alias, id)
	if !strings.Contains(stderr, "Write to") {
		t.Errorf("creating a contact should say which address writes as the alias, got: %s", truncateOutput(stderr))
	}

	rows := runJSONArrayPaid(t, "pass", "aliases", "contacts", "list", alias)
	if len(rows) != 1 {
		t.Fatalf("the alias should have one contact, got %d", len(rows))
	}
	contact, _ := rows[0].(map[string]interface{})
	if reverse, _ := contact["reverse_alias"].(string); !strings.Contains(reverse, "@") {
		t.Errorf("no address to write to came back: %v", contact["reverse_alias"])
	}
	if got, _ := contact["email"].(string); got != email {
		t.Errorf("the contact is %q, want %q", got, email)
	}

	runOKPaid(t, "pass", "aliases", "contacts", "block", alias, id)
	if blocked := contactField(t, alias, "blocked"); blocked != true {
		t.Error("the contact should be blocked")
	}
	runOKPaid(t, "pass", "aliases", "contacts", "allow", alias, id)
	if blocked := contactField(t, alias, "blocked"); blocked != false {
		t.Error("the contact should not be blocked any more")
	}
}

// contactField reads one field of an alias's only contact.
func contactField(t *testing.T, alias, field string) interface{} {
	t.Helper()
	rows := runJSONArrayPaid(t, "pass", "aliases", "contacts", "list", alias)
	if len(rows) != 1 {
		t.Fatalf("expected one contact, got %d", len(rows))
	}
	contact, _ := rows[0].(map[string]interface{})
	return contact[field]
}

// Adding an address for aliases to forward to, which a free plan will not do.
//
// The code Proton emails it is not part of this: everything under a mailbox's
// /verify is metered the way a brute-force guard is, and a suite that spends a
// code attempt on every run fails on that rather than on anything real. What is
// checked here is that the mailbox arrives unverified and can be taken away
// again, which is the part a run can repeat.
func TestPaidPassAliasMailbox(t *testing.T) {
	t.Parallel()

	// The second account's own address, because a mailbox has to be one somebody
	// could actually read the confirmation at.
	address := secondaryEmail()

	clearConfirmationMail(t)
	_, stderr := runOKStderrPaid(t, "pass", "settings", "mailboxes", "create", address)
	cleanup(t, "Trash the confirmation mail Proton sent the second account",
		func() error { return trashConfirmationMail() })
	cleanupRunPaid(t, "Delete alias mailbox: proton pass settings mailboxes delete "+address,
		"pass", "settings", "mailboxes", "delete", address)
	if !strings.Contains(stderr, "code") {
		t.Errorf("adding a mailbox should say a code is on its way, got: %s", truncateOutput(stderr))
	}

	// It receives nothing until it answers, and says so.
	if verified := mailboxField(t, address, "verified"); verified != false {
		t.Error("a new mailbox should arrive unverified, since it has not answered yet")
	}
	// Proton really did write to it, which is the half of the confirmation a run
	// can check without spending an attempt at the other half.
	if code := verificationCode(t); len(code) != 6 {
		t.Errorf("the confirmation mail carried %q, want six digits", code)
	}

	// The default is set to the one that already is it: the endpoint is worth
	// exercising, and moving a real account's default is not worth risking.
	runOKPaid(t, "pass", "settings", "mailboxes", "update", "--default", defaultMailbox(t))
}

// verificationCode reads the six digits out of the confirmation mail Proton
// sent the second account. The inbox is cleared of them before whatever causes
// one, so the mail this waits for is unambiguously that one.
func verificationCode(t *testing.T) string {
	t.Helper()
	var code string
	digits := regexp.MustCompile(`\b\d{6}\b`)
	found := waitFor(90*time.Second, 3*time.Second, func() bool {
		id := secondaryMessageAbout(confirmationSubject)
		if id == "" {
			return false
		}
		stdout, _, exit, err := runArgs(nil, "--profile", secondary,
			"mail", "messages", "get", "--body-only", "--", id)
		if err != nil || exit != 0 {
			return false
		}
		code = digits.FindString(stdout)
		return code != ""
	})
	if !found {
		t.Fatal("no confirmation code arrived within 90s")
	}
	return code
}

// confirmationSubject is what Proton calls the mail carrying the code.
const confirmationSubject = "confirm your mailbox"

// clearConfirmationMail empties the second account's inbox of confirmation mail
// and waits until it is gone, so the next one to arrive is the one being waited
// for. Two of these mails differ only in the digits inside them.
func clearConfirmationMail(t *testing.T) {
	t.Helper()
	if err := trashConfirmationMail(); err != nil {
		t.Fatalf("could not clear the confirmation mail already in the inbox: %v", err)
	}
	if !waitFor(30*time.Second, 2*time.Second, func() bool {
		return secondaryMessageAbout(confirmationSubject) == ""
	}) {
		t.Fatal("confirmation mail is still in the inbox after being trashed")
	}
}

// trashConfirmationMail moves every confirmation mail out of the inbox. It is
// trash rather than delete, like everything else this suite removes on an
// account somebody reads.
func trashConfirmationMail() error {
	for {
		id := secondaryMessageAbout(confirmationSubject)
		if id == "" {
			return nil
		}
		if _, stderr, code, err := runArgs(nil, "--profile", secondary, "--yes",
			"mail", "messages", "trash", "--", id); err != nil {
			return err
		} else if code != 0 {
			return fmt.Errorf("exit %d: %s", code, stderr)
		}
	}
}

// secondaryMessageAbout is the newest mail in the second account's inbox whose
// subject contains what is being waited for.
func secondaryMessageAbout(subject string) string {
	stdout, _, code, err := runArgs(nil, "--profile", secondary, "--output", "json",
		"mail", "messages", "list", "--folder", "inbox", "--page-size", "20")
	if err != nil || code != 0 {
		return ""
	}
	var data struct {
		Messages []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(stdout), &data) != nil {
		return ""
	}
	for _, m := range data.Messages {
		if strings.Contains(strings.ToLower(m.Subject), strings.ToLower(subject)) {
			return m.ID
		}
	}
	return ""
}

// mailboxField reads one field of a mailbox, by the address it is.
func mailboxField(t *testing.T, address, field string) interface{} {
	t.Helper()
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "mailboxes", "list") {
		m, _ := row.(map[string]interface{})
		if got, _ := m["email"].(string); got == address {
			return m[field]
		}
	}
	t.Fatalf("no mailbox called %s", address)
	return nil
}

// defaultMailbox is the address new aliases arrive in today.
func defaultMailbox(t *testing.T) string {
	t.Helper()
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "mailboxes", "list") {
		m, _ := row.(map[string]interface{})
		if isDefault, _ := m["default"].(bool); isDefault {
			email, _ := m["email"].(string)
			return email
		}
	}
	t.Fatal("no mailbox is the default one, which should not be possible")
	return ""
}
