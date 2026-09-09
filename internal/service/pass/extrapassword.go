package pass

import (
	"context"
	"slices"

	"github.com/roman-16/proton-cli/internal/proton"
)

// The password Pass can be protected with, on top of the account's own.
//
// Proton stores a verifier rather than the password, so setting one sends
// something a password can be checked against and never the password. Taking one
// off is guarded by the password it is taking off, which is proved by the client
// before the removal is sent.

const extraPasswordPath = "/pass/v1/user/srp"

// ExtraPassword is whether Pass is protected with one.
type ExtraPassword struct {
	Enabled bool `json:"enabled"`
}

// ExtraPassword reports whether Pass is protected with an extra password.
//
// The session's scopes answer it first, because they answer it without asking
// anybody for anything: Proton withholds the Pass scope from a session that has
// not proved an extra password, so a session without the scope belongs to an
// account that has one. A session holding the scope belongs to an account that
// either never had one or has already proved it, and only Pass tells those apart.
func (s *Service) ExtraPassword(ctx context.Context) (ExtraPassword, error) {
	scopes, err := proton.Scopes(ctx, s.C)
	if err != nil {
		return ExtraPassword{}, err
	}
	if !slices.Contains(scopes, string(proton.ScopePass)) {
		return ExtraPassword{Enabled: true}, nil
	}
	var r struct{ HasSRP bool }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: extraPasswordPath}, &r); err != nil {
		return ExtraPassword{}, err
	}
	return ExtraPassword{Enabled: r.HasSRP}, nil
}

// ExtraPasswordSet protects Pass with a password of its own.
func (s *Service) ExtraPasswordSet(ctx context.Context, password string) error {
	modulus, err := proton.FetchModulus(ctx, s.C)
	if err != nil {
		return err
	}
	v, err := modulus.Verifier([]byte(password))
	if err != nil {
		return err
	}
	return s.C.Decode(ctx, proton.Request{Method: "POST", Path: extraPasswordPath, Body: map[string]any{
		"SrpModulusID": v.ModulusID,
		"SrpSalt":      v.Salt,
		"SrpVerifier":  v.Value,
	}}, nil)
}

// ExtraPasswordRemove takes it off again. The caller proves the password first:
// what authorises the removal is the secret being removed.
func (s *Service) ExtraPasswordRemove(ctx context.Context) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: extraPasswordPath}, nil)
}
