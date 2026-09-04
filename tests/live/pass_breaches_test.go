package live

import (
	"testing"
)

// Pass Monitor, which a plan gates: which of your addresses have turned up in
// somebody else's data breach.
//
// Read-only - it says what has already happened - and the only place in the
// suite where a skip is honest, because reading a breach needs an address that
// has actually been in one.

func TestPassBreachesAreListedAndRead(t *testing.T) {
	rows := runJSONArrayPaid(t, "pass", "breaches", "list")
	if len(rows) == 0 {
		t.Skip("this account has no watched addresses to read")
	}

	// Worst first, so the reason to run it is answered by the first row.
	var last float64 = -1
	var withBreaches string
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
		if n > 0 && withBreaches == "" {
			withBreaches = email
		}
	}

	if withBreaches == "" {
		t.Skip("no watched address has a breach to read")
	}
	// The record says which breaches, which is what tells somebody what to
	// change. The values are this account's, so only the shape is asserted.
	shown := runJSONPaid(t, "pass", "breaches", "get", withBreaches)
	list, _ := shown["breach_list"].([]interface{})
	if len(list) == 0 {
		t.Fatalf("%s reports breaches but names none", withBreaches)
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
