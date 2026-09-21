package live

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/otp"
)

// The authenticator app as a second factor.
//
// The round trip is the only way a run sees any of it, and it is the one test
// in the suite that leaves the account unable to sign in if it stops halfway -
// so the cleanup reads the account's state rather than remembering what it did,
// and the secret it would need is printed where a person can pick it up.
//
// A code is computed here the way an authenticator app computes one, from the
// secret Proton just handed over. That is also what makes the test worth
// having: it is the only thing that checks the secret Proton mints is one the
// codes it then demands can be computed from.

// codeLife is how much of a code's thirty seconds has to be left for it to be
// worth sending. A code that expires between here and Proton fails a test over
// nothing.
const codeLife = 5 * time.Second

func TestAccountTwoFactorRoundTrip(t *testing.T) {
	if app := runJSON(t, "account", "settings", "two-factor", "get")["authenticator_app"]; app != "off" {
		t.Fatalf("the primary account already asks for a code (authenticator_app = %v), "+
			"which the rest of the suite does not expect", app)
	}

	// Minting a secret changes nothing until a code confirms it.
	minted := runJSON(t, "account", "settings", "two-factor", "generate")
	secret, _ := minted["secret"].(string)
	uri, _ := minted["uri"].(string)
	if secret == "" {
		t.Fatalf("no secret in %v", keysOf(minted))
	}
	if !strings.HasPrefix(uri, "otpauth://totp/") || !strings.Contains(uri, "issuer=Proton") {
		t.Errorf("uri = %q, want an otpauth URI an authenticator app reads", uri)
	}
	if app := runJSON(t, "account", "settings", "two-factor", "get")["authenticator_app"]; app != "off" {
		t.Errorf("authenticator_app = %v after minting a secret, want off until a code confirms it", app)
	}

	cleanup(t, "the primary account asks for a two-factor code. Turn it off with the secret "+
		"printed above: proton --profile primary account settings two-factor disable --totp CODE",
		func() error { return turnTwoFactorOff(t, secret) })

	answer := runJSON(t, "account", "settings", "two-factor", "enable", "--totp", code(t, secret))
	codes, _ := answer["recovery_codes"].([]interface{})
	if len(codes) == 0 {
		t.Fatalf("no recovery codes in %v; they are printed once and nowhere else", keysOf(answer))
	}
	if app := runJSON(t, "account", "settings", "two-factor", "get")["authenticator_app"]; app != "on" {
		t.Errorf("authenticator_app = %v after confirming the secret, want on", app)
	}
	// The session that turned it on goes on working: a second factor is asked
	// for at a sign-in, not on every request.
	assertContains(t, runOK(t, "account", "get"), "Email")

	// Turning it off proves the password and answers the second factor in the
	// same request. A recovery code is what Proton hands out for exactly this
	// moment, so it is what this uses.
	recovery, _ := codes[0].(string)
	_, stderr := runOKStderr(t, "account", "settings", "two-factor", "disable", "--totp", recovery)
	assertContains(t, stderr, "authenticator app")
	if app := runJSON(t, "account", "settings", "two-factor", "get")["authenticator_app"]; app != "off" {
		t.Errorf("authenticator_app = %v after turning it off, want off", app)
	}
}

// What `get` says about an account with nothing registered, which is both free
// accounts and is what the refusals are measured against.
func TestAccountTwoFactorGetReportsBothMethods(t *testing.T) {
	stdout := runOK(t, "account", "settings", "two-factor", "get")
	assertField(t, stdout, "Authenticator App:", "off")
	assertField(t, stdout, "Security Keys:", "none")

	data := runJSON(t, "account", "settings", "two-factor", "get")
	for _, key := range []string{"authenticator_app", "security_keys"} {
		if _, ok := data[key]; !ok {
			t.Errorf("missing %q in %v", key, keysOf(data))
		}
	}
}

func TestAccountTwoFactorRefusesTheStateItCannotActOn(t *testing.T) {
	_, stderr, code := run(t, "account", "settings", "two-factor", "disable", "--totp", "123456")
	if code != 1 {
		t.Errorf("turning off what is off: exit %d, want 1\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "does not ask for a code")
}

// With nobody to ask and no code, the run says which two commands do it
// instead - before anything is minted.
func TestAccountTwoFactorEnableWithoutACodeSaysHowToScriptIt(t *testing.T) {
	_, stderr, code := run(t, "account", "settings", "two-factor", "enable")
	if code != 2 {
		t.Errorf("exit %d, want 2\nstderr: %s", code, truncateOutput(stderr))
	}
	assertContains(t, stderr, "two-factor generate")
	assertContains(t, stderr, "--totp")
}

// code is what an authenticator app holding this secret would show, with enough
// of its life left to reach Proton.
func code(t *testing.T, secret string) string {
	t.Helper()
	parsed, err := otp.Parse(secret)
	if err != nil {
		t.Fatalf("read the secret Proton minted: %v", err)
	}
	now, err := parsed.Now()
	if err != nil {
		t.Fatalf("compute a code: %v", err)
	}
	if time.Duration(now.Expires)*time.Second < codeLife {
		time.Sleep(time.Duration(now.Expires+1) * time.Second)
		if now, err = parsed.Now(); err != nil {
			t.Fatalf("compute a code: %v", err)
		}
	}
	return now.Code
}

// turnTwoFactorOff is the cleanup, which asks the account what state it is in
// rather than trusting a record of what the test did.
func turnTwoFactorOff(t *testing.T, secret string) error {
	t.Helper()
	if app := runJSON(t, "account", "settings", "two-factor", "get")["authenticator_app"]; app != "on" {
		return nil
	}
	// A fresh code rather than the one that turned it on: Proton takes each one
	// once.
	_, stderr, exit := run(t, "account", "settings", "two-factor", "disable", "--totp", code(t, secret))
	if exit != 0 {
		return fmt.Errorf("exit %d (the secret is %s): %s", exit, secret, strings.TrimSpace(stderr))
	}
	return nil
}
