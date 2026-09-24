package keys

import (
	"bytes"
	"context"
	"fmt"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Bringing keys in from a file.
//
// A key somebody used elsewhere, or a copy of one of this account's they once
// exported, arrives as a file: armoured or not, locked or not, one key or
// several. Everything that can be judged of it is judged before a request is
// sent - that it holds a private key, that the passphrase opens it, that it is
// whole and can encrypt - and then it meets the address. A key the address
// already uses is refused. One Proton holds for the address locked, as a
// password reset leaves a key, comes back as itself. Anything else joins the
// address as a key it reads with, or the one it writes with when it has no
// other.

// Offered is one private key a file brought, opened.
type Offered struct {
	Key *pgp.Key
	// Source is what the key was read from, as it was named on the command line.
	Source string
}

// PassphraseRefused is a locked key the passphrase given did not open. It is a
// refusal of its own because one passphrase serves a whole run, which is worth
// saying when the run read more than one file.
type PassphraseRefused struct{ *errs.Problem }

// ReadKeys is the private keys a file holds, opened. ask hands over the
// passphrase the first time a locked key needs one; a file whose keys are not
// locked asks for nothing.
func ReadKeys(data []byte, source string, ask func() (string, error)) ([]Offered, error) {
	entities, err := readEntities(data)
	if err != nil || len(entities) == 0 {
		return nil, errs.Problemf("%s holds no key this build can read.", source)
	}
	var out []Offered
	for _, entity := range entities {
		if entity.PrivateKey == nil {
			continue
		}
		if entity.PrimaryKey.Version == 6 {
			return nil, errs.Unsupportedf("The key in %s is a post-quantum key, which this build cannot import.", source).
				Hint("import it in a Proton client")
		}
		key, err := pgp.NewKeyFromEntity(entity)
		if err != nil {
			return nil, fmt.Errorf("read the key in the file: %w", err)
		}
		if locked, err := key.IsLocked(); err != nil || locked {
			passphrase, err := ask()
			if err != nil {
				return nil, err
			}
			if key, err = key.Unlock([]byte(passphrase)); err != nil {
				return nil, &PassphraseRefused{errs.Problemf(
					"The key in %s did not open with that passphrase. Nothing was imported.", source)}
			}
		}
		if sound, err := key.Check(); err != nil || !sound {
			return nil, errs.Problemf("The key in %s is damaged: its public and private halves do not match.", source)
		}
		if !key.CanEncrypt() {
			return nil, errs.Problemf("The key in %s cannot encrypt, which a key of an address has to.", source)
		}
		out = append(out, Offered{Key: key, Source: source})
	}
	if len(out) == 0 {
		return nil, errs.Problemf("%s holds no private key.", source).
			Hint("importing takes a private key; a public key is pinned to a contact instead")
	}
	return out, nil
}

// readEntities reads every key a file holds, whether it is armoured - one block
// or several, one after the other - or the bare packets.
func readEntities(data []byte) (openpgp.EntityList, error) {
	marker := []byte("-----BEGIN PGP ")
	at := bytes.Index(data, marker)
	if at < 0 {
		return openpgp.ReadKeyRing(bytes.NewReader(data))
	}
	var out openpgp.EntityList
	for rest := data[at:]; len(rest) > 0; {
		chunk := rest
		rest = nil
		if next := bytes.Index(chunk[len(marker):], marker); next >= 0 {
			chunk, rest = chunk[:len(marker)+next], chunk[len(marker)+next:]
		}
		block, err := armor.Decode(bytes.NewReader(chunk))
		if err != nil {
			return nil, err
		}
		if block.Type != openpgp.PrivateKeyType && block.Type != openpgp.PublicKeyType {
			continue
		}
		entities, err := openpgp.ReadKeyRing(block.Body)
		if err != nil {
			return nil, err
		}
		out = append(out, entities...)
	}
	return out, nil
}

// Arrival is one key an import brings to an address.
type Arrival struct {
	Key    *pgp.Key
	Source string
	// Returning is the record of the key Proton holds for the address locked,
	// which this is a copy of, and nil for a key the address has never held.
	Returning *Key
}

// Arrivals sorts what files brought to an address before anything is sent: a
// key the address already uses is refused, one it holds locked comes back, and
// the rest are new. A key offered twice arrives once.
func (u *Unlocked) Arrivals(addressID string, offered []Offered) ([]Arrival, error) {
	if err := u.Manages(); err != nil {
		return nil, err
	}
	addr, err := u.Address(addressID)
	if err != nil {
		return nil, err
	}
	records := map[string]Key{}
	for _, record := range addr.Keys {
		if key, err := pgp.NewKeyFromArmored(record.PrivateKey); err == nil {
			records[key.GetFingerprint()] = record
		}
	}
	var out []Arrival
	seen := map[string]bool{}
	for _, o := range offered {
		fingerprint := o.Key.GetFingerprint()
		if seen[fingerprint] {
			continue
		}
		seen[fingerprint] = true
		record, held := records[fingerprint]
		switch {
		case held && record.Active != 0:
			return nil, errs.Problemf("The key in %s is already on %s.", o.Source, addr.Email)
		case held:
			out = append(out, Arrival{Key: o.Key, Source: o.Source, Returning: &record})
		default:
			out = append(out, Arrival{Key: o.Key, Source: o.Source})
		}
	}
	return out, nil
}

// Import brings one key to its address and hands back the ID Proton files it
// under: the new key's, or the one the returning key always had.
//
// A new key is locked under a passphrase of its own, as Proton's clients lock an
// imported one (importKeysProcessV2, packages/shared/lib/keys/import).
func (u *Unlocked) Import(ctx context.Context, c proton.Doer, addressID string, a Arrival) (string, error) {
	if err := u.Manages(); err != nil {
		return "", err
	}
	addr, err := u.Address(addressID)
	if err != nil {
		return "", err
	}
	if a.Returning != nil {
		return u.bringBack(ctx, c, addr, *a.Returning, a.Key)
	}
	token, err := u.newAddressKeyToken()
	if err != nil {
		return "", err
	}
	return u.addKey(ctx, c, addr, a.Key, token, false)
}

// bringBack hands Proton a copy of a key it holds for the address locked, so the
// key opens again: locked under a fresh passphrase sealed to the account, and
// named in the address's list as a key that reads and is sealed to nothing new.
// Mirrors reactivateAddressKeysV2 in WebClients
// (packages/shared/lib/keys/reactivation/reactivateKeysProcessV2.ts).
func (u *Unlocked) bringBack(ctx context.Context, c proton.Doer, addr Address, record Key, key *pgp.Key) (string, error) {
	restored, err := u.withUserIDsOf(record, key, addr.Email)
	if err != nil {
		return "", err
	}
	token, err := u.newAddressKeyToken()
	if err != nil {
		return "", err
	}
	armored, err := LockAndArmor(restored, []byte(token.passphrase))
	if err != nil {
		return "", fmt.Errorf("lock the returning key: %w", err)
	}
	signers, err := u.writers(addr)
	if err != nil {
		return "", err
	}
	returning := []reactivated{{record: record, key: restored}}
	list, err := u.relisted(addr, noKeyList(addr, "bringing one of its keys back"),
		func(data string) (string, error) { return withReactivated(data, addr, returning) }, signers)
	if err != nil {
		return "", err
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/keys/address/" + record.ID,
		Body: map[string]any{
			"PrivateKey": armored, "SignedKeyList": list.body(),
			"Token": token.sealed, "Signature": token.signature,
		},
	}, nil); err != nil {
		return "", err
	}
	flags := record.Flags
	if flags == 0 {
		flags = defaultKeyFlags(addr)
	}
	for i := range addr.Keys {
		if addr.Keys[i].ID == record.ID {
			addr.Keys[i].Active, addr.Keys[i].PrivateKey = 1, armored
			addr.Keys[i].Token, addr.Keys[i].Signature = token.sealed, token.signature
			addr.Keys[i].Flags = flags &^ keyNotObsolete
		}
	}
	addr.SignedKeyList = &list
	rings := u.AddrKRs[addr.ID]
	u.put(addr, Rings{Read: ringHolding(append(rings.Read.GetKeys(), restored)), Write: rings.Write})
	return record.ID, nil
}

// withUserIDsOf is a key given back the user IDs Proton holds its locked record
// under.
//
// A copy exported before Proton split account keys from address keys may carry
// the user IDs of either, so the record's own are the ones it comes back with. A
// record whose only user ID is the placeholder some early keys carry comes back
// addressed to the address instead. Mirrors resetOrReplaceUserId in WebClients
// (packages/shared/lib/keys/reactivation/reactivateKeyHelper.ts).
func (u *Unlocked) withUserIDsOf(record Key, key *pgp.Key, email string) (*pgp.Key, error) {
	held, err := pgp.NewKeyFromArmored(record.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("read the locked key: %w", err)
	}
	source, entity := held.GetEntity(), key.GetEntity()
	placeholder := len(source.Identities) > 0
	for name := range source.Identities {
		if name != "UserID" {
			placeholder = false
		}
	}
	if !placeholder {
		entity.Identities = source.Identities
		return pgp.NewKeyFromEntity(entity)
	}
	entity.Identities = map[string]*openpgp.Identity{}
	if err := entity.AddUserId(email, "", email, u.Generation()); err != nil {
		return nil, fmt.Errorf("address the returning key: %w", err)
	}
	return pgp.NewKeyFromEntity(entity)
}
