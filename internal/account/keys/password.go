package keys

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Changing the secret the account's keys are locked with.
//
// Proton never holds a password. It holds a verifier to check one against, and
// the keys are locked with a passphrase stretched from the password and a salt.
// So a password change is those two things written together - a fresh verifier,
// and every open user key locked again - and it makes no new key: the material
// is untouched, which is what keeps everything ever sealed to it readable.
//
// Address keys are not in it. Since Proton moved them under the user key they
// are locked with a token the user key opens, and no password is part of that.
// An account from before that move, and an administrator whose organization key
// is locked with their password, are refused rather than half-changed - see
// canRelock.

// keySaltBytes is the salt a key password is stretched with, as Proton's own
// clients generate it (generateKeySalt, @protontech/crypto/srp). It is longer
// than the ten bytes an SRP verifier's salt takes, which is a different salt
// for a different derivation.
const keySaltBytes = 16

const privateKeysPath = "/core/v4/keys/private"

// Relock locks the account's keys under a new secret and hands them to Proton.
//
// signsIn says the secret is the one that signs in as well, so the verifier
// goes with it. In two-password mode it is not: the password that signs in
// stays as it is, and only what opens the keys moves.
//
// The session is resealed with the new passphrase before this returns. Without
// that the next command would find the sealed key password stale and ask for a
// password nobody has changed yet.
func (u *Unlocked) Relock(ctx context.Context, c *proton.Client, secret string, signsIn bool) error {
	if err := u.canRelock(ctx, c); err != nil {
		return err
	}
	salt := make([]byte, keySaltBytes)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("key salt: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(salt)
	pass, err := stretch(secret, encoded)
	if err != nil {
		return fmt.Errorf("derive the new key password: %w", err)
	}
	relocked, err := u.relockedUserKeys([]byte(pass))
	if err != nil {
		return err
	}
	body := map[string]any{"KeySalt": encoded, "UserKeys": relocked}
	if signsIn {
		auth, err := proton.AuthFor(ctx, c, secret)
		if err != nil {
			return err
		}
		body["Auth"] = auth
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: privateKeysPath, Body: body,
	}, nil); err != nil {
		return err
	}
	slog.DebugContext(ctx, "keys: relocked the account's keys under a new secret",
		"user_keys", len(relocked), "keys_locked", len(u.LockedAccountKeys()))
	u.keyPass = []byte(pass)
	wrapAndPersist(ctx, c, pass)
	return nil
}

// relockedUserKeys is every user key that opened, written out under a new
// passphrase and matched to the record it belongs to.
//
// The match is by fingerprint because that is the only thing an open key and
// its record share - the ring holds keys, not IDs - and handing Proton a key
// under the wrong ID would lock the account out of both.
//
// A key that did not open is left alone, which is what Proton's own clients do:
// nothing here can re-lock what it cannot open, and a key a password reset shut
// stays shut until it is reactivated with the secret of its own time.
func (u *Unlocked) relockedUserKeys(passphrase []byte) ([]map[string]string, error) {
	ids := map[string]string{}
	for _, k := range u.UserKeys {
		record, err := pgp.NewKeyFromArmored(k.PrivateKey)
		if err != nil {
			// Recorded and not counted: the key this stands for is either one that
			// opened - in which case the mismatch below stops the whole change - or
			// one that never opens anyway.
			slog.Debug("keys: a user key record could not be read", "ref", k.ID, "error", err.Error())
			continue
		}
		ids[record.GetFingerprint()] = k.ID
	}
	open := u.UserKR.GetKeys()
	out := make([]map[string]string, 0, len(open))
	for _, key := range open {
		id, ok := ids[key.GetFingerprint()]
		if !ok {
			return nil, fmt.Errorf("an opened user key matches no key Proton holds")
		}
		armored, err := LockAndArmor(key, passphrase)
		if err != nil {
			return nil, fmt.Errorf("lock a user key under the new password: %w", err)
		}
		out = append(out, map[string]string{"ID": id, "PrivateKey": armored})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no user key is open to be locked again")
	}
	return out, nil
}

// canRelock refuses the two accounts whose keys this cannot move on its own.
//
// Both are accounts where something else is locked with the same password, and
// both would be left behind by a change that only moved the user keys: the
// account would still sign in and its mail would still open, and the thing left
// behind would be discovered much later by whoever needed it. Proton's own
// clients carry them; this refuses instead, because a wrong write here is not
// one anybody can put right - and says where the change can be made.
func (u *Unlocked) canRelock(ctx context.Context, c proton.Doer) error {
	if !u.migratedAddressKeys() {
		return errs.Problemf("This account's address keys are from before Proton moved them under the account key, " +
			"so changing the password here would leave them locked with the old one.").
			Hint("change it at https://account.proton.me/mail/account-password, " +
				"which brings the keys forward as it goes")
	}
	if u.role != adminRole {
		return nil
	}
	passwordless, err := passwordlessOrganizationKey(ctx, c)
	if err != nil {
		return err
	}
	if !passwordless {
		return errs.Problemf("This account administers an organization whose key is locked with its password, " +
			"and that key would be left behind by a change made here.").
			Hint("change it at https://account.proton.me/mail/account-password")
	}
	return nil
}

// migratedAddressKeys reports whether the address keys are held under the user
// key, which is what makes them independent of the password.
//
// One migrated key is the whole answer, as it is for Proton's own clients
// (getHasMigratedAddressKeys, packages/shared/lib/keys/keyMigration.ts): the
// migration moves an account as a whole.
func (u *Unlocked) migratedAddressKeys() bool {
	for _, a := range u.Addresses {
		for _, k := range a.Keys {
			if k.Token != "" && k.Signature != "" {
				return true
			}
		}
	}
	return false
}

// passwordlessOrganizationKey reports whether the organization's key is held
// under the administrator's own key rather than under their password.
//
// An organization that has no key at all counts as passwordless: there is
// nothing for a password change to leave behind. Mirrors getIsPasswordless
// (packages/shared/lib/keys/organizationKeys.ts).
func passwordlessOrganizationKey(ctx context.Context, c proton.Doer) (bool, error) {
	var r struct {
		OrganizationKey struct {
			PrivateKey   string
			Token        string
			Signature    string
			Passwordless bool
		}
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/organizations/keys",
	}, &r); err != nil {
		return false, fmt.Errorf("ask Proton about the organization's key: %w", err)
	}
	k := r.OrganizationKey
	return k.PrivateKey == "" || k.Passwordless || (k.Token != "" && k.Signature != ""), nil
}
