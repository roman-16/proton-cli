package live

import (
	"strings"
	"testing"
)

// Dark Web Monitoring, which a plan gates: which of your addresses have turned
// up in somebody else's data breach.
//
// Three kinds of address are watched - the ones on the account, the aliases in
// its vaults, and the ones somebody adds by hand - and only the third can be
// made, so it is the one the writing tests act on. The two skips left here are
// the honest ones: reading a breach needs an address that has actually been in
// one, which no run can arrange.

func TestPassBreachesAreListedAndRead(t *testing.T) {
	rows := runJSONArrayPaid(t, "pass", "breaches", "list")
	if len(rows) == 0 {
		t.Fatal("the account owns addresses and holds aliases, so the listing cannot be empty")
	}

	// Worst first, so the reason to run it is answered by the first row.
	var last float64 = -1
	var withBreaches string
	kinds := map[string]bool{}
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
		kind, _ := m["type"].(string)
		switch kind {
		case "proton", "custom", "alias":
			kinds[kind] = true
		default:
			t.Errorf("%s came back as type %q", email, kind)
		}
		switch state, _ := m["state"].(string); state {
		case "watched", "paused", "unverified":
		default:
			t.Errorf("%s came back in state %q", email, state)
		}
		if n > 0 && withBreaches == "" {
			withBreaches = email
		}
	}
	if !kinds["proton"] {
		t.Error("the account's own addresses should be among the watched ones")
	}

	if withBreaches == "" {
		t.Skip("no watched address has a breach to read")
	}
	// The record says which breaches, which is what tells somebody what to
	// change. The values are this account's, so only the shape is asserted.
	shown := runJSONPaid(t, "pass", "breaches", "get", withBreaches)
	list, _ := shown["breach_list"].([]interface{})
	withheld, _ := shown["withheld"].(float64)
	// Proton sends the detail or a count of what it is keeping back, and an
	// address with breaches has to come back as one or the other: a list of none
	// with nothing withheld is the answer this CLI used to give wrongly.
	if len(list) == 0 && withheld == 0 {
		t.Fatalf("%s reports breaches but names none and withholds none", withBreaches)
	}
	if len(list) == 0 {
		return
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

// An alias is watched as an item rather than as an address Proton holds a record
// of, so it is asked for at an endpoint of its own. Reading one that has not been
// breached is what proves the path is wired to the right alias.
func TestPassBreachesReadAnAlias(t *testing.T) {
	_, address := paidAlias(t)

	shown := runJSONPaid(t, "pass", "breaches", "get", address)
	if got, _ := shown["email"].(string); got != address {
		t.Errorf("asked about %s and was told about %q", address, got)
	}
	if got, _ := shown["type"].(string); got != "alias" {
		t.Errorf("%s came back as type %q, want alias", address, got)
	}
	if _, ok := shown["breach_list"]; !ok {
		t.Error("an alias with no breaches should still report an empty list")
	}
}

// The life of an address somebody adds by hand: Proton takes it, emails it a
// code, watches nothing until the code comes back, and forgets it on request.
//
// Nothing here asks for the code again. Proton meters verification emails the
// way a brute-force guard does, and a run that spent two of them would start
// failing on that quota rather than on anything real - and stay failing for the
// best part of an hour. Adding the address spends the one email a run may.
func TestPassBreachesWatchAnAddressAddedByHand(t *testing.T) {
	// example.com is reserved and takes no mail, so the code Proton sends bounces
	// at Proton's end rather than reaching a mailbox somebody reads. The address
	// carries the test prefix, so an interrupted run leaves something findable.
	address := testID() + "@example.com"

	stdout := runOKPaid(t, "pass", "breaches", "create", address)
	id := assertBareID(t, stdout, "pass breaches create")
	cleanupRunPaid(t, "Stop watching: proton pass breaches delete "+address,
		"pass", "breaches", "delete", address)

	row := watched(t, address)
	if got, _ := row["type"].(string); got != "custom" {
		t.Errorf("%s came back as type %q, want custom", address, got)
	}
	// Proton watches nothing until the code is handed back, and the listing has
	// to say so rather than reading as though it were being watched.
	if got, _ := row["state"].(string); got != "unverified" {
		t.Errorf("%s came back in state %q, want unverified", address, got)
	}

	// Proton is not watching an address whose code has not come back, so there is
	// nothing there to read and nothing to pause. Each refusal says which address
	// it is about and what to do about it - and the one aimed at the ID the
	// creation printed is what proves that ID is a reference like any other.
	for _, args := range [][]string{
		{"get", id},
		{"disable", address},
		{"enable", address},
	} {
		_, stderr, code := runPaid(t, append([]string{"--yes", "pass", "breaches"}, args...)...)
		if code == 0 {
			t.Errorf("pass breaches %s on an unverified address was accepted", args[0])
		}
		if !strings.Contains(stderr, "unverified") || !strings.Contains(stderr, "breaches verify") {
			t.Errorf("pass breaches %s said: %s", args[0], stderr)
		}
	}
}

// watched is one row of the listing, for the addresses a record cannot be read
// for: Proton refuses to be asked about an address nobody has verified.
func watched(t *testing.T, address string) map[string]interface{} {
	t.Helper()
	for _, row := range runJSONArrayPaid(t, "pass", "breaches", "list") {
		m, _ := row.(map[string]interface{})
		if got, _ := m["email"].(string); got == address {
			return m
		}
	}
	t.Fatalf("%s is not among the watched addresses", address)
	return nil
}

// The three commands that only an address added by hand answers to say so before
// they reach Proton, so nothing is sent on behalf of an address that could never
// have taken it.
func TestPassBreachesRefuseWhatOnlyACustomAddressAnswersTo(t *testing.T) {
	_, alias := paidAlias(t)
	for _, verb := range [][]string{
		{"delete", alias},
		{"resend", alias},
		{"verify", alias, "--code", "123456"},
	} {
		args := append([]string{"--yes", "pass", "breaches"}, verb...)
		stdout, stderr, code := runPaid(t, args...)
		if code == 0 {
			t.Errorf("pass breaches %s on an alias was accepted: %s", verb[0], stdout)
		}
		if !strings.Contains(stderr, "added yourself") {
			t.Errorf("pass breaches %s on an alias said: %s", verb[0], stderr)
		}
	}
}

// Pausing an alias is an item flag rather than a record at Proton, and it is the
// same flag that leaves a login out of the password checks - so this is the one
// test that proves the flag is written and read back.
func TestPassBreachesPauseAnAlias(t *testing.T) {
	_, address := paidAlias(t)
	cleanupRunPaid(t, "Resume watching: proton pass breaches enable "+address,
		"pass", "breaches", "enable", address)

	runOKPaid(t, "pass", "breaches", "disable", address)
	if got, _ := runJSONPaid(t, "pass", "breaches", "get", address)["monitored"].(bool); got {
		t.Error("the alias was paused and still reports as monitored")
	}

	runOKPaid(t, "pass", "breaches", "enable", address)
	if got, _ := runJSONPaid(t, "pass", "breaches", "get", address)["monitored"].(bool); !got {
		t.Error("the alias was resumed and still reports as paused")
	}
}

// An address on the account is a record Proton keeps, so pausing it is a third
// endpoint again. It is put back immediately, and the photograph compares each
// watched address's state either side of the run.
func TestPassBreachesPauseAnAddressOnTheAccount(t *testing.T) {
	var address string
	for _, row := range runJSONArrayPaid(t, "pass", "breaches", "list") {
		m, _ := row.(map[string]interface{})
		if kind, _ := m["type"].(string); kind == "proton" {
			address, _ = m["email"].(string)
			break
		}
	}
	if address == "" {
		t.Fatal("the account owns at least one address, so one should be watched")
	}
	cleanupRunPaid(t, "Resume watching: proton pass breaches enable "+address,
		"pass", "breaches", "enable", address)

	runOKPaid(t, "pass", "breaches", "disable", address)
	if got, _ := runJSONPaid(t, "pass", "breaches", "get", address)["monitored"].(bool); got {
		t.Error("the address was paused and still reports as monitored")
	}

	runOKPaid(t, "pass", "breaches", "enable", address)
	if got, _ := runJSONPaid(t, "pass", "breaches", "get", address)["monitored"].(bool); !got {
		t.Error("the address was resumed and still reports as paused")
	}
}
