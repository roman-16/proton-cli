package live

import "testing"

func TestAccountSentinelGet(t *testing.T) {
	stdout := runOK(t, "account", "settings", "sentinel", "get")
	for _, want := range []string{"Status:", "Emails:"} {
		assertContains(t, stdout, want)
	}
	state := runJSON(t, "account", "settings", "sentinel", "get")
	for _, key := range []string{"status", "emails", "eligible", "enforced"} {
		if _, ok := state[key]; !ok {
			t.Errorf("missing %q in %v", key, keysOf(state))
		}
	}
	if eligible, _ := state["eligible"].(bool); eligible {
		t.Error("the primary account is on the free plan, which does not include Proton Sentinel")
	}
	if got := runJSON(t, "account", "settings", "get")["sentinel"]; got != state["status"] {
		t.Errorf("settings get reports Sentinel %v, sentinel get %v", got, state["status"])
	}
}

func TestAccountSentinelNeedsThePlan(t *testing.T) {
	for _, args := range [][]string{
		{"account", "settings", "sentinel", "enable"},
		{"account", "settings", "sentinel", "enable", "--emails"},
	} {
		_, stderr, code := run(t, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1\nstderr: %s", args, code, truncateOutput(stderr))
		}
		assertContains(t, stderr, "does not include Proton Sentinel")
	}
}

func TestAccountSentinelSwitchAndPutBack(t *testing.T) {
	if eligible, _ := runJSONPaid(t, "account", "settings", "sentinel", "get")["eligible"].(bool); !eligible {
		t.Fatal("the paid account's plan does not include Proton Sentinel, and it is the account the switch is tested on")
	}
	switchFeatureAndPutBack(t, []string{"account", "settings", "sentinel", "get"},
		[]string{"account", "settings", "sentinel"}, "status", "emails")
}
