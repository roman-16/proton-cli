package live

import (
	"fmt"
	"testing"
)

func TestAccountNotificationsList(t *testing.T) {
	stdout := runOK(t, "account", "settings", "notifications", "list")
	for _, want := range []string{"NAME", "DESCRIPTION", "newsletter", "in-app"} {
		assertContains(t, stdout, want)
	}
	rows := runJSONArray(t, "account", "settings", "notifications", "list")
	if len(rows) != 15 {
		t.Errorf("%d notifications, want the 15 Proton's settings offer", len(rows))
	}
	for _, row := range rows {
		m, _ := row.(map[string]interface{})
		for _, key := range []string{"name", "description", "detail"} {
			if value, _ := m[key].(string); value == "" {
				t.Errorf("%s is empty in %v", key, m)
			}
		}
		if _, ok := m["on"].(bool); !ok {
			t.Errorf("on is not a yes or no in %v", m)
		}
	}
}

func TestAccountNotificationsSwitchAndPutBack(t *testing.T) {
	const name = "surveys"
	before := notificationOn(t, name)
	flip, back := "disable", "enable"
	if !before {
		flip, back = "enable", "disable"
	}
	cleanup(t, fmt.Sprintf("put the primary account's %s notification back: "+
		"proton --profile primary account settings notifications %s %s", name, back, name),
		func() error {
			if notificationOn(t, name) == before {
				return nil
			}
			if _, _, code := run(t, "account", "settings", "notifications", back, name); code != 0 {
				return fmt.Errorf("exit %d", code)
			}
			return nil
		})

	_, stderr := runOKStderr(t, "account", "settings", "notifications", flip, name)
	assertContains(t, stderr, `notification "`+name+`"`)
	if notificationOn(t, name) == before {
		t.Errorf("%s is unchanged after %s", name, flip)
	}
	_, stderr, code := run(t, "account", "settings", "notifications", flip, name)
	if code != 1 {
		t.Errorf("a second %s: exit %d, want 1\nstderr: %s", flip, code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "already")
	runOK(t, "account", "settings", "notifications", back, name)
	if notificationOn(t, name) != before {
		t.Errorf("%s did not come back after %s", name, back)
	}
}

func notificationOn(t *testing.T, name string) bool {
	t.Helper()
	for _, row := range runJSONArray(t, "account", "settings", "notifications", "list") {
		m, _ := row.(map[string]interface{})
		if m["name"] == name {
			on, _ := m["on"].(bool)
			return on
		}
	}
	t.Fatalf("no %s notification in the listing", name)
	return false
}
