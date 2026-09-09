package live

import (
	"strings"
	"testing"
)

// Forwarding: what an account can be told about its own, and what Proton takes
// from here.
//
// Setting one up is the one thing it does not take. Everything the request
// carries has been held against what Proton's own client sends for the same
// forwarder and forwardee - the derived key packet by packet, the sealed
// passphrase, the proxy parameters, every field of the body - and it is the
// same; Proton stores theirs and refuses this. So no run can bring a forwarding
// about, and what a run can do is read them and prove the refusal is still
// there.

// Both directions come back as one collection, whichever account is asking.
func TestMailForwardingListsBothDirections(t *testing.T) {
	for _, row := range runJSONArray(t, "mail", "settings", "forwarding", "list") {
		f, _ := row.(map[string]interface{})
		switch f["direction"] {
		case "incoming", "outgoing":
		default:
			t.Errorf("a forwarding came back going neither way: %v", f["direction"])
		}
	}
}

// Proton refuses to set one up from here, and this is what says so.
//
// It fails the day Proton starts accepting it, which is the day the limit in
// docs/help/limits.md comes off and the tests for accepting, pausing, resuming
// and taking one down can exist at all.
func TestMailForwardingSetupIsRefusedByProton(t *testing.T) {
	from, to := paidForwarder(t), selfEmail()

	stdout, stderr, code := runPaid(t, "mail", "settings", "forwarding", "create", from, to)
	if code == 0 {
		t.Fatalf("Proton accepted a forwarding set up from here: %s\n"+
			"\ttake the limit off docs/help/limits.md and write the tests this could not have",
			truncateOutput(stdout))
	}
	if !strings.Contains(stderr, "Invalid forwardee key") {
		t.Errorf("Proton refused it for a reason nothing here has seen before: %s", truncateOutput(stderr))
	}
	if !strings.Contains(stderr, "account.proton.me") {
		t.Errorf("the refusal does not say where a forwarding can be set up: %s", truncateOutput(stderr))
	}
}

// Forwarding to an address outside Proton is refused before anything is
// derived: Proton emails such an address a link its owner must follow, which no
// command can answer.
func TestMailForwardingRefusesAnAddressOutsideProton(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "forwarding", "create", selfEmail(), "nobody@example.com")
	if code == 0 {
		t.Fatal("an address outside Proton was accepted")
	}
	if !strings.Contains(stderr, "not a Proton address") &&
		!strings.Contains(stderr, "address does not exist") {
		t.Errorf("the refusal does not name the address as the problem: %s", truncateOutput(stderr))
	}
}
