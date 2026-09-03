package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

type Service struct {
	C    proton.Doer
	keys keys.Get
	// A share's keys are wanted twice by most commands - once to read the vault's
	// name and again to read what is in it - and once per vault by any command that
	// covers them all.
	shareKeys fetch.Memo[*shareKeys]
	// Which shares this account holds is the first thing nearly every command
	// needs, and several need it twice: a listing walks the vaults, and resolving
	// a name walks them again for the items shared on their own.
	shareList fetch.Memo[[]Share]
}

func New(c proton.Doer, k keys.Get) *Service { return &Service{C: c, keys: k} }

// What a share points at. A vault share opens everything in the vault, now and
// later; an item share opens one item and nothing around it.
const (
	targetVault = 1
	targetItem  = 2
)

// Share is what links this account to something it can open, and says what it
// may do with it.
//
// Proton makes one per person per resource, so the vault you made and the copy
// you gave somebody are two shares of the same vault with different IDs, and an
// item somebody gave you is a share that points at that item alone.
type Share struct {
	ShareID    string
	VaultID    string
	TargetType int
	TargetID   string
	Owner      bool
	Shared     bool
	Members    int
	AddressID  string
	// Access is what this account may do with it: viewer, editor or manager.
	Access string
	// Content is the encrypted name and display of a vault share, and empty on an
	// item share, which has nothing of its own to name.
	Content            string
	ContentKeyRotation int
}

// Vault reports whether the share opens a whole vault.
func (sh Share) Vault() bool { return sh.TargetType == targetVault }

// shares reads every share this account holds, of either kind.
//
// The account's keys are asked for alongside, because whatever the caller does
// with a share needs them: a vault's name and an item's content are both sealed
// under a share key that only they open.
func (s *Service) shares(ctx context.Context) ([]Share, error) {
	return s.shareList.Do("", func() ([]Share, error) { return s.fetchShares(ctx) })
}

func (s *Service) fetchShares(ctx context.Context) ([]Share, error) {
	var raw []json.RawMessage
	if _, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		var err error
		raw, err = s.getShares(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	out := make([]Share, 0, len(raw))
	for _, r := range raw {
		var sh struct {
			ShareID            string
			VaultID            string
			TargetType         int
			TargetID           string
			Owner              bool
			Shared             bool
			TargetMembers      int
			AddressID          string
			ShareRoleID        string
			Content            string
			ContentKeyRotation int
		}
		if err := json.Unmarshal(r, &sh); err != nil {
			skip.Record(ctx, skip.KindVault, "", skip.Malformed, err)
			continue
		}
		out = append(out, Share{
			ShareID: sh.ShareID, VaultID: sh.VaultID,
			TargetType: sh.TargetType, TargetID: sh.TargetID,
			Owner: sh.Owner, Shared: sh.Shared, Members: sh.TargetMembers,
			AddressID: sh.AddressID, Access: roleWord(sh.ShareRoleID),
			Content: sh.Content, ContentKeyRotation: sh.ContentKeyRotation,
		})
	}
	return out, nil
}

type shareKeys struct{ keys map[int][]byte }

func (sk *shareKeys) latest() ([]byte, int) {
	max := -1
	for r := range sk.keys {
		if r > max {
			max = r
		}
	}
	return sk.keys[max], max
}

func (s *Service) getShares(ctx context.Context) ([]json.RawMessage, error) {
	var r struct{ Shares []json.RawMessage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/share"}, &r); err != nil {
		return nil, err
	}
	return r.Shares, nil
}

func (s *Service) decryptShareKeys(ctx context.Context, shareID string) (*shareKeys, error) {
	return s.shareKeys.Do(shareID, func() (*shareKeys, error) {
		var r struct {
			ShareKeys struct {
				Keys []json.RawMessage
			}
		}
		u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
			return s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/share/" + shareID + "/key", Query: proton.Query("Page", "0")}, &r)
		})
		if err != nil {
			return nil, err
		}
		out := &shareKeys{keys: map[int][]byte{}}
		for _, raw := range r.ShareKeys.Keys {
			var k struct {
				Key         string
				KeyRotation int
			}
			if err := json.Unmarshal(raw, &k); err != nil {
				skip.Record(ctx, skip.KindKey, shareID, skip.Malformed, err)
				continue
			}
			kb, err := base64.StdEncoding.DecodeString(k.Key)
			if err != nil {
				skip.Record(ctx, skip.KindKey, shareID, skip.Malformed, err)
				continue
			}
			msg := pgp.NewPGPMessage(kb)
			dec, err := u.UserKR.Decrypt(msg, u.UserKR, pgp.GetUnixTime())
			if err != nil {
				skip.Record(ctx, skip.KindKey, shareID, skip.Undecryptable, err)
				continue
			}
			out.keys[k.KeyRotation] = dec.GetBinary()
		}
		if len(out.keys) == 0 {
			return nil, fmt.Errorf("failed to decrypt share keys for %s", shareID)
		}
		return out, nil
	})
}
