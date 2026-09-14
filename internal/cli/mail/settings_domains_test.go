package mail

import (
	"strings"
	"testing"

	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
)

// What DOMAIN may be is judged from the command line, so a name that is not one
// costs nothing to refuse.
func TestDomainNameTakesABareNameAndNothingElse(t *testing.T) {
	for _, tt := range []struct{ arg, want string }{
		{"example.com", "example.com"},
		{"  Example.COM  ", "example.com"},
		{"mail.example.co.uk", "mail.example.co.uk"},
	} {
		got, err := domainName(tt.arg)
		if err != nil || got != tt.want {
			t.Errorf("domainName(%q) = %q, %v; want %q", tt.arg, got, err, tt.want)
		}
	}
	for _, arg := range []string{
		"", "example", "me@example.com", "https://example.com", "example.com/mail",
		"example .com", ".example.com", "example.com.",
	} {
		if _, err := domainName(arg); err == nil {
			t.Errorf("domainName(%q) was accepted", arg)
		} else if !strings.Contains(err.Error(), "is not a domain name") {
			t.Errorf("domainName(%q) refused with %q", arg, err)
		}
	}
}

// A listing has one cell for five checks, so it says they pass or it says which
// ones do not - never a count, which names nothing to go and fix.
func TestTheListingSaysWhichChecksAreNotPassing(t *testing.T) {
	passing := mailsvc.Domain{DNS: []mailsvc.RecordGroup{
		{Name: "verification", Status: "ok"},
		{Name: "mx", Status: "ok"},
	}}
	if got := dnsSummary(passing); got != "ok" {
		t.Errorf("summary = %q, want ok", got)
	}
	short := mailsvc.Domain{DNS: []mailsvc.RecordGroup{
		{Name: "verification", Status: "ok"},
		{Name: "mx", Status: "missing"},
		{Name: "dkim", Status: "warning"},
	}}
	if got := dnsSummary(short); got != "mx, dkim" {
		t.Errorf("summary = %q, want \"mx, dkim\"", got)
	}
}

// An entry is copied by hand into somebody's DNS, so it is written whole, in the
// order a zone file writes it, with the columns lined up down the group.
func TestAnEntryIsWrittenTheWayAZoneFileWritesIt(t *testing.T) {
	mx := groupText(mailsvc.RecordGroup{Name: "mx", Status: "wrong-priority", Records: []mailsvc.Record{
		{Type: "MX", Host: "@", Value: "mail.protonmail.ch", Priority: 10},
		{Type: "MX", Host: "@", Value: "mailsec.protonmail.ch", Priority: 20},
	}})
	want := "wrong-priority\nMX  @  10  mail.protonmail.ch\nMX  @  20  mailsec.protonmail.ch"
	if mx != want {
		t.Errorf("MX group =\n%s\nwant\n%s", mx, want)
	}

	dkim := groupText(mailsvc.RecordGroup{Name: "dkim", Status: "ok", Records: []mailsvc.Record{
		{Type: "CNAME", Host: "protonmail._domainkey", Value: "one.domains.proton.ch."},
		{Type: "CNAME", Host: "protonmail2._domainkey", Value: "two.domains.proton.ch."},
	}})
	want = "ok\n" +
		"CNAME  protonmail._domainkey   one.domains.proton.ch.\n" +
		"CNAME  protonmail2._domainkey  two.domains.proton.ch."
	if dkim != want {
		t.Errorf("DKIM group =\n%s\nwant\n%s", dkim, want)
	}
}

// A check with nothing under it is still a verdict worth reading: DKIM reports
// what it found whether or not Proton handed back the entries it wants.
func TestACheckWithNoEntriesIsStillItsVerdict(t *testing.T) {
	if got := groupText(mailsvc.RecordGroup{Name: "dkim", Status: "missing"}); got != "missing" {
		t.Errorf("group = %q, want missing", got)
	}
}

func TestADomainWithNoCatchAllReadsAsNone(t *testing.T) {
	if got := catchAllText(mailsvc.Domain{}); got != "(none)" {
		t.Errorf("catch-all = %q, want (none)", got)
	}
	if got := catchAllText(mailsvc.Domain{CatchAll: "stray@example.com"}); got != "stray@example.com" {
		t.Errorf("catch-all = %q", got)
	}
}
