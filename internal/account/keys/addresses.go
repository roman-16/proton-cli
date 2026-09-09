package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/ecdh"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Giving an address its first key.
//
// An address without a key can neither send nor receive, so bringing one into
// being is the one moment a key has to be made rather than read. It is bounded
// by what makes it safe: the address holds nothing yet, so nothing is replaced;
// Proton serves no key list for it, so the list published names one key and can
// contradict nothing; and afterwards the address is read back, so the account
// knows Proton holds a key it can open and a list it composed.
//
// Keys an address already has are never touched, here or anywhere.

// NewAddressKey generates the first key of an address, publishes it, and hands
// back the ID Proton files it under.
//
// The key is locked with a passphrase minted here and sealed to the account's
// primary user key, exactly as every other address key of the account is - so
// nothing but the account's own password opens it, and the next run unlocks it
// with what it already has.
func (u *Unlocked) NewAddressKey(ctx context.Context, c proton.Doer, addr Address) (string, error) {
	userKR, err := u.PrimaryUserKey()
	if err != nil {
		return "", fmt.Errorf("take the primary user key: %w", err)
	}
	token, err := newAddressKeyToken(userKR)
	if err != nil {
		return "", err
	}
	key, err := u.generateAddressKey(ctx, addr.Email)
	if err != nil {
		return "", err
	}
	armored, err := LockAndArmor(key, []byte(token.passphrase))
	if err != nil {
		return "", fmt.Errorf("lock the new address key: %w", err)
	}
	data, err := composeKeyList(key)
	if err != nil {
		return "", err
	}
	signature, err := signKeyList(data, []*pgp.Key{key})
	if err != nil {
		return "", err
	}

	id, err := publishKey(ctx, c, addressKeyRequest{
		addressID: addr.ID, armoredKey: armored, primary: 1,
		token: token.sealed, signature: token.signature,
		keyList: SignedKeyList{Data: data, Signature: signature},
	})
	if err != nil {
		return "", err
	}
	if err := u.verifyPublishedKey(ctx, c, addr.ID, key, data); err != nil {
		return "", err
	}
	return id, nil
}

// generateAddressKey makes the key an address is given: the key this account's
// other addresses already hold, generated fresh.
//
// Generation settles everything about its shape except one thing OpenPGP leaves
// to whoever generates the key - how its encryption subkey turns a shared secret
// into a key-encryption key. Two implementations of the same standard pick
// differently, so a key generated here would carry a choice no other client of
// this account has ever written, and a key that is unlike the account's own is
// refused where one is read as Proton expects to have written it. So that choice
// is not made here at all: it is copied from a key Proton made.
func (u *Unlocked) generateAddressKey(ctx context.Context, email string) (*pgp.Key, error) {
	config := u.Generation()
	entity, err := openpgp.NewEntity(email, "", email, config)
	if err != nil {
		return nil, fmt.Errorf("generate a key for the address: %w", err)
	}
	kdf, ok := u.keyDerivation(ctx)
	if !ok {
		// Recorded and not counted: the address is made and works either way, and
		// this is what says why its key may not read like the account's others.
		slog.DebugContext(ctx, "keys: no key of the account's own to take the key derivation from",
			"addresses", len(u.Addresses))
		return pgp.NewKeyFromEntity(entity)
	}

	for i, sub := range entity.Subkeys {
		if sub.PublicKey.PubKeyAlgo != packet.PubKeyAlgoECDH {
			continue
		}
		if err := sub.PublicKey.ReplaceKDF(kdf); err != nil {
			return nil, fmt.Errorf("set the encryption subkey's key derivation: %w", err)
		}
		// The subkey's fingerprint moved with its key derivation, so what binds it
		// to the key has to be signed again.
		if err := sub.Sig.SignKey(sub.PublicKey, entity.PrivateKey, config); err != nil {
			return nil, fmt.Errorf("sign the encryption subkey: %w", err)
		}
		entity.Subkeys[i] = sub
	}
	return pgp.NewKeyFromEntity(entity)
}

// keyDerivation is how this account's addresses turn a shared secret into a
// key-encryption key, read off a key Proton made.
//
// A forwarding key is never the answer: what it carries there is the derivation
// of the account it was derived from, marked as forwarding and pointing at
// somebody else's fingerprint.
func (u *Unlocked) keyDerivation(ctx context.Context) (ecdh.KDF, bool) {
	for _, addr := range u.Addresses {
		for _, record := range addr.Keys {
			if record.Active == 0 {
				continue
			}
			key, err := pgp.NewKeyFromArmored(record.PrivateKey)
			if err != nil {
				// Recorded and not counted: unlocking logs this key by name and
				// reason already, and one unreadable key is not what decides
				// whether the account has an answer to this question.
				slog.DebugContext(ctx, "keys: a key could not be read for its key derivation",
					"kind", "key", "ref", record.ID, "error", err.Error())
				continue
			}
			for _, sub := range key.GetEntity().Subkeys {
				public, ok := sub.PublicKey.PublicKey.(*ecdh.PublicKey)
				if !ok || public.Version == ecdh.KDFVersionForwarding {
					continue
				}
				return ecdh.KDF{Hash: public.Hash, Cipher: public.Cipher}, true
			}
		}
	}
	return ecdh.KDF{}, false
}

// addressKeyToken is the passphrase an address's keys are locked with, in the
// three forms a request needs: the secret itself, sealed to the user key, and
// signed as the user key so reading it back proves where it came from.
type addressKeyToken struct {
	passphrase string
	sealed     string
	signature  string
}

// newAddressKeyToken mints a passphrase for an address's keys.
//
// It is thirty-two bytes of randomness in hex, sealed to the primary user key
// and signed by it - the shape every Proton client writes and the shape unlocking
// reads back. Both go over one message, so the signature and the sealed copy
// carry the same moment.
func newAddressKeyToken(userKR *pgp.KeyRing) (*addressKeyToken, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	message := pgp.NewPlainMessageFromString(hex.EncodeToString(raw))

	encrypted, err := userKR.Encrypt(message, nil)
	if err != nil {
		return nil, fmt.Errorf("seal the new address key's token: %w", err)
	}
	sealed, err := encrypted.GetArmored()
	if err != nil {
		return nil, err
	}
	signed, err := userKR.SignDetached(message)
	if err != nil {
		return nil, fmt.Errorf("sign the new address key's token: %w", err)
	}
	signature, err := signed.GetArmored()
	if err != nil {
		return nil, err
	}
	return &addressKeyToken{
		passphrase: message.GetString(), sealed: sealed, signature: signature,
	}, nil
}

// verifyPublishedKey reads the address back and checks that what Proton holds
// is what was published.
//
// It is the difference between having sent a key and having given the address
// one: the record has to open with the token that was sealed, and the list
// Proton serves has to be the text that was signed. A mismatch is this build's
// fault rather than the account's, so it says so and asks for a report - and it
// says it while the address is still empty, which is the only moment at which
// finding out is worth anything.
func (u *Unlocked) verifyPublishedKey(
	ctx context.Context, c proton.Doer, addressID string, key *pgp.Key, data string,
) error {
	addrs, err := getAddresses(ctx, c)
	if err != nil {
		return fmt.Errorf("read the address back: %w", err)
	}
	for _, addr := range addrs {
		if addr.ID != addressID {
			continue
		}
		if addr.SignedKeyList == nil || addr.SignedKeyList.Data != data {
			return fmt.Errorf("proton published a key list for the new address and Proton serves a different one")
		}
		held, err := addressKeys(addr)
		if err != nil {
			return err
		}
		for _, k := range held {
			if k.key.GetFingerprint() != key.GetFingerprint() {
				continue
			}
			if _, err := unlockKeyRing(ctx, []Key{k.record}, nil, u.UserKR); err != nil {
				return fmt.Errorf("the key published for the new address does not open: %w", err)
			}
			return nil
		}
		return fmt.Errorf("the key published for the new address is not among the %d Proton holds for it", len(held))
	}
	return fmt.Errorf("the new address is not among the %d the account holds", len(addrs))
}

// addressKeyRequest is what publishing a key to an address carries. Both ways of
// getting one - a key made here for a new address, a key another account derived
// for an existing one - send exactly this, which is why they send it from one
// place.
type addressKeyRequest struct {
	addressID  string
	armoredKey string
	primary    int
	// token and signature are the address's passphrase as Proton holds it: minted
	// for an address that had no keys, and handed back untouched for one that
	// has.
	token     string
	signature string
	keyList   SignedKeyList
	// forwardingID files the key against the forwarding it decrypts for, and is
	// empty for a key of the address's own.
	forwardingID string
}

func publishKey(ctx context.Context, c proton.Doer, req addressKeyRequest) (string, error) {
	body := map[string]any{
		"AddressID":  req.addressID,
		"Primary":    req.primary,
		"PrivateKey": req.armoredKey,
		"Signature":  req.signature,
		"SignedKeyList": map[string]string{
			"Data": req.keyList.Data, "Signature": req.keyList.Signature,
		},
		"Token": req.token,
	}
	if req.forwardingID != "" {
		body["AddressForwardingID"] = req.forwardingID
	}
	var r struct {
		Key struct{ ID string }
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/keys/address", Body: body,
	}, &r); err != nil {
		return "", err
	}
	return r.Key.ID, nil
}

// PostQuantum reports whether the account makes post-quantum keys.
//
// It decides what a new address may be given: an account that opted in expects
// every address to hold a post-quantum key beside the ordinary one, and this
// build reads those keys without being able to generate them. So the answer is
// asked for before an address is made rather than found out afterwards.
func PostQuantum(ctx context.Context, c proton.Doer) (bool, error) {
	var r struct {
		UserSettings struct {
			Flags struct{ SupportPgpV6Keys int }
		}
	}
	if err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/settings"}, &r); err != nil {
		return false, err
	}
	return r.UserSettings.Flags.SupportPgpV6Keys != 0, nil
}

// UnsupportedPostQuantum is the refusal for making a key on an account that
// holds post-quantum ones, phrased once because both a new address and a repair
// of one reach it.
func UnsupportedPostQuantum() error {
	return errs.Unsupportedf("This account creates post-quantum keys, which this build cannot generate.").
		Hint("add the address in a Proton client")
}
