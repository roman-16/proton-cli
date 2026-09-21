//go:build !windows

package fido

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/telesma-app/ctap/authenticator"
	"github.com/telesma-app/ctap/backend"
	"github.com/telesma-app/ctap/protocol"
	"github.com/telesma-app/ctap/transport"
)

// What both ceremonies do before and around the one command they send: find a
// key, satisfy it that somebody is there, and turn what it says into what this
// package promises.
//
// Everything here talks CTAP2 over USB, which is the transport every roaming
// security key answers on. A key reached over Bluetooth or lying in a phone is
// not one of these; nothing on this side of the wire can reach those.

// selected opens the key the person chose, announcing the wait first.
//
// The announcement comes before the search, not after: choosing between two
// connected keys is itself done by touching one, so the first thing that can
// block on a person is already behind this line.
func selected(
	ctx context.Context, enumerate backend.Enumerator, instruction string, p Prompts,
) (*authenticator.Device, error) {
	p.touch(instruction)
	device, err := authenticator.Select(ctx, enumerate)
	if err != nil {
		return nil, found(ctx, err)
	}
	return device, nil
}

// attempt runs one command against the key, asking for its PIN if the key turns
// out to insist on one.
//
// A key may hold its own opinion about being verified - one configured to always
// ask says so when the command arrives rather than in its capabilities, and the
// only answer is its PIN. Registering is where this is the rule rather than the
// exception: a key with a PIN set demands it for a new credential whatever the
// relying party asked for.
func attempt[T any](
	ctx context.Context, device *authenticator.Device, t terms,
	permission protocol.Permission, p Prompts, ask func(token []byte) (T, error),
) (T, error) {
	var zero T
	token, err := verification(ctx, device, t, permission, p)
	if err != nil {
		return zero, err
	}
	out, err := ask(token)
	if errors.Is(err, errNeedsPIN) && token == nil {
		if token, err = pinToken(ctx, device, t, permission, p); err != nil {
			return zero, err
		}
		return ask(token)
	}
	return out, err
}

// errNeedsPIN is the key saying it will not act until the person is verified.
// It never reaches a caller: it is what turns into asking for the PIN.
var errNeedsPIN = errors.New("security key requires verification")

// verification obtains what the key needs to be satisfied the person is there,
// before anything is asked of it - but only when the relying party or the key
// itself insists. Asking for a PIN that nothing wanted spends the person's
// attention and, on a key that counts attempts, some of its patience.
func verification(
	ctx context.Context, device *authenticator.Device, t terms,
	permission protocol.Permission, p Prompts,
) ([]byte, error) {
	info, err := device.GetInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("reading the security key: %w", err)
	}
	if !t.needsVerification() && !info.Options[protocol.OptionAlwaysUv] {
		return nil, nil
	}
	// A key with no PIN set verifies by fingerprint or not at all, and either way
	// there is nothing to ask for.
	if set, supported := info.Options[protocol.OptionClientPIN]; !supported || !set {
		return nil, nil
	}
	return pinToken(ctx, device, t, permission, p)
}

func pinToken(
	ctx context.Context, device *authenticator.Device, t terms,
	permission protocol.Permission, p Prompts,
) ([]byte, error) {
	pin, err := p.pin()
	if err != nil {
		return nil, err
	}
	// The token is asked for with the one permission it is about to be used with,
	// so a key that supports scoped tokens hands over nothing wider.
	token, err := device.GetPinUvAuthTokenUsingPIN(ctx, pin, permission, t.rpID)
	if err != nil {
		return nil, answered(err)
	}
	return token, nil
}

// found says why no key answered. A key that is present but unopenable is the
// common Linux case and a different sentence from no key at all.
func found(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, fs.ErrPermission) {
		return ErrPermission
	}
	return ErrNoDevice
}

// answered turns what a key said into what this package promises. A status this
// does not name is passed through: an unrecognised refusal is better read in the
// key's own words than flattened into somebody else's.
func answered(err error) error {
	// The stack refuses some commands on the key's behalf, from what the key said
	// about itself when it was opened, so a demand for verification arrives either
	// as a status or as this.
	if errors.Is(err, authenticator.ErrPinUvAuthTokenRequired) {
		return errNeedsPIN
	}
	var status *transport.CTAPError
	if !errors.As(err, &status) {
		return err
	}
	switch status.StatusCode {
	case transport.CTAP2_ERR_NO_CREDENTIALS:
		return ErrNoCredential
	case transport.CTAP2_ERR_CREDENTIAL_EXCLUDED:
		return ErrRegistered
	case transport.CTAP2_ERR_OPERATION_DENIED, transport.CTAP2_ERR_ACTION_TIMEOUT, transport.CTAP1_ERR_TIMEOUT:
		return ErrDenied
	case transport.CTAP2_ERR_PUAT_REQUIRED:
		return errNeedsPIN
	case transport.CTAP2_ERR_PIN_INVALID:
		return ErrPINWrong
	case transport.CTAP2_ERR_PIN_BLOCKED, transport.CTAP2_ERR_PIN_AUTH_BLOCKED, transport.CTAP2_ERR_UV_BLOCKED:
		return ErrPINBlocked
	}
	return err
}
