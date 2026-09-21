package mail

import (
	"testing"

	"github.com/roman-16/proton-cli/internal/dns"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
)

// A listing has one cell for five checks, so it says they pass or it says which
// ones do not - never a count, which names nothing to go and fix.
func TestTheListingSaysWhichChecksAreNotPassing(t *testing.T) {
	passing := mailsvc.Domain{DNS: []dns.Check{
		{Name: "verification", Status: "ok"},
		{Name: "mx", Status: "ok"},
	}}
	if got := dnsSummary(passing); got != "ok" {
		t.Errorf("summary = %q, want ok", got)
	}
	short := mailsvc.Domain{DNS: []dns.Check{
		{Name: "verification", Status: "ok"},
		{Name: "mx", Status: "missing"},
		{Name: "dkim", Status: "warning"},
	}}
	if got := dnsSummary(short); got != "mx, dkim" {
		t.Errorf("summary = %q, want \"mx, dkim\"", got)
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
