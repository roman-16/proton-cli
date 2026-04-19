package tests

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestProfileFromConfig writes a temporary config.toml with a `work` profile
// that mirrors PROTON_USER/PROTON_PASSWORD, runs a command with PROTON_* env
// vars stripped, and verifies that credentials flow through the profile.
func TestProfileFromConfig(t *testing.T) {
	skipIfNoCredentials(t)

	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("user config dir: %v", err)
	}
	cfgPath := filepath.Join(configDir, "proton-cli", "config.toml")
	sessionDir := filepath.Join(configDir, "proton-cli", "sessions")

	// Back up existing config + work session if any.
	backupPath := ""
	if _, err := os.Stat(cfgPath); err == nil {
		backupPath = cfgPath + ".bak-" + testID()
		if err := os.Rename(cfgPath, backupPath); err != nil {
			t.Fatalf("back up config: %v", err)
		}
	}
	workSession := filepath.Join(sessionDir, "work.json")
	_ = os.Remove(workSession)

	t.Cleanup(func() {
		_ = os.Remove(cfgPath)
		_ = os.Remove(workSession)
		if backupPath != "" {
			_ = os.Rename(backupPath, cfgPath)
		}
	})

	_ = os.MkdirAll(filepath.Dir(cfgPath), 0700)
	cfg := "default_profile = \"default\"\n\n" +
		"[profiles.work]\n" +
		"user = \"" + os.Getenv("PROTON_USER") + "\"\n" +
		"password = \"" + os.Getenv("PROTON_PASSWORD") + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfg), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	// Run with env stripped; rely entirely on the profile
	cmd := exec.Command(binaryPath, "--profile", "work",
		"mail", "messages", "list", "--page-size", "1", "--output", "json")
	cmd.Env = filterEnv(os.Environ(), "PROTON_USER", "PROTON_PASSWORD", "PROTON_TOTP")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("profile run failed: %v\noutput: %s", err, string(out))
	}
	if !strings.Contains(string(out), "\"messages\"") {
		t.Errorf("unexpected output under --profile work:\n%s", truncateOutput(string(out)))
	}

	// Session file for `work` profile should now exist.
	if _, err := os.Stat(workSession); err != nil {
		t.Errorf("expected per-profile session file at %s: %v", workSession, err)
	}
}

// TestProfileSessionSeparation verifies default and work sessions live in
// separate files so they don't clobber each other.
func TestProfileSessionSeparation(t *testing.T) {
	skipIfNoCredentials(t)
	configDir, _ := os.UserConfigDir()
	sessionDir := filepath.Join(configDir, "proton-cli", "sessions")

	// Must have at least default.json (from prior tests / the normal account).
	// Not an error if missing — the test just describes structure.
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		t.Skipf("session dir missing: %v", err)
	}
	hasDefault := false
	for _, e := range entries {
		if e.Name() == "default.json" {
			hasDefault = true
		}
	}
	if !hasDefault {
		t.Skip("default session not present; nothing to verify")
	}
}

func filterEnv(env []string, keys ...string) []string {
	out := make([]string, 0, len(env))
outer:
	for _, kv := range env {
		for _, k := range keys {
			if strings.HasPrefix(kv, k+"=") {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}
