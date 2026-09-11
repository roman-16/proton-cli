// Package account reads the account itself: who it belongs to, how much space it
// uses, and what the current session is allowed to do.
//
// Plan and billing are deliberately absent. They live behind the payments
// endpoints, they are not something a script should be changing in one line, and
// the CLI's scope is what the web clients let a user do with their data.
package account

import (
	"context"

	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

type Service struct {
	C proton.Doer
}

func New(c proton.Doer) *Service { return &Service{C: c} }

// Account is the account as the CLI reports it. Field names follow
// /core/v4/users, converted to the CLI's snake_case convention.
type Account struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	UsedSpace   int64  `json:"used_space"`
	MaxSpace    int64  `json:"max_space"`
	MaxUpload   int64  `json:"max_upload"`
	CreateTime  int64  `json:"create_time"`
	// LockedKeys is how many of the account's keys a password reset left shut,
	// counting the user's own and every address's. Everything sealed to them
	// stays sealed until they are reactivated.
	LockedKeys int `json:"locked_keys,omitempty"`
}

// keyed is anything Proton hands back with its keys: the user, an address.
type keyed struct {
	Keys []struct{ Active int }
}

// locked counts the keys that are shut.
func (k keyed) locked() int {
	n := 0
	for _, key := range k.Keys {
		if key.Active == 0 {
			n++
		}
	}
	return n
}

// Get fetches the account record.
//
// The account and its addresses are asked for together: the keys of both are
// what says whether a password reset left anything locked, and neither answer
// is needed to ask for the other.
func (s *Service) Get(ctx context.Context) (*Account, error) {
	var r struct {
		User struct {
			ID          string
			Name        string
			Email       string
			DisplayName string
			UsedSpace   int64
			MaxSpace    int64
			MaxUpload   int64
			CreateTime  int64
			keyed
		}
	}
	var a struct{ Addresses []keyed }
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/users"}, &r)
		},
		func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/addresses"}, &a)
		},
	); err != nil {
		return nil, err
	}
	u := r.User
	// Proton leaves Email empty on some accounts and carries the username in
	// Name instead; the address is what a person recognises, so prefer it and
	// fall back rather than showing nothing.
	email := u.Email
	if email == "" {
		email = u.Name
	}
	name := u.DisplayName
	if name == "" {
		name = u.Name
	}
	locked := u.locked()
	for _, addr := range a.Addresses {
		locked += addr.locked()
	}
	return &Account{
		ID: u.ID, Email: email, DisplayName: name,
		UsedSpace: u.UsedSpace, MaxSpace: u.MaxSpace,
		MaxUpload: u.MaxUpload, CreateTime: u.CreateTime,
		LockedKeys: locked,
	}, nil
}
