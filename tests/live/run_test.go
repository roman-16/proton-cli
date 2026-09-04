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

// Running the binary.
//
// Every invocation goes through runAs. It is the one place a target account is
// chosen, so it is the one place the choice can be enforced: whatever a
// developer happens to have exported, the binary under test sees a stated
// environment and can act only as the profile named here - and a command the
// paid account refuses is refused before a process starts.

// runAs executes the CLI as one of the accounts. It returns stdout, stderr, the
// exit code, and a non-nil error only when the process failed to start.
//
// The arguments are otherwise passed through untouched. Consent is added by the
// helpers that demand success, never here: a runner that quietly agreed to
// everything would make `run` unable to observe a command being refused, which
// is the one thing the tests about refusal have to see.
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

// reauthCommands are the commands Proton may ask to re-authenticate, and so the
// ones that carry the credential flags. It mirrors the set the CLI declares,
// which internal/cli/conformance_test.go pins.
var reauthCommands = [][]string{
	{"calendar", "settings", "calendars", "delete"},
	{"mail", "messages", "expire"},
	{"mail", "settings", "autoreply", "disable"},
	{"mail", "settings", "autoreply", "enable"},
	{"mail", "settings", "autoreply", "set"},
}

// withPassword hands such a command the profile's password file.
//
// It goes directly after the command's own words: the flag belongs to that
// command rather than to the root, so it is unknown before the subcommand, and
// anything after the `--` the binary inserts ahead of a leading-dash ID would be
// read as an argument.
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
// something. Answering an invitation is the whole set: the owner is told, by
// mail, and nothing on this end turns that off.
//
// It is recognised from the args in the one place a command is run, so no test
// has to remember that what it just did will land in somebody's inbox a moment
// later - and the paid account's sweep knows how many to wait for.
var answeringCommands = [][]string{
	{"invitations", "accept"},
	{"invitations", "decline"},
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

// childEnv is the whole environment the binary under test runs in: what it needs
// to find its toolchain, its home and its session store, plus the profile to act
// as. Nothing else is carried over.
func childEnv(profile string) []string {
	env := []string{
		"PROTON_PROFILE=" + profile,
		// There is no terminal here, so a missing credential should be an error
		// rather than a question asked of nobody.
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

// withEnv overrides entries in a child environment by name, so a caller never
// has to reason about which of two settings of the same variable wins.
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

// ── running as an account ──
//
// One implementation, three names. Which account a test acts as is part of what
// it is testing, so it says so at the call site rather than in a variable.

// runProfile runs a command as one account and fails the test only if the
// process could not start. A non-zero exit is an answer, and the tests about
// refusal have to see it.
func runProfile(t *testing.T, profile string, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runAs(profile, nil, args...)
	if err != nil {
		t.Fatalf("failed to run %v as the %s account: %v", args, profile, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

// runOKProfile demands success, consenting on the test's behalf: the suite has
// no terminal, so anything that stops to ask before removing something would be
// refused here with nobody to answer, and every helper that demands success is
// doing setup or clearing up after itself.
func runOKProfile(t *testing.T, profile string, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := runProfile(t, profile, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed as the %s account (exit %d):\nstdout: %s\nstderr: %s",
			args, profile, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

// ── the primary account ──

func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runProfile(t, account.Primary, args...)
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, _ := runOKProfile(t, account.Primary, args...)
	return stdout
}

// runOKStderr is runOK when what the command said matters too.
func runOKStderr(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	return runOKProfile(t, account.Primary, args...)
}

// runOKBothStreams joins the two, for the commands that answer on whichever
// stream fits - `drive items share get` reports "Not shared." as information
// rather than as a record.
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

// runArgs executes the CLI as the primary account without a *testing.T, so the
// fixtures and cleanups that run outside a test can invoke it.
func runArgs(stdin io.Reader, args ...string) (stdout, stderr string, exitCode int, err error) {
	return runAs(account.Primary, stdin, args...)
}

// runWithStdin is run with arbitrary stdin bytes attached.
func runWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runArgs(stdin, args...)
	if err != nil {
		t.Fatalf("failed to run command %v: %v", args, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

// runWithEnv runs the CLI with extra variables layered over the child
// environment, for the handful of tests whose subject is one of them.
func runWithEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(binaryPath, withPassword(accounts[account.Primary], args)...)
	cmd.Env = withEnv(childEnv(account.Primary), env)
	var outB, errB strings.Builder
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	err := cmd.Run()
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("run %v: %v", args, err)
		}
		exit = ee.ExitCode()
	}
	return outB.String(), errB.String(), exit
}

// ── the second account ──
//
// A scenario needs it whenever it genuinely takes two Proton users: accepting a
// share invitation, receiving mail, or organizing an invite the primary RSVPs
// to. Run order matters - the primary invites or sends, the secondary accepts or
// receives - and a mutation made as one account registers its cleanup as the
// same one.

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

// ── what the runners put in front ──

// consenting puts --yes ahead of everything.
//
// The suite has no terminal, so anything that stops to ask before removing
// something would be refused here with nobody to answer. Every helper that
// demands success is doing setup or clearing up after itself, and means yes.
// `run` is deliberately left without it, so a test can still watch a command
// decline to act.
//
// The flag leads for the same reason --output json does: a Proton ID may begin
// with a dash, and anything after the `--` the binary then inserts would be read
// as an argument.
func consenting(args []string) []string {
	return append([]string{"--yes"}, args...)
}

// asJSON puts `--output json` ahead of everything, for the reason above.
func asJSON(args []string) []string {
	return append([]string{"--output", "json"}, args...)
}

// ── reading the answer ──

func parseJSONObject(t *testing.T, stdout string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON object: %v\nraw: %s", err, truncateOutput(stdout))
	}
	return result
}

// parseJSONArray returns the rows of a collection.
//
// Every list is an envelope keyed by its plural noun - {"messages": [...],
// "count": 3} - so this unwraps whichever array it finds rather than making every
// caller know the noun. The count is checked against it, since the two disagreeing
// would be a bug in the envelope itself.
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

// rowsOf unwraps a collection envelope without a *testing.T, for the photographs
// taken before any test starts and after the last one ends.
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
	// An empty collection is an envelope with no array in it, which is a
	// photograph of nothing rather than a failure to take one.
	if _, ok := env["count"]; ok {
		return nil, true
	}
	return nil, false
}

// ── when Proton asks for room ──

// rateLimited is what the client says when Proton asks for room.
const rateLimited = "rate limited by Proton"

// refuseToPush stops the run the first time Proton throttles it.
//
// The client backs off and would very likely succeed, so the suite would pass
// and nobody would learn anything - except that these are real accounts, and a
// run that has started being throttled is a run that should be asking for less
// rather than pressing on.
func refuseToPush(t *testing.T, stderr string) {
	t.Helper()
	if strings.Contains(stderr, rateLimited) {
		t.Fatalf("Proton rate-limited this run. Give the account a few minutes before running it again.\n%s",
			truncateOutput(stderr))
	}
}

// busy is how Proton says something else holds what a command wants.
//
// This is not rate limiting - which fails a run on purpose - but an account-wide
// lock on one folder or one background job, and Proton's own answer says to try
// again. It has more than one sentence for it ("Another action is currently in
// progress", "There is already an active operation on your folders or labels"),
// so the status is what is matched: the wording is Proton's to reword, 409 is
// not.
const busy = "[HTTP 409]"

// runOKUntilFree runs a command Proton may be busy with, and fails only once it
// has been saying so for a minute.
func runOKUntilFree(t *testing.T, args ...string) {
	t.Helper()
	var last string
	if waitFor(60*time.Second, 3*time.Second, func() bool {
		_, stderr, code := run(t, args...)
		if code == 0 {
			return true
		}
		last = stderr
		if strings.Contains(stderr, busy) {
			return false
		}
		t.Fatalf("command %v failed (exit %d): %s", args, code, truncateOutput(stderr))
		return false
	}) {
		return
	}
	t.Fatalf("command %v was still refused after a minute: %s", args, truncateOutput(last))
}

// waitFor polls check every interval until it returns true or the timeout
// elapses. It checks immediately, before the first sleep, so an already-true
// condition costs nothing.
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
