package live

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// Aliases: an address Proton mints that forwards to a real mailbox.
//
// Making one is what Proton meters hardest here, so the suite reads a fixture
// alias rather than making its own - except in the one test about making one.
// Writing *as* an alias needs a plan, so that runs on the paid account, against
// an alias somebody created by hand: an address cannot be un-minted.

// Writing as an alias, which a free plan will not do. The address Proton mints
// is the whole feature: without it a reply leaves from the real mailbox.
func TestPassAliasContacts(t *testing.T) {
	// The alias is the one fixture the suite reads and never makes: an address
	// cannot be un-minted, so minting a throwaway one every run would spend one
	// of somebody's for good. Contacts are reversible, so they are what this
	// makes and removes.
	ref, address := paidAlias(t)

	email := fmt.Sprintf("pcli-%d@example.com", time.Now().UnixNano()%1_000_000_000)
	stdout, stderr := runOKStderrPaid(t, "pass", "aliases", "contacts", "create", "--name", "Seller", ref, email)
	id := strings.TrimSpace(stdout)
	cleanupRunPaid(t, "Delete alias contact: proton pass aliases contacts delete "+ref+" "+id,
		"pass", "aliases", "contacts", "delete", ref, id)
	if !strings.Contains(stderr, "Write to") {
		t.Errorf("creating a contact should say which address writes as the alias, got: %s", truncateOutput(stderr))
	}

	// The address Proton mints for the contact is the whole feature: a reply
	// sent there reaches them as though the alias had written it, so the real
	// mailbox is never shown.
	contact := aliasContact(t, ref, id)
	if reverse, _ := contact["reverse_alias"].(string); !strings.Contains(reverse, "@") {
		t.Errorf("no address to write to came back: %v", contact["reverse_alias"])
	}
	if got, _ := contact["email"].(string); got != email {
		t.Errorf("the contact is %q, want %q", got, email)
	}
	if address == "" || !strings.Contains(address, "@") {
		t.Errorf("the fixture alias has no address of its own: %q", address)
	}

	runOKPaid(t, "pass", "aliases", "contacts", "block", ref, id)
	if blocked, _ := aliasContact(t, ref, id)["blocked"].(bool); !blocked {
		t.Error("the contact should be blocked")
	}
	runOKPaid(t, "pass", "aliases", "contacts", "allow", ref, id)
	if blocked, _ := aliasContact(t, ref, id)["blocked"].(bool); blocked {
		t.Error("the contact should not be blocked any more")
	}
}

// aliasContact is one of an alias's contacts, by the ID it was created under.
//
// The alias is long-lived, so it may carry more than this test's contact - which
// is why the lookup is by ID rather than by being the only one there.
func aliasContact(t *testing.T, ref, id string) map[string]interface{} {
	t.Helper()
	var seen []string
	for _, row := range runJSONArrayPaid(t, "pass", "aliases", "contacts", "list", ref) {
		contact, _ := row.(map[string]interface{})
		// Proton numbers a contact rather than giving it an opaque ID, so it
		// arrives from JSON as a float. What `contacts create` printed is the
		// same number as text.
		got := fmt.Sprint(contact["id"])
		if f, ok := contact["id"].(float64); ok {
			got = strconv.FormatInt(int64(f), 10)
		}
		if got == id {
			return contact
		}
		seen = append(seen, got)
	}
	t.Fatalf("the alias has no contact %s; it has %v", id, seen)
	return nil
}

func TestPassAliasMailbox(t *testing.T) {
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
		stdout, _, exit, err := runAs(account.Secondary, nil,
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
		if _, stderr, code, err := runAs(account.Secondary, nil, "--yes",
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
	stdout, _, code, err := runAs(account.Secondary, nil, "--output", "json",
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

func TestPassAliasOptions(t *testing.T) {
	// Both kinds come back in one table, told apart by KIND rather than by two
	// headed sections.
	stdout := runOK(t, "pass", "aliases", "options")
	assertContains(t, stdout, "KIND")
	assertContains(t, stdout, "suffix")
	assertContains(t, stdout, "mailbox")
}

// What the listing offers has to be something --suffix will take.
//
// Proton mints the word in front of a suffix afresh on every request, so a
// listing that showed the whole thing would be offering a value that has already
// stopped working. Making an alias here would prove it end to end and would also
// spend one of the handful Proton allows in an hour, so what is checked is the
// shape: --suffix takes the domain.
func TestPassAliasOptionsOfferDomains(t *testing.T) {
	suffixes := 0
	for _, row := range runJSONArray(t, "pass", "aliases", "options") {
		o, _ := row.(map[string]interface{})
		if kind, _ := o["kind"].(string); kind != "suffix" {
			continue
		}
		suffixes++
		value, _ := o["value"].(string)
		if strings.Contains(value, "@") || strings.HasPrefix(value, ".") {
			t.Errorf("the suffix %q is a whole address ending, which Proton regenerates "+
				"on every request and refuses when it is passed back; --suffix takes the domain", value)
		}
		if !strings.Contains(value, ".") {
			t.Errorf("the suffix %q does not look like a domain", value)
		}
	}
	if suffixes == 0 {
		t.Fatal("no suffix was offered, so no alias could be made")
	}
}

// An alias is an address Proton makes for you, so making one is its own request
// rather than another kind of item written locally.
func TestPassAliasesCreate(t *testing.T) {
	name := testID() + "-alias"
	// The prefix becomes part of an email address, so it is short and plain, and
	// the item's name carries the suite's own prefix instead.
	prefix := fmt.Sprintf("pcli-%d", time.Now().UnixNano()%1_000_000_000)
	stdout, stderr := runOKStderr(t, "pass", "aliases", "create", "--prefix", prefix, "--name", name)
	ref := strings.TrimSpace(stdout)
	cleanupRun(t, fmt.Sprintf("Delete alias: proton pass items delete %s", name),
		"pass", "items", "delete", name)
	if !looksLikePairRef(ref) {
		t.Fatalf("expected SHARE_ID/ITEM_ID on stdout, got %q", ref)
	}

	// The address is what an alias is for, so creating one says which address it
	// made rather than the prefix it was asked for.
	assertContains(t, stderr, "@")
	said := addressIn(t, stderr)

	got := runJSON(t, "pass", "items", "get", "--", ref)
	if got["type"] != "alias" || got["name"] != name {
		t.Errorf("the item reads type %v name %v, want an alias called %s", got["type"], got["name"], name)
	}
	// The address Proton made from the prefix is the whole point of an alias, so
	// the item has to carry it. Proton appends a word of its own to the prefix.
	address, _ := got["alias"].(string)
	if !strings.HasPrefix(address, prefix) || !strings.Contains(address, "@") {
		t.Fatalf("alias address is %q, want an address built from %q", address, prefix)
	}
	if said != address {
		t.Errorf("creating said %q but the alias is %q", said, address)
	}
	assertContains(t, runOK(t, "pass", "items", "get", "--", ref), address)
	assertContains(t, runOK(t, "pass", "aliases", "list"), address)
}

// An address is half an answer: an alias is a route, so reading one says where
// its mail arrives, whether it is receiving at all, and what it has carried.
func TestPassAliasesGetShowsTheRoute(t *testing.T) {
	ref, _ := alias(t)

	got := runJSON(t, "pass", "items", "get", "--", ref)
	if got["alias_status"] != "enabled" {
		t.Errorf("a new alias reads status %v, want enabled", got["alias_status"])
	}
	boxes, _ := got["alias_mailboxes"].([]interface{})
	if len(boxes) == 0 {
		t.Fatalf("the alias forwards nowhere: %v", got)
	}
	if mailbox, _ := boxes[0].(string); !strings.Contains(mailbox, "@") {
		t.Errorf("it forwards to %q, want an address", mailbox)
	}
	if _, ok := got["alias_activity"].(map[string]interface{}); !ok {
		t.Errorf("no activity on the alias: %v", got)
	}
	assertField(t, runOK(t, "pass", "items", "get", "--", ref), "Forwards To:", boxes[0].(string))
}

// An alias that starts attracting spam is switched off, not deleted: deleting it
// burns the address, and nothing brings it back.
func TestPassAliasesDisableAndEnable(t *testing.T) {
	ref, address := alias(t)
	cleanupRun(t, "Switch the shared alias back on: proton pass aliases enable "+ref,
		"pass", "aliases", "enable", "--", ref)

	_, stderr := runOKStderr(t, "pass", "aliases", "disable", "--", ref)
	assertContains(t, stderr, "Disabled alias")
	if got := runJSON(t, "pass", "items", "get", "--", ref); got["alias_status"] != "disabled" {
		t.Errorf("after disabling, the alias reads %v", got["alias_status"])
	}
	// The list knows without asking after each address, so it says so too.
	listed := runOK(t, "pass", "aliases", "list")
	assertContains(t, listed, "disabled")
	assertContains(t, listed, address)

	runOK(t, "pass", "aliases", "enable", "--", ref)
	if got := runJSON(t, "pass", "items", "get", "--", ref); got["alias_status"] != "enabled" {
		t.Errorf("after enabling, the alias reads %v", got["alias_status"])
	}
}

// Where an alias forwards and what it sends as are fields of the item, so they
// are changed by the same command that changes every other field.
func TestPassItemsUpdateAliasRoute(t *testing.T) {
	ref, _ := alias(t)
	mailbox := runJSON(t, "pass", "items", "get", "--", ref)["alias_mailboxes"].([]interface{})[0].(string)
	sender := "Jane " + testID()
	// The display name is the shared alias's, so it goes back to having none.
	cleanupRun(t, "Clear the shared alias's display name: proton pass items update --display-name \"\" "+ref,
		"pass", "items", "update", "--display-name", "", "--", ref)

	runOK(t, "pass", "items", "update", "--mailbox", mailbox, "--display-name", sender, "--", ref)

	got := runJSON(t, "pass", "items", "get", "--", ref)
	boxes, _ := got["alias_mailboxes"].([]interface{})
	if len(boxes) != 1 || boxes[0] != mailbox {
		t.Errorf("the alias forwards to %v, want %q", boxes, mailbox)
	}
	if got["alias_display_name"] != sender {
		t.Errorf("the alias sends as %v, want %q", got["alias_display_name"], sender)
	}
}

// addressIn picks the email address out of a confirmation line.
func addressIn(t *testing.T, line string) string {
	t.Helper()
	for _, word := range strings.Fields(strings.TrimSuffix(strings.TrimSpace(line), ".")) {
		if strings.Contains(word, "@") {
			return strings.TrimSuffix(word, ".")
		}
	}
	t.Fatalf("no address in %q", line)
	return ""
}

// An alias is a route rather than a mailbox of its own, so what an account can
// build one out of - where it arrives, and what comes after the @ - is worth
// being able to see.
func TestPassAliasMailboxesAndDomains(t *testing.T) {
	boxes := runJSONArray(t, "pass", "settings", "mailboxes", "list")
	if len(boxes) == 0 {
		t.Fatal("an account always forwards its aliases somewhere, but no mailbox came back")
	}
	first, _ := boxes[0].(map[string]interface{})
	if email, _ := first["email"].(string); !strings.Contains(email, "@") {
		t.Errorf("a mailbox should be an address, got %q", email)
	}
	// The one an alias goes to by default has to be usable, or nothing arrives.
	verified := false
	for _, row := range boxes {
		m, _ := row.(map[string]interface{})
		if d, _ := m["default"].(bool); d {
			verified, _ = m["verified"].(bool)
		}
	}
	if !verified {
		t.Error("the default mailbox is not verified, so aliases would receive nothing")
	}

	domains := runJSONArray(t, "pass", "settings", "domains", "list")
	if len(domains) == 0 {
		t.Fatal("no alias domain came back, so no alias could be made")
	}
	for _, row := range domains {
		d, _ := row.(map[string]interface{})
		if name, _ := d["domain"].(string); !strings.Contains(name, ".") {
			t.Errorf("a domain should look like one, got %q", name)
		}
	}
}

// Only an alias has contacts, and saying so costs no request.
func TestPassAliasContactsRefuseANonAlias(t *testing.T) {
	_, stderr, code := run(t, "pass", "aliases", "contacts", "list", "GitHub")
	if code == 0 {
		t.Fatal("a login is not an alias, but listing its contacts was allowed")
	}
	assertContains(t, stderr, "not an alias")
}
