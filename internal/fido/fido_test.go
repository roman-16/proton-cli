package fido

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Proton writes the binary parts of a challenge as arrays of numbers rather than
// as strings, which is the one thing about its shape a client has to know.
func TestParseReadsProtonsChallenge(t *testing.T) {
	a, err := parseAuthentication(json.RawMessage(`{"publicKey":{
		"challenge":[1,2,255],
		"rpId":"account.proton.me",
		"timeout":60000,
		"allowCredentials":[{"id":[9,8],"type":"public-key"},{"id":[7],"type":"public-key"}],
		"userVerification":"discouraged"}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if a.rpID != "account.proton.me" {
		t.Errorf("rpId = %q", a.rpID)
	}
	if string(a.challenge) != string([]byte{1, 2, 255}) {
		t.Errorf("challenge = %v", a.challenge)
	}
	if len(a.allowCredentials) != 2 || string(a.allowCredentials[0]) != string([]byte{9, 8}) {
		t.Errorf("allowCredentials = %v", a.allowCredentials)
	}
	if a.timeout() != time.Minute {
		t.Errorf("timeout = %v, want 1m", a.timeout())
	}
	if a.needsVerification() {
		t.Error("a discouraged verification was read as a required one")
	}
}

func TestParseRefusesAChallengeNothingCanAnswer(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"no relying party", `{"publicKey":{"challenge":[1],"allowCredentials":[{"id":[2]}]}}`},
		{"nothing to sign", `{"publicKey":{"rpId":"proton.me","allowCredentials":[{"id":[2]}]}}`},
		{"no registered key", `{"publicKey":{"rpId":"proton.me","challenge":[1],"allowCredentials":[]}}`},
		{"not an answer at all", `"nonsense"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseAuthentication(json.RawMessage(tc.raw)); err == nil {
				t.Error("a challenge nothing could answer was accepted")
			}
		})
	}
}

// A registration challenge says who the credential is for as well as who is
// asking, and names the relying party a level deeper than a sign-in does.
func TestParseReadsProtonsRegistrationChallenge(t *testing.T) {
	c, err := parseCreation(json.RawMessage(`{"publicKey":{
		"rp":{"id":"account.proton.me","name":"Proton"},
		"user":{"id":[7,7],"name":"alice@proton.me","displayName":"Alice"},
		"challenge":[1,2,255],
		"timeout":60000,
		"pubKeyCredParams":[{"type":"public-key","alg":-7},{"type":"public-key","alg":-257}],
		"excludeCredentials":[{"id":[9,8],"type":"public-key"}],
		"authenticatorSelection":{"userVerification":"discouraged"},
		"attestation":"none"}}`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.rpID != "account.proton.me" || c.rpName != "Proton" {
		t.Errorf("relying party = %q %q", c.rpID, c.rpName)
	}
	if string(c.user.id) != string([]byte{7, 7}) || c.user.name != "alice@proton.me" || c.user.displayName != "Alice" {
		t.Errorf("user = %v", c.user)
	}
	if len(c.algorithms) != 2 || c.algorithms[0] != -7 {
		t.Errorf("algorithms = %v, want the relying party's order", c.algorithms)
	}
	if len(c.exclude) != 1 || string(c.exclude[0]) != string([]byte{9, 8}) {
		t.Errorf("excludeCredentials = %v", c.exclude)
	}
	if c.wantsAttestation() {
		t.Error("a challenge asking for no attestation was read as asking for one")
	}
}

func TestParseRefusesARegistrationNothingCanAnswer(t *testing.T) {
	for _, tc := range []struct{ name, raw string }{
		{"no relying party", `{"publicKey":{"challenge":[1],"pubKeyCredParams":[{"alg":-7}]}}`},
		{"nothing to sign", `{"publicKey":{"rp":{"id":"proton.me"},"pubKeyCredParams":[{"alg":-7}]}}`},
		{"no algorithm", `{"publicKey":{"rp":{"id":"proton.me"},"challenge":[1],"pubKeyCredParams":[]}}`},
		{"not an answer at all", `"nonsense"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseCreation(json.RawMessage(tc.raw)); err == nil {
				t.Error("a challenge nothing could answer was accepted")
			}
		})
	}
}

// The check a browser makes on every ceremony, and the reason a security key
// cannot be phished through one.
func TestChallengeMustComeFromTheHostItNames(t *testing.T) {
	for _, tc := range []struct {
		rpID, host string
		ok         bool
	}{
		{rpID: "account.proton.me", host: "account.proton.me", ok: true},
		{rpID: "proton.me", host: "account.proton.me", ok: true},
		{rpID: "proton.me", host: "mail.proton.me", ok: true},
		{rpID: "account.proton.me", host: "proton.me"},
		{rpID: "evil.example", host: "account.proton.me"},
		{rpID: "notproton.me", host: "account.proton.me"},
		{rpID: "me", host: "account.proton.me"},
		{rpID: "account.proton.me", host: ""},
		{rpID: "", host: "account.proton.me"},
	} {
		t.Run(tc.rpID+" from "+tc.host, func(t *testing.T) {
			err := terms{rpID: tc.rpID}.belongsTo(tc.host)
			if tc.ok && err != nil {
				t.Errorf("refused a challenge from the host that sent it: %v", err)
			}
			if !tc.ok && err == nil {
				t.Error("answered a challenge about a relying party the host has no claim to")
			}
		})
	}
}

// What the key signs the hash of, and what Proton checks the signature against.
// The challenge is base64url without padding, as WebAuthn writes it, and the
// ceremony is named in it - a registration answered as a sign-in is refused.
func TestClientDataIsWhatWebAuthnAsksFor(t *testing.T) {
	asked := terms{rpID: "account.proton.me", challenge: []byte{0xff, 0xef, 0xbf}}
	for ceremony, want := range map[string]string{
		ceremonyGet:    `{"type":"webauthn.get","challenge":"_--_","origin":"https://account.proton.me"}`,
		ceremonyCreate: `{"type":"webauthn.create","challenge":"_--_","origin":"https://account.proton.me"}`,
	} {
		raw, err := asked.clientData(ceremony)
		if err != nil {
			t.Fatalf("client data: %v", err)
		}
		if string(raw) != want {
			t.Errorf("client data =\n\t%s\nwant\n\t%s", raw, want)
		}
		if strings.Contains(string(raw), "=") {
			t.Error("the challenge is padded, and WebAuthn's is not")
		}
	}
}

func TestCredentialNamesWhoAnswered(t *testing.T) {
	one := authentication{allowCredentials: [][]byte{[]byte("only")}}
	two := authentication{allowCredentials: [][]byte{[]byte("first"), []byte("second")}}

	if got, err := two.credential([]byte("second")); err != nil || string(got) != "second" {
		t.Errorf("credential = %q, %v; want the one the key named", got, err)
	}
	if got, err := one.credential(nil); err != nil || string(got) != "only" {
		t.Errorf("credential = %q, %v; want the only one offered", got, err)
	}
	if _, err := two.credential(nil); err == nil {
		t.Error("a key that named nothing out of two credentials was believed")
	}
}

func TestTimeoutFallsBackToSomethingAPersonCanMeet(t *testing.T) {
	if got := (terms{}).timeout(); got != 2*time.Minute {
		t.Errorf("timeout = %v, want 2m", got)
	}
}

// What a relying party that asked for no attestation is given, whatever the key
// answered with: no statement, and no identifier saying which make of key it is.
func TestAttestationObjectSaysNothingNobodyAskedFor(t *testing.T) {
	serial := bytes.Repeat([]byte{0xab}, 16)
	authData := append(bytes.Repeat([]byte{0x11}, 37), serial...)
	authData = append(authData, []byte("the credential")...)
	statement := map[string]any{"alg": -7, "sig": []byte("a signature")}

	raw, err := creation{}.attestationObject(context.Background(), "packed", statement, authData)
	if err != nil {
		t.Fatalf("attestation object: %v", err)
	}
	var object struct {
		Format    string         `cbor:"fmt"`
		AuthData  []byte         `cbor:"authData"`
		Statement map[string]any `cbor:"attStmt"`
	}
	if err := cbor.Unmarshal(raw, &object); err != nil {
		t.Fatalf("not CBOR a relying party can read: %v", err)
	}
	if object.Format != "none" {
		t.Errorf("fmt = %q, want none", object.Format)
	}
	if len(object.Statement) != 0 {
		t.Errorf("attStmt = %v, want it empty", object.Statement)
	}
	if bytes.Contains(object.AuthData, serial) {
		t.Error("the authenticator data still says which make of key answered")
	}
	if !bytes.Contains(object.AuthData, []byte("the credential")) {
		t.Error("the credential went missing with the model identifier")
	}
}

// And what one that asked is given: exactly what the key said.
func TestAttestationObjectCarriesWhatWasAskedFor(t *testing.T) {
	authData := bytes.Repeat([]byte{0x11}, 64)
	statement := map[string]any{"sig": []byte("a signature")}

	raw, err := creation{attestation: "direct"}.
		attestationObject(context.Background(), "packed", statement, authData)
	if err != nil {
		t.Fatalf("attestation object: %v", err)
	}
	var object struct {
		Format    string         `cbor:"fmt"`
		AuthData  []byte         `cbor:"authData"`
		Statement map[string]any `cbor:"attStmt"`
	}
	if err := cbor.Unmarshal(raw, &object); err != nil {
		t.Fatalf("not CBOR a relying party can read: %v", err)
	}
	if object.Format != "packed" {
		t.Errorf("fmt = %q, want packed", object.Format)
	}
	if signature, _ := object.Statement["sig"].([]byte); string(signature) != "a signature" {
		t.Errorf("attStmt = %v, want the key's own statement", object.Statement)
	}
	if !bytes.Equal(object.AuthData, authData) {
		t.Error("the authenticator data was altered for a relying party that asked to see it")
	}
}
