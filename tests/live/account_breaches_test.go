package live

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestAccountBreachesListOnAFreePlan(t *testing.T) {
	stdout, stderr := runOKStderr(t, "account", "breaches", "list")
	answer := stdout + stderr
	if !strings.Contains(answer, "No breaches.") && !strings.Contains(answer, "SEVERITY") {
		t.Errorf("the listing said neither that there are none nor what there is:\n%s", answer)
	}
	for _, row := range runJSONArray(t, "account", "breaches", "list") {
		assertBreachRow(t, row)
	}
}

func TestAccountBreachesAreListedAndRead(t *testing.T) {
	rows := runJSONArrayPaid(t, "account", "breaches", "list")
	resolvedSeen := false
	for _, row := range rows {
		m := assertBreachRow(t, row)
		if m["state"] == "resolved" {
			resolvedSeen = true
		} else if resolvedSeen {
			t.Error("an open breach came after a resolved one; open ones lead")
		}
	}
	if len(rows) == 0 {
		t.Skip("Dark Web Monitoring has found no breach for this account to read")
	}
	first, _ := rows[0].(map[string]interface{})
	id, _ := first["id"].(string)
	shown := runJSONPaid(t, "account", "breaches", "get", id)
	if shown["id"] != id {
		t.Errorf("get %s answered with %v", id, shown["id"])
	}
	for _, key := range []string{"exposed", "actions"} {
		if _, ok := shown[key].([]interface{}); !ok {
			t.Errorf("%s is not a list in %v", key, keysOf(shown))
		}
	}
	stdout := runOKPaid(t, "account", "breaches", "get", id)
	for _, want := range []string{"Name:", "Severity:", "State:"} {
		assertContains(t, stdout, want)
	}
}

func assertBreachRow(t *testing.T, row interface{}) map[string]interface{} {
	t.Helper()
	m, _ := row.(map[string]interface{})
	for _, key := range []string{"id", "name", "email", "severity", "state"} {
		if _, ok := m[key]; !ok {
			t.Errorf("missing %q in %v", key, keysOf(m))
		}
	}
	if state, _ := m["state"].(string); !slices.Contains([]string{"new", "open", "resolved"}, state) {
		t.Errorf("state = %q, want new, open or resolved", state)
	}
	if severity, _ := m["severity"].(string); !slices.Contains([]string{"low", "medium", "high"}, severity) {
		t.Errorf("severity = %q, want low, medium or high", severity)
	}
	return m
}

func TestAccountBreachesResolveAndReopen(t *testing.T) {
	var id, state string
	for _, row := range runJSONArrayPaid(t, "account", "breaches", "list") {
		m, _ := row.(map[string]interface{})
		if s, _ := m["state"].(string); s == "open" || s == "resolved" {
			id, _ = m["id"].(string)
			state = s
			break
		}
	}
	if id == "" {
		t.Skip("every breach this account holds is still new, and none could be put back as new")
	}
	first, second := "resolve", "reopen"
	if state == "resolved" {
		first, second = "reopen", "resolve"
	}
	cleanup(t, fmt.Sprintf("put the paid account's breach back: proton --profile paid account breaches %s %s",
		second, id), func() error {
		if stateOfBreach(t, id) == state {
			return nil
		}
		if _, _, code := runPaid(t, "account", "breaches", second, id); code != 0 {
			return fmt.Errorf("exit %d", code)
		}
		return nil
	})

	_, stderr := runOKStderrPaid(t, "account", "breaches", first, id)
	assertContains(t, stderr, "breach")
	if got := stateOfBreach(t, id); got == state {
		t.Errorf("state is still %s after %s", got, first)
	}
	_, stderr, code := runPaid(t, "account", "breaches", first, id)
	if code != 1 {
		t.Errorf("a second %s: exit %d, want 1\nstderr: %s", first, code, truncateOutput(stderr))
	}
	runOKPaid(t, "account", "breaches", second, id)
	if got := stateOfBreach(t, id); got != state {
		t.Errorf("state = %s after %s, want %s", got, second, state)
	}
}

func stateOfBreach(t *testing.T, id string) string {
	t.Helper()
	state, _ := runJSONPaid(t, "account", "breaches", "get", id)["state"].(string)
	return state
}

func TestAccountBreachesNeedThePlan(t *testing.T) {
	for _, args := range [][]string{
		{"account", "breaches", "enable"},
		{"account", "breaches", "enable", "--emails"},
	} {
		_, stderr, code := run(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1\nstderr: %s", args, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "does not include Dark Web Monitoring")
	}
}

func TestAccountBreachesSwitchAndPutBack(t *testing.T) {
	switchFeatureAndPutBack(t, []string{"account", "settings", "get"}, []string{"account", "breaches"},
		"dark_web_monitoring", "breach_emails")
}

func switchFeatureAndPutBack(t *testing.T, read, command []string, status, emails string) {
	t.Helper()
	was := putBack(t, status, "", read, command)
	putBack(t, emails, "--emails", read, command)
	if was == "off" {
		runOKPaid(t, switchArgs(command, "enable", "")...)
	}
	switchAndPutBack(t, emails, "--emails", read, command)
	switchAndPutBack(t, status, "", read, command)
	if was == "off" {
		runOKPaid(t, switchArgs(command, "disable", "")...)
	}
	if got := paidSwitch(t, read, status); got != was {
		t.Errorf("%s = %s at the end, and it was %s", status, got, was)
	}
}

func switchAndPutBack(t *testing.T, field, flag string, read, command []string) {
	t.Helper()
	before := putBack(t, field, flag, read, command)
	flip, back := "disable", "enable"
	if before == "off" {
		flip, back = "enable", "disable"
	}
	runOKPaid(t, switchArgs(command, flip, flag)...)
	if got := paidSwitch(t, read, field); got == before {
		t.Errorf("%s is still %s after %s", field, got, flip)
	}
	_, stderr, code := runPaid(t, switchArgs(command, flip, flag)...)
	if code != 1 {
		t.Errorf("a second %s: exit %d, want 1\nstderr: %s", flip, code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already")
	runOKPaid(t, switchArgs(command, back, flag)...)
	if got := paidSwitch(t, read, field); got != before {
		t.Errorf("%s = %s after %s, want %s", field, got, back, before)
	}
}

func putBack(t *testing.T, field, flag string, read, command []string) string {
	t.Helper()
	before := paidSwitch(t, read, field)
	if before != "on" && before != "off" {
		t.Fatalf("%s = %q, want on or off", field, before)
	}
	back := "disable"
	if before == "on" {
		back = "enable"
	}
	args := switchArgs(command, back, flag)
	cleanup(t, fmt.Sprintf("put %s back: proton --profile paid %s", field, strings.Join(args, " ")),
		func() error {
			if paidSwitch(t, read, field) == before {
				return nil
			}
			if _, _, code := runPaid(t, args...); code != 0 {
				return fmt.Errorf("exit %d", code)
			}
			return nil
		})
	return before
}

func paidSwitch(t *testing.T, read []string, field string) string {
	t.Helper()
	value, _ := runJSONPaid(t, read...)[field].(string)
	return value
}

func switchArgs(command []string, verb, flag string) []string {
	out := append(slices.Clone(command), verb)
	if flag != "" {
		out = append(out, flag)
	}
	return out
}
