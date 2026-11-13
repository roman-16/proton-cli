package live

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
	"github.com/roman-16/proton-cli/tests/argv"
	"github.com/roman-16/proton-cli/tests/paid"
)

func runAs(profile string, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int, err error) {
	a, ok := accounts[profile]
	if !ok {
		return "", "", -1, fmt.Errorf("unknown test profile %q", profile)
	}
	if profile == account.Paid {
		if why := paid.OffLimits(args); why != "" {
			return "", "", -1, fmt.Errorf("refusing to run %v as the paid account: %s", args, why)
		}
	}
	args = withPassword(a, args)

	var outBuf, errBuf bytes.Buffer
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.Stdin = stdin
	cmd.Env = childEnv(profile)
	if tracingRequests() {
		cmd.Env = append(cmd.Env, "PROTON_LOG_LEVEL=debug")
	}

	started := time.Now()
	runErr := cmd.Run()
	elapsed := time.Since(started)

	exitCode = 0
	if runErr == nil && answersInvitation(args) {
		noticesCaused.Add(1)
	}
	if runErr != nil {
		exitErr, ok := runErr.(*exec.ExitError)
		if !ok {
			return outBuf.String(), errBuf.String(), -1, runErr
		}
		exitCode = exitErr.ExitCode()
	}
	return outBuf.String(), trace(profile, args, elapsed, exitCode, errBuf.String()), exitCode, nil
}

var reauthCommands = [][]string{
	{"account", "delete"},
	{"account", "keys", "create"},
	{"account", "keys", "delete"},
	{"account", "keys", "export"},
	{"account", "keys", "import"},
	{"account", "keys", "update"},
	{"account", "security-log", "delete"},
	{"account", "security-log", "disable"},
	{"account", "security-log", "enable"},
	{"account", "sessions", "revoke"},
	{"account", "settings", "emergency-access", "add"},
	{"account", "settings", "emergency-access", "cancel"},
	{"account", "settings", "emergency-access", "grant"},
	{"account", "settings", "emergency-access", "remove"},
	{"account", "settings", "emergency-access", "update"},
	{"account", "settings", "password", "set"},
	{"account", "settings", "recovery-contacts", "add"},
	{"account", "settings", "recovery-contacts", "remove"},
	{"account", "settings", "recovery-email", "disable"},
	{"account", "settings", "recovery-email", "enable"},
	{"account", "settings", "recovery-email", "set"},
	{"account", "settings", "recovery-phone", "disable"},
	{"account", "settings", "recovery-phone", "enable"},
	{"account", "settings", "recovery-phone", "set"},
	{"account", "settings", "recovery-phrase", "disable"},
	{"account", "settings", "recovery-phrase", "set"},
	{"account", "settings", "second-password", "disable"},
	{"account", "settings", "second-password", "enable"},
	{"account", "settings", "second-password", "set"},
	{"account", "settings", "security-keys", "create"},
	{"account", "settings", "security-keys", "delete"},
	{"account", "settings", "two-factor", "disable"},
	{"account", "settings", "two-factor", "enable"},
	{"calendar", "settings", "calendars", "delete"},
	{"mail", "conversations", "update"},
	{"mail", "messages", "update"},
	{"mail", "settings", "addresses", "create"},
	{"mail", "settings", "addresses", "delete"},
	{"mail", "settings", "addresses", "disable"},
	{"mail", "settings", "addresses", "enable"},
	{"mail", "settings", "domains", "create"},
	{"mail", "settings", "domains", "delete"},
	{"mail", "settings", "domains", "update"},
	{"mail", "settings", "forwarding", "accept"},
	{"mail", "settings", "forwarding", "create"},
	{"mail", "settings", "forwarding", "delete"},
	{"mail", "settings", "autoreply", "disable"},
	{"mail", "settings", "autoreply", "enable"},
	{"mail", "settings", "autoreply", "set"},
	{"mail", "settings", "smtp-tokens", "create"},
	{"mail", "settings", "smtp-tokens", "delete"},
}

func withPassword(a *testAccount, args []string) []string {
	if a.passwordFile == "" {
		return args
	}
	for _, cmd := range reauthCommands {
		at := argv.At(args, cmd...)
		if at < 0 {
			continue
		}
		out := make([]string, 0, len(args)+2)
		out = append(out, args[:at+len(cmd)]...)
		out = append(out, "--password-file", a.passwordFile)
		return append(out, args[at+len(cmd):]...)
	}
	return args
}

// answeringCommands are the commands that make Proton write to whoever offered
// something. Answering is the whole set: the offerer is told, by mail, whichever
// way it was answered, and nothing on this end turns that off.
//
// It is recognised from the args in the one place a command is run, so no test
// has to remember that what it just did will land in somebody's inbox a moment
// later - and the paid account's sweep knows how many to wait for.
var answeringCommands = [][]string{
	{"invitations", "accept"},
	{"invitations", "decline"},
	{"mail", "settings", "forwarding", "accept"},
	{"mail", "settings", "forwarding", "decline"},
}

func answersInvitation(args []string) bool {
	if slices.Contains(args, "--dry-run") {
		return false
	}
	for _, cmd := range answeringCommands {
		if argv.Has(args, cmd...) {
			return true
		}
	}
	return false
}

func childEnv(profile string) []string {
	env := []string{
		"PROTON_PROFILE=" + profile,
		"PROTON_NO_INPUT=1",
	}
	for _, k := range []string{
		"PATH", "HOME", "TMPDIR", "USER", "LANG", "TZ",
		"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR",
	} {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	return env
}

func withEnv(env []string, overrides map[string]string) []string {
	out := make([]string, 0, len(env)+len(overrides))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if _, replaced := overrides[name]; !replaced {
			out = append(out, kv)
		}
	}
	for k, v := range overrides {
		out = append(out, k+"="+v)
	}
	return out
}

func runProfile(t *testing.T, profile string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runAs(profile, nil, args...)
	if err != nil {
		t.Fatalf("failed to run %v as the %s account: %v", args, profile, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

func runOKProfile(t *testing.T, profile string, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := runProfile(t, profile, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed as the %s account (exit %d):\nstdout: %s\nstderr: %s",
			args, profile, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runProfile(t, account.Primary, args...)
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runOKProfile(t, account.Primary, args...)
	return stdout
}

func runOKStderr(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	return runOKProfile(t, account.Primary, args...)
}

func runOKBothStreams(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr := runOKProfile(t, account.Primary, args...)
	return stdout + stderr
}

func runJSON(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOK(t, asJSON(args)...))
}

func runJSONArray(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOK(t, asJSON(args)...))
}

// listAll is runJSONArray for a listing a test searches rather than samples.
//
// A page holds fifty rows unless something says otherwise, so a test asking
// whether the account holds the thing it just made is otherwise asking only
// about the first page of it. What is on that page depends on how much the
// account happens to hold, which is how a test that looks for one item passes
// for a year and then fails without anything having changed.
func listAll(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return runJSONArray(t, whole(args)...)
}

func listAllSecondary(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return runJSONArraySecondary(t, whole(args)...)
}

func whole(args []string) []string {
	return append(append([]string{}, args...), "--limit", "0")
}

func runArgs(stdin io.Reader, args ...string) (stdout, stderr string, exitCode int, err error) {
	return runAs(account.Primary, stdin, args...)
}

func runWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runArgs(stdin, args...)
	if err != nil {
		t.Fatalf("failed to run command %v: %v", args, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

func runWithEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	args = withPassword(accounts[account.Primary], args)
	cmd := exec.Command(binaryPath, args...)
	cmd.Env = withEnv(childEnv(account.Primary), env)
	if tracingRequests() {
		cmd.Env = append(cmd.Env, "PROTON_LOG_LEVEL=debug")
	}
	var outB, errB strings.Builder
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	started := time.Now()
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		exit = ee.ExitCode()
	}
	return outB.String(), trace(account.Primary, args, time.Since(started), exit, errB.String()), exit
}

func runSecondary(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runProfile(t, account.Secondary, args...)
}

func runOKSecondary(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runOKProfile(t, account.Secondary, args...)
	return stdout
}

func runOKStderrSecondary(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	return runOKProfile(t, account.Secondary, args...)
}

func runJSONSecondary(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOKSecondary(t, asJSON(args)...))
}

func runJSONArraySecondary(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOKSecondary(t, asJSON(args)...))
}

// ── the paid account ──
//
// It is somebody's, so a test acts only on what it made: it never lists the
// account's own calendars, vaults, messages or files and acts on what it finds.
// What may not be done to it at all is declared in tests/paid and refused by
// runAs; the photograph either side of the run is what checks the promise.

func runPaid(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runProfile(t, account.Paid, args...)
}

func runOKPaid(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runOKProfile(t, account.Paid, args...)
	return stdout
}

func runOKStderrPaid(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	return runOKProfile(t, account.Paid, args...)
}

func runJSONPaid(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOKPaid(t, asJSON(args)...))
}

func runJSONArrayPaid(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOKPaid(t, asJSON(args)...))
}

func consenting(args []string) []string {
	return append([]string{"--yes"}, args...)
}

func asJSON(args []string) []string {
	return append([]string{"--output", "json"}, args...)
}

func parseJSONObject(t *testing.T, stdout string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON object: %v\nraw: %s", err, truncateOutput(stdout))
	}
	return result
}

func parseJSONArray(t *testing.T, stdout string) []interface{} {
	t.Helper()
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("collection output is not an envelope: %v\nraw: %s", err, truncateOutput(stdout))
	}
	var rows []interface{}
	found := ""
	for key, value := range env {
		if arr, ok := value.([]interface{}); ok {
			if found != "" {
				t.Fatalf("envelope has two arrays (%q and %q): %s", found, key, truncateOutput(stdout))
			}
			rows, found = arr, key
		}
	}
	if found == "" {
		t.Fatalf("envelope has no array of rows: %s", truncateOutput(stdout))
	}
	if count, ok := env["count"].(float64); !ok {
		t.Errorf("envelope has no count: %s", truncateOutput(stdout))
	} else if int(count) != len(rows) {
		t.Errorf("count is %d but %q has %d rows", int(count), found, len(rows))
	}
	return rows
}

func rowsOf(stdout string) ([]interface{}, bool) {
	var env map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		return nil, false
	}
	for key, value := range env {
		if key == "count" {
			continue
		}
		if arr, ok := value.([]interface{}); ok {
			return arr, true
		}
	}
	if _, ok := env["count"]; ok {
		return nil, true
	}
	return nil, false
}

// rateLimited is what the client says when Proton asks for room, and jailed how
// Proton says it has shut the account out of something for a while. Its own
// clients read the second as the first arriving in a body instead of a status,
// so the code is what is matched here: the sentence carrying it is Proton's to
// reword, 2028 is not.
const (
	rateLimited = "Proton is rate limiting this run"
	jailed      = "] 2028: "
)

// refuseToPush fails the test the first time Proton pushes back.
//
// The client backs off from a rate limit and would very likely succeed, so the
// suite would pass and nobody would learn anything - except that these are real
// accounts, and a run that has started being throttled is a run that should be
// asking for less rather than pressing on. A jail says the same thing with the
// waiting already decided, which is longer than any command sits there for:
// minutes after a burst, and days for one action repeated run after run, which
// is a test asking for more than Proton allows on every run.
func refuseToPush(t *testing.T, stderr string) {
	t.Helper()
	switch {
	case strings.Contains(stderr, rateLimited):
		t.Fatalf("Proton rate-limited this run. Give the account a few minutes before running it again.\n%s",
			truncateOutput(stderr))
	case strings.Contains(stderr, jailed):
		t.Fatalf("Proton has temporarily limited this account. A burst clears in a few minutes; "+
			"a test refused this way run after run asks for more than Proton allows every run.\n%s",
			truncateOutput(stderr))
	}
}

func waitFor(timeout, interval time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if check() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(interval)
	}
}
