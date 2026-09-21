package pass

import (
	"testing"

	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
)

// Where stray mail ends up is one field: the mailboxes it makes an alias into,
// or nowhere.
func TestTheCatchAllReadsAsWhereStrayMailGoes(t *testing.T) {
	if got := catchAllText(passsvc.DomainSettings{}); got != "(none)" {
		t.Errorf("off = %q", got)
	}
	if got := catchAllText(passsvc.DomainSettings{CatchAll: true}); got != "on" {
		t.Errorf("on with nowhere to go = %q", got)
	}
	got := catchAllText(passsvc.DomainSettings{CatchAll: true, Mailboxes: []string{"me@proton.me", "work@proton.me"}})
	if got != "me@proton.me, work@proton.me" {
		t.Errorf("on = %q", got)
	}
}
