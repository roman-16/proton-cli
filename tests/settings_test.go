package tests

import "testing"

func TestSettingsGet(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "get")
	assertContains(t, stdout, "Locale")
}

func TestSettingsMail(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "mail")
	assertContains(t, stdout, "Display Name")
	assertContains(t, stdout, "Page Size")
}

func TestSettingsGetJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "settings", "get")
	if _, ok := data["UserSettings"]; !ok {
		t.Error("expected UserSettings key in JSON output")
	}
}
