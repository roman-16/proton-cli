package tests

import "testing"

func TestSettingsGet(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "get")
	assertContains(t, stdout, "Locale:")
	assertContains(t, stdout, "Recovery Email:")
	assertContains(t, stdout, "Week Start:")
	assertContains(t, stdout, "Date Format:")
	assertContains(t, stdout, "High Security:")
}

func TestSettingsGetJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "settings", "get")
	us, ok := data["UserSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected UserSettings in JSON output")
	}
	if us["Locale"] == nil {
		t.Error("UserSettings missing Locale")
	}
}

func TestSettingsMail(t *testing.T) {
	skipIfNoCredentials(t)
	stdout := runOK(t, "settings", "mail")
	assertContains(t, stdout, "Page Size:")
	assertContains(t, stdout, "View Mode:")
	assertContains(t, stdout, "Composer Mode:")
	assertContains(t, stdout, "Draft MIME Type:")
	assertContains(t, stdout, "Delay Send:")
}

func TestSettingsMailJSON(t *testing.T) {
	skipIfNoCredentials(t)
	data := runJSON(t, "settings", "mail")
	ms, ok := data["MailSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("expected MailSettings in JSON output")
	}
	if ms["PageSize"] == nil {
		t.Error("MailSettings missing PageSize")
	}
	if ms["ViewMode"] == nil {
		t.Error("MailSettings missing ViewMode")
	}
}
