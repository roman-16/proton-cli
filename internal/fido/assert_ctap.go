//go:build !windows

package fido

import (
	"context"

	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/backend"
	hidkeys "github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/protocol"
)

// assert asks a key over USB for an assertion.
func assert(ctx context.Context, a authentication, clientData []byte, p Prompts) (Assertion, error) {
	return assertVia(ctx, hidkeys.Enumerate, a, clientData, p)
}

// assertVia is the ceremony against whichever keys enumerate finds. Production
// finds them on the USB bus; a test can hand it one that answers in-process,
// which is the only way this path runs without hardware in the room.
func assertVia(
	ctx context.Context, enumerate backend.Enumerator,
	a authentication, clientData []byte, p Prompts,
) (Assertion, error) {
	device, err := selected(ctx, enumerate, "Touch your security key.", p)
	if err != nil {
		return Assertion{}, err
	}
	defer func() { _ = device.Close() }()

	return attempt(ctx, device, a.terms, protocol.PermissionGetAssertion, p,
		func(token []byte) (Assertion, error) {
			return assertion(ctx, device, token, a, clientData)
		})
}

func assertion(
	ctx context.Context, device *authenticator.Device, token []byte,
	a authentication, clientData []byte,
) (Assertion, error) {
	allow := make([]credential.PublicKeyCredentialDescriptor, 0, len(a.allowCredentials))
	for _, id := range a.allowCredentials {
		allow = append(allow, credential.PublicKeyCredentialDescriptor{
			Type: credential.PublicKeyCredentialTypePublicKey,
			ID:   id,
		})
	}
	for resp, err := range device.GetAssertion(ctx, token, a.rpID, clientData, allow,
		nil, map[protocol.Option]bool{protocol.OptionUserPresence: true}) {
		if err != nil {
			return Assertion{}, answered(err)
		}
		// The first assertion is the answer: Proton names the credentials it will
		// accept, so any of them proves the same thing, and asking the key for the
		// rest would be another touch for nothing.
		return Assertion{
			CredentialID:      resp.Credential.ID,
			AuthenticatorData: resp.AuthDataRaw,
			Signature:         resp.Signature,
		}, nil
	}
	return Assertion{}, ErrNoCredential
}
