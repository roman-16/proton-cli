package account

import (
	"context"
	"strings"

	"github.com/roman-16/proton-cli/internal/proton"
)

// The ways back into an account nobody can sign in to.
//
// There are three, and they are not alternatives so much as layers: an address
// and a number Proton can reach the owner at and then let them set a new
// password, and a phrase that opens the keys without anybody's help. The first
// two are contact details Proton also uses for security notices, which is why
// having one and allowing recovery with it are two separate things.

// Written out rather than built from settingsPath: what the CLI can send is
// read off these literals by the coverage test, which reads one file at a time.
const (
	recoveryEmailPath = "/core/v4/settings/email"
	recoveryPhonePath = "/core/v4/settings/phone"
)

// SetRecoveryEmail writes the recovery address. An empty address removes the
// one there is.
func (s *Service) SetRecoveryEmail(ctx context.Context, address string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: recoveryEmailPath,
		Body: map[string]any{"Email": address},
	}, nil)
}

// SetRecoveryPhone writes the recovery number. An empty number removes the one
// there is.
func (s *Service) SetRecoveryPhone(ctx context.Context, number string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: recoveryPhonePath,
		Body: map[string]any{"Phone": number},
	}, nil)
}

// AllowRecoveryByEmail says whether the recovery address may be used to reset
// the password. The address stays either way, and so do the notices Proton
// sends to it.
func (s *Service) AllowRecoveryByEmail(ctx context.Context, allow bool) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: recoveryEmailPath + "/reset",
		Body: map[string]any{"Reset": boolInt(allow)},
	}, nil)
}

// AllowRecoveryByPhone says whether the recovery number may be used to reset
// the password.
func (s *Service) AllowRecoveryByPhone(ctx context.Context, allow bool) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: recoveryPhonePath + "/reset",
		Body: map[string]any{"Reset": boolInt(allow)},
	}, nil)
}

// SendEmailVerification asks Proton to mail the recovery address a link that
// proves it is reachable.
//
// Sending twice sends two of them, so it is not repeatable: a mail that may
// have gone out is left alone rather than sent again on a guess.
func (s *Service) SendEmailVerification(ctx context.Context) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/verify/send",
		Body: map[string]any{"Type": "recovery_email"},
	}, nil)
}

// SendPhoneCode asks Proton to text the recovery number a code.
func (s *Service) SendPhoneCode(ctx context.Context, number string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/users/code",
		Body: map[string]any{"Type": "sms", "Destination": map[string]any{"Phone": number}},
	}, nil)
}

// VerifyPhone hands the code back, which is what marks the number verified.
//
// Proton takes it as a human verification rather than as a field: the endpoint
// answers an unverified request by asking for one, and the answer goes in the
// header a verification travels in - the number and the code together, with
// the spacing a person types stripped out (getFormattedCode,
// packages/components/containers/api/humanVerification/helper.ts).
func (s *Service) VerifyPhone(ctx context.Context, number, code string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/verify/phone",
		HVToken: strings.Join(strings.Fields(number+":"+code), ""),
		HVType:  "sms",
	}, nil)
}

// DisableRecoveryPhrase takes the phrase away, which deletes the copy of the
// keys it opened. The password is proved inside the request, as it is for
// taking a second factor away.
func (s *Service) DisableRecoveryPhrase(ctx context.Context) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/settings/mnemonic/disable",
		AccountHost: true, Proves: true,
	}, nil)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
