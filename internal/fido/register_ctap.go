//go:build !windows

package fido

import (
	"context"
	"errors"

	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/backend"
	hidkeys "github.com/telesma-app/ctap/backend/hid"
	"github.com/telesma-app/ctap/cose"
	"github.com/telesma-app/ctap/credential"
	"github.com/telesma-app/ctap/protocol"
)

// register asks a key over USB to make a credential for the account.
func register(ctx context.Context, c creation, clientData []byte, p Prompts) (Attestation, error) {
	return registerVia(ctx, hidkeys.Enumerate, c, clientData, p)
}

// registerVia is the ceremony against whichever keys enumerate finds, and the
// seam a test hands a key that answers in-process.
func registerVia(
	ctx context.Context, enumerate backend.Enumerator,
	c creation, clientData []byte, p Prompts,
) (Attestation, error) {
	device, err := selected(ctx, enumerate, "Touch your security key.", p)
	if err != nil {
		return Attestation{}, err
	}
	defer func() { _ = device.Close() }()

	return attempt(ctx, device, c.terms, protocol.PermissionMakeCredential, p,
		func(token []byte) (Attestation, error) {
			return made(ctx, device, token, c, clientData)
		})
}

// made is the one command that creates the credential, and what the key said
// about it afterwards.
func made(
	ctx context.Context, device *authenticator.Device, token []byte,
	c creation, clientData []byte,
) (Attestation, error) {
	algorithms := make([]credential.PublicKeyCredentialParameters, 0, len(c.algorithms))
	for _, alg := range c.algorithms {
		algorithms = append(algorithms, credential.PublicKeyCredentialParameters{
			Type:      credential.PublicKeyCredentialTypePublicKey,
			Algorithm: cose.Algorithm(alg),
		})
	}
	// The keys already on the account. A key holding one of them refuses rather
	// than making a second, which is how registering the same key twice becomes
	// a sentence instead of a duplicate nobody can tell apart.
	exclude := make([]credential.PublicKeyCredentialDescriptor, 0, len(c.exclude))
	for _, id := range c.exclude {
		exclude = append(exclude, credential.PublicKeyCredentialDescriptor{
			Type: credential.PublicKeyCredentialTypePublicKey,
			ID:   id,
		})
	}

	resp, err := device.MakeCredential(ctx, token, clientData,
		credential.PublicKeyCredentialRpEntity{ID: c.rpID, Name: c.rpName},
		credential.PublicKeyCredentialUserEntity{
			ID: c.user.id, Name: c.user.name, DisplayName: c.user.displayName,
		},
		algorithms, exclude, nil, nil, 0, nil)
	if err != nil {
		return Attestation{}, answered(err)
	}
	if resp.AuthData == nil || resp.AuthData.AttestedCredentialData == nil {
		return Attestation{}, errors.New("the security key answered without the credential it made")
	}

	object, err := c.attestationObject(ctx,
		string(resp.Format), resp.AttestationStatement, resp.AuthDataRaw)
	if err != nil {
		return Attestation{}, err
	}
	return Attestation{
		AttestationObject: object,
		CredentialID:      resp.AuthData.AttestedCredentialData.CredentialID,
		Transports:        reaches(device),
	}, nil
}

// reaches is how this key says it can be talked to, which Proton keeps so a
// later sign-in can say where to look. It is read from what the key said when
// it was opened rather than asked for again; a key that said nothing is left to
// the ceremony's own answer.
func reaches(device *authenticator.Device) []string {
	info, valid := device.GetInfoCached()
	if !valid {
		return nil
	}
	out := make([]string, 0, len(info.Transports))
	for _, t := range info.Transports {
		out = append(out, string(t))
	}
	return out
}
