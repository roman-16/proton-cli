package app

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fido"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/service/account"
)

// The transport has exactly three reasons to need something from a person:
// Proton asked for human verification, Proton asked the session to be elevated,
// or Proton asked for a second factor. All three arrive as callbacks installed
// here, so the request path stays free of any notion of a user interface and no
// command has to anticipate any of them.

type scopeReasonKey struct{}

// WithScopeReason records what the command is about to do, so a password prompt
// can say why it appeared. Commands that knowingly touch a guarded endpoint set
// it; anything else falls back to a generic phrasing.
//
// It travels in the context rather than on the App because it describes one
// operation, not the invocation.
func WithScopeReason(ctx context.Context, reason string) context.Context {
	return context.WithValue(ctx, scopeReasonKey{}, reason)
}

func scopeReason(ctx context.Context, s proton.Scope) string {
	if r, ok := ctx.Value(scopeReasonKey{}).(string); ok && r != "" {
		return r
	}
	if s == proton.ScopePassword {
		return "change your account credentials"
	}
	return "confirm this operation"
}

// installScopeResolver teaches the client how to elevate a session when the
// server refuses a request for want of a scope.
//
// Which secret answers is the scope's to say. Pass wants the password protecting
// it, and nothing else about the account: no address, because SRP at version 4
// hashes none, and no second factor, because the exchange never asks for one.
func (a *App) installScopeResolver() {
	a.API.SetScopeResolver(func(ctx context.Context, s proton.Scope) (proton.ScopeCredentials, error) {
		if s == proton.ScopePass {
			extra, err := a.Creds.ExtraPassword()
			if err != nil {
				return proton.ScopeCredentials{}, err
			}
			return proton.ScopeCredentials{Password: []byte(extra)}, nil
		}
		user, err := a.Creds.User()
		if err != nil {
			return proton.ScopeCredentials{}, err
		}
		password, err := a.Creds.Password(scopeReason(ctx, s))
		if err != nil {
			return proton.ScopeCredentials{}, err
		}
		return proton.ScopeCredentials{Username: user, Password: []byte(password)}, nil
	})
}

// installSecondFactorResolver teaches the client how to answer Proton when it
// asks for a second factor, whether that is signing in or a session being asked
// to prove itself again.
//
// Which factor answers is decided here and not by the caller: an account may
// have both, only the person at the terminal knows whether the key is in the
// room, and a code that was passed as a flag is a person who has already said.
func (a *App) installSecondFactorResolver() {
	a.API.SetSecondFactorResolver(func(ctx context.Context, offer proton.SecondFactorOffer) (proton.SecondFactorAnswer, error) {
		if offer.SecurityKey != nil && a.Creds.prefersSecurityKey(offer.TOTP) {
			return a.securityKey(ctx, *offer.SecurityKey)
		}
		code, err := a.Creds.TOTP()
		if err != nil {
			return proton.SecondFactorAnswer{}, err
		}
		return proton.SecondFactorAnswer{TOTP: code}, nil
	})
}

// securityKey runs the WebAuthn ceremony and turns whatever it ran into into a
// sentence about what to do next.
func (a *App) securityKey(ctx context.Context, req proton.SecurityKeyRequest) (proton.SecondFactorAnswer, error) {
	assertion, err := fido.Assert(ctx, fido.Request{Options: req.Options, Host: req.Host}, fido.Prompts{
		Touch: a.UI.Instruct,
		PIN:   a.Creds.SecurityKeyPIN,
	})
	if err != nil {
		return proton.SecondFactorAnswer{}, keyProblem(err)
	}
	return proton.SecondFactorAnswer{SecurityKey: &proton.SecurityKeyAssertion{
		ClientData:        assertion.ClientData,
		AuthenticatorData: assertion.AuthenticatorData,
		Signature:         assertion.Signature,
		CredentialID:      assertion.CredentialID,
	}}, nil
}

// AttestSecurityKey has a key make a credential for the account, and turns
// whatever it ran into into a sentence about what to do next.
//
// It is the registering half of what securityKey does for a sign-in, and it is
// here for the same reason: the ceremony needs somebody to touch a key and tell
// a PIN, and which relying party may be asked is the client's to say.
func (a *App) AttestSecurityKey(ctx context.Context, challenge json.RawMessage) (account.SecurityKeyCredential, error) {
	attestation, err := fido.Register(ctx, fido.Request{Options: challenge, Host: a.API.Host()}, fido.Prompts{
		Touch: a.UI.Instruct,
		PIN:   a.Creds.SecurityKeyPIN,
	})
	if err != nil {
		return account.SecurityKeyCredential{}, registrationProblem(err)
	}
	return account.SecurityKeyCredential{
		ID:                account.SecurityKeyID(attestation.CredentialID),
		ClientData:        attestation.ClientData,
		AttestationObject: attestation.AttestationObject,
		Transports:        attestation.Transports,
	}, nil
}

// keyProblem says what a key refused to do and what would get past it. Every one
// of these ends a sign-in, so each exits 2 the way a wrong password does.
func keyProblem(err error) error {
	switch {
	case errors.Is(err, fido.ErrNoCredential):
		return errs.Problemf("This security key is not one this account is registered with.").
			Hint("try the key you registered with Proton").Exit(2)
	case errors.Is(err, fido.ErrPINRequired):
		return errs.Problemf("This security key asks for its PIN, and there is nobody here to ask.").
			Hint("run this in a terminal, or sign in with --totp instead").Exit(2)
	case errors.Is(err, fido.ErrPINBlocked):
		return errs.Problemf("This security key has locked itself after too many wrong PINs.").
			Hint("reset it with its manufacturer's own tool, or sign in with --totp").Exit(2)
	case errors.Is(err, fido.ErrUnsupported):
		return errs.Problemf("This build cannot reach a security key on this machine.").
			Hint("sign in with --totp, or install proton from a release rather than with go install").Exit(2)
	}
	return reachingKey(err)
}

// registrationProblem is the same for the ceremony that makes a credential. The
// key is in the same states and refuses in the same ways; what differs is what
// there is to suggest, because a code from an authenticator app is not another
// way of doing this one.
func registrationProblem(err error) error {
	switch {
	case errors.Is(err, fido.ErrRegistered):
		return errs.Problemf("This security key is already registered with the account.").
			Hint("proton account settings security-keys list shows the keys it has").Exit(2)
	case errors.Is(err, fido.ErrPINRequired):
		return errs.Problemf("This security key asks for its PIN, and there is nobody here to ask.").
			Hint("run this in a terminal - registering a key takes somebody at the machine").Exit(2)
	case errors.Is(err, fido.ErrPINBlocked):
		return errs.Problemf("This security key has locked itself after too many wrong PINs.").
			Hint("reset it with its manufacturer's own tool, which clears everything on it").Exit(2)
	case errors.Is(err, fido.ErrUnsupported):
		return errs.Problemf("This build cannot reach a security key on this machine.").
			Hint("install proton from a release rather than with go install,",
				"or register the key at https://account.proton.me").Exit(2)
	}
	return reachingKey(err)
}

// reachingKey is what both ceremonies say about the states a key can be in
// before it has been asked anything, where there is nothing to suggest that
// depends on what it was going to be asked.
func reachingKey(err error) error {
	switch {
	case errors.Is(err, fido.ErrNoDevice):
		return errs.Problemf("No security key is connected to this machine.").
			Hint("plug the key in and run this again").Exit(2)
	case errors.Is(err, fido.ErrPermission):
		return errs.Problemf("A security key is connected but proton cannot open it.").
			Hint("install the udev rules for FIDO devices (the libfido2 package ships them),",
				"then unplug the key and plug it in again").Exit(2)
	case errors.Is(err, fido.ErrDenied):
		return errs.Problemf("The security key was not used in time.").
			Hint("run this again and touch the key when it lights up").Exit(2)
	case errors.Is(err, fido.ErrPINWrong):
		return errs.Problemf("That is not the PIN of this security key.").
			Hint("run this again - a key locks itself after a few wrong PINs").Exit(2)
	}
	return err
}
