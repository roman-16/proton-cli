package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binaryPath string

func TestMain(m *testing.M) {
	// Build the binary once before all tests.
	dir, err := os.MkdirTemp("", "proton-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	binaryPath = filepath.Join(dir, "proton-cli")
	cmd := exec.Command("go", "build", "-o", binaryPath, "..")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ── Helpers ──

func skipIfNoCredentials(t *testing.T) {
	t.Helper()
	if os.Getenv("PROTON_USER") == "" || os.Getenv("PROTON_PASSWORD") == "" {
		t.Skip("PROTON_USER and PROTON_PASSWORD not set")
	}
}

// run executes the CLI binary with the given args and returns stdout, stderr, and exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Env = os.Environ()
	err := cmd.Run()

	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run command: %v", err)
		}
	}

	return outBuf.String(), errBuf.String(), exitCode
}

// runOK runs a command and fails the test if it exits non-zero.
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := run(t, args...)
	if code != 0 {
		t.Fatalf("command %v failed (exit %d):\nstdout: %s\nstderr: %s", args, code, stdout, stderr)
	}
	return stdout
}

// runJSON runs a command with --json and parses the output.
func runJSON(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	stdout := runOK(t, append(args, "--json")...)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nraw: %s", err, stdout)
	}
	return result
}

// runJSONArray runs a command with --json and parses the output as an array.
func runJSONArray(t *testing.T, args ...string) []interface{} {
	t.Helper()
	stdout := runOK(t, append(args, "--json")...)
	var result []interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON array output: %v\nraw: %s", err, stdout)
	}
	return result
}

// testID generates a unique identifier for test artifacts.
func testID() string {
	return fmt.Sprintf("proton-cli-test-%d-%d", time.Now().UnixMilli(), rand.Intn(10000))
}

// jsonField extracts a string field from a JSON map.
func jsonField(data map[string]interface{}, path ...string) string {
	current := data
	for i, key := range path {
		val, ok := current[key]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			switch v := val.(type) {
			case string:
				return v
			case float64:
				return fmt.Sprintf("%.0f", v)
			default:
				return fmt.Sprintf("%v", v)
			}
		}
		next, ok := val.(map[string]interface{})
		if !ok {
			return ""
		}
		current = next
	}
	return ""
}

// assertContains fails if stdout doesn't contain the substring.
func assertContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if !strings.Contains(stdout, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

// assertNotContains fails if stdout contains the substring.
func assertNotContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if strings.Contains(stdout, substr) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

// cleanup registers a cleanup function that logs a loud error if it fails.
// The description should tell the user exactly what to delete manually.
func cleanup(t *testing.T, description string, fn func() error) {
	t.Helper()
	t.Cleanup(func() {
		if err := fn(); err != nil {
			t.Logf("\n" +
				"╔══════════════════════════════════════════════════════════════╗\n" +
				"║  ⚠️  CLEANUP FAILED — MANUAL ACTION REQUIRED                ║\n" +
				"╠══════════════════════════════════════════════════════════════╣\n" +
				"║  %s\n" +
				"║  Error: %s\n" +
				"╚══════════════════════════════════════════════════════════════╝",
				description, err)
		}
	})
}

// cleanupRun registers a cleanup that runs a CLI command. Logs loudly on failure.
func cleanupRun(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanup(t, description, func() error {
		_, stderr, code := run(t, args...)
		if code != 0 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})
}

func truncateOutput(s string) string {
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// firstMessageID returns the first message ID from mail list in a given folder.
func firstMessageID(t *testing.T, folder string) string {
	t.Helper()
	data := runJSON(t, "mail", "list", "--folder", folder)
	messages, ok := data["Messages"].([]interface{})
	if !ok || len(messages) == 0 {
		t.Fatalf("no messages found in %s", folder)
	}
	msg := messages[0].(map[string]interface{})
	return msg["ID"].(string)
}

// selfEmail returns the PROTON_USER email.
func selfEmail() string {
	return os.Getenv("PROTON_USER")
}

// sendTestMail sends a message to self and returns the message ID.
// Registers cleanup to permanently delete the message.
func sendTestMail(t *testing.T, subject string) string {
	t.Helper()

	// Send
	runOK(t, "mail", "send", "--to", selfEmail(), "--subject", subject, "--body", "Integration test body: "+subject)

	// Wait a moment for delivery
	time.Sleep(3 * time.Second)

	// Find it in sent
	data := runJSON(t, "mail", "list", "--folder", "sent")
	messages := data["Messages"].([]interface{})
	for _, m := range messages {
		msg := m.(map[string]interface{})
		if msg["Subject"].(string) == subject {
			sentID := msg["ID"].(string)
			cleanupRun(t, fmt.Sprintf("Delete sent message: proton-cli mail delete -- %s", sentID),
				"mail", "delete", "--", sentID)

			// Also find and delete the inbox copy
			time.Sleep(2 * time.Second)
			inboxData := runJSON(t, "mail", "list", "--folder", "inbox")
			inboxMsgs := inboxData["Messages"].([]interface{})
			for _, im := range inboxMsgs {
				imsg := im.(map[string]interface{})
				if imsg["Subject"].(string) == subject {
					inboxID := imsg["ID"].(string)
					cleanupRun(t, fmt.Sprintf("Delete inbox message: proton-cli mail delete -- %s", inboxID),
						"mail", "delete", "--", inboxID)
					return inboxID
				}
			}

			// If not in inbox yet, return sent ID
			return sentID
		}
	}

	t.Fatalf("sent message with subject %q not found", subject)
	return ""
}
