package live

import (
	"fmt"
	"testing"
)

// The mail settings page.
//
// Named values are the point of the typed key table: nobody should have to
// remember that "conversations" is zero.

func mailViewMode(t *testing.T) int {
	t.Helper()
	data := runJSON(t, "api", "GET", "/mail/v4/settings")
	ms, ok := data["MailSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("no MailSettings in response: %v", data)
	}
	vm, ok := ms["ViewMode"].(float64)
	if !ok {
		t.Fatalf("no ViewMode in MailSettings: %v", ms)
	}
	return int(vm)
}

// Named values are the point of the typed key table: nobody should have to
// remember that "conversations" is zero.
func TestMailSettingsSetByName(t *testing.T) {
	orig := mailViewMode(t)
	origName, targetName, targetValue := "conversations", "messages", 1
	if orig == 1 {
		origName, targetName, targetValue = "messages", "conversations", 0
	}
	cleanup(t, fmt.Sprintf("Restore mail view mode: proton mail settings set view-mode %s", origName),
		func() error {
			if _, _, code := run(t, "mail", "settings", "set", "view-mode", origName); code != 0 {
				return fmt.Errorf("restore exit %d", code)
			}
			return nil
		})

	runOK(t, "mail", "settings", "set", "view-mode", targetName)
	if got := mailViewMode(t); got != targetValue {
		t.Errorf("ViewMode after setting %q: got %d want %d", targetName, got, targetValue)
	}
	// The numeric form Proton itself uses stays valid.
	runOK(t, "mail", "settings", "set", "view-mode", fmt.Sprintf("%d", targetValue))
}

// With no arguments, `set` lists the writable keys grouped by the settings page
// they come from.
func TestMailSettingsSetListsKeysByPage(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "list")
	for _, want := range []string{"General", "Email privacy", "view-mode", "hide-remote-images"} {
		assertContains(t, stdout, want)
	}
}

func TestMailSettingsSetDryRun(t *testing.T) {
	orig := mailViewMode(t)
	_, stderr := runOKStderr(t, "--dry-run", "mail", "settings", "set", "view-mode", "messages")
	assertContains(t, stderr, "Dry run")
	if got := mailViewMode(t); got != orig {
		t.Error("--dry-run changed the setting")
	}
}

// A display name belongs to an address, not to the mail settings page.
func TestMailSettings(t *testing.T) {
	stdout := runOK(t, "mail", "settings", "get")
	for _, want := range []string{"Page Size", "View Mode", "Draft Format", "Auto-reply"} {
		assertContains(t, stdout, want)
	}
}
