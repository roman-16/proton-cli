package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/skip"
)

// The keys of an account, handed to somebody it trusts.
//
// Emergency access and data-recovery contacts rest on one move: the account's
// user keys, re-locked under a fresh random token, and that token sealed to the
// contact and signed by this account. The contact can open the token, and with
// it the keys - which is the whole of what "let them in if I can't" and "help me
// back in" come down to. Nothing here rotates or removes a key; it exports a
// copy locked under a secret only the named contact can reach.

// delegatedTokenContext is the notation the token is signed under, so a
// signature over one cannot be read as a signature over anything else. Mirrors
// KEY_TOKEN_SIGNATURE_CONTEXT in WebClients
// (packages/account/delegatedAccess/crypto.ts).
const delegatedTokenContext = "account.key-token.delegated"

// DelegatedUserKey is one of the account's user keys, re-locked under a token so
// a contact who opens the token opens the key.
type DelegatedUserKey struct {
	UserKeyID  string `json:"UserKeyID"`
	PrivateKey string `json:"PrivateKey"`
}

// DelegatedToken is a fresh 32-byte secret, hex-encoded: the passphrase the
// account's user keys are re-locked under for one contact.
func DelegatedToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate a delegated-access token: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SealDelegatedToken seals the token to a contact's published key and signs it
// with this account's address key, under the token's own context. The contact
// verifies that signature when they open it, which is what says the token came
// from the account it names rather than from whoever wrote the record.
func SealDelegatedToken(token string, to, signWith *pgp.KeyRing) (string, error) {
	msg, err := to.EncryptWithContext(
		pgp.NewPlainMessageFromString(token), signWith,
		pgp.NewSigningContext(delegatedTokenContext, true),
	)
	if err != nil {
		return "", fmt.Errorf("seal the delegated-access token: %w", err)
	}
	return msg.GetArmored()
}

// OpenDelegatedToken opens a token this account was sent, with its own keys, and
// requires the signature over it to check against the granting account's keys -
// which is what says the token came from the account it names rather than from
// whoever wrote the record. The plaintext is the passphrase that opens the
// granting account's user keys.
func OpenDelegatedToken(armored string, decrypt, verify *pgp.KeyRing) (string, error) {
	msg, err := pgp.NewPGPMessageFromArmored(armored)
	if err != nil {
		return "", err
	}
	plain, err := decrypt.DecryptWithContext(msg, verify, pgp.GetUnixTime(),
		pgp.NewVerificationContext(delegatedTokenContext, true, 0))
	if err != nil {
		return "", err
	}
	return plain.GetString(), nil
}

// UserKeysUnder re-locks every decrypted user key under the token, keeping each
// key's record ID, so a contact who opens the token opens the account's keys.
//
// Only the keys that opened are exported: a key a password reset left shut is
// not this account's to hand on, and comes back through reactivation before it
// can travel.
func (u *Unlocked) UserKeysUnder(ctx context.Context, token string) ([]DelegatedUserKey, error) {
	byFingerprint := map[string]string{}
	for _, rec := range u.UserKeys {
		k, err := pgp.NewKeyFromArmored(rec.PrivateKey)
		if err != nil {
			slog.DebugContext(ctx, "keys: a user key record could not be read to hand a contact",
				"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", rec.ID, "error", err.Error())
			continue
		}
		byFingerprint[k.GetFingerprint()] = rec.ID
	}
	var out []DelegatedUserKey
	for _, k := range u.UserKR.GetKeys() {
		id, ok := byFingerprint[k.GetFingerprint()]
		if !ok {
			continue
		}
		armored, err := LockAndArmor(k, []byte(token))
		if err != nil {
			return nil, fmt.Errorf("re-lock a user key under the delegated-access token: %w", err)
		}
		out = append(out, DelegatedUserKey{UserKeyID: id, PrivateKey: armored})
	}
	if len(out) == 0 {
		return nil, errs.Problemf("None of this account's user keys are open, so there is nothing to hand a contact.").
			Hint("proton account keys reactivate")
	}
	return out, nil
}
