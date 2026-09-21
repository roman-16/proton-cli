package account

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/roman-16/proton-cli/internal/proton"
)

// The security keys the account is registered with.
//
// Registering one is two requests with a ceremony in between: Proton states
// what the key is to sign, the key signs it, and what comes back goes to Proton
// with a name on it. Nothing here holds that ceremony - what a key is asked and
// how it answers is internal/fido's, and this is only the two ends of it.

const securityKeysPath = "/core/v4/settings/2fa"

// SecurityKey is one key the account signs in with.
type SecurityKey struct {
	// ID is the credential as Proton addresses it: the bytes the key named
	// itself with, written the way the endpoints that act on one take it.
	ID   string `json:"id"`
	Name string `json:"name"`
}

// SecurityKeyCredential is a credential a key has just made, in the shape
// Proton stores it.
type SecurityKeyCredential struct {
	// ID is what the account will address this key by. Proton reads it out of
	// the attestation rather than being told, so it is carried here for the one
	// thing that needs it before the next listing: saying what was created.
	ID                string
	Name              string
	ClientData        []byte
	AttestationObject []byte
	// Transports is how the key that made it can be reached, which Proton keeps
	// so a later sign-in can say where to look.
	Transports []string
}

// SecurityKeyChallenge asks Proton what a new key is to sign.
//
// It asks for a key somebody plugs in. Proton registers a built-in
// authenticator - Windows Hello, a fingerprint reader, the machine's own secure
// element - under the same setting, and one of those is the computer it lives
// in rather than something that can be carried to the next one.
func (s *Service) SecurityKeyChallenge(ctx context.Context) (json.RawMessage, error) {
	var r struct{ RegistrationOptions json.RawMessage }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: securityKeysPath + "/register",
		Query: url.Values{"CrossPlatform": {"1"}},
	}, &r); err != nil {
		return nil, err
	}
	if len(r.RegistrationOptions) == 0 {
		return nil, fmt.Errorf("proton offered no security-key challenge")
	}
	return r.RegistrationOptions, nil
}

// SecurityKeyRegister hands Proton the credential a key made for the challenge.
//
// The challenge goes back with it exactly as it arrived: Proton checks the
// answer against what it asked, and re-encoding the question is a way of
// disagreeing with it about what was asked.
func (s *Service) SecurityKeyRegister(
	ctx context.Context, challenge json.RawMessage, c SecurityKeyCredential,
) error {
	transports := c.Transports
	if transports == nil {
		transports = []string{}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: securityKeysPath + "/register",
		Body: map[string]any{
			"RegistrationOptions": challenge,
			"ClientData":          base64.StdEncoding.EncodeToString(c.ClientData),
			"AttestationObject":   base64.StdEncoding.EncodeToString(c.AttestationObject),
			"Transports":          transports,
			"Name":                c.Name,
		},
	}, nil)
}

// SecurityKeyRename changes what a registered key is called. Nothing about what
// it signs changes, which is why this is the one of the three Proton does not
// guard behind a proved password.
func (s *Service) SecurityKeyRename(ctx context.Context, id, name string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/core/v4/settings/2fa/%s/rename", id),
		Body: map[string]any{"Name": name},
	}, nil)
}

// SecurityKeyRemove takes a key off the account. The credential stays on the
// key itself, where only the key's own tool can clear it, and Proton stops
// accepting it.
func (s *Service) SecurityKeyRemove(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/core/v4/settings/2fa/%s/remove", id),
		Body: map[string]any{},
	}, nil)
}

// SecurityKeyID is how Proton addresses one registered key: the credential the
// key named itself with, written the way the endpoints that rename and remove
// one take it in a path. It is what the CLI shows as the key's ID, so the two
// are the same string.
func SecurityKeyID(credential []byte) string {
	return base64.RawURLEncoding.EncodeToString(credential)
}

// credentialID is the same thing read off the settings record, where Proton
// writes the credential as an array of numbers.
func credentialID(numbers []int) string {
	credential := make([]byte, 0, len(numbers))
	for _, b := range numbers {
		credential = append(credential, byte(b))
	}
	return SecurityKeyID(credential)
}
