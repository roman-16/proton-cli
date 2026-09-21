package pass

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// An access token lets a program into Pass without the account's password:
// Proton's own pass-cli, or an AI agent working through it. This is Pass's
// "Access tokens" settings page.
//
// A token has a key of its own, made here and never seen by Proton in the clear:
// Proton holds it sealed to the account's user key, and the program holds it
// beside the token. A vault is opened to the token by sealing every rotation of
// the vault's share key under that key, so what the program can read is exactly
// the vaults that were handed to it, read-only, and nothing about the account
// beyond them.

// The words a token's state is reported in. Expiring is within the hour.
const (
	TokenActive   = "active"
	TokenExpiring = "expiring"
	TokenExpired  = "expired"
)

// tokenProduct is which of Proton's products a token here is for.
const tokenProduct = "pass"

// The shortest and longest a token may live, which Proton enforces.
const (
	TokenMinLife = time.Hour
	TokenMaxLife = 365 * 24 * time.Hour
)

// tokenBase is where Proton keeps tokens, which is the account's rather than
// Pass's: the same token could be for another product. It answers only at the
// door Proton serves its account app from, so every request to it says so.
const tokenBase = "/account/v4/personal-access-token"

// AccessToken is one token, as the account sees it.
type AccessToken struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"`
	Agent   bool   `json:"agent"`
	Created int64  `json:"created"`
	Expires int64  `json:"expires"`
	// Secret is the token as a program is handed it, known at the moment it is
	// made and never again.
	Secret string `json:"secret,omitempty"`
	// key is the token's own key, sealed to the account's user key.
	key string
}

// tokenStatus is the one word for where a token is in its life.
func tokenStatus(expires, now int64) string {
	switch {
	case expires < now:
		return TokenExpired
	case expires-now <= int64(time.Hour/time.Second):
		return TokenExpiring
	}
	return TokenActive
}

// rawToken is a token as Proton writes it.
type rawToken struct {
	PersonalAccessTokenID  string
	Name                   string
	PersonalAccessTokenKey string
	ExpireTime             int64
	CreateTime             int64
	Token                  string
	Flags                  *struct{ PassAgent bool }
}

func (r rawToken) token(now int64) AccessToken {
	return AccessToken{
		ID: r.PersonalAccessTokenID, Name: r.Name, Status: tokenStatus(r.ExpireTime, now),
		Agent: r.Flags != nil && r.Flags.PassAgent, Created: r.CreateTime, Expires: r.ExpireTime,
		key: r.PersonalAccessTokenKey,
	}
}

// AccessTokens lists every token, the ones that have expired included: Proton
// keeps those for thirty days, and one that stopped working is worth seeing
// beside the ones that have not.
func (s *Service) AccessTokens(ctx context.Context) ([]AccessToken, error) {
	now := pgp.GetUnixTime()
	var out []AccessToken
	since := ""
	for {
		var r struct {
			PersonalAccessTokens struct {
				PersonalAccessTokens []rawToken
				LastToken            *string
			}
		}
		q := proton.Query("IncludeExpired", "1", "Product", tokenProduct)
		if since != "" {
			q.Set("Since", since)
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: tokenBase, Query: q, AccountHost: true,
		}, &r); err != nil {
			return nil, err
		}
		for _, t := range r.PersonalAccessTokens.PersonalAccessTokens {
			out = append(out, t.token(now))
		}
		page := r.PersonalAccessTokens
		if len(page.PersonalAccessTokens) == 0 || page.LastToken == nil || *page.LastToken == "" {
			break
		}
		since = *page.LastToken
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

// NewAccessToken is what a token is made of.
type NewAccessToken struct {
	Name string
	// Life is how long the token works for, within TokenMinLife and TokenMaxLife.
	Life time.Duration
	// Agent marks a token for an AI agent, which then has to give a reason for
	// every action it takes and has each one recorded.
	Agent bool
	// ShareIDs are the vaults the token may read.
	ShareIDs []string
}

// AccessTokenCreate makes a token and hands the vaults named to it.
//
// The plan is asked first: Pass keeps tokens from the free plan, and a paid one
// is the whole of what is missing. The token's key is made on this machine and
// sealed to the account's primary user key for Proton to hold, signed with the
// same key so that a key nobody here made is refused when it comes back. What
// the program is handed is the token Proton minted and the key in the clear,
// joined the way pass-cli reads them.
func (s *Service) AccessTokenCreate(ctx context.Context, spec NewAccessToken) (*AccessToken, error) {
	limits, err := s.Limits(ctx)
	if err != nil {
		return nil, err
	}
	if limits.Free {
		return nil, errs.Problemf("Access tokens need a paid Pass plan.")
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	own, err := u.PrimaryUserKey()
	if err != nil {
		return nil, err
	}
	raw, err := aead.NewKey()
	if err != nil {
		return nil, err
	}
	sealed, err := own.Encrypt(pgp.NewPlainMessage(raw), own)
	if err != nil {
		return nil, fmt.Errorf("seal the token's key: %w", err)
	}
	now := pgp.GetUnixTime()
	body := map[string]any{
		"Name":                   spec.Name,
		"Products":               []string{tokenProduct},
		"PersonalAccessTokenKey": base64.StdEncoding.EncodeToString(sealed.GetBinary()),
		"ExpireTime":             now + int64(spec.Life/time.Second),
		"Flags":                  nil,
	}
	if spec.Agent {
		body["Flags"] = map[string]any{"PassAgent": true}
	}
	var r struct{ PersonalAccessToken rawToken }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: tokenBase, Body: body, AccountHost: true,
	}, &r); err != nil {
		return nil, err
	}
	if r.PersonalAccessToken.Token == "" {
		return nil, fmt.Errorf("proton made the token and did not hand it back")
	}
	token := r.PersonalAccessToken.token(now)
	token.Secret = tokenSecret(r.PersonalAccessToken.Token, raw)
	for _, shareID := range spec.ShareIDs {
		if err := s.accessTokenGrant(ctx, token.ID, shareID, raw); err != nil {
			return nil, errs.Naming(spec.Name, fmt.Errorf("the token was made, and could not be given a vault: %w", err))
		}
	}
	return &token, nil
}

// tokenSecret is the string a program is handed: the token Proton minted, then
// the key that opens what it was given, which Proton never sees in the clear.
func tokenSecret(token string, raw []byte) string {
	return token + "::" + base64.RawURLEncoding.EncodeToString(raw)
}

// accessTokenGrant opens one vault to a token, read-only.
//
// Every rotation of the vault's share key travels, sealed under the token's key,
// because an item made before the last rotation is still sealed under an older
// one - a program given only the newest key would see a vault half of which will
// not open.
func (s *Service) accessTokenGrant(ctx context.Context, tokenID, shareID string, raw []byte) error {
	sk, err := s.decryptShareKeys(ctx, shareID)
	if err != nil {
		return err
	}
	rotations := make([]int, 0, len(sk.keys))
	for r := range sk.keys {
		rotations = append(rotations, r)
	}
	sort.Ints(rotations)
	keys := make([]map[string]any, 0, len(rotations))
	for _, rotation := range rotations {
		sealed, err := aead.Encrypt(raw, sk.keys[rotation], []byte(aead.TagShareKey))
		if err != nil {
			return err
		}
		keys = append(keys, map[string]any{
			"KeyRotation": rotation,
			"Key":         base64.StdEncoding.EncodeToString(sealed),
		})
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/personal-access-token/" + tokenID + "/access",
		Body: map[string]any{
			"ShareID": shareID, "TargetType": targetVault, "ShareRoleID": roleRead, "Keys": keys,
		},
	}, nil)
}

// TokenGrant is one vault a token may read.
type TokenGrant struct {
	// ID is the grant's own, which is what taking it back names.
	ID string `json:"id"`
	// ShareID is the account's own share of the vault, and Vault its name.
	ShareID string `json:"share_id"`
	Vault   string `json:"vault,omitempty"`
	Created int64  `json:"created"`
}

// AccessTokenGrants lists the vaults a token may read, by name where the vault
// is one of the account's.
func (s *Service) AccessTokenGrants(ctx context.Context, tokenID string) ([]TokenGrant, error) {
	var out []TokenGrant
	since := ""
	for {
		var r struct {
			Shares []struct {
				ShareID       string
				ParentShareID string
				CreateTime    int64
			}
			LastToken *string
		}
		req := proton.Request{Method: "GET", Path: "/pass/v1/personal-access-token/" + tokenID + "/access"}
		if since != "" {
			req.Query = proton.Query("Since", since)
		}
		if err := s.C.Decode(ctx, req, &r); err != nil {
			return nil, err
		}
		for _, sh := range r.Shares {
			out = append(out, TokenGrant{ID: sh.ShareID, ShareID: sh.ParentShareID, Created: sh.CreateTime})
		}
		if len(r.Shares) == 0 || r.LastToken == nil || *r.LastToken == "" {
			break
		}
		since = *r.LastToken
	}
	vaults, err := s.VaultsList(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(vaults))
	for _, v := range vaults {
		names[v.ShareID] = v.Name
	}
	for i := range out {
		out[i].Vault = names[out[i].ShareID]
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Vault < out[j].Vault })
	return out, nil
}

// AccessTokenSetVaults makes the vaults a token may read exactly the ones named:
// what is missing is handed to it, and what is no longer named is taken back.
// It reports how many of each.
func (s *Service) AccessTokenSetVaults(ctx context.Context, token AccessToken, shareIDs []string) (granted, revoked int, err error) {
	grants, err := s.AccessTokenGrants(ctx, token.ID)
	if err != nil {
		return 0, 0, err
	}
	wanted := make(map[string]bool, len(shareIDs))
	for _, id := range shareIDs {
		wanted[id] = true
	}
	held := make(map[string]bool, len(grants))
	for _, g := range grants {
		held[g.ShareID] = true
	}
	var raw []byte
	for _, id := range shareIDs {
		if held[id] {
			continue
		}
		if raw == nil {
			if raw, err = s.openTokenKey(ctx, token); err != nil {
				return granted, revoked, err
			}
		}
		if err := s.accessTokenGrant(ctx, token.ID, id, raw); err != nil {
			return granted, revoked, err
		}
		granted++
	}
	for _, g := range grants {
		if wanted[g.ShareID] {
			continue
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "DELETE", Path: "/pass/v1/personal-access-token/" + token.ID + "/access/" + g.ID,
		}, nil); err != nil {
			return granted, revoked, err
		}
		revoked++
	}
	return granted, revoked, nil
}

// openTokenKey brings a token's key back into the clear, checking that the
// account's own key signed it when it was made.
func (s *Service) openTokenKey(ctx context.Context, token AccessToken) ([]byte, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	sealed, err := base64.StdEncoding.DecodeString(token.key)
	if err != nil {
		return nil, errs.Naming(token.Name, fmt.Errorf("the token's key is not base64: %w", err))
	}
	msg := pgp.NewPGPMessage(sealed)
	opened, err := u.UserKR.Decrypt(msg, u.UserKR, pgp.GetUnixTime())
	if err != nil {
		return nil, errs.Naming(token.Name, u.Explain(fmt.Errorf("open the token's key: %w", err), "access token", msg))
	}
	raw := opened.GetBinary()
	if len(raw) != aead.KeyLen {
		return nil, errs.Naming(token.Name, fmt.Errorf("the token's key is %d bytes, not %d", len(raw), aead.KeyLen))
	}
	return raw, nil
}

// AccessTokenDelete stops a token working, for good.
func (s *Service) AccessTokenDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: tokenBase + "/" + id, AccountHost: true,
	}, nil)
}

// TokenAction is one thing a program did with a token, as the token recorded
// it: which kind of action, on what, and - for an agent - why.
type TokenAction struct {
	ID     string `json:"id"`
	Time   int64  `json:"time"`
	Action string `json:"action"`
	// VaultID and ItemID are what the action was on, as Proton filed it.
	VaultID string `json:"vault_id,omitempty"`
	ItemID  string `json:"item_id,omitempty"`
	// What the program wrote down about it, which only the token's key reads
	// back.
	Vault  string `json:"vault,omitempty"`
	Item   string `json:"item,omitempty"`
	Folder string `json:"folder,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Sealed says the program wrote something down and it would not open.
	Sealed bool `json:"sealed,omitempty"`
}

// actionWords name what a token did, from Proton's own event numbers. A number
// outside the set reads as its number rather than as nothing: an action nobody
// can name is still an action that happened.
var actionWords = map[int]string{
	1: "vault created", 2: "vault updated", 3: "vault deleted", 4: "vault transferred", 5: "vault restored",
	20: "item created", 21: "item updated", 22: "item trashed", 23: "item restored", 24: "item deleted",
	25: "item used", 26: "item pinned", 27: "item unpinned", 28: "item flags updated",
	29: "item history pruned", 30: "key rotated", 31: "item read",
	40: "invitation sent", 41: "invitation accepted", 42: "invitation declined", 43: "invitation withdrawn",
	60: "link created", 61: "link revoked",
	74: "file attached", 75: "file detached", 76: "file deleted", 77: "file renamed", 78: "file restored",
	100: "share created", 101: "share updated", 102: "share removed",
	121: "alias note changed", 122: "item restored",
	130: "folder created", 131: "folder moved", 132: "folder updated", 133: "item moved to folder", 134: "folder deleted",
	160: "vault access granted",
}

// actionWord is the word for one of Proton's event numbers.
func actionWord(n int) string {
	if w, ok := actionWords[n]; ok {
		return w
	}
	return "action " + strconv.Itoa(n)
}

// activityPage is how many records Proton hands over at once, and so how a
// short page is told from a full one.
const activityPage = 100

// AccessTokenActivity is everything a token has done, newest first, with what
// the program wrote down about each action brought back into the clear.
//
// A record whose note will not open is still listed, with the action and the
// time Proton filed it under and a mark saying the note is shut: what happened
// is Proton's record, and only the reason is the token's.
func (s *Service) AccessTokenActivity(ctx context.Context, token AccessToken) ([]TokenAction, error) {
	raw, err := s.openTokenKey(ctx, token)
	if err != nil {
		return nil, err
	}
	var out []TokenAction
	since := ""
	for {
		var r struct {
			Actions struct {
				Records []struct {
					PatMonitorRecordID string
					VaultID            string
					ObjectID           *string
					Action             int
					Payload            *string
					ActionTime         int64
				}
				NextSince *string
			}
		}
		q := proton.Query("PageSize", strconv.Itoa(activityPage))
		if since != "" {
			q.Set("Since", since)
		}
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/pass/v1/pat/monitor/" + token.ID, Query: q,
		}, &r); err != nil {
			return nil, err
		}
		for _, rec := range r.Actions.Records {
			a := TokenAction{
				ID: rec.PatMonitorRecordID, Time: rec.ActionTime,
				Action: actionWord(rec.Action), VaultID: rec.VaultID,
			}
			if rec.ObjectID != nil {
				a.ItemID = *rec.ObjectID
			}
			if rec.Payload != nil && *rec.Payload != "" {
				if err := openActionPayload(*rec.Payload, raw, &a); err != nil {
					// Recorded and not counted: the row is on the screen with its
					// action and time and says its note is shut, so nothing has gone
					// missing from the answer - which is what the tally is for. Why
					// is in the log.
					slog.DebugContext(ctx, "pass: a token's note on an action would not open",
						"ref", rec.PatMonitorRecordID, "error", err)
					a.Sealed = true
				}
			}
			out = append(out, a)
		}
		// Proton names a next page on the first answer whether or not there is
		// one, so a page shorter than a full one is what says the log is over.
		if len(r.Actions.Records) < activityPage || r.Actions.NextSince == nil || *r.Actions.NextSince == "" {
			break
		}
		since = *r.Actions.NextSince
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out, nil
}

// openActionPayload reads what the program wrote down about one action into it.
func openActionPayload(payload string, raw []byte, into *TokenAction) error {
	sealed, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return err
	}
	plain, err := aead.Decrypt(raw, sealed, []byte(aead.TagActionPayload))
	if err != nil {
		return err
	}
	var msg pb.ActionPayload
	if err := proto.Unmarshal(plain, &msg); err != nil {
		return err
	}
	if agent := msg.GetAgentAction(); agent != nil {
		into.Vault, into.Item = agent.GetVaultName(), agent.GetItemName()
		into.Folder, into.Reason = agent.GetFolderName(), agent.GetReason()
	}
	return nil
}
