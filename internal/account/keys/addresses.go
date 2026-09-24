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
	"github.com/roman-16/proton-cli/internal/skip"
)

// Adding a key to an address.
//
// A key reaches an address in one of two ways: made here, or brought in from a
// file. Either way it is locked with a passphrase sealed to the account's primary
// user key, published beside the address's other keys, and named in the
// address's key list - first, when the address is to write with it, and last
// otherwise. An address with no key in use has no list to contradict, so its
// list is composed naming the one key; every other list is edited. Afterwards
// the address is read back, so the account knows Proton holds a key it can open
// and the list that was signed.

// NewAddressKey makes a key for an address and publishes it as the key the
// address writes with, handing back the ID Proton files it under.
func (u *Unlocked) NewAddressKey(ctx context.Context, c proton.Doer, addr Address) (string, error) {
	key, err := u.GenerateAddressKey(ctx, addr.Email)
	if err != nil {
		return "", err
	}
	return u.PublishAddressKey(ctx, c, addr, key)
}

// PublishAddressKey publishes a key made for an address as the key the address
// writes with.
//
// It is locked with the passphrase the address's primary key already has, as
// Proton's clients lock a key they make, so every key of the address opens with
// the one token; an address with no key yet is given a passphrase minted here.
func (u *Unlocked) PublishAddressKey(ctx context.Context, c proton.Doer, addr Address, key *pgp.Key) (string, error) {
	token, err := u.addressToken(ctx, addr)
	if err != nil {
		return "", err
	}
	return u.addKey(ctx, c, addr, key, token, true)
}

// addKey locks a key, names it in the address's key list, publishes both, and
// reads the address back.
func (u *Unlocked) addKey(
	ctx context.Context, c proton.Doer, addr Address, key *pgp.Key, token *addressKeyToken, primary bool,
) (string, error) {
	armored, err := LockAndArmor(key, []byte(token.passphrase))
	if err != nil {
		return "", fmt.Errorf("lock the address key: %w", err)
	}
	list, primary, err := u.listWithKey(addr, key, primary)
	if err != nil {
		return "", err
	}
	id, err := publishKey(ctx, c, addressKeyRequest{
		addressID: addr.ID, armoredKey: armored, primary: boolBit(primary),
		token: token.sealed, signature: token.signature,
		keyList: list,
	})
	if err != nil {
		return "", err
	}
	published, err := u.verifyPublishedKey(ctx, c, addr.ID, key, list.Data)
	if err != nil {
		return "", err
	}
	u.adopt(published, key, primary)
	return id, nil
}

// listWithKey is the key list an address publishes once a key joins it, and
// whether the key joins as the one it writes with.
//
// An address with no key in use is given a list naming the new key alone, as
// its primary, since there is nothing else for it to write with. Any other
// address's list has to describe the keys the account holds before it is added
// to, and is signed by the keys that are primary afterwards.
func (u *Unlocked) listWithKey(addr Address, key *pgp.Key, primary bool) (SignedKeyList, bool, error) {
	held, err := addressKeys(addr)
	if err != nil {
		return SignedKeyList{}, false, err
	}
	flags := defaultKeyFlags(addr)
	if listable(held) == 0 {
		data, err := composeKeyList(key, flags)
		if err != nil {
			return SignedKeyList{}, false, err
		}
		signature, err := signKeyList(data, []*pgp.Key{key}, u.clock())
		if err != nil {
			return SignedKeyList{}, false, err
		}
		return SignedKeyList{Data: data, Signature: signature}, true, nil
	}
	signers, err := u.writers(addr)
	if err != nil {
		return SignedKeyList{}, false, err
	}
	if primary {
		signers = replacingPrimary(signers, key)
	}
	list, err := u.relisted(addr, noKeyList(addr, "adding a key"),
		func(data string) (string, error) { return withAdded(data, key, flags, primary) }, signers)
	return list, primary, err
}

// GenerateAddressKey makes the key an address is given: the key this account's
// other addresses already hold, generated fresh.
//
// Generation settles everything about its shape except one thing OpenPGP leaves
// to whoever generates the key - how its encryption subkey turns a shared secret
// into a key-encryption key. Two implementations of the same standard pick
// differently, so a key generated here would carry a choice no other client of
// this account has ever written, and a key that is unlike the account's own is
// refused where one is read as Proton expects to have written it. So that choice
// is not made here at all: it is copied from a key Proton made.
func (u *Unlocked) GenerateAddressKey(ctx context.Context, email string) (*pgp.Key, error) {
	config := u.Generation()
	entity, err := openpgp.NewEntity(email, "", email, config)
	if err != nil {
		return nil, fmt.Errorf("generate a key for the address: %w", err)
	}
	kdf, ok := u.keyDerivation(ctx)
	if !ok {
		// Recorded and not counted: the key is made and works either way, and
		// this is what says why it may not read like the account's others.
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

// addressToken is the passphrase a key made for the address is locked with: the
// one its primary key is locked with, handed back exactly as Proton holds it.
//
// An address with no key, or whose primary's token this account cannot open,
// is given a fresh one - which is what Proton's clients fall back to as well
// (getNewAddressKeyToken, packages/shared/lib/keys/addressKeys.ts).
func (u *Unlocked) addressToken(ctx context.Context, addr Address) (*addressKeyToken, error) {
	if len(addr.Keys) == 0 {
		return u.newAddressKeyToken()
	}
	primary, err := primaryRecord(addr)
	if err == nil {
		var passphrase []byte
		if passphrase, err = decryptToken(primary.Token, primary.Signature, u.UserKR); err == nil {
			return &addressKeyToken{
				passphrase: string(passphrase), sealed: primary.Token, signature: primary.Signature,
			}, nil
		}
	}
	// Recorded and not counted: a fresh token opens the new key just as well, and
	// this line is what says why it is not the one the address's others share.
	slog.DebugContext(ctx, "keys: the primary key's token cannot be reused; minting one",
		"kind", string(skip.KindAddress), "ref", addr.ID, "error", err.Error())
	return u.newAddressKeyToken()
}

func (u *Unlocked) newAddressKeyToken() (*addressKeyToken, error) {
	userKR, err := u.PrimaryUserKey()
	if err != nil {
		return nil, fmt.Errorf("take the primary user key: %w", err)
	}
	return newAddressKeyToken(userKR)
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
		return nil, fmt.Errorf("seal the address key's token: %w", err)
	}
	sealed, err := encrypted.GetArmored()
	if err != nil {
		return nil, err
	}
	signed, err := userKR.SignDetached(message)
	if err != nil {
		return nil, fmt.Errorf("sign the address key's token: %w", err)
	}
	signature, err := signed.GetArmored()
	if err != nil {
		return nil, err
	}
	return &addressKeyToken{
		passphrase: message.GetString(), sealed: sealed, signature: signature,
	}, nil
}

// verifyPublishedKey reads the address back, checks that what Proton holds is
// what was published, and hands back the address as Proton now holds it.
//
// It is the difference between having sent a key and having given the address
// one: the record has to open with the token that was sealed, and the list
// Proton serves has to be the text that was signed. A mismatch is this build's
// fault rather than the account's, so it says so and asks for a report.
func (u *Unlocked) verifyPublishedKey(
	ctx context.Context, c proton.Doer, addressID string, key *pgp.Key, data string,
) (Address, error) {
	addrs, err := getAddresses(ctx, c)
	if err != nil {
		return Address{}, fmt.Errorf("read the address back: %w", err)
	}
	for _, addr := range addrs {
		if addr.ID != addressID {
			continue
		}
		if addr.SignedKeyList == nil || addr.SignedKeyList.Data != data {
			return Address{}, fmt.Errorf("proton published a key list for the address and Proton serves a different one")
		}
		held, err := addressKeys(addr)
		if err != nil {
			return Address{}, err
		}
		for _, k := range held {
			if k.key.GetFingerprint() != key.GetFingerprint() {
				continue
			}
			if _, err := unlockKeyRing(ctx, []Key{k.record}, nil, u.UserKR); err != nil {
				return Address{}, fmt.Errorf("the key published for the address does not open: %w", err)
			}
			return addr, nil
		}
		return Address{}, fmt.Errorf("the key published for the address is not among the %d Proton holds for it", len(held))
	}
	return Address{}, fmt.Errorf("the address is not among the %d the account holds", len(addrs))
}

// addressKeyRequest is what publishing a key to an address carries. Every way of
// getting one - a key made here, a key brought in from a file, a key another
// account derived for a forwarding - sends exactly this, which is why they send
// it from one place.
type addressKeyRequest struct {
	addressID  string
	armoredKey string
	primary    int
	// token and signature are the address's passphrase as Proton holds it: minted
	// here, or handed back untouched.
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
// It decides whether this build may make a key for the account at all: an
// account that opted in expects every address to hold a post-quantum key beside
// the ordinary one, and this build reads those keys without being able to
// generate them. So the answer is asked for before a key is made rather than
// found out afterwards.
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
// holds post-quantum ones, phrased once for everything that reaches it. instead
// is what to do in a Proton client, which is the command's to say.
func UnsupportedPostQuantum(instead string) error {
	return errs.Unsupportedf("This account creates post-quantum keys, which this build cannot generate.").
		Hint(instead)
}
