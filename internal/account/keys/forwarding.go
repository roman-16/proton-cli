package keys

import (
	"context"
	"fmt"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Publishing a key another Proton account derived for one of this account's
// addresses.
//
// None of it is a key this build made: the forwarder derived the key, and what
// happens here is re-locking it under the passphrase the address's own primary
// key is already locked with, and publishing it beside the others. The address
// keeps every key it had, and the list it publishes is the one Proton already
// serves.

// AddForwardingKey publishes a forwarding key for one of the account's
// addresses and hands back the ID Proton files it under.
//
// The key arrives unlocked and goes up locked under the address's primary key
// token, which is the passphrase the address's own keys are locked with - so
// this account opens it afterwards with what it already has, and the token
// itself is handed back to Proton exactly as Proton holds it.
func (u *Unlocked) AddForwardingKey(
	ctx context.Context, c proton.Doer, addr Address, key *pgp.Key, forwardingID string,
) (string, error) {
	primary, err := primaryRecord(addr)
	if err != nil {
		return "", err
	}
	token, err := decryptToken(primary.Token, primary.Signature, u.UserKR)
	if err != nil {
		return "", fmt.Errorf("open the primary address key's token: %w", err)
	}
	armored, err := LockAndArmor(key, token)
	if err != nil {
		return "", fmt.Errorf("lock the forwarding key under the address's token: %w", err)
	}
	skl, err := u.republishKeyList(addr)
	if err != nil {
		return "", err
	}
	// Never primary: a forwarding key decrypts what the forwarder's server
	// re-wrapped and is not something this address writes with.
	return publishKey(ctx, c, addressKeyRequest{
		addressID: addr.ID, armoredKey: armored, primary: 0,
		token: primary.Token, signature: primary.Signature,
		keyList: skl, forwardingID: forwardingID,
	})
}

// primaryRecord is the key record whose token every other key of the address is
// locked with, and whose token a key being added is locked with in turn.
//
// Proton returns an address's keys primary first and its own clients take the
// first of them; a record with no token is an address from before Proton moved
// address keys under the user key, which nothing here can add a key to.
func primaryRecord(addr Address) (Key, error) {
	for _, k := range addr.Keys {
		if k.Primary != 1 || k.Active == 0 {
			continue
		}
		if k.Token == "" || k.Signature == "" {
			return Key{}, errs.Unsupportedf(
				"The keys of %s are held in a form this cannot add to.", addr.Email).
				Hint("a Proton client will move them the next time it opens the account")
		}
		return k, nil
	}
	return Key{}, errs.Problemf("%s has no active primary key.", addr.Email)
}

// republishKeyList signs the key list Proton already serves for the address.
//
// A forwarding key is never listed - Proton's clients leave it out, and the
// account's own Key Transparency audit expects it left out - so adding one
// leaves the list saying exactly what it said before. What the request needs is
// therefore the same list under a fresh signature, and Data goes back byte for
// byte: a list rebuilt here would be a second opinion about what the address
// holds, and every way it could differ from Proton's would publish something
// false about somebody's keys.
//
// What is checked instead of rebuilt is that the served list names the keys this
// account actually holds. If it does, re-signing it asserts nothing new; if it
// does not, something is wrong that this is not the place to paper over.
func (u *Unlocked) republishKeyList(addr Address) (SignedKeyList, error) {
	if addr.SignedKeyList == nil || addr.SignedKeyList.Data == "" {
		return SignedKeyList{}, errs.Unsupportedf(
			"Proton publishes no key list for %s, and accepting a forwarding has to sign one.", addr.Email).
			Hint("accept this one in a Proton client, which writes the list; later ones work here")
	}
	held, err := addressKeys(addr)
	if err != nil {
		return SignedKeyList{}, err
	}
	if err := describesAddress(addr.SignedKeyList.Data, held); err != nil {
		return SignedKeyList{}, err
	}
	rings, ok := u.AddrRings(addr.ID)
	if !ok {
		return SignedKeyList{}, errs.Problemf(
			"The keys for %s did not open, so its key list cannot be signed.", addr.Email)
	}
	signature, err := signKeyList(addr.SignedKeyList.Data, rings.Write.GetKeys())
	if err != nil {
		return SignedKeyList{}, err
	}
	return SignedKeyList{Data: addr.SignedKeyList.Data, Signature: signature}, nil
}

// PrimaryKeys are the keys an address writes with: its primary records, as they
// opened.
//
// There is one, except on an account that keeps a post-quantum key beside the
// ordinary one. Which of them a caller may use is the caller's to judge - a
// forwarding can only be derived from the v4 key - so all of them come back.
func (u *Unlocked) PrimaryKeys(addr Address) ([]*pgp.Key, error) {
	rings, ok := u.AddrRings(addr.ID)
	if !ok {
		return nil, errs.Problemf("The keys for %s did not open.", addr.Email)
	}
	primary := rings.Write.GetKeys()
	if len(primary) == 0 {
		return nil, errs.Problemf("%s has no active primary key that opened.", addr.Email)
	}
	return primary, nil
}
