package account

import (
	"context"
	"slices"
	"strings"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

type Service struct {
	C    proton.Doer
	keys keys.Get
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

type Account struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	UsedSpace   int64  `json:"used_space"`
	MaxSpace    int64  `json:"max_space"`
	MaxUpload   int64  `json:"max_upload"`
	CreateTime  int64  `json:"create_time"`
	LockedKeys  int    `json:"locked_keys,omitempty"`

	names []string
	spent map[string]int64
}

func (a *Account) SpentOn(product string) int64 { return a.spent[product] }

func (a *Account) AnswersTo(name string) bool {
	return name != "" && slices.ContainsFunc(a.names, func(n string) bool { return strings.EqualFold(n, name) })
}

type keyed struct {
	Keys []struct{ Active int }
}

func (k keyed) locked() int {
	n := 0
	for _, key := range k.Keys {
		if key.Active == 0 {
			n++
		}
	}
	return n
}

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

			ProductUsedSpace map[string]int64
		}
	}
	var a struct {
		Addresses []struct {
			Email string
			keyed
		}
	}
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
	email := u.Email
	if email == "" {
		email = u.Name
	}
	username := u.Name
	if username == email {
		username = ""
	}
	locked := u.locked()
	names := []string{u.Name}
	for _, addr := range a.Addresses {
		locked += addr.locked()
		names = append(names, addr.Email)
	}
	return &Account{
		ID: u.ID, Email: email, Username: username, DisplayName: u.DisplayName,
		UsedSpace: u.UsedSpace, MaxSpace: u.MaxSpace,
		MaxUpload: u.MaxUpload, CreateTime: u.CreateTime,
		LockedKeys: locked,
		names:      names,
		spent:      u.ProductUsedSpace,
	}, nil
}
