package tests

import "testing"

func TestSettingsGet(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "get")
	assertContains(t, stdout, "Locale:")
	assertContains(t, stdout, "Recovery Email:")
}

func TestSettingsGetJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "settings", "get")
	if _, ok := data["UserSettings"]; !ok {
		t.Error("expected UserSettings in JSON output")
	}
}

func TestSettingsMail(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "mail")
	assertContains(t, stdout, "Page Size:")
	assertContains(t, stdout, "View Mode:")
}

func TestSettingsMailJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "settings", "mail")
	if _, ok := data["MailSettings"]; !ok {
		t.Error("expected MailSettings in JSON output")
	}
}
