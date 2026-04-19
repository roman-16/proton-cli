package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var binaryPath string

// TestMain builds the CLI binary once before any integration test runs.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "proton-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	binaryPath = filepath.Join(dir, "proton-cli")
	cmd := exec.Command("go", "build", "-o", binaryPath, "..")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

// ── Credential gate ──

func skipIfNoCredentials(t *testing.T) {
	t.Helper()
	if os.Getenv("PROTON_USER") == "" || os.Getenv("PROTON_PASSWORD") == "" {
		t.Skip("PROTON_USER and PROTON_PASSWORD not set")
	}
}

// ── Running the binary ──

// run executes the CLI with args and returns stdout, stderr, exit code.
func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runWithStdin(t, nil, args...)
}

// runWithStdin is run() with arbitrary stdin bytes attached.
func runWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = stdin
	cmd.Env = os.Environ()
	err := cmd.Run()

	exitCode = 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("failed to run command %v: %v", args, err)
		}
	}
	return outBuf.String(), errBuf.String(), exitCode
}

// runOK runs a command and fails the test on non-zero exit.
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := run(t, args...)
	if code != 0 {
		t.Fatalf("command %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout
}

// runOKStderr runs a command and returns both stdout + stderr on success.
func runOKStderr(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := run(t, args...)
	if code != 0 {
		t.Fatalf("command %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

// runJSON runs with `--output json` and parses stdout as a JSON object.
func runJSON(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	stdout := runOK(t, append(args, "--output", "json")...)
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON object: %v\nraw: %s", err, truncateOutput(stdout))
	}
	return result
}

// runJSONArray runs with `--output json` and parses stdout as a JSON array.
func runJSONArray(t *testing.T, args ...string) []interface{} {
	t.Helper()
	stdout := runOK(t, append(args, "--output", "json")...)
	var result []interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON array: %v\nraw: %s", err, truncateOutput(stdout))
	}
	return result
}

// ── Naming ──

// testID returns a unique prefix for artifacts. Also usable as part of a name.
func testID() string {
	return fmt.Sprintf("proton-cli-test-%d-%d", time.Now().UnixMilli(), rand.Intn(10000))
}

// ── Assertions ──

func assertContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if !strings.Contains(stdout, substr) {
		t.Errorf("expected output to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

func assertNotContains(t *testing.T, stdout, substr string) {
	t.Helper()
	if strings.Contains(stdout, substr) {
		t.Errorf("expected output NOT to contain %q, got:\n%s", substr, truncateOutput(stdout))
	}
}

// assertField checks that "Key: Value" line exists.
func assertField(t *testing.T, stdout, field, expected string) {
	t.Helper()
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, field) {
			value := strings.TrimSpace(strings.TrimPrefix(line, field))
			if value == expected {
				return
			}
			t.Errorf("field %s: got %q, want %q", field, value, expected)
			return
		}
	}
	t.Errorf("field %s not found in:\n%s", field, truncateOutput(stdout))
}

// ── Cleanup ──

// cleanup registers a cleanup fn that logs loudly on failure.
func cleanup(t *testing.T, description string, fn func() error) {
	t.Helper()
	t.Cleanup(func() {
		if err := fn(); err != nil {
			t.Logf("\n"+
				"╔══════════════════════════════════════════════════════════════╗\n"+
				"║  ⚠️  CLEANUP FAILED — MANUAL ACTION REQUIRED                ║\n"+
				"╠══════════════════════════════════════════════════════════════╣\n"+
				"║  %s\n"+
				"║  Error: %s\n"+
				"╚══════════════════════════════════════════════════════════════╝",
				description, err)
		}
	})
}

// cleanupRun registers a cleanup that invokes the CLI.
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

// ── Convenience ──

func truncateOutput(s string) string {
	if len(s) > 500 {
		return s[:500] + "...(truncated)"
	}
	return s
}

// selfEmail returns PROTON_USER.
func selfEmail() string { return os.Getenv("PROTON_USER") }

// sendTestMail sends a mail to self, polls until delivered, registers cleanup
// for both the sent and inbox copies, and returns the inbox message ID.
func sendTestMail(t *testing.T, subject string) string {
	t.Helper()

	runOK(t, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject,
		"--body", "Integration test body: "+subject)

	// Find in sent, with a few retries while the mail propagates.
	var sentID string
	for attempt := 0; attempt < 10; attempt++ {
		time.Sleep(2 * time.Second)
		data := runJSON(t, "mail", "messages", "list", "--folder", "sent", "--page-size", "20")
		messages, _ := data["messages"].([]interface{})
		for _, m := range messages {
			msg := m.(map[string]interface{})
			if s, _ := msg["subject"].(string); s == subject {
				sentID, _ = msg["id"].(string)
				break
			}
		}
		if sentID != "" {
			break
		}
	}
	if sentID == "" {
		t.Fatalf("sent mail %q did not show up in sent folder", subject)
	}
	cleanupRun(t, fmt.Sprintf("Delete sent mail: proton-cli mail messages delete -- %s", sentID),
		"mail", "messages", "delete", "--", sentID)

	// Inbox copy may take another beat.
	var inboxID string
	for attempt := 0; attempt < 10; attempt++ {
		time.Sleep(2 * time.Second)
		data := runJSON(t, "mail", "messages", "list", "--folder", "inbox", "--page-size", "20")
		messages, _ := data["messages"].([]interface{})
		for _, m := range messages {
			msg := m.(map[string]interface{})
			if s, _ := msg["subject"].(string); s == subject {
				inboxID, _ = msg["id"].(string)
				break
			}
		}
		if inboxID != "" {
			break
		}
	}
	if inboxID != "" {
		cleanupRun(t, fmt.Sprintf("Delete inbox mail: proton-cli mail messages delete -- %s", inboxID),
			"mail", "messages", "delete", "--", inboxID)
		return inboxID
	}
	// Fall back to sent ID if inbox copy never appeared.
	return sentID
}
