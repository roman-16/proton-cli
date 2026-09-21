package account

import (
	"context"
	"fmt"
	"net/url"

	"github.com/roman-16/proton-cli/internal/proton"
)

// The authenticator app as a second factor.
//
// Proton mints the secret and holds it pending until a code proves it arrived
// somewhere that can compute one; only then is the account asked for a code at
// every sign-in. So turning it on is two requests with a person in between,
// which is why the CLI can offer them as two commands as well as one.

const totpPath = "/core/v4/settings/2fa/totp"

// TOTPSecret asks Proton for a secret to put in an authenticator app.
//
// It is held pending, and nothing about the account changes until a code
// computed from it is handed back. Asking again replaces what is pending.
func (s *Service) TOTPSecret(ctx context.Context) (string, error) {
	var r struct{ Secret string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: totpPath + "/secret",
	}, &r); err != nil {
		return "", err
	}
	if r.Secret == "" {
		return "", fmt.Errorf("proton offered no two-factor secret")
	}
	return r.Secret, nil
}

// OTPAuthURI is the secret as an authenticator app reads it, with the
// parameters Proton's own clients write (getTOTPData,
// packages/shared/lib/settings/twoFactor.ts). identifier is what the app will
// list the account under.
func OTPAuthURI(identifier, secret string) string {
	return fmt.Sprintf("otpauth://totp/%s?secret=%s&issuer=Proton&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(identifier), url.QueryEscape(secret))
}

// TOTPEnable confirms the pending secret with a code from it, and answers with
// the recovery codes Proton hands out once.
//
// Each code signs in once in place of the app, and this is the only time they
// are shown: Proton keeps hashes of them the way it keeps a password.
func (s *Service) TOTPEnable(ctx context.Context, code string) ([]string, error) {
	var r struct{ TwoFactorRecoveryCodes []string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: totpPath,
		Body: map[string]any{"TOTPConfirmation": code},
	}, &r); err != nil {
		return nil, err
	}
	return r.TwoFactorRecoveryCodes, nil
}

// TOTPDisable turns the authenticator app off.
//
// The password is proved inside the request rather than beforehand, which is
// what Proton wants for taking a second factor away; the code is the second
// factor itself, and one of the recovery codes does as well.
func (s *Service) TOTPDisable(ctx context.Context) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: totpPath, Proves: true,
	}, nil)
}
