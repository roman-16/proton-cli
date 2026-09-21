//go:build windows && webauthn

package fido

import (
	"context"

	"github.com/go-ctap/ctaphid/pkg/webauthntypes"
	"github.com/go-ctap/winhello"
)

// assert hands the sign-in to Windows.
func assert(_ context.Context, a authentication, clientData []byte, p Prompts) (Assertion, error) {
	hwnd, err := dialog()
	if err != nil {
		return Assertion{}, err
	}

	allow := make([]webauthntypes.PublicKeyCredentialDescriptor, 0, len(a.allowCredentials))
	for _, id := range a.allowCredentials {
		allow = append(allow, webauthntypes.PublicKeyCredentialDescriptor{
			Type: webauthntypes.PublicKeyCredentialTypePublicKey,
			ID:   id,
		})
	}

	p.touch("Follow the prompt from Windows to use your security key.")
	assertion, err := winhello.GetAssertion(hwnd, a.rpID, clientData, allow, nil,
		&winhello.AuthenticatorGetAssertionOptions{
			Timeout: a.timeout(),
			// Neither attachment is ruled out: Proton registers a key plugged into
			// the machine and a Windows Hello credential living in it under the same
			// setting, and either can be the one this account has.
			AuthenticatorAttachment: winhello.WinHelloAuthenticatorAttachmentAny,
			UserVerificationRequirement: winhello.WinHelloUserVerificationRequirement(
				verificationRequirement(a.terms)),
		})
	if err != nil {
		return Assertion{}, answered(err)
	}
	return Assertion{
		CredentialID:      assertion.Credential.ID,
		AuthenticatorData: assertion.AuthDataRaw,
		Signature:         assertion.Signature,
	}, nil
}
