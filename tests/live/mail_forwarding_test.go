package live

import (
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
)

// Forwarding, both ends of it, on the accounts that can play them.
//
// The forwarder has to be the paid account - Proton gates setting one up behind
// a plan - and it forwards from the fixture address, which exists for this and
// receives nothing. The forwardee is the primary account, and that is what makes
// the round trip worth running: what one account derives, the other has to be
// able to open, and nothing short of two real accounts proves it.

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

// The whole life of a forwarding between two Proton accounts, from both ends.
//
// Every verb the collection has is in here because each needs the state the one
// before it leaves: only a pending forwarding can be declined, only a declined
// one can be offered again, only an active one can be disabled. Doing them apart
// would mean a forwarding set up per verb, and each of those has Proton write to
// somebody's inbox.
func TestMailForwardingRoundTrip(t *testing.T) {
	from, to := paidForwarder(t), selfEmail()

	stdout := runOKPaid(t, "mail", "settings", "forwarding", "create", from, to)
	id := assertBareID(t, stdout, "forwarding create")
	cleanupRunPaid(t, "Delete forwarding: proton mail settings forwarding delete "+id,
		"mail", "settings", "forwarding", "delete", "--", id)

	assertForwarding(t, id, map[string]any{
		"direction": "outgoing", "state": "pending", "encrypted": true, "from": from, "to": to,
	})

	// Nothing is forwarded until the forwardee answers, so there is nothing to
	// disable yet - and Proton is not asked, because the row already says so.
	if _, stderr, code := runPaid(t, "mail", "settings", "forwarding", "disable", "--", id); code == 0 {
		t.Error("a pending forwarding was disabled")
	} else if !strings.Contains(stderr, "pending") {
		t.Errorf("the refusal does not say the forwarding is pending: %s", truncateOutput(stderr))
	}

	runOK(t, "mail", "settings", "forwarding", "decline", "--", awaitIncoming(t, from))
	assertForwardingState(t, id, "rejected")

	runOKPaid(t, "mail", "settings", "forwarding", "resend", "--", id)
	assertForwardingState(t, id, "pending")

	runOK(t, "mail", "settings", "forwarding", "accept", "--", awaitIncoming(t, from))
	assertForwardingState(t, id, "active")

	runOKPaid(t, "mail", "settings", "forwarding", "disable", "--", id)
	assertForwardingState(t, id, "paused")
	runOKPaid(t, "mail", "settings", "forwarding", "enable", "--", id)
	assertForwardingState(t, id, "active")

	runOKPaid(t, "mail", "settings", "forwarding", "delete", "--", id)
	if left := incomingFrom(t, from); left != "" {
		t.Errorf("the forwarding from %s is still on the forwardee's account as %s", from, left)
	}
	for _, row := range runJSONArrayPaid(t, "mail", "settings", "forwarding", "list") {
		f, _ := row.(map[string]interface{})
		if f["id"] == id {
			t.Errorf("the forwarding is still listed after being taken down: %v", f)
		}
	}
}

// Forwarding to an address outside Proton is the one kind that cannot stay
// end-to-end encrypted: there is nobody holding a Proton key to hand it to. So
// the address gives its own encryption up for as long as the arrangement lasts,
// and taking the arrangement down gives it back.
func TestMailForwardingOutsideProtonTurnsEncryptionOffAndBackOn(t *testing.T) {
	from := paidForwarder(t)
	// Reserved by the IETF and accepts no mail, so the confirmation Proton sends
	// reaches nobody and the forwarding stays pending, which is all this needs.
	const outside = "nobody@example.com"
	assertEndToEnd(t, account.Paid, from, true)

	stdout := runOKPaid(t, "mail", "settings", "forwarding", "create", from, outside)
	id := assertBareID(t, stdout, "forwarding create")
	cleanupRunPaid(t, "Delete forwarding: proton mail settings forwarding delete "+id,
		"mail", "settings", "forwarding", "delete", "--", id)

	assertForwarding(t, id, map[string]any{
		"direction": "outgoing", "state": "pending", "encrypted": false, "from": from, "to": outside,
	})
	assertEndToEnd(t, account.Paid, from, false)

	// The confirmation email is the only thing there is to send again.
	runOKPaid(t, "mail", "settings", "forwarding", "resend", "--", id)

	runOKPaid(t, "mail", "settings", "forwarding", "delete", "--", id)
	assertEndToEnd(t, account.Paid, from, true)
}

// Setting one up needs a plan, and an account without one is told so before
// anything of its own is touched.
func TestMailForwardingNeedsAPaidMailPlan(t *testing.T) {
	_, stderr, code := run(t, "mail", "settings", "forwarding", "create", selfEmail(), secondaryEmail())
	if code == 0 {
		t.Fatal("a free account set a forwarding up")
	}
	if !strings.Contains(stderr, "paid Mail plan") {
		t.Errorf("the refusal does not name the plan as the problem: %s", truncateOutput(stderr))
	}
	assertEndToEnd(t, account.Primary, selfEmail(), true)
}

// assertForwarding reads one of the paid account's forwardings and checks the
// fields a test named.
func assertForwarding(t *testing.T, id string, want map[string]any) {
	t.Helper()
	row := runJSONPaid(t, "mail", "settings", "forwarding", "get", "--", id)
	for field, expected := range want {
		if row[field] != expected {
			t.Errorf("the forwarding's %s is %v, want %v", field, row[field], expected)
		}
	}
}

// assertForwardingState waits for one of the paid account's forwardings to say
// what it should. The forwardee answers through Proton, so the two ends of one
// arrangement do not change at the same instant.
func assertForwardingState(t *testing.T, id, want string) {
	t.Helper()
	var state string
	if waitFor(30*time.Second, 3*time.Second, func() bool {
		row := runJSONPaid(t, "mail", "settings", "forwarding", "get", "--", id)
		state, _ = row["state"].(string)
		return state == want
	}) {
		return
	}
	t.Fatalf("the forwarding is %q, want %q", state, want)
}

// awaitIncoming is the forwardee's own reference for a forwarding sent to it,
// waiting for the offer to arrive. Each end has its own ID for one arrangement.
func awaitIncoming(t *testing.T, forwarder string) string {
	t.Helper()
	var id string
	if !waitFor(30*time.Second, 3*time.Second, func() bool {
		id = incomingFrom(t, forwarder)
		return id != ""
	}) {
		t.Fatalf("the forwarding from %s never reached the forwardee", forwarder)
	}
	return id
}

// incomingFrom is that reference as it stands, or "" when there is none.
func incomingFrom(t *testing.T, forwarder string) string {
	t.Helper()
	for _, row := range runJSONArray(t, "mail", "settings", "forwarding", "list") {
		f, _ := row.(map[string]interface{})
		if f["direction"] == "incoming" && f["from"] == forwarder {
			id, _ := f["id"].(string)
			return id
		}
	}
	return ""
}

// assertEndToEnd reads whether mail arriving at one of an account's addresses is
// end-to-end encrypted.
func assertEndToEnd(t *testing.T, profile, address string, want bool) {
	t.Helper()
	stdout, _ := runOKProfile(t, profile,
		asJSON([]string{"mail", "settings", "addresses", "get", address})...)
	if got := parseJSONObject(t, stdout)["end_to_end"]; got != want {
		t.Errorf("%s reads as end-to-end encrypted=%v, want %v", address, got, want)
	}
}
