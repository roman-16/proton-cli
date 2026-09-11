package keys

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/bip39"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Bringing back keys a password reset locked.
//
// A reset makes new keys and leaves the old ones shut: Proton keeps them, nothing
// opens them, and everything sealed to them - mail, contacts, calendars, vaults,
// the shares Drive kept - stays sealed. Reactivating is opening an old user key
// with a secret from before the reset, locking it under the password the account
// has now, and handing it back to Proton. The address keys whose tokens it opens
// come back with it, each named again in its address's key list as a key that
// reads and verifies and is sealed to nothing new - which is what Proton's own
// clients write for one.

// Secret is one of the three things Proton's clients accept for opening a
// locked user key: the password from before the reset, the recovery phrase, or
// a recovery file.
type Secret interface {
	// open tries every locked user key and hands back the ones that opened, by
	// record ID. A key that stays shut is written to the log by name and is not
	// an error: the secret may be from between two resets.
	open(ctx context.Context, c proton.Doer, u *Unlocked, locked []LockedKey) (map[string]*pgp.Key, error)
	// nothingOpened is the refusal when not one key opened, phrased for the
	// secret that was tried.
	nothingOpened() error
}

// PreviousPassword is the account password from before the reset - the second
// password, for an account that kept two - stretched with each key's own salt.
type PreviousPassword string

// RecoveryPhrase is the twelve words Proton's Recovery page hands out.
type RecoveryPhrase string

// RecoveryFile is a recovery file as downloaded from Proton's Recovery page: the
// account's user keys, encrypted under a secret Proton keeps beside the keys.
type RecoveryFile []byte

// ErrNoRecoveryPhrase reports that the account has no recovery phrase for a
// phrase to be checked against.
var ErrNoRecoveryPhrase = errors.New("the account has no recovery phrase")

// ErrNoAccountKeyLocked reports that what is locked is address keys alone. An
// address key comes back with the account key whose token opens it, so there is
// nothing for a secret to open here.
var ErrNoAccountKeyLocked = errors.New("no account key is locked")

// Phrase judges a recovery phrase before anything is asked of Proton: the wrong
// number of words, a word not on the list and a checksum that does not add up
// are all the command line's to answer for.
func Phrase(phrase string) (RecoveryPhrase, error) {
	if _, err := bip39.Entropy(phrase); err != nil {
		switch {
		case errors.Is(err, bip39.ErrLength):
			return "", errs.Problemf("A recovery phrase is twelve words.")
		case errors.Is(err, bip39.ErrWord):
			return "", errs.Problemf("One of the words is not one a recovery phrase can hold.").
				Hint("check the spelling against the phrase you wrote down")
		}
		return "", errs.Problemf("That is not a recovery phrase: one of its words is wrong.").
			Hint("check every word against the phrase you wrote down")
	}
	return RecoveryPhrase(phrase), nil
}

// Outcome is what a reactivation did, key by key.
type Outcome struct {
	// Reactivated is every key brought back, each user key followed by the
	// address keys it opened.
	Reactivated []LockedKey
	// StillLocked is every user key the secret did not open, which stays as it
	// was: a key from between two resets is opened by the secret of its own time.
	StillLocked []LockedKey
	// Unsupported is every address key held in a form only a Proton client
	// brings back, with the address it belongs to.
	Unsupported []LockedKey
}

// Reactivate brings back the locked user keys the secret opens, and with each
// the address keys it holds the tokens for.
//
// One request per user key, as Proton's clients send it: the key locked under
// the account's current key password, the fingerprints of the address keys that
// come back with it, and each of their addresses' key lists signed again with
// them named.
func (u *Unlocked) Reactivate(ctx context.Context, c proton.Doer, secret Secret) (*Outcome, error) {
	lockedUser := u.LockedAccountKeys()
	if len(lockedUser) == 0 {
		return nil, ErrNoAccountKeyLocked
	}
	opened, err := secret.open(ctx, c, u, lockedUser)
	if err != nil {
		return nil, err
	}
	if len(opened) == 0 {
		return nil, secret.nothingOpened()
	}

	out := &Outcome{}
	for _, locked := range lockedUser {
		key, ok := opened[locked.Key.ID]
		if !ok {
			out.StillLocked = append(out.StillLocked, locked)
			continue
		}
		if sound, err := key.Check(); err != nil || !sound {
			// A key whose halves do not match was written broken and would be
			// broken again for every client of the account; Proton's clients leave
			// it shut too.
			slog.DebugContext(ctx, "keys: a locked key opened and is not sound",
				"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", locked.Key.ID, "error", detail(err))
			out.StillLocked = append(out.StillLocked, locked)
			continue
		}
		following, err := u.reactivateUserKey(ctx, c, locked, key, out)
		if err != nil {
			return out, err
		}
		out.Reactivated = append(out.Reactivated, locked)
		out.Reactivated = append(out.Reactivated, following...)
	}
	slog.DebugContext(ctx, "keys: reactivated",
		"keys_reactivated", len(out.Reactivated), "keys_locked", len(out.StillLocked),
		"keys_unsupported", len(out.Unsupported))
	return out, nil
}

// reactivateUserKey hands one opened user key back to Proton, with the address
// keys it opens, and reports which address keys came back with it.
func (u *Unlocked) reactivateUserKey(
	ctx context.Context, c proton.Doer, locked LockedKey, key *pgp.Key, out *Outcome,
) ([]LockedKey, error) {
	armored, err := LockAndArmor(key, u.keyPass)
	if err != nil {
		return nil, fmt.Errorf("lock the reactivated user key: %w", err)
	}
	ring, err := pgp.NewKeyRing(key)
	if err != nil {
		return nil, err
	}

	var (
		following    []LockedKey
		fingerprints = []string{}
		lists        = map[string]map[string]string{}
	)
	for _, addr := range u.Addresses {
		opening := u.addressKeysOpenedBy(ctx, addr, ring, out)
		if len(opening) == 0 {
			continue
		}
		list, err := u.listWithReactivated(ctx, addr, opening)
		if err != nil {
			// Recorded and not counted: the keys stay locked and are named on the
			// screen as such; this is what says which address and why.
			slog.DebugContext(ctx, "keys: an address's key list cannot name its reactivated keys",
				"kind", string(skip.KindAddress), "reason", string(skip.Unlockable), "ref", addr.ID, "error", err.Error())
			for _, k := range opening {
				out.Unsupported = append(out.Unsupported, k.locked)
			}
			continue
		}
		lists[addr.ID] = map[string]string{"Data": list.Data, "Signature": list.Signature}
		for _, k := range opening {
			fingerprints = append(fingerprints, k.key.GetFingerprint())
			following = append(following, k.locked)
		}
	}

	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/keys/user/" + locked.Key.ID,
		Body: map[string]any{
			"AddressKeyFingerprints": fingerprints,
			"PrivateKey":             armored,
			"SignedKeyLists":         lists,
		},
	}, nil); err != nil {
		return nil, err
	}
	return following, nil
}

// reactivated is one address key coming back: its record, and the key itself
// for the fingerprints the list names it by.
type reactivated struct {
	locked LockedKey
	record Key
	key    *pgp.Key
}

// addressKeysOpenedBy is every locked key of an address whose token the user key
// opens, which is the whole of what ties an address key to a user key.
//
// A locked key with no token is from before Proton moved address keys under the
// user key, and is locked with a password rather than a token; only a Proton
// client brings one back, so it is set aside and said so.
//
// Recorded and not counted: a key the user key does not open is not this user
// key's to bring back, and is left to whichever one is.
func (u *Unlocked) addressKeysOpenedBy(ctx context.Context, addr Address, userRing *pgp.KeyRing, out *Outcome) []reactivated {
	var opening []reactivated
	for _, k := range u.Locked() {
		if k.AddressID != addr.ID {
			continue
		}
		if k.Key.Token == "" || k.Key.Signature == "" {
			out.Unsupported = append(out.Unsupported, k)
			continue
		}
		token, err := decryptToken(k.Key.Token, k.Key.Signature, userRing)
		if err != nil {
			slog.DebugContext(ctx, "keys: a locked address key is not this user key's",
				"kind", string(skip.KindKey), "reason", string(skip.Untokenized), "ref", k.Key.ID, "error", err.Error())
			continue
		}
		key, err := pgp.NewKeyFromArmored(k.Key.PrivateKey)
		if err != nil {
			slog.DebugContext(ctx, "keys: a locked address key could not be read",
				"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", k.Key.ID, "error", err.Error())
			continue
		}
		if _, err := key.Unlock(token); err != nil {
			slog.DebugContext(ctx, "keys: a locked address key did not open with its token",
				"kind", string(skip.KindKey), "reason", string(skip.Unlockable), "ref", k.Key.ID, "error", err.Error())
			continue
		}
		opening = append(opening, reactivated{locked: k, record: k.Key, key: key})
	}
	return opening
}

// listWithReactivated is the address's published key list with the keys coming
// back named in it, signed by the keys the address writes with now.
//
// The list Proton serves has to describe the address before it is added to, and
// the address has to have a key that opened to sign with: an address whose every
// key is locked has nothing to vouch for the list, and is left to a Proton
// client.
func (u *Unlocked) listWithReactivated(_ context.Context, addr Address, keys []reactivated) (SignedKeyList, error) {
	if addr.SignedKeyList == nil || addr.SignedKeyList.Data == "" {
		return SignedKeyList{}, fmt.Errorf("the address has no published key list")
	}
	held, err := addressKeys(addr)
	if err != nil {
		return SignedKeyList{}, err
	}
	if err := describesAddress(addr.SignedKeyList.Data, held); err != nil {
		return SignedKeyList{}, err
	}
	rings, ok := u.AddrRings(addr.ID)
	if !ok || len(rings.Write.GetKeys()) == 0 {
		return SignedKeyList{}, fmt.Errorf("no key of the address opened to sign its key list")
	}
	data, err := withReactivated(addr.SignedKeyList.Data, keys)
	if err != nil {
		return SignedKeyList{}, err
	}
	signature, err := signKeyList(data, rings.Write.GetKeys())
	if err != nil {
		return SignedKeyList{}, err
	}
	return SignedKeyList{Data: data, Signature: signature}, nil
}

// ── the secrets ──

func (p PreviousPassword) open(ctx context.Context, c proton.Doer, _ *Unlocked, locked []LockedKey) (map[string]*pgp.Key, error) {
	salts, err := getKeySalts(ctx, c)
	if err != nil {
		return nil, fmt.Errorf("get key salts: %w", err)
	}
	opened := map[string]*pgp.Key{}
	for _, k := range locked {
		pass, err := stretch(string(p), saltOf([]Key{k.Key}, salts))
		if err != nil {
			return nil, fmt.Errorf("derive the previous key password: %w", err)
		}
		key, err := openLocked(ctx, k, []byte(pass))
		if key != nil {
			opened[k.Key.ID] = key
			continue
		}
		slog.DebugContext(ctx, "keys: a locked key did not open with the previous password",
			"kind", string(skip.KindKey), "reason", string(skip.Unlockable), "ref", k.Key.ID, "error", err.Error())
	}
	return opened, nil
}

func (PreviousPassword) nothingOpened() error {
	return errs.Problemf("That password did not open any of the locked keys.").
		Hint("in two-password mode it is the second password from before the reset")
}

// mnemonicKey is one user key as Proton keeps it for the recovery phrase:
// locked under the phrase's bytes stretched with a salt of its own.
type mnemonicKey struct {
	ID         string
	PrivateKey string
	Salt       string
}

func (p RecoveryPhrase) open(ctx context.Context, c proton.Doer, u *Unlocked, locked []LockedKey) (map[string]*pgp.Key, error) {
	if !u.recoveryPhrase {
		return nil, ErrNoRecoveryPhrase
	}
	entropy, err := bip39.Entropy(string(p))
	if err != nil {
		return nil, err
	}
	var r struct{ MnemonicUserKeys []mnemonicKey }
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/settings/mnemonic"}, &r); err != nil {
		return nil, fmt.Errorf("get the keys the recovery phrase holds: %w", err)
	}
	secret := base64.StdEncoding.EncodeToString(entropy)
	opened := map[string]*pgp.Key{}
	for _, k := range locked {
		for _, held := range r.MnemonicUserKeys {
			if held.ID != k.Key.ID {
				continue
			}
			pass, err := stretch(secret, held.Salt)
			if err != nil {
				return nil, fmt.Errorf("derive the recovery phrase's key password: %w", err)
			}
			key, err := openLocked(ctx, LockedKey{Key: Key{ID: held.ID, PrivateKey: held.PrivateKey}}, []byte(pass))
			if key != nil {
				opened[k.Key.ID] = key
				break
			}
			slog.DebugContext(ctx, "keys: a locked key did not open with the recovery phrase",
				"kind", string(skip.KindKey), "reason", string(skip.Unlockable), "ref", k.Key.ID, "error", err.Error())
		}
	}
	return opened, nil
}

func (RecoveryPhrase) nothingOpened() error {
	return errs.Problemf("That recovery phrase did not open any of the locked keys.").
		Hint("a phrase set after the reset holds only the new keys; the one from before opens the old")
}

func (f RecoveryFile) open(ctx context.Context, _ proton.Doer, u *Unlocked, locked []LockedKey) (map[string]*pgp.Key, error) {
	msg, err := pgp.NewPGPMessageFromArmored(string(f))
	if err != nil {
		return nil, errs.Problemf("That is not a recovery file.").
			Hint("a recovery file is the proton_recovery.asc that Proton's Recovery page downloads")
	}
	var secrets int
	var keys []byte
	for _, k := range u.UserKeys {
		if k.RecoverySecret == "" {
			continue
		}
		secrets++
		plain, err := pgp.DecryptMessageWithPassword(msg, []byte(k.RecoverySecret))
		if err == nil {
			keys = plain.GetBinary()
			break
		}
		slog.DebugContext(ctx, "keys: a recovery secret did not open the recovery file",
			"kind", string(skip.KindKey), "reason", string(skip.Undecryptable), "ref", k.ID, "error", err.Error())
	}
	if secrets == 0 {
		return nil, errs.Problemf("This account has no recovery file to recover with.").
			Hint("a recovery file made before the reset opens the keys from before it")
	}
	if keys == nil {
		return nil, errs.Problemf("This recovery file was not made for this account.")
	}
	entities, err := openpgp.ReadKeyRing(bytes.NewReader(keys))
	if err != nil {
		return nil, fmt.Errorf("read the keys inside the recovery file: %w", err)
	}
	byFingerprint := map[string]*pgp.Key{}
	for _, entity := range entities {
		key, err := pgp.NewKeyFromEntity(entity)
		if err != nil {
			return nil, err
		}
		byFingerprint[key.GetFingerprint()] = key
	}
	opened := map[string]*pgp.Key{}
	for _, k := range locked {
		shut, err := pgp.NewKeyFromArmored(k.Key.PrivateKey)
		if err != nil {
			slog.DebugContext(ctx, "keys: a locked key could not be read",
				"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", k.Key.ID, "error", err.Error())
			continue
		}
		key, ok := byFingerprint[shut.GetFingerprint()]
		if !ok {
			slog.DebugContext(ctx, "keys: the recovery file holds no copy of a locked key",
				"kind", string(skip.KindKey), "reason", string(skip.NoKey), "ref", k.Key.ID)
			continue
		}
		opened[k.Key.ID] = key
	}
	return opened, nil
}

func (RecoveryFile) nothingOpened() error {
	return errs.Problemf("The recovery file holds none of the locked keys.").
		Hint("a recovery file made before the reset opens the keys from before it")
}

// openLocked unlocks one locked key record with a passphrase. It answers with
// the key or with why not, never both.
func openLocked(_ context.Context, k LockedKey, passphrase []byte) (*pgp.Key, error) {
	shut, err := pgp.NewKeyFromArmored(k.Key.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errKeyMalformed, err)
	}
	key, err := shut.Unlock(passphrase)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errKeyLocked, err)
	}
	return key, nil
}

func detail(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
