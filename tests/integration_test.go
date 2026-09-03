package tests

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/tests/account"
	"github.com/roman-16/proton-cli/tests/fixture"
)

var binaryPath string

// TestMain builds the CLI binary once before any integration test runs.
func TestMain(m *testing.M) {
	requireCredentials()

	dir, err := os.MkdirTemp("", "proton-cli-test-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create temp dir: %v\n", err)
		os.Exit(1)
	}
	workDir = dir

	binaryPath = filepath.Join(dir, "proton")
	cmd := exec.Command("go", "build", "-o", binaryPath, "../cmd/proton")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		fmt.Fprintf(os.Stderr, "failed to build binary: %v\n", err)
		os.Exit(1)
	}

	writePasswordFiles()
	openTrace()
	// A paid run touches an account somebody depends on, so it does as little as
	// it can: it signs that account in and records what it looked like. A run
	// without the tag never reaches it at all.
	//
	// Nothing is seeded here. What the account has to hold is brought about by
	// the test that reads it, so a run pays only for the fixtures it actually
	// asks for and one that cannot be made fails those tests rather than all of
	// them. `just seed` fills an account by hand, through the same declaration.
	signIn()
	if paidBuild {
		snapshotPaid()
	}

	code := m.Run()

	if paidBuild && !comparePaid() {
		code = 1
	}

	// os.Exit skips deferred funcs, so the temp-dir removal is explicit.
	closeTrace()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// ── The two accounts ──

// The suite creates, mutates and deletes real data, so it runs on accounts kept
// for that and nothing else. Most tests act as `primary`; the handful that need
// two Proton users bring in `secondary`.
//
// These are the harness's own variables, not the CLI's: proton takes an
// account from a signed-in profile, which signIn establishes below. The
// PROTON_CLI_TEST_ prefix keeps them clear of anything the binary reads.
const (
	primary   = account.Primary
	secondary = account.Secondary
	// paid is a third account on a plan that includes what Proton gates, and is
	// required exactly when the tests that use it are compiled in - see required.
	// No seeding can make a free account able to do what a subscription buys, so
	// there is nothing for a run under the tag to fall back to.
	//
	// It is a third account rather than an upgrade of the primary, so the free
	// plan's limits - three calendars, three folders, one filter - keep being
	// exercised by the tests that exist to exercise them.
	paid = account.Paid
)

// workDir is the per-run temp directory, where the password files live.
var workDir string

type testAccount struct {
	account.Account
	// The files each of the account's secrets is written to for the run, so a
	// command that has to be handed one can be.
	passwordFile string
	secondFile   string
	extraFile    string
}

var accounts = accountTable()

// accountTable is the shared declaration, given somewhere to remember the files
// this run wrote each secret to.
func accountTable() map[string]*testAccount {
	out := map[string]*testAccount{}
	for _, a := range account.All() {
		out[a.Profile] = &testAccount{Account: a}
	}
	return out
}

// required are the accounts without which there is no suite, which is every
// account the running binary holds tests for.
//
// The paid one is required exactly when the paid tests are compiled in. Without
// the tag there is no paid account in this binary at all - nothing signs it in
// and nothing reads it - so demanding its credentials would be demanding
// something the run could not use. With the tag it is as required as the other
// two, because every test under it acts on that account and a run that skipped
// them all would report success for having done nothing.
var required = requiredAccounts()

func requiredAccounts() []string {
	if paidBuild {
		return []string{primary, secondary, paid}
	}
	return []string{primary, secondary}
}

// requireCredentials verifies every account this binary has tests for is
// configured before any test runs, exiting instantly - ahead of the expensive
// binary build - if one of them is incomplete.
func requireCredentials() {
	var missing []string
	for _, name := range required {
		for _, v := range accounts[name].Secrets() {
			if os.Getenv(v) == "" {
				missing = append(missing, v)
			}
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "integration tests require these env vars: %s\n", strings.Join(missing, ", "))
		os.Exit(1)
	}
	for _, name := range required {
		if err := looksSwapped(accounts[name]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

// looksSwapped catches an address put in the password variable and a password
// put in the address one.
//
// Signing in with them the wrong way round is a failed login attempt against a
// real account, which is what Proton's anti-abuse system counts before it starts
// demanding a CAPTCHA - so it is worth refusing to start over. Neither value is
// ever printed: the message says which variables to look at.
func looksSwapped(a *testAccount) error {
	user, password := os.Getenv(a.User), os.Getenv(a.Password)
	if user == "" || password == "" {
		return nil
	}
	// Proton takes a bare username as well as an address, so an address is not
	// required. Only the pair being the wrong way round is.
	if !isAddress(user) && isAddress(password) {
		return fmt.Errorf("%s and %s look swapped: the password holds an address and the user does not",
			a.User, a.Password)
	}
	return nil
}

// isAddress reports whether a value is shaped like an email address.
func isAddress(v string) bool {
	at := strings.LastIndex(v, "@")
	return at > 0 && strings.Contains(v[at:], ".")
}

// writePasswordFiles puts each account's secrets where a command that has to be
// handed one can read it: the password for the commands Proton guards behind an
// elevated session, and the second and extra passwords for a test that signs an
// account in itself.
//
// A session cannot carry elevation: Proton re-authenticates over SRP, and the
// key blob sealed at login is a one-way derivation of the password rather than
// the password itself.
func writePasswordFiles() {
	for _, name := range required {
		a := accounts[name]
		for _, secret := range []struct {
			kind  string
			value string
			into  *string
		}{
			{"password", os.Getenv(a.Password), &a.passwordFile},
			{"second", os.Getenv(a.Second), &a.secondFile},
			{"extra", os.Getenv(a.Extra), &a.extraFile},
		} {
			if secret.value == "" {
				continue
			}
			path := filepath.Join(workDir, a.Profile+"."+secret.kind)
			if err := os.WriteFile(path, []byte(secret.value), 0o600); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write the %s %s file: %v\n", a.Profile, secret.kind, err)
				os.Exit(1)
			}
			*secret.into = path
		}
	}
}

// signIn brings every required account to a signed-in session, which every test
// needs and nothing else can arrange for itself.
//
// A session already in place costs one read: `account login` does no SRP
// exchange while the saved one still works, so an ordinary run pays almost
// nothing for this and recovers by itself from a session that expired.
//
// Establishing one from nothing is a different matter. Proton may raise a
// CAPTCHA, `go test` gives this binary /dev/null on standard input, and a
// challenge cannot be solved by anything but a person with a browser - so a run
// that has to sign in from scratch says which command can, rather than failing
// with a page nobody is watching for.
func signIn() {
	for _, name := range required {
		a := accounts[name]
		args := []string{"account", "login", "--user", os.Getenv(a.User)}
		for _, secret := range []struct{ flag, file string }{
			{"--password-file", a.passwordFile},
			{"--second-password-file", a.secondFile},
			{"--extra-password-file", a.extraFile},
		} {
			if secret.file != "" {
				args = append(args, secret.flag, secret.file)
			}
		}
		_, stderr, code, err := runAs(name, nil, args...)
		if err == nil && code == 0 {
			continue
		}
		fmt.Fprint(os.Stderr, stderr)
		fmt.Fprintf(os.Stderr,
			"\ncould not sign %s in without asking anything.\n"+
				"Run `just login` once - it can open Proton's CAPTCHA page and wait for you -\n"+
				"then run this again.\n", name)
		os.Exit(1)
	}
}

// ── Running the binary ──

// run executes the CLI as the primary account and returns stdout, stderr, exit
// code.
func run(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	return runWithStdin(t, nil, args...)
}

// runArgs executes the CLI as the primary account without a *testing.T, so
// suite-level fixtures and cleanup (which run outside a test) can invoke it.
func runArgs(stdin io.Reader, args ...string) (stdout, stderr string, exitCode int, err error) {
	return runAs(primary, stdin, args...)
}

// runAs executes the CLI as one of the two accounts. It returns stdout, stderr,
// the exit code, and a non-nil error only when the process failed to start.
//
// The child environment is built from an allowlist rather than inherited. This
// is the one place a target account is chosen, so it is the one place the choice
// can be enforced: whatever a developer happens to have exported, the binary
// under test sees a stated environment and can act only as the profile named
// here.
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
	if profile == paid {
		if why := offLimits(args); why != "" {
			return "", "", -1, fmt.Errorf(
				"refusing to run %v as the paid account: %s", args, why)
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
	var runErr error
	if at := sendsMail(args); at {
		// Sending is metered by the hour and judged by Proton, so two never go out
		// at once however many tests are running. The lease covers the send itself
		// and not the wait for delivery that follows it.
		holding(sending, func() { runErr = cmd.Run() })
	} else {
		runErr = cmd.Run()
	}
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
		at := indexOfRun(args, cmd...)
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

// sendingCommands are the commands that put a message on the wire. It mirrors
// what the CLI's own compose paths do, and is the one place the suite states it,
// so no test has to remember to space its sending.
var sendingCommands = [][]string{
	{"mail", "messages", "send"},
	{"mail", "messages", "reply"},
	{"mail", "messages", "forward"},
	{"mail", "drafts", "send"},
	{"calendar", "events", "respond"},
}

func sendsMail(args []string) bool {
	if slices.Contains(args, "--dry-run") {
		return false
	}
	for _, cmd := range sendingCommands {
		if indexOfRun(args, cmd...) >= 0 {
			return true
		}
	}
	return false
}

// answeringCommands are the commands that make Proton write to whoever offered
// something. Answering an invitation is the whole set: the owner is told, by
// mail, and nothing on this end turns that off.
//
// It is stated here for the reason sendingCommands is: recognised from the args
// in the one place a command is run, so no test has to remember that what it
// just did will land in somebody's inbox a moment later.
var answeringCommands = [][]string{
	{"invitations", "accept"},
	{"invitations", "decline"},
}

// noticesCaused counts them, for the sweep that clears them up.
var noticesCaused atomic.Int64

func answersInvitation(args []string) bool {
	if slices.Contains(args, "--dry-run") {
		return false
	}
	for _, cmd := range answeringCommands {
		if indexOfRun(args, cmd...) >= 0 {
			return true
		}
	}
	return false
}

// indexOfRun reports where args holds the words in order and adjacent, or -1.
// That is how a command is recognised once the helpers have put their own flags
// in front of it.
func indexOfRun(args []string, run ...string) int {
	for i := 0; i+len(run) <= len(args); i++ {
		if slices.Equal(args[i:i+len(run)], run) {
			return i
		}
	}
	return -1
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

// runWithStdin is run() with arbitrary stdin bytes attached.
func runWithStdin(t *testing.T, stdin io.Reader, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runArgs(stdin, args...)
	if err != nil {
		t.Fatalf("failed to run command %v: %v", args, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

// watch is a long-running subprocess under the same environment runAs builds,
// for the commands that stay attached until told to stop.
type watch struct {
	cmd  *exec.Cmd
	errb *bytes.Buffer

	// lines carries each stdout line as it is written, and buf keeps the whole
	// run for a report when a timeout needs one.
	lines chan string
	buf   *bytes.Buffer
	// ready closes once the opening line has reached stderr, which is how a
	// test knows the watch has authenticated and begun.
	ready chan struct{}
	once  sync.Once
	done  chan error

	// profile, args and started are what the trace needs to record the
	// invocation once the watch has stopped.
	profile string
	args    []string
	started time.Time
	traced  sync.Once
}

// record hands the watch's stderr to the trace, so the requests it made count
// towards what the live suite reached. It runs once, whenever the watch ends.
func (w *watch) record(exitCode int) {
	w.traced.Do(func() {
		_ = trace(w.profile, w.args, time.Since(w.started), exitCode, w.errb.String())
	})
}

// watchAs starts the command as the named profile and returns once it begins.
//
// It records what the watch asked Proton for, the same as runAs does. A watch is
// the only way two of the requests this CLI can send are ever sent, so a helper
// that skipped the trace would have `just coverage` record a suite that never
// reached them - which is exactly what happened until this did it too.
func watchAs(profile string, args ...string) (*watch, error) {
	a, ok := accounts[profile]
	if !ok {
		return nil, fmt.Errorf("unknown test profile %q", profile)
	}
	args = withPassword(a, args)

	w := &watch{
		cmd:     exec.Command(binaryPath, args...),
		errb:    &bytes.Buffer{},
		lines:   make(chan string),
		buf:     &bytes.Buffer{},
		ready:   make(chan struct{}),
		done:    make(chan error, 1),
		profile: profile,
		args:    args,
		started: time.Now(),
	}
	w.cmd.Env = childEnv(profile)
	if tracingRequests() {
		w.cmd.Env = append(w.cmd.Env, "PROTON_LOG_LEVEL=debug")
	}

	outPipe, err := w.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errPipe, err := w.cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := w.cmd.Start(); err != nil {
		return nil, err
	}
	go func() { w.done <- w.cmd.Wait() }()
	go func() {
		scanner := bufio.NewScanner(outPipe)
		scanner.Buffer(make([]byte, 64*1024), 1024*1024)
		for scanner.Scan() {
			w.buf.WriteString(scanner.Text() + "\n")
			w.lines <- scanner.Text()
		}
	}()
	go func() {
		scanner := bufio.NewScanner(errPipe)
		for scanner.Scan() {
			w.errb.WriteString(scanner.Text() + "\n")
			if strings.HasPrefix(scanner.Text(), "Watching ") {
				w.once.Do(func() { close(w.ready) })
			}
		}
	}()
	return w, nil
}

// waitReady blocks until the watch has begun streaming, or fails on timeout.
func (w *watch) waitReady(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-w.ready:
	case err := <-w.done:
		t.Fatalf("watch exited before it began: %v\nstderr:\n%s", err, w.errb.String())
	case <-time.After(timeout):
		t.Fatalf("watch did not begin within %s\nstderr:\n%s", timeout, w.errb.String())
	}
}

// waitForLine returns the first streamed line satisfying check, or fails after
// the timeout. The watch keeps running either way.
func (w *watch) waitForLine(t *testing.T, timeout time.Duration, check func(string) bool) string {
	t.Helper()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case line := <-w.lines:
			if check(line) {
				return line
			}
		case err := <-w.done:
			t.Fatalf("watch ended before the expected line: %v\nreceived:\n%s\nstderr:\n%s",
				err, w.buf.String(), w.errb.String())
		case <-timer.C:
			t.Fatalf("watch produced no matching line within %s\nreceived:\n%s\nstderr:\n%s",
				timeout, w.buf.String(), w.errb.String())
		}
	}
}

// stop interrupts the watch and expects a clean exit, which is what Ctrl+C
// means to it.
func (w *watch) stop(t *testing.T) {
	t.Helper()
	if w.cmd.Process == nil {
		return
	}
	_ = w.cmd.Process.Signal(os.Interrupt)
	select {
	case err := <-w.done:
		w.record(w.cmd.ProcessState.ExitCode())
		if err != nil {
			t.Fatalf("watch did not stop cleanly: %v\nstderr:\n%s", err, w.errb.String())
		}
	case <-time.After(5 * time.Second):
		w.record(-1)
		t.Fatalf("watch did not stop after SIGINT\nstderr:\n%s", w.errb.String())
	}
}

// rateLimited is what the client says when Proton asks for room.
const rateLimited = "rate limited by Proton"

// refuseToPush stops the run the first time Proton throttles it.
//
// The client backs off and would very likely succeed, so the suite would pass and
// nobody would learn anything - except that these are real accounts, and a run
// that has started being throttled is a run that should be asking for less rather
// than pressing on. The concurrency is a setting; this is how it gets corrected.
func refuseToPush(t *testing.T, stderr string) {
	t.Helper()
	if strings.Contains(stderr, rateLimited) {
		t.Fatalf("Proton rate-limited this run. Lower the concurrency (go test -parallel N) "+
			"and give the account a few minutes.\n%s", truncateOutput(stderr))
	}
}

// runWithEnv runs the CLI with extra env vars layered on top of os.Environ().
// Returns stdout, stderr, exit code.
func runWithEnv(t *testing.T, env map[string]string, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	cmd := exec.Command(binaryPath, withPassword(accounts[primary], args)...)
	cmd.Env = withEnv(childEnv(primary), env)
	var outB, errB strings.Builder
	cmd.Stdout = &outB
	cmd.Stderr = &errB
	err := cmd.Run()
	exit = 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exit = ee.ExitCode()
		} else {
			t.Fatalf("run %v: %v", args, err)
		}
	}
	return outB.String(), errB.String(), exit
}

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

// runOK runs a command and fails the test on non-zero exit.
func runOK(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := run(t, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout
}

// runOKStderr runs a command and returns both stdout + stderr on success.
func runOKStderr(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	stdout, stderr, code := run(t, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout, stderr
}

// asJSON puts `--output json` ahead of everything.
//
// It has to lead rather than trail: a Proton ID may begin with '-', which the
// binary auto-protects with a `--`, and any flag after that becomes a positional
// argument. Appending the flag would therefore break roughly one call in sixty,
// depending on which ID the account happened to hand out.
func asJSON(args []string) []string {
	return append([]string{"--output", "json"}, args...)
}

// runJSON runs with `--output json` and parses stdout as a JSON object.
func runJSON(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOK(t, asJSON(args)...))
}

func parseJSONObject(t *testing.T, stdout string) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("failed to parse JSON object: %v\nraw: %s", err, truncateOutput(stdout))
	}
	return result
}

// runJSONArray runs with `--output json` and parses stdout as a JSON array.
// runJSONArray returns the rows of a collection.
//
// Every list is an envelope keyed by its plural noun - {"messages": [...],
// "count": 3} - so this unwraps whichever array it finds rather than making every
// caller know the noun. The count is checked against it, since the two disagreeing
// would be a bug in the envelope itself.
func runJSONArray(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOK(t, asJSON(args)...))
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
				"║  ⚠️  CLEANUP FAILED - MANUAL ACTION REQUIRED                ║\n"+
				"╠══════════════════════════════════════════════════════════════╣\n"+
				"║  %s\n"+
				"║  Error: %s\n"+
				"╚══════════════════════════════════════════════════════════════╝",
				description, err)
		}
	})
}

// cleanupRun registers a cleanup that invokes the CLI.
// A cleanup's job is that nothing is left behind, so finding the thing already
// gone - exit 3 - is the job done. A test whose subject is deletion would
// otherwise raise the alarm every time it worked.
func cleanupRun(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanup(t, description, func() error {
		_, stderr, code := run(t, consenting(args)...)
		if code != 0 && code != 3 {
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

// selfEmail returns the primary account's address.
func selfEmail() string { return os.Getenv(accounts[primary].User) }

// secondaryEmail returns the second account's address.
func secondaryEmail() string { return os.Getenv(accounts[secondary].User) }

// The secondary-account runners. A scenario needs one whenever it genuinely
// takes two Proton users: accepting a share invitation, receiving mail, or
// organizing an invite the primary RSVPs to.
//
// Run order matters - the primary invites or sends, the secondary accepts or
// receives - and a mutation made as one account registers its cleanup as the
// same one.

func runSecondary(t *testing.T, args ...string) (stdout, stderr string, exitCode int) {
	t.Helper()
	stdout, stderr, exitCode, err := runAs(secondary, nil, args...)
	if err != nil {
		t.Fatalf("failed to run command %v as the secondary account: %v", args, err)
	}
	refuseToPush(t, stderr)
	return stdout, stderr, exitCode
}

func runOKSecondary(t *testing.T, args ...string) string {
	t.Helper()
	stdout, stderr, code := runSecondary(t, consenting(args)...)
	if code != 0 {
		t.Fatalf("command %v failed as the secondary account (exit %d):\nstdout: %s\nstderr: %s",
			args, code, truncateOutput(stdout), truncateOutput(stderr))
	}
	return stdout
}

func runJSONSecondary(t *testing.T, args ...string) map[string]interface{} {
	t.Helper()
	return parseJSONObject(t, runOKSecondary(t, asJSON(args)...))
}

func runJSONArraySecondary(t *testing.T, args ...string) []interface{} {
	t.Helper()
	return parseJSONArray(t, runOKSecondary(t, asJSON(args)...))
}

// cleanupRunSecondary is cleanupRun for something the secondary account owns.
func cleanupRunSecondary(t *testing.T, description string, args ...string) {
	t.Helper()
	cleanup(t, description, func() error {
		_, stderr, code := runSecondary(t, consenting(args)...)
		if code != 0 && code != 3 {
			return fmt.Errorf("exit %d: %s", code, strings.TrimSpace(stderr))
		}
		return nil
	})
}

// externalRecipient is a non-Proton mailbox, for tests that must deliver outside
// Proton. Sending to a fake @example.com address instead bounces (nullMX),
// littering the inbox with MAILER-DAEMON returns, so a test that needs one skips
// when none is configured.
func externalRecipient(t *testing.T) string {
	t.Helper()
	v := os.Getenv("PROTON_CLI_TEST_EXTERNAL_RECIPIENT")
	if v == "" {
		t.Skip("PROTON_CLI_TEST_EXTERNAL_RECIPIENT is not set")
	}
	return v
}

// ── mail delivery + polling ──

// waitFor polls check every interval until it returns true or timeout elapses.
// It checks immediately (before the first sleep), so an already-true condition
// costs nothing. Returns whether check ultimately succeeded.
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

// busy is how Proton says something else holds what a command wants.
//
// This is not rate limiting - which fails a run on purpose - but an account-wide
// lock on one folder or one background job, and Proton's own answer says to try
// again. It has more than one sentence for it ("Another action is currently in
// progress", "There is already an active operation on your folders or labels"),
// so the status is what is matched: the wording is Proton's to reword, 409 is
// not.
const busy = "[HTTP 409]"

// runOKUntilFree runs a command that another test may be holding the lock for,
// and fails only once Proton has been saying so for a minute.
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

// messageIDInFolder returns the ID of the first message in folder whose subject
// matches exactly, or "" if none. t-free so fixtures/helpers can call it.
func messageIDInFolder(folder, subject string) string {
	stdout, _, code, err := runArgs(nil, "mail", "messages", "list",
		"--folder", folder, "--page-size", "20", "--output", "json")
	if err != nil || code != 0 {
		return ""
	}
	var data struct {
		Messages []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(stdout), &data) != nil {
		return ""
	}
	for _, m := range data.Messages {
		if m.Subject == subject {
			return m.ID
		}
	}
	return ""
}

// conversationIDOf returns the thread a message belongs to, or "" on failure.
// t-free.
func conversationIDOf(msgID string) string {
	stdout, _, code, err := runArgs(nil, "--output", "json", "mail", "messages", "get", msgID)
	if err != nil || code != 0 {
		return ""
	}
	var v struct {
		ConversationID string `json:"conversation_id"`
	}
	if json.Unmarshal([]byte(stdout), &v) != nil {
		return ""
	}
	return v.ConversationID
}

// sendTestMailSecondary sends a message from the second account to the primary
// one, so a watch on the primary sees a genuine arrival. It registers cleanup
// for the copy that lands in the primary's inbox.
func sendTestMailSecondary(t *testing.T, subject string) string {
	t.Helper()
	stdout, stderr, code, err := runAs(secondary, nil, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", "Integration test body: "+subject)
	if err != nil || code != 0 {
		t.Fatalf("secondary send %q failed (exit %d): %v %s", subject, code, err, strings.TrimSpace(stderr))
	}
	_ = stdout
	var inboxID string
	waitFor(25*time.Second, 750*time.Millisecond, func() bool {
		inboxID = messageIDInFolder("inbox", subject)
		return inboxID != ""
	})
	if inboxID == "" {
		t.Fatalf("secondary send %q never reached the primary's inbox", subject)
	}
	cleanupRun(t, "Delete inbox mail: proton mail messages delete -- "+inboxID,
		"mail", "messages", "delete", "--", inboxID)
	return inboxID
}

// sendSelfMail sends body to self with the given subject and waits for
// delivery, polling sent then inbox. Either returned ID may be "" if it never
// appeared. t-free: callers decide how to report failure and register cleanup.
func sendSelfMail(subject, body string) (sentID, inboxID string, err error) {
	if _, stderr, code, e := runArgs(nil, "mail", "messages", "send",
		"--to", selfEmail(), "--subject", subject, "--body", body); e != nil || code != 0 {
		return "", "", fmt.Errorf("send %q failed (exit %d): %v %s", subject, code, e, strings.TrimSpace(stderr))
	}
	waitFor(15*time.Second, 500*time.Millisecond, func() bool {
		sentID = messageIDInFolder("sent", subject)
		return sentID != ""
	})
	waitFor(25*time.Second, 750*time.Millisecond, func() bool {
		inboxID = messageIDInFolder("inbox", subject)
		return inboxID != ""
	})
	return sentID, inboxID, nil
}

// sendTestMail sends a mail to self, waits for delivery, registers per-test
// cleanup for the sent and inbox copies, and returns the inbox message ID
// (falling back to the sent ID if the inbox copy never appeared).
//
// Use it only when a test MUTATES its message (mark/star/move/trash) or exercises
// the send path itself. Read-only tests should use a shared fixture (plainMail,
// quotedMail, sharedAttachment) instead of sending their own.
func sendTestMail(t *testing.T, subject string) string {
	t.Helper()
	sentID, inboxID, err := sendSelfMail(subject, "Integration test body: "+subject)
	if err != nil {
		t.Fatal(err)
	}
	if sentID != "" {
		cleanupRun(t, "Delete sent mail: proton mail messages delete -- "+sentID,
			"mail", "messages", "delete", "--", sentID)
	}
	if inboxID != "" {
		cleanupRun(t, "Delete inbox mail: proton mail messages delete -- "+inboxID,
			"mail", "messages", "delete", "--", inboxID)
		return inboxID
	}
	if sentID == "" {
		t.Fatalf("mail %q was not delivered", subject)
	}
	return sentID
}

// ── shared mail fixtures ──
//
// Most mail tests need *a* delivered message of a particular shape rather than a
// freshly sent one. Those shapes live on the account: `scripts/seed` puts them
// there and this finds them, so a run spends its sending allowance on the send
// path it is actually testing. What each one is for is declared in tests/fixture.
//
// A test that mutates its message, or that exercises the send path itself, sends
// its own with sendTestMail instead.

// seeded is a fixture message as the suite needs it: what to address, what thread
// it is in, and - for the one carrying attachments - which attachment to act on.
type seeded struct {
	msgID   string
	convID  string
	subject string
	attID   string
	attName string
}

// find locates one seeded message, sending it if the account has not got it, and
// reads whatever else a test will ask of it.
//
// It sends rather than failing because a fixture is brought about when something
// needs it: a run that reads no mail sends none, and an account that has never
// been seeded fills itself as the tests that need it ask.
func find(m fixture.Mail) (seeded, error) {
	id := inboxMessageID(m.Subject)
	if id == "" {
		if err := deliver(m); err != nil {
			return seeded{}, err
		}
		id = inboxMessageID(m.Subject)
	}
	if id == "" {
		return seeded{}, fmt.Errorf("the inbox holds no message subject %q and one could not be sent", m.Subject)
	}
	s := seeded{msgID: id, subject: m.Subject, convID: conversationIDOf(id)}
	if m.Attach == "" {
		return s, nil
	}
	out, _, code, err := runArgs(nil, "--output", "json", "mail", "messages", "attachments", "list", id)
	if err != nil || code != 0 {
		return seeded{}, fmt.Errorf("list the attachments of %q (exit %d): %v", m.Subject, code, err)
	}
	var env struct {
		Attachments []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"attachments"`
	}
	if json.Unmarshal([]byte(out), &env) != nil || len(env.Attachments) == 0 {
		return seeded{}, fmt.Errorf("the seeded message %q carries no attachment", m.Subject)
	}
	// The listing leaves inline parts out by default, so this is the regular one.
	s.attID, s.attName = env.Attachments[0].ID, env.Attachments[0].Name
	return s, nil
}

// deliver sends one of the fixture's messages and waits for it to arrive.
//
// The files an attachment needs are written where this run can reach them, which
// is why the fixture declares their contents rather than a path.
func deliver(m fixture.Mail) error {
	send := []string{"mail", "messages", "send", "--to", selfEmail(), "--subject", m.Subject, "--body", m.Body}
	if m.HTML {
		send = append(send, "--html")
	}
	for flag, name := range map[string]string{"--attach": m.Attach, "--attach-inline": m.Inline} {
		if name == "" {
			continue
		}
		path, err := fixtureFile(name)
		if err != nil {
			return err
		}
		send = append(send, flag, path)
	}
	if _, stderr, code, err := runArgs(nil, send...); err != nil || code != 0 {
		return fmt.Errorf("send the %q fixture (exit %d): %v: %s", m.Subject, code, err, stderr)
	}
	if !waitFor(90*time.Second, 3*time.Second, func() bool { return inboxMessageID(m.Subject) != "" }) {
		return fmt.Errorf("the %q fixture was sent and has not arrived", m.Subject)
	}
	return nil
}

// fixtureFile writes one of the files the fixture uploads, once per run, and
// says where it is.
var fixtureDir = sync.OnceValues(func() (string, error) { return os.MkdirTemp("", "proton-cli-fixture-*") })

func fixtureFile(name string) (string, error) {
	dir, err := fixtureDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	body := fixture.Files()[name]
	if body == "" {
		// Some bulk, so a listing shows a size worth reading.
		body = strings.Repeat("proton-cli\n", 4000)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// The lookups happen at most once per run, and only if something asks.
var (
	plainFixture       = sync.OnceValues(func() (seeded, error) { return find(fixture.Plain) })
	quotedFixture      = sync.OnceValues(func() (seeded, error) { return find(fixture.Quoted) })
	attachmentsFixture = sync.OnceValues(func() (seeded, error) { return find(fixture.Attachments) })
)

// inboxMessageID is the ID of the inbox message with exactly this subject.
//
// It reads the listing rather than the search index, because a message the seed
// sent moments ago is in the mailbox before it is findable in the index.
func inboxMessageID(subject string) string {
	stdout, _, code, err := runArgs(nil, "--output", "json", "mail", "messages", "list",
		"--folder", "inbox", "--page-size", "150")
	if err != nil || code != 0 {
		return ""
	}
	var data struct {
		Messages []struct {
			ID      string `json:"id"`
			Subject string `json:"subject"`
		} `json:"messages"`
	}
	if json.Unmarshal([]byte(stdout), &data) != nil {
		return ""
	}
	for _, m := range data.Messages {
		if m.Subject == subject {
			return m.ID
		}
	}
	return ""
}

func fixtureOr(t *testing.T, load func() (seeded, error)) seeded {
	t.Helper()
	s, err := load()
	if err != nil {
		t.Fatalf("shared mail fixture: %v", err)
	}
	return s
}

// plainMail is a delivered self-mail with a plain body: no quote markers, no
// attachments. Read-only.
func plainMail(t *testing.T) (msgID, convID, subject string) {
	t.Helper()
	s := fixtureOr(t, plainFixture)
	return s.msgID, s.convID, s.subject
}

// quotedMail is a delivered self-mail whose body carries the canonical
// "On <date>, <name> <addr> wrote:" reply block. Read-only.
func quotedMail(t *testing.T) (msgID, subject string) {
	t.Helper()
	s := fixtureOr(t, quotedFixture)
	return s.msgID, s.subject
}

// sharedAttachment is a delivered self-mail carrying one regular attachment, plus
// that attachment's ID and name. Read-only.
func sharedAttachment(t *testing.T) (msgID, attID, attName string) {
	t.Helper()
	s := fixtureOr(t, attachmentsFixture)
	return s.msgID, s.attID, s.attName
}

// ── the mutable pool ──
//
// A test that marks, stars, moves or trashes a message needs one it may change,
// not a freshly sent one: what it proves is that the change happens and can be
// undone, and a seeded message proves that exactly as well. The pool hands them
// out one at a time, so two tests running together never change the same message.
//
// A test that finishes as it should leaves its message as it found it. One that
// fails may not have got that far, so the state is put back here rather than
// trusted - otherwise the next run would find the message somewhere it does not
// look for it, and the seed would send another.
var mutablePool = func() chan fixture.Mail {
	pool := make(chan fixture.Mail, len(fixture.Mutable))
	for _, m := range fixture.Mutable {
		pool <- m
	}
	return pool
}()

func mutableMail(t *testing.T) string {
	t.Helper()
	m := <-mutablePool
	id := inboxMessageID(m.Subject)
	t.Cleanup(func() {
		if t.Failed() && id != "" {
			for _, args := range [][]string{
				{"mail", "messages", "move", "--into", "inbox", "--", id},
				{"mail", "messages", "unstar", "--", id},
				{"mail", "messages", "mark", "read", "--", id},
			} {
				_, _, _, _ = runArgs(nil, consenting(args)...)
			}
		}
		mutablePool <- m
	})
	if id == "" {
		t.Fatalf("the inbox holds no message subject %q; run `just seed`", m.Subject)
	}
	return id
}

// sharedMixedAttachment is a delivered self-mail carrying both an inline image
// and a regular attachment, for the tests about telling the two apart. It is the
// same message: a mail with an inline image and an attachment is one shape rather
// than two. Read-only.
func sharedMixedAttachment(t *testing.T) string {
	t.Helper()
	return fixtureOr(t, attachmentsFixture).msgID
}

// ── the runner is the only way in ──

// TestEveryInvocationGoesThroughTheRunner keeps the binary from being spawned
// anywhere but here.
//
// runAs is the one place that chooses which account a command acts as and builds
// the environment it runs in. A test that starts the process itself inherits
// whatever the developer has exported and acts as whatever profile that names -
// which is how a stdin upload once landed in a personal Drive instead of the
// primary account's, and reported an empty folder rather than a failure.
func TestEveryInvocationGoesThroughTheRunner(t *testing.T) {
	t.Parallel()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the test directory: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_test.go") || e.Name() == "integration_test.go" {
			continue
		}
		src, err := os.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		if strings.Contains(string(src), "exec.Command(binaryPath") {
			t.Errorf("%s starts the binary itself; go through run, runOK or runAs so the account is chosen in one place", e.Name())
		}
	}
}
