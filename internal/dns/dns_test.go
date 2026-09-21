package dns

import (
	"slices"
	"strings"
	"testing"
)

// An entry is copied by hand into somebody's DNS, so it is written whole, in the
// order a zone file writes it, with the columns lined up down the check.
func TestAnEntryIsWrittenTheWayAZoneFileWritesIt(t *testing.T) {
	mx := Check{Name: "mx", Status: "wrong-priority", Records: []Record{
		{Type: "MX", Host: "@", Value: "mail.protonmail.ch", Priority: 10},
		{Type: "MX", Host: "@", Value: "mailsec.protonmail.ch", Priority: 20},
	}}.Text()
	want := "wrong-priority\nMX  @  10  mail.protonmail.ch\nMX  @  20  mailsec.protonmail.ch"
	if mx != want {
		t.Errorf("MX check =\n%s\nwant\n%s", mx, want)
	}

	dkim := Check{Name: "dkim", Status: "ok", Records: []Record{
		{Type: "CNAME", Host: "protonmail._domainkey", Value: "one.domains.proton.ch."},
		{Type: "CNAME", Host: "protonmail2._domainkey", Value: "two.domains.proton.ch."},
	}}.Text()
	want = "ok\n" +
		"CNAME  protonmail._domainkey   one.domains.proton.ch.\n" +
		"CNAME  protonmail2._domainkey  two.domains.proton.ch."
	if dkim != want {
		t.Errorf("DKIM check =\n%s\nwant\n%s", dkim, want)
	}
}

// A check with nothing under it is still a verdict worth reading, and an entry
// with no verdict yet is still an entry worth copying.
func TestAVerdictAndAnEntryEachStandAlone(t *testing.T) {
	if got := (Check{Name: "dkim", Status: "missing"}).Text(); got != "missing" {
		t.Errorf("check = %q, want missing", got)
	}
	entry := Check{Records: []Record{{Type: "TXT", Host: "@", Value: "sl-verification=abc"}}}.Text()
	if entry != "TXT  @  sl-verification=abc" {
		t.Errorf("entry = %q", entry)
	}
}

// What Proton found in an entry's place is the one thing that tells a value not
// yet propagated from a value typed wrong, so it is written under the entries.
func TestWhatWasFoundInsteadIsWrittenLast(t *testing.T) {
	got := Check{Name: "mx", Status: "wrong", Records: []Record{
		{Type: "MX", Host: "@", Value: "mx1.alias.proton.me", Priority: 10},
	}, Found: []string{"10 mail.protonmail.ch."}}.Text()
	want := "wrong\nMX  @  10  mx1.alias.proton.me\nfound: 10 mail.protonmail.ch."
	if got != want {
		t.Errorf("check =\n%s\nwant\n%s", got, want)
	}
}

func TestFailingNamesTheChecksThatAreNotPassing(t *testing.T) {
	checks := []Check{
		{Name: "verification", Status: Ok},
		{Name: "mx", Status: "missing"},
		{Name: "dkim", Status: "warning"},
	}
	if got := Failing(checks); !slices.Equal(got, []string{"mx", "dkim"}) {
		t.Errorf("failing = %v", got)
	}
	if got := Failing([]Check{{Name: "mx", Status: Ok}}); len(got) != 0 {
		t.Errorf("failing = %v, want none", got)
	}
}

// What DOMAIN may be is judged from the command line, so a name that is not one
// costs nothing to refuse.
func TestNameTakesABareNameAndNothingElse(t *testing.T) {
	for _, tt := range []struct{ arg, want string }{
		{"example.com", "example.com"},
		{"  Example.COM  ", "example.com"},
		{"mail.example.co.uk", "mail.example.co.uk"},
	} {
		got, err := Name(tt.arg)
		if err != nil || got != tt.want {
			t.Errorf("Name(%q) = %q, %v; want %q", tt.arg, got, err, tt.want)
		}
	}
	for _, arg := range []string{
		"", "example", "me@example.com", "https://example.com", "example.com/mail",
		"example .com", ".example.com", "example.com.",
	} {
		if _, err := Name(arg); err == nil {
			t.Errorf("Name(%q) was accepted", arg)
		} else if !strings.Contains(err.Error(), "is not a domain name") {
			t.Errorf("Name(%q) refused with %q", arg, err)
		}
	}
}
