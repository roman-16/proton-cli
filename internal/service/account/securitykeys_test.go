package account

import (
	"context"
	"encoding/json"
	"testing"
)

// What each of the four requests looks like on the wire. Nothing else checks
// them: no test may register a key against a real account, so the shape of
// what would be sent is the only thing that can be held to anything.

func TestSecurityKeyChallengeAsksForAKeyYouPlugIn(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"GET /core/v4/settings/2fa/register": json.RawMessage(
			`{"RegistrationOptions":{"publicKey":{"rp":{"id":"account.proton.me"}}}}`),
	}}

	challenge, err := New(a).SecurityKeyChallenge(context.Background())
	if err != nil {
		t.Fatalf("SecurityKeyChallenge: %v", err)
	}
	if got := a.sent[0].Query.Get("CrossPlatform"); got != "1" {
		t.Errorf("CrossPlatform = %q, want 1 - a key somebody plugs in", got)
	}
	// The challenge goes back to Proton exactly as it arrived, so it is carried
	// as it was written rather than decoded and built again.
	if !json.Valid(challenge) || string(challenge) == "" {
		t.Errorf("challenge = %s, want Proton's own answer", challenge)
	}
}

// An answer with no challenge in it is a failure rather than a ceremony with
// nothing to sign.
func TestSecurityKeyChallengeRefusesAnEmptyAnswer(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"GET /core/v4/settings/2fa/register": json.RawMessage(`{"Code":1000}`),
	}}
	if _, err := New(a).SecurityKeyChallenge(context.Background()); err == nil {
		t.Error("an answer carrying no challenge was accepted")
	}
}

func TestSecurityKeyRegisterSendsWhatTheKeyMade(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"POST /core/v4/settings/2fa/register": json.RawMessage(`{"Code":1000}`),
	}}
	challenge := json.RawMessage(`{"publicKey":{"challenge":[1,2,3]}}`)

	if err := New(a).SecurityKeyRegister(context.Background(), challenge, SecurityKeyCredential{
		Name:              "YubiKey 5C",
		ClientData:        []byte("client data"),
		AttestationObject: []byte("attestation"),
		Transports:        []string{"usb"},
	}); err != nil {
		t.Fatalf("SecurityKeyRegister: %v", err)
	}

	body, _ := a.sent[0].Body.(map[string]any)
	if got, _ := body["ClientData"].(string); got != "Y2xpZW50IGRhdGE=" {
		t.Errorf("ClientData = %q, want standard base64", got)
	}
	if got, _ := body["AttestationObject"].(string); got != "YXR0ZXN0YXRpb24=" {
		t.Errorf("AttestationObject = %q, want standard base64", got)
	}
	if got, _ := body["Name"].(string); got != "YubiKey 5C" {
		t.Errorf("Name = %q", got)
	}
	if got, _ := body["RegistrationOptions"].(json.RawMessage); string(got) != string(challenge) {
		t.Errorf("RegistrationOptions = %s, want the challenge exactly as it arrived", got)
	}
}

// A key that named no transport sends an empty list rather than a null, which
// is a list Proton can store and not the absence of one.
func TestSecurityKeyRegisterAlwaysNamesTheTransports(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"POST /core/v4/settings/2fa/register": json.RawMessage(`{"Code":1000}`),
	}}
	if err := New(a).SecurityKeyRegister(context.Background(), json.RawMessage(`{}`),
		SecurityKeyCredential{Name: "a key"}); err != nil {
		t.Fatalf("SecurityKeyRegister: %v", err)
	}
	body, _ := a.sent[0].Body.(map[string]any)
	transports, ok := body["Transports"].([]string)
	if !ok || transports == nil {
		t.Errorf("Transports = %v, want an empty list", body["Transports"])
	}
}

func TestSecurityKeyRenameAndRemoveAddressTheCredential(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"PUT /core/v4/settings/2fa/_--_/rename":  json.RawMessage(`{"Code":1000}`),
		"POST /core/v4/settings/2fa/_--_/remove": json.RawMessage(`{"Code":1000}`),
	}}
	service := New(a)

	if err := service.SecurityKeyRename(context.Background(), "_--_", "Key in the safe"); err != nil {
		t.Fatalf("SecurityKeyRename: %v", err)
	}
	body, _ := a.sent[0].Body.(map[string]any)
	if got, _ := body["Name"].(string); got != "Key in the safe" {
		t.Errorf("Name = %q", got)
	}
	if err := service.SecurityKeyRemove(context.Background(), "_--_"); err != nil {
		t.Fatalf("SecurityKeyRemove: %v", err)
	}
}

// The ID a listing shows is the one the two endpoints above take in a path, so
// the bytes Proton writes as numbers and the string those paths are built from
// are one conversion.
func TestSecurityKeyIDIsWhatTheEndpointsTake(t *testing.T) {
	if got := credentialID([]int{255, 239, 191}); got != "_--_" {
		t.Errorf("credentialID = %q, want base64url without padding", got)
	}
	if got := SecurityKeyID([]byte{255, 239, 191}); got != "_--_" {
		t.Errorf("SecurityKeyID = %q, want base64url without padding", got)
	}
}
