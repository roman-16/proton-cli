package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Changing a password.
//
// An account keeps one secret or two. With one, it both signs in and opens the
// keys, so changing it is both halves at once - the verifier here, the keys in
// internal/account/keys, which is where anything holding a key belongs. With
// two - Proton calls it two-password mode - the halves move independently, and
// this is the whole of changing the one that signs in.

// SetLoginPassword writes a verifier for the password that signs in, touching
// no key.
//
// It is also what puts an account into two-password mode: writing the login
// password on its own is what tells Proton the two secrets are separate, and
// without it the request that locks the keys under a second password is
// refused. Proton holds a verifier rather than a password, so the same
// password written again is a new verifier and does the same job.
//
// more says another guarded change follows in the same run, so Proton keeps
// the elevated session for it rather than dropping it here.
func (s *Service) SetLoginPassword(ctx context.Context, secret string, more bool) error {
	auth, err := proton.AuthFor(ctx, s.C, secret)
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/settings/password",
		Body: map[string]any{"Auth": auth, "PersistPasswordScope": more},
	}, nil)
}
