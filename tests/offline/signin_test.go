package offline

import (
	"strings"
	"testing"
)

// Signing in from another device, so far as it can be judged here: what a code
// has to look like, and which flags mean nothing beside --qr. All of it is the
// command line alone, so none of it costs a code, a session or a request.

// A code is read before a fork is opened for it, so a mistyped one costs
// nothing and says so in terms of where a real one comes from.
func TestSigningADeviceInRefusesWhatIsNotACode(t *testing.T) {
	for _, code := range []string{
		"nonsense",
		"0:8FJ3K2QP:web-account",
		"9:8FJ3K2QP:qm4Xb2v9tR1sLp0yZcHwKdNfEuAgJi7MoBxVn3Tl5Qs=:web-account",
		"0::qm4Xb2v9tR1sLp0yZcHwKdNfEuAgJi7MoBxVn3Tl5Qs=:web-account",
		"0:8FJ3K2QP:not-base64:web-account",
	} {
		refuses(t, 1, []string{"account", "sessions", "create", code},
			"That is not a sign-in code")
	}
}

// --qr signs in as whoever approves the code, so the flags that name an account
// or carry one of its secrets have nothing to do there - and being told so
// before a code is shown is the difference between reading a refusal and
// standing at a screen wondering why the password was never asked for.
func TestSigningInWithACodeTakesNoCredentials(t *testing.T) {
	for _, flag := range [][]string{
		{"--user", "alice@proton.me"},
		{"--password-file", "/dev/null"},
		{"--second-password-file", "/dev/null"},
		{"--totp", "123456"},
	} {
		args := append([]string{"account", "login", "--qr"}, flag...)
		refuses(t, 1, args, "--qr")
	}
}

// A preview of signing in signs nobody in, and says so without an address it
// would have had to sign in to learn.
func TestPreviewingASignInWithACodeNamesNobody(t *testing.T) {
	stdout, stderr, code := run(t, "--dry-run", "account", "login", "--qr")
	if code != 0 {
		t.Errorf("exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	if stdout != "" {
		t.Errorf("wrote to stdout: %q", truncate(stdout))
	}
	if !strings.Contains(stderr, "Dry run - would sign in as whoever approves the code") {
		t.Errorf("the preview does not say who it would sign in as:\n%s", truncate(stderr))
	}
	if strings.Contains(stderr, "Scan this") {
		t.Errorf("the preview showed a code, which is the change it exists not to make:\n%s", truncate(stderr))
	}
}

// The same for a sign-in that would prove a password: a preview reads no
// password file and reaches no network, and names the account it was given.
func TestPreviewingASignInNamesTheAccountItWasGiven(t *testing.T) {
	stdout, stderr, code := run(t, "--dry-run", "account", "login",
		"--user", "alice@proton.me", "--password-file", "/nonexistent")
	if code != 0 {
		t.Errorf("exit %d, want 0\nstderr: %s", code, truncate(stderr))
	}
	if stdout != "" {
		t.Errorf("wrote to stdout: %q", truncate(stdout))
	}
	if !strings.Contains(stderr, "Dry run - would sign in as alice@proton.me") {
		t.Errorf("the preview does not name the account:\n%s", truncate(stderr))
	}
}
