package live

import (
	"strings"
	"testing"
)

// Custom domains, on the account that can have one.
//
// Proton gates a custom domain behind a plan, so everything that reads one is
// the paid account's - and everything that writes acts on a domain the run made
// and nothing else: a subdomain of the account's own, carrying the suite's
// prefix, which nobody else can claim and which tests/paid is what keeps a
// delete pointed at.

// paidDomainSuffix is the account's own custom domain, which is what a domain
// the run makes is put under.
//
// It is read rather than made: a domain that verifies needs entries in somebody
// real's DNS, so the only verified domain a run will ever see is the one the
// account already has. Nothing is done to it.
func paidDomainSuffix(t *testing.T) string {
	t.Helper()
	rows := runJSONArrayPaid(t, "mail", "settings", "domains", "list")
	for _, row := range rows {
		d, _ := row.(map[string]interface{})
		if name, _ := d["domain"].(string); name != "" {
			return name
		}
	}
	t.Fatal("the paid account has no custom domain, and the suite never verifies one. " +
		"Add one once, by hand, in Proton's Domain names settings.")
	return ""
}

// aDomainTheRunMade adds a subdomain of the account's own and hands back its
// name, having registered its removal.
func aDomainTheRunMade(t *testing.T) string {
	t.Helper()
	name := testID() + "." + paidDomainSuffix(t)
	stdout := runOKPaid(t, "mail", "settings", "domains", "create", name)
	assertBareID(t, stdout, "domains create")
	cleanupRunPaid(t, "Delete domain: proton mail settings domains delete "+name,
		"mail", "settings", "domains", "delete", name)
	return name
}

// Every custom domain comes back with a status, a count of the addresses on it
// and whichever of them catches stray mail.
func TestMailSettingsDomainsList(t *testing.T) {
	stdout := runOKPaid(t, "mail", "settings", "domains", "list")
	for _, header := range []string{"DOMAIN", "STATUS", "DNS", "ADDRESSES", "CATCH-ALL"} {
		assertContains(t, stdout, header)
	}
	for _, row := range runJSONArrayPaid(t, "mail", "settings", "domains", "list") {
		d, _ := row.(map[string]interface{})
		switch d["status"] {
		case "active", "unverified", "warning":
		default:
			t.Errorf("a domain came back in a state nothing declares: %v", d["status"])
		}
	}
}

// What `get` is for is the entries somebody has to put in their DNS, so every
// one of them is there in full, with the verdict its check reached.
func TestMailSettingsDomainsGetReportsEveryDNSCheck(t *testing.T) {
	name := paidDomainSuffix(t)

	stdout := runOKPaid(t, "mail", "settings", "domains", "get", name)
	assertField(t, stdout, "Domain:", name)
	for _, check := range []string{"Verification:", "MX:", "SPF:", "DKIM:", "DMARC:"} {
		assertContains(t, stdout, check)
	}
	for _, entry := range []string{
		"protonmail-verification=",
		"mail.protonmail.ch", "mailsec.protonmail.ch",
		"v=spf1 include:_spf.protonmail.ch ~all",
		"_domainkey", "v=DMARC1; p=quarantine",
	} {
		assertContains(t, stdout, entry)
	}

	d := runJSONPaid(t, "mail", "settings", "domains", "get", name)
	checks, _ := d["dns"].([]interface{})
	if len(checks) != 5 {
		t.Fatalf("a domain reported %d checks, want 5: %v", len(checks), keysOf(d))
	}
	for _, check := range checks {
		g, _ := check.(map[string]interface{})
		if g["status"] == "" || g["status"] == nil {
			t.Errorf("the %v check reached no verdict", g["name"])
		}
	}
}

// A domain nobody has verified is the state every domain starts in, and the one
// thing it can say is what to go and add.
func TestMailSettingsDomainsCreateAndDelete(t *testing.T) {
	name := aDomainTheRunMade(t)

	d := runJSONPaid(t, "mail", "settings", "domains", "get", name)
	if d["status"] != "unverified" {
		t.Errorf("a domain nobody has verified came back as %v", d["status"])
	}
	if d["addresses"] != float64(0) {
		t.Errorf("a domain nobody has verified has %v addresses on it", d["addresses"])
	}
	checks, _ := d["dns"].([]interface{})
	first, _ := checks[0].(map[string]interface{})
	if first["name"] != "verification" || first["status"] != "missing" {
		t.Errorf("the first check on a fresh domain = %v", first)
	}
	entries, _ := first["records"].([]interface{})
	entry, _ := entries[0].(map[string]interface{})
	value, _ := entry["value"].(string)
	if entry["type"] != "TXT" || entry["host"] != "@" || !strings.HasPrefix(value, "protonmail-verification=") {
		t.Errorf("the entry that proves the domain is yours = %v", entry)
	}

	runOKPaid(t, "mail", "settings", "domains", "delete", name)
	if _, _, code := runPaid(t, "--yes", "mail", "settings", "domains", "get", name); code != 3 {
		t.Errorf("a deleted domain answers with exit %d, want 3", code)
	}
}

// The catch-all is written on the domain, and only an address on that domain may
// take it - which is judged before anything is sent.
func TestMailSettingsDomainsCatchAll(t *testing.T) {
	name := aDomainTheRunMade(t)

	runOKPaid(t, "mail", "settings", "domains", "update", name, "--clear-catch-all")
	if d := runJSONPaid(t, "mail", "settings", "domains", "get", name); d["catch_all"] != nil {
		t.Errorf("a domain with no addresses on it catches mail at %v", d["catch_all"])
	}

	elsewhere := paidForwarder(t)
	_, stderr, code := runPaid(t, "--yes", "mail", "settings", "domains", "update", name,
		"--catch-all", elsewhere)
	if code == 0 {
		t.Fatalf("%s was made the catch-all for a domain it is not on", elsewhere)
	}
	if !strings.Contains(stderr, "not an address on that domain") {
		t.Errorf("the refusal does not say the address is on another domain: %s", truncateOutput(stderr))
	}

	_, stderr, code = runPaid(t, "--yes", "mail", "settings", "domains", "update", name,
		"--catch-all", elsewhere, "--clear-catch-all")
	if code != 1 {
		t.Errorf("asking to set and clear the catch-all at once exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "contradict") {
		t.Errorf("the refusal does not say the two flags contradict: %s", truncateOutput(stderr))
	}
}

// An account with no plan cannot have a custom domain, and Proton answers the
// listing with a server error rather than an empty one - so the refusal is the
// CLI's to phrase, and a free account is the only thing that can prove it does.
func TestMailSettingsDomainsNeedAPlan(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "domains", "list")
	if code != 1 {
		t.Errorf("a free account's listing exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "need a paid Mail plan") {
		t.Errorf("the refusal does not say what is missing: %s", truncateOutput(stderr))
	}
	if strings.Contains(stderr, "HTTP 500") {
		t.Errorf("Proton's own server error reached the screen: %s", truncateOutput(stderr))
	}

	name := testID() + ".example.com"
	if _, _, code := run(t, "--yes", "mail", "settings", "domains", "create", name); code == 0 {
		t.Errorf("a free account added the custom domain %s", name)
	}
}
