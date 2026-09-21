//go:build !windows

package fido

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// The registration, run end to end against the software key, and the answer
// read back the way a relying party reads one: the attestation object decoded,
// the credential found inside it, and the signature checked over what the key
// said it signed.
func TestRegisterMakesACredentialProtonCanStore(t *testing.T) {
	key := newSoftKey(t)
	challenge := []byte("a challenge from Proton, 32 bytes")
	c := mustParseCreation(t, protonRegistration(challenge, nil, "", "direct"))

	attestation, err := c.ceremony(context.Background(), key.enrol, Prompts{})
	if err != nil {
		t.Fatalf("ceremony: %v", err)
	}

	var clientData struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}
	if err := json.Unmarshal(attestation.ClientData, &clientData); err != nil {
		t.Fatalf("client data is not JSON: %v", err)
	}
	if clientData.Type != "webauthn.create" {
		t.Errorf("type = %q, want webauthn.create", clientData.Type)
	}
	if clientData.Origin != "https://account.proton.me" {
		t.Errorf("origin = %q, want https://account.proton.me", clientData.Origin)
	}
	if want := base64.RawURLEncoding.EncodeToString(challenge); clientData.Challenge != want {
		t.Errorf("challenge = %q, want %q", clientData.Challenge, want)
	}
	if string(attestation.CredentialID) != string(key.credID) {
		t.Errorf("credential = %x, want %x", attestation.CredentialID, key.credID)
	}
	// A key that says nothing about how it is reached was reached over USB.
	if len(attestation.Transports) != 1 || attestation.Transports[0] != "usb" {
		t.Errorf("transports = %q, want [usb]", attestation.Transports)
	}

	object := decodeObject(t, attestation.AttestationObject)
	if object.Format != "packed" {
		t.Errorf("fmt = %q, want packed - the relying party asked to be told", object.Format)
	}
	signature, _ := object.Statement["sig"].([]byte)
	if !key.verifies(object.AuthData, attestation.ClientData, signature) {
		t.Error("the attestation signature does not verify over the authenticator data and client data")
	}
	if !bytes.Contains(object.AuthData, key.credID) {
		t.Error("the authenticator data does not carry the credential the key made")
	}
}

// The relying party that asks for no attestation is given none: the statement
// is dropped and the model identifier zeroed, so nothing in the answer says
// which make of key somebody carries.
func TestRegisterHandsOverNoAttestationWhenNoneWasAskedFor(t *testing.T) {
	for _, asked := range []string{"none", ""} {
		t.Run("attestation "+asked, func(t *testing.T) {
			key := newSoftKey(t)
			c := mustParseCreation(t, protonRegistration([]byte("challenge"), nil, "", asked))

			attestation, err := c.ceremony(context.Background(), key.enrol, Prompts{})
			if err != nil {
				t.Fatalf("ceremony: %v", err)
			}
			object := decodeObject(t, attestation.AttestationObject)
			if object.Format != "none" {
				t.Errorf("fmt = %q, want none", object.Format)
			}
			if len(object.Statement) != 0 {
				t.Errorf("attStmt = %v, want it empty", object.Statement)
			}
			if bytes.Contains(object.AuthData, model) {
				t.Error("the authenticator data still says which make of key answered")
			}
			if !bytes.Contains(object.AuthData, key.credID) {
				t.Error("the authenticator data does not carry the credential the key made")
			}
		})
	}
}

// Registering a key the account already has is the key's own refusal: Proton
// names what it holds, and the key declines rather than making a second
// credential nobody could tell from the first.
func TestRegisterRefusesAKeyTheAccountAlreadyHas(t *testing.T) {
	key := newSoftKey(t)
	c := mustParseCreation(t, protonRegistration([]byte("challenge"), [][]byte{key.credID}, "", "none"))

	_, err := c.ceremony(context.Background(), key.enrol, Prompts{})
	if !errors.Is(err, ErrRegistered) {
		t.Errorf("err = %v, want %v", err, ErrRegistered)
	}
}

// Register is what production calls, so the test that a challenge for somebody
// else never reaches a key goes through it.
func TestRegisterRefusesAChallengeForSomebodyElse(t *testing.T) {
	key := newSoftKey(t)

	_, err := Register(context.Background(), Request{
		Options: protonRegistration([]byte("challenge"), nil, "evil.example", "none"),
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

// A key with a PIN set demands it for a new credential whatever the relying
// party asked for, which is the case registering meets and signing in does not.
func TestRegisterAsksForThePINWhenTheKeyInsists(t *testing.T) {
	key := newSoftKey(t)
	key.refuseWith = 0x36 // CTAP2_ERR_PUAT_REQUIRED
	c := mustParseCreation(t, protonRegistration([]byte("challenge"), nil, "", "none"))

	asked := false
	if _, err := c.ceremony(context.Background(), key.enrol, Prompts{
		PIN: func() (string, error) { asked = true; return "1234", nil },
	}); err == nil {
		t.Error("a key that never accepts its PIN ended in success")
	}
	if !asked {
		t.Error("the key asked to verify the person and nothing asked for a PIN")
	}

	_, err := c.ceremony(context.Background(), key.enrol, Prompts{})
	if !errors.Is(err, ErrPINRequired) {
		t.Errorf("err = %v, want %v", err, ErrPINRequired)
	}
}

func TestRegisterAnnouncesTheTouchBeforeAnythingBlocks(t *testing.T) {
	key := newSoftKey(t)
	c := mustParseCreation(t, protonRegistration([]byte("challenge"), nil, "", "none"))

	var said []string
	if _, err := c.ceremony(context.Background(), key.enrol, Prompts{
		Touch: func(instruction string) { said = append(said, instruction) },
	}); err != nil {
		t.Fatalf("ceremony: %v", err)
	}
	if len(said) != 1 || said[0] == "" {
		t.Fatalf("touch prompts = %q, want exactly one instruction", said)
	}
}

// attestationObject as a relying party decodes one.
type attestationObject struct {
	Format    string         `cbor:"fmt"`
	AuthData  []byte         `cbor:"authData"`
	Statement map[string]any `cbor:"attStmt"`
}

func decodeObject(t *testing.T, raw []byte) attestationObject {
	t.Helper()
	var object attestationObject
	if err := cbor.Unmarshal(raw, &object); err != nil {
		t.Fatalf("the attestation object is not CBOR a relying party can read: %v", err)
	}
	return object
}
