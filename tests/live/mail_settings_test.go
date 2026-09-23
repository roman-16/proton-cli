package live

import (
	"fmt"
	"strings"
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
	for _, want := range []string{"Messages and composing", "Email privacy", "view-mode", "hide-remote-images"} {
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
	for _, want := range []string{"Page Size", "View Mode", "Draft Type", "Auto-reply"} {
		assertContains(t, stdout, want)
	}
}

func TestMailSettingsRoundTripNames(t *testing.T) {
	for key, values := range map[string][2]string{
		"block-sender-confirmation": {"on", "off"},
		"category-view":             {"on", "off"},
		"category-view-counters":    {"on", "off"},
		"font-face":                 {"arial", "georgia"},
		"font-size":                 {"14", "16"},
		"image-proxy":               {"on", "off"},
		"remove-image-metadata":     {"on", "off"},
		"spam-action":               {"ask", "just-move"},
	} {
		t.Run(key, func(t *testing.T) {
			field := strings.ReplaceAll(key, "-", "_")
			original, ok := runJSON(t, "mail", "settings", "get")[field].(string)
			if !ok || original == "" {
				t.Fatalf("get reports no %s", field)
			}
			target := values[0]
			if original == target {
				target = values[1]
			}
			cleanupRun(t, "Restore "+key+": proton mail settings set "+key+" "+original,
				"mail", "settings", "set", key, original)

			result := runJSON(t, "mail", "settings", "set", key, target)
			if result["value"] != target {
				t.Errorf("set reported value %v, want %q", result["value"], target)
			}
			if got := runJSON(t, "mail", "settings", "get")[field]; got != target {
				t.Errorf("%s after setting %q: got %v", field, target, got)
			}
		})
	}
}

func imageProxyBits(t *testing.T) int {
	t.Helper()
	ms, ok := runJSON(t, "api", "GET", "/mail/v4/settings")["MailSettings"].(map[string]interface{})
	if !ok {
		t.Fatal("no MailSettings in response")
	}
	bits, ok := ms["ImageProxy"].(float64)
	if !ok {
		t.Fatalf("no ImageProxy in MailSettings: %v", ms)
	}
	return int(bits)
}

func putImageProxyBit(t *testing.T, bit, action int) error {
	_, _, code := run(t, "api", "PUT", "/mail/v4/settings/imageproxy",
		"--body", fmt.Sprintf(`{"ImageProxy":%d,"Action":%d}`, bit, action))
	if code != 0 {
		return fmt.Errorf("imageproxy bit %d action %d: exit %d", bit, action, code)
	}
	return nil
}

func TestMailSettingsImageProxyOffAlsoClearsStoringRemoteContent(t *testing.T) {
	original := imageProxyBits(t)
	cleanup(t, fmt.Sprintf("Restore the image proxy: proton api PUT /mail/v4/settings/imageproxy for ImageProxy %d",
		original), func() error {
		for _, step := range [][2]int{{2, original & 2 / 2}, {1, original & 1}} {
			if err := putImageProxyBit(t, step[0], step[1]); err != nil {
				return err
			}
		}
		return nil
	})

	for _, bit := range []int{2, 1} {
		if err := putImageProxyBit(t, bit, 1); err != nil {
			t.Fatal(err)
		}
	}
	runOK(t, "mail", "settings", "set", "image-proxy", "off")
	if got := imageProxyBits(t); got != 0 {
		t.Errorf("ImageProxy after image-proxy off: got %d, want 0", got)
	}
	runOK(t, "mail", "settings", "set", "image-proxy", "on")
	if got := imageProxyBits(t); got != 2 {
		t.Errorf("ImageProxy after image-proxy on: got %d, want 2", got)
	}
}
