//go:build !windows

package fido

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/telesma-app/ctap/backend"
	"github.com/telesma-app/ctap/transport"
)

// The sign-in, run end to end against the software key: the real CTAP stack,
// real CTAPHID framing over an in-memory device, and an answer checked the way
// Proton checks it - the signature verified over the authenticator data and the
// hash of the clientDataJSON this package wrote.
func TestAssertAnswersTheChallenge(t *testing.T) {
	key := newSoftKey(t)
	challenge := []byte("a challenge from Proton, 32 bytes")
	a := mustParse(t, protonChallenge(challenge, key.credID, ""))

	assertion, err := a.ceremony(context.Background(), key.sign, Prompts{})
	if err != nil {
		t.Fatalf("ceremony: %v", err)
	}

	var clientData struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(assertion.ClientData, &clientData); err != nil {
		t.Fatalf("client data is not JSON: %v", err)
	}
	if clientData.Type != "webauthn.get" {
		t.Errorf("type = %q, want webauthn.get", clientData.Type)
	}
	if clientData.Origin != "https://account.proton.me" {
		t.Errorf("origin = %q, want https://account.proton.me", clientData.Origin)
	}
	if want := base64.RawURLEncoding.EncodeToString(challenge); clientData.Challenge != want {
		t.Errorf("challenge = %q, want %q", clientData.Challenge, want)
	}
	if !key.verifies(assertion.AuthenticatorData, assertion.ClientData, assertion.Signature) {
		t.Error("the signature does not verify over the authenticator data and client data")
	}
	if string(assertion.CredentialID) != string(key.credID) {
		t.Errorf("credential = %x, want %x", assertion.CredentialID, key.credID)
	}
}

// A key given one credential to choose from may answer without naming it, which
// the specification allows and Proton's API does not: it wants to be told which
// credential answered.
func TestAssertNamesTheCredentialAKeyLeftOut(t *testing.T) {
	key := newSoftKey(t)
	key.omitCredential = true
	a := mustParse(t, protonChallenge([]byte("challenge"), key.credID, ""))

	assertion, err := a.ceremony(context.Background(), key.sign, Prompts{})
	if err != nil {
		t.Fatalf("ceremony: %v", err)
	}
	if string(assertion.CredentialID) != string(key.credID) {
		t.Errorf("credential = %x, want %x", assertion.CredentialID, key.credID)
	}
	if !key.verifies(assertion.AuthenticatorData, assertion.ClientData, assertion.Signature) {
		t.Error("the signature does not verify")
	}
}

// Assert is what production calls, so the test that a challenge for somebody
// else never reaches a key goes through it rather than through the seam below.
func TestAssertRefusesAChallengeForSomebodyElse(t *testing.T) {
	key := newSoftKey(t)

	_, err := Assert(context.Background(), Request{
		Options: protonChallenge([]byte("challenge"), key.credID, "evil.example"),
		Host:    "account.proton.me",
	}, Prompts{})
	if err == nil {
		t.Fatal("a challenge naming another relying party was answered")
	}
	if !strings.Contains(err.Error(), "evil.example") {
		t.Errorf("error = %v, want it to name the relying party it refused", err)
	}
	if key.asked {
		t.Error("the key was asked about a relying party it should never have heard")
	}
}

func TestAssertReportsWhatWentWrong(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enumerate backend.Enumerator
		status    transport.StatusCode
		want      error
	}{
		{name: "nothing plugged in", enumerate: noKeys, want: ErrNoDevice},
		{name: "a key this user may not open", enumerate: unopenableKey, want: ErrPermission},
		{name: "a key that holds nothing for the account", status: transport.CTAP2_ERR_NO_CREDENTIALS, want: ErrNoCredential},
		{name: "a key nobody touched", status: transport.CTAP2_ERR_ACTION_TIMEOUT, want: ErrDenied},
		{name: "a ceremony called off", status: transport.CTAP2_ERR_OPERATION_DENIED, want: ErrDenied},
		{name: "a key that has locked itself", status: transport.CTAP2_ERR_PIN_BLOCKED, want: ErrPINBlocked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key := newSoftKey(t)
			key.refuseWith = tc.status
			enumerate := tc.enumerate
			if enumerate == nil {
				enumerate = key.enumerate
			}
			a := mustParse(t, protonChallenge([]byte("challenge"), key.credID, ""))
			_, err := assertVia(context.Background(), enumerate, a, []byte("{}"), Prompts{})
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// A key that insists on being verified is answered with its PIN, and a run with
// nobody to ask says so rather than hanging or failing as something else.
func TestAssertAsksForThePINOnlyWhenTheKeyInsists(t *testing.T) {
	key := newSoftKey(t)
	key.refuseWith = transport.CTAP2_ERR_PUAT_REQUIRED
	a := mustParse(t, protonChallenge([]byte("challenge"), key.credID, ""))

	asked := false
	if _, err := a.ceremony(context.Background(), key.sign, Prompts{
		PIN: func() (string, error) { asked = true; return "1234", nil },
	}); err == nil {
		t.Error("a key that never accepts its PIN ended in success")
	}
	if !asked {
		t.Error("the key asked to verify the person and nothing asked for a PIN")
	}

	_, err := a.ceremony(context.Background(), key.sign, Prompts{})
	if !errors.Is(err, ErrPINRequired) {
		t.Errorf("err = %v, want %v", err, ErrPINRequired)
	}
}

// Nothing that can block on a person does so silently.
func TestTouchIsAnnouncedBeforeAnythingBlocks(t *testing.T) {
	key := newSoftKey(t)
	a := mustParse(t, protonChallenge([]byte("challenge"), key.credID, ""))

	var said []string
	if _, err := a.ceremony(context.Background(), key.sign, Prompts{
		Touch: func(instruction string) { said = append(said, instruction) },
	}); err != nil {
		t.Fatalf("ceremony: %v", err)
	}
	if len(said) != 1 || said[0] == "" {
		t.Fatalf("touch prompts = %q, want exactly one instruction", said)
	}
}
