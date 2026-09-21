package live

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/tests/account"
)

// The domains an alias can be made on: Proton's own, and a domain of the
// account's own, which a plan gates.
//
// Everything that writes acts on a domain the run made and nothing else: a
// subdomain of the paid account's own, carrying the suite's prefix, which is
// what tests/paid keeps a delete pointed at. It is never verified - that needs
// entries in somebody real's DNS - so what a run sees of the DNS checks is the
// state every domain starts in.

// aPassDomainTheRunMade adds a subdomain of the account's own for aliases and
// hands back its name, having registered its removal.
func aPassDomainTheRunMade(t *testing.T) string {
	t.Helper()
	name := testID() + "." + paidDomainSuffix(t)
	stdout, stderr := runOKStderrPaid(t, "pass", "settings", "domains", "create", name)
	assertBareRef(t, stdout, "domains create", looksLikeNumber)
	cleanupRunPaid(t, "Delete alias domain: proton pass settings domains delete "+name,
		"pass", "settings", "domains", "delete", name)
	// What comes back with the domain is the one entry that proves it is yours.
	assertContains(t, stderr, "TXT")
	assertContains(t, stderr, "pass settings domains get "+name)
	return name
}

// looksLikeNumber matches the reference Proton files an alias domain under,
// which is a number rather than an ID.
func looksLikeNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

// Every domain is listed whatever state it is in: Proton's own with no state to
// be in, and a custom one with the next thing its owner has to do.
func TestPassSettingsDomainsListReportsEveryState(t *testing.T) {
	stdout := runOKPaid(t, "pass", "settings", "domains", "list")
	for _, header := range []string{"DOMAIN", "DEFAULT", "CUSTOM", "STATUS", "ALIASES"} {
		assertContains(t, stdout, header)
	}
	var protons int
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "domains", "list") {
		d, _ := row.(map[string]interface{})
		custom, _ := d["custom"].(bool)
		if !custom {
			protons++
			if d["status"] != nil || d["id"] != nil {
				t.Errorf("one of Proton's domains carries a state or a number of its own: %v", d)
			}
			continue
		}
		switch d["status"] {
		case "active", "unverified", "unconfigured":
		default:
			t.Errorf("a custom domain came back in a state nothing declares: %v", d["status"])
		}
	}
	if protons == 0 {
		t.Fatal("no domain of Proton's was offered, so no alias could be made")
	}
}

// A domain nobody has verified is the state every domain starts in: every check
// is failing, the entry that proves it is yours is there to copy, and there are
// no settings to change yet.
func TestPassSettingsDomainsCreateGetAndDelete(t *testing.T) {
	name := aPassDomainTheRunMade(t)

	stdout := runOKPaid(t, "pass", "settings", "domains", "get", name)
	assertField(t, stdout, "Domain:", name)
	assertField(t, stdout, "Status:", "unverified")
	for _, check := range []string{"Ownership:", "MX:", "SPF:", "DKIM:", "DMARC:"} {
		assertContains(t, stdout, check)
	}
	for _, entry := range []string{
		"mx1.alias.proton.me", "mx2.alias.proton.me",
		"v=spf1 include:alias.proton.me ~all",
		"dkim._domainkey.alias.proton.me", "v=DMARC1; p=quarantine",
	} {
		assertContains(t, stdout, entry)
	}

	d := runJSONPaid(t, "pass", "settings", "domains", "get", name)
	if d["custom"] != true || d["status"] != "unverified" {
		t.Errorf("a domain nobody has verified came back as %v", d)
	}
	if n, _ := d["aliases"].(float64); n != 0 {
		t.Errorf("a domain nobody has verified has %v aliases on it", n)
	}
	if d["settings"] != nil {
		t.Errorf("a domain nobody has verified came back with settings: %v", d["settings"])
	}
	checks, _ := d["dns"].([]interface{})
	if len(checks) != 5 {
		t.Fatalf("a domain reported %d checks, want 5: %v", len(checks), keysOf(d))
	}
	first, _ := checks[0].(map[string]interface{})
	if first["name"] != "ownership" || first["status"] == "ok" || first["status"] == nil {
		t.Errorf("the first check on a fresh domain = %v", first)
	}
	entries, _ := first["records"].([]interface{})
	entry, _ := entries[0].(map[string]interface{})
	if value, _ := entry["value"].(string); entry["type"] != "TXT" || entry["host"] != "@" || value == "" {
		t.Errorf("the entry that proves the domain is yours = %v", entry)
	}

	// Its settings wait for the domain, and the refusal says so before anything
	// is sent.
	_, stderr, code := runPaid(t, "--yes", "pass", "settings", "domains", "update", name,
		"--catch-all", "none")
	if code != 1 {
		t.Errorf("changing the settings of an unverified domain exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "not verified yet") {
		t.Errorf("the refusal does not say the domain is unverified: %s", truncateOutput(stderr))
	}

	runOKPaid(t, "pass", "settings", "domains", "delete", name)
	if _, _, code := runPaid(t, "--yes", "pass", "settings", "domains", "get", name); code != 3 {
		t.Errorf("a deleted domain answers with exit %d, want 3", code)
	}
}

// The default domain is written on the account, and put back exactly as it was
// found: chosen again where one was chosen, and unchosen again where none was.
func TestPassSettingsDomainsDefault(t *testing.T) {
	var current, offered string
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "domains", "list") {
		d, _ := row.(map[string]interface{})
		name, _ := d["domain"].(string)
		if isDefault, _ := d["default"].(bool); isDefault {
			current = name
		}
		if custom, _ := d["custom"].(bool); !custom && offered == "" {
			offered = name
		}
	}
	if current != "" {
		runOKPaid(t, "pass", "settings", "domains", "update", "--default", current)
		if !domainIsDefault(t, current) {
			t.Errorf("%s stopped being the default for having been chosen again", current)
		}
		return
	}
	if offered == "" {
		t.Fatal("no domain of Proton's was offered, so none could be made the default")
	}
	runOKPaid(t, "pass", "settings", "domains", "update", "--default", offered)
	// Only while it is still chosen: the test unchooses it itself, and asking
	// again would be refused for describing nothing.
	cleanup(t, "Leave no default alias domain chosen: proton pass settings domains update --default=false "+offered,
		func() error {
			current, err := defaultAliasDomain()
			if err != nil || current != offered {
				return err
			}
			_, stderr, code, err := runAs(account.Paid, nil, "--yes",
				"pass", "settings", "domains", "update", "--default=false", offered)
			if err != nil {
				return err
			}
			if code != 0 {
				return fmt.Errorf("exit %d: %s", code, stderr)
			}
			return nil
		})
	if !domainIsDefault(t, offered) {
		t.Errorf("%s was chosen and is not the default", offered)
	}
	runOKPaid(t, "pass", "settings", "domains", "update", "--default=false", offered)
	if domainIsDefault(t, offered) {
		t.Errorf("%s was unchosen and is still the default", offered)
	}
}

// Unchoosing a domain that is not the default is a command line that describes
// nothing, and is refused as one.
func TestPassSettingsDomainsOnlyTheDefaultCanBeUnchosen(t *testing.T) {
	var other string
	for _, row := range runJSONArrayPaid(t, "pass", "settings", "domains", "list") {
		d, _ := row.(map[string]interface{})
		isDefault, _ := d["default"].(bool)
		custom, _ := d["custom"].(bool)
		if !isDefault && !custom {
			other, _ = d["domain"].(string)
			break
		}
	}
	if other == "" {
		t.Fatal("every domain of Proton's is the default, which should not be possible")
	}
	_, stderr, code := runPaid(t, "--yes", "pass", "settings", "domains", "update", "--default=false", other)
	if code != 1 {
		t.Errorf("unchoosing a domain that is not the default exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "is not the default domain") {
		t.Errorf("the refusal does not say the domain is not the default: %s", truncateOutput(stderr))
	}
}

// domainIsDefault reads whether a domain is the one new aliases take.
func domainIsDefault(t *testing.T, name string) bool {
	t.Helper()
	current, err := defaultAliasDomain()
	if err != nil {
		t.Fatalf("could not read which domain is the default: %v", err)
	}
	return current == name
}

// defaultAliasDomain is the domain new aliases take, or "" when none is chosen.
// It has no *testing.T so a cleanup can ask it too.
func defaultAliasDomain() (string, error) {
	stdout, stderr, code, err := runAs(account.Paid, nil, "--output", "json",
		"pass", "settings", "domains", "list")
	if err != nil {
		return "", err
	}
	if code != 0 {
		return "", fmt.Errorf("exit %d: %s", code, stderr)
	}
	var data struct {
		Domains []struct {
			Domain  string `json:"domain"`
			Default bool   `json:"default"`
		} `json:"domains"`
	}
	if err := json.Unmarshal([]byte(stdout), &data); err != nil {
		return "", err
	}
	for _, d := range data.Domains {
		if d.Default {
			return d.Domain, nil
		}
	}
	return "", nil
}

// An account with no plan cannot bring a domain of its own, and is told what is
// missing before anything is sent to Proton about the domain.
func TestPassSettingsDomainsNeedAPlan(t *testing.T) {
	name := testID() + ".example.com"
	_, stderr, code := run(t, "--yes", "pass", "settings", "domains", "create", name)
	if code != 1 {
		t.Errorf("a free account adding a custom domain exits %d, want 1", code)
	}
	if !strings.Contains(stderr, "need a paid Pass plan") {
		t.Errorf("the refusal does not say what is missing: %s", truncateOutput(stderr))
	}
}
