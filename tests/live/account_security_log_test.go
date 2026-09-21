package live

import (
	"strings"
	"testing"
)

// The log Proton keeps of sign-ins to the account.
//
// One thing here is not testable and is worth saying: nothing the suite does
// appears in it. Proton records a sign-in from a named client, and this CLI is
// not one - so the events these accounts hold are the ones somebody made in a
// browser, and a run can neither add one nor put one back. That is why the wipe
// is only ever previewed: `delete` for real would leave every test below with
// nothing to read for good.

// What the listing shows, in both formats. Either answer is the command
// working, since an account that has never been signed in to from a browser has
// nothing recorded.
func TestAccountSecurityLogList(t *testing.T) {
	stdout, stderr := runOKStderr(t, "account", "security-log", "list")
	answer := stdout + stderr
	if !strings.Contains(answer, "No events.") && !strings.Contains(answer, "EVENT") {
		t.Errorf("the listing said neither that there are none nor what there is:\n%s", answer)
	}

	for _, event := range listAll(t, "account", "security-log", "list") {
		row, _ := event.(map[string]interface{})
		for _, field := range []string{"time", "event", "status"} {
			if _, ok := row[field]; !ok {
				t.Errorf("missing %q in %v", field, keysOf(row))
			}
		}
		// The columns Proton fills in depend on the level and on Sentinel, so an
		// event says what it has and leaves out what it has not.
		if ip, ok := row["ip"].(string); ok && ip == "" {
			t.Error("an address that was not recorded is left out rather than empty")
		}
	}
}

// What `get` answers is the state both toggles are in, and how much there is to
// lose by turning the first one off.
func TestAccountSecurityLogGet(t *testing.T) {
	stdout := runOK(t, "account", "security-log", "get")
	for _, want := range []string{"Status:", "Detailed:", "Events:"} {
		assertContains(t, stdout, want)
	}

	state := runJSON(t, "account", "security-log", "get")
	for _, key := range []string{"status", "detailed", "events"} {
		if _, ok := state[key]; !ok {
			t.Errorf("missing %q in %v", key, keysOf(state))
		}
	}
	if status := state["status"]; status != "on" && status != "off" {
		t.Errorf("status = %v, want on or off", status)
	}
}

// The count `get` reports is the whole log, whatever page a listing asked for.
func TestAccountSecurityLogCountsEveryEvent(t *testing.T) {
	state := runJSON(t, "account", "security-log", "get")
	events, ok := state["events"].(float64)
	if !ok {
		t.Fatalf("events should be a number, got %T", state["events"])
	}
	listed := runJSON(t, "account", "security-log", "list", "--limit", "1")
	if total, ok := listed["total"].(float64); !ok || total != events {
		t.Errorf("the listing totals %v events and `get` counts %v", listed["total"], events)
	}
}

// Recording the IP address of each event, on and straight off again. It is the
// one toggle here a run may work: it changes what is written from now on and
// leaves what is already written alone.
func TestAccountSecurityLogDetailedRoundTrip(t *testing.T) {
	if runJSON(t, "account", "security-log", "get")["detailed"] == "on" {
		t.Fatal("the primary account records IP addresses, and this test turns that on itself;" +
			" run `proton --profile primary account security-log disable --detailed`")
	}
	cleanupRun(t, "Stop recording IP addresses",
		"account", "security-log", "disable", "--detailed")

	_, stderr := runOKStderr(t, "account", "security-log", "enable", "--detailed")
	assertContains(t, stderr, "detailed events")
	if detailed := runJSON(t, "account", "security-log", "get")["detailed"]; detailed != "on" {
		t.Errorf("detailed = %v after enabling it, want on", detailed)
	}

	runOK(t, "account", "security-log", "disable", "--detailed")
	after := runJSON(t, "account", "security-log", "get")
	if after["detailed"] != "off" {
		t.Errorf("detailed = %v after disabling it, want off", after["detailed"])
	}
	// Only the detail went: the log itself is still recording.
	if after["status"] != "on" {
		t.Errorf("status = %v, want the log still on", after["status"])
	}
}

// A toggle already in the position asked for is a refusal rather than a request
// that changes nothing.
func TestAccountSecurityLogRefusesWhatIsAlreadyTheCase(t *testing.T) {
	state := runJSON(t, "account", "security-log", "get")
	if state["status"] != "on" {
		t.Fatal("the primary account records sign-ins, and the tests here read them;" +
			" run `proton --profile primary account security-log enable`")
	}

	_, stderr, code := run(t, "account", "security-log", "enable")
	if code != 1 {
		t.Errorf("enabling what is already on: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already on")

	_, stderr, code = run(t, "account", "security-log", "disable", "--detailed")
	if code != 1 {
		t.Errorf("turning off a detail that is already off: exit %d, want 1\nstderr: %s",
			code, truncateOutput(stderr))
	}
}

// Turning the log off would take its events with it, so it is refused while
// there are any and says which command removes them.
func TestAccountSecurityLogRefusesToDisableWhileItHoldsEvents(t *testing.T) {
	events, _ := runJSON(t, "account", "security-log", "get")["events"].(float64)
	if events == 0 {
		t.Fatal("the primary account's log is empty, so there is nothing for this to protect;" +
			" sign in to https://account.proton.me as it once")
	}

	_, stderr, code := run(t, "account", "security-log", "disable")
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "security-log delete")
}

// The preview of a wipe, which is as far as a run may go: it counts what would
// go and sends nothing.
func TestAccountSecurityLogDeleteDryRun(t *testing.T) {
	_, stderr := runOKStderr(t, "--dry-run", "account", "security-log", "delete")
	assertContains(t, stderr, "Dry run")
	assertContains(t, stderr, "event")

	// Nothing went: the count is what it was.
	if events, _ := runJSON(t, "account", "security-log", "get")["events"].(float64); events == 0 {
		t.Error("a preview removed the events it was previewing")
	}
}
