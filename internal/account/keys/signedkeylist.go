package keys

import (
	"bytes"
	"crypto"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	"github.com/ProtonMail/gopenpgp/v2/constants"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// The account's own statement of which keys an address holds.
//
// Proton serves it beside the keys, Key Transparency audits it, and every client
// that changes an address's keys publishes it again - so it is the one thing
// this package says about somebody's keys that other clients will believe. Two
// rules follow, and everything here is one of them. A list is composed only for
// an address that has no key to contradict it. A list that exists is checked
// against the keys the account holds, changed exactly as Proton's clients change
// it for the same action, and signed again by the keys that are primary once the
// change lands.
//
// The changes are few and each touches as little as it can: a key named first,
// as the new primary, or last, as one that reads; the primary moved to another
// key; one key's flags set; one key dropped; and end-to-end encryption flipped
// on every entry at once.

// sklSigningContext is the notation Proton's clients sign a key list under,
// which is what stops a signature over one being read as a signature over
// anything else. Mirrors KT_SKL_SIGNING_CONTEXT in WebClients
// (packages/key-transparency/lib/constants/constants.ts).
const sklSigningContext = "key-transparency.key-list"

// What a key list entry's flags say about the key. Mirrors KEY_FLAG in
// WebClients (packages/shared/lib/constants.ts).
const (
	// keyNotCompromised is carried while signatures by the key are trusted.
	keyNotCompromised = 1
	// keyNotObsolete is carried while the key may still be encrypted to.
	keyNotObsolete = 2
	// keyEncryptionOff is carried when mail arriving at the address cannot be
	// encrypted to the key at all, which is what an address forwarding outside
	// Proton leaves.
	keyEncryptionOff = 4
	// keySignaturesOff is carried when Proton expects no signature on what
	// arrives at the address.
	keySignaturesOff = 8
)

// mailKeyFlags is what a key that may encrypt and sign mail carries: neither
// obsolete nor compromised.
const mailKeyFlags = keyNotCompromised | keyNotObsolete

// defaultKeyFlags is what a key joining an address carries: one that encrypts
// and signs, under whatever the address says about encryption and signatures.
// Mirrors getDefaultKeyFlags in WebClients (packages/shared/lib/keys/keyFlags.ts).
func defaultKeyFlags(addr Address) int {
	flags := mailKeyFlags
	if !EndToEnd(addr.Flags) {
		flags |= keyEncryptionOff
	}
	if !ExpectsSigned(addr.Flags) {
		flags |= keySignaturesOff
	}
	return flags
}

// keyListEntry is one key as a key list names it. The field order is the order
// Proton's clients write, since the list is stored as the text that was signed.
type keyListEntry struct {
	Primary            int      `json:"Primary"`
	Flags              int      `json:"Flags"`
	Fingerprint        string   `json:"Fingerprint"`
	SHA256Fingerprints []string `json:"SHA256Fingerprints"`
}

func entryFor(key *pgp.Key, primary bool, flags int) keyListEntry {
	return keyListEntry{
		Primary:            boolBit(primary),
		Flags:              flags,
		Fingerprint:        key.GetFingerprint(),
		SHA256Fingerprints: key.GetSHA256Fingerprints(),
	}
}

// fingerprintVersion is the OpenPGP version of the key a fingerprint names. A
// version 4 fingerprint is a SHA-1, forty hex digits; a version 6 one is a
// SHA-256, sixty-four. An address keeps a primary of each version, so which
// entries a new primary displaces is read off this.
func fingerprintVersion(fingerprint string) int {
	if len(fingerprint) == 64 {
		return 6
	}
	return 4
}

// composeKeyList states that an address holds one key, which is the only claim
// this package composes rather than edits.
//
// It is for an address with no key that is in use: Proton holds no list naming
// one, the key named here is the one being published, and there is therefore
// nothing the statement could disagree with.
func composeKeyList(key *pgp.Key, flags int) (string, error) {
	data, err := json.Marshal([]keyListEntry{entryFor(key, true, flags)})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// editKeyList reads a published list, changes it, and writes it back out.
func editKeyList(data string, edit func([]keyListEntry) ([]keyListEntry, error)) (string, error) {
	var items []keyListEntry
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		return "", fmt.Errorf("read the published key list: %w", err)
	}
	if len(items) == 0 {
		return "", fmt.Errorf("the published key list names no key")
	}
	items, err := edit(items)
	if err != nil {
		return "", err
	}
	out, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// primariesFirst orders a list the way Proton's clients write one: the version
// 4 primary first, the version 6 primary after it, and every other key in the
// order it already had.
func primariesFirst(items []keyListEntry) []keyListEntry {
	rank := func(e keyListEntry) int {
		switch {
		case e.Primary == 1 && fingerprintVersion(e.Fingerprint) == 4:
			return 0
		case e.Primary == 1:
			return 1
		}
		return 2
	}
	sort.SliceStable(items, func(i, j int) bool { return rank(items[i]) < rank(items[j]) })
	return items
}

// displacePrimary takes the primary mark off every key of the version a new
// primary is, which is the one it replaces.
func displacePrimary(items []keyListEntry, version int) {
	for i := range items {
		if fingerprintVersion(items[i].Fingerprint) == version {
			items[i].Primary = 0
		}
	}
}

// withAdded is the published list naming one more key: first, as the key the
// address writes with, or last, as one it only reads with.
func withAdded(data string, key *pgp.Key, flags int, primary bool) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		entry := entryFor(key, primary, flags)
		if !primary {
			return append(items, entry), nil
		}
		displacePrimary(items, fingerprintVersion(entry.Fingerprint))
		return primariesFirst(append([]keyListEntry{entry}, items...)), nil
	})
}

// withPrimary is the published list with another key as the one the address
// writes with. The key it replaces stays, as one the address reads with.
func withPrimary(data, fingerprint string) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		displacePrimary(items, fingerprintVersion(fingerprint))
		found := false
		for i := range items {
			if strings.EqualFold(items[i].Fingerprint, fingerprint) {
				items[i].Primary = 1
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("the published key list does not name the key being made primary")
		}
		return primariesFirst(items), nil
	})
}

// withFlags is the published list with one key's flags changed. A key the list
// does not name - one a password reset locked - leaves it as it was, and its
// flags travel on its record alone.
func withFlags(data, fingerprint string, flags int) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		for i := range items {
			if strings.EqualFold(items[i].Fingerprint, fingerprint) {
				items[i].Flags = flags
			}
		}
		return items, nil
	})
}

// without is the published list with one key taken out of it.
func without(data, fingerprint string) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		kept := items[:0]
		for _, item := range items {
			if !strings.EqualFold(item.Fingerprint, fingerprint) {
				kept = append(kept, item)
			}
		}
		if len(kept) == 0 {
			return nil, fmt.Errorf("the published key list would name no key")
		}
		return kept, nil
	})
}

// withEncryption is the published list saying that mail to the address is
// end-to-end encrypted, or that it is not: one flag bit, on every entry.
func withEncryption(data string, on bool) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		for i := range items {
			if on {
				items[i].Flags &^= keyEncryptionOff
				continue
			}
			items[i].Flags |= keyEncryptionOff
		}
		return items, nil
	})
}

// withReactivated is the published list with keys a password reset had locked
// named again at its end.
//
// Every entry the list had stays as it was: the address's live keys are not what
// changed. Each key coming back is added primary to nothing and no longer
// encryptable to, under the flags its record carried otherwise, which is the
// entry Proton's own clients write for a reactivated key.
func withReactivated(data string, addr Address, keys []reactivated) (string, error) {
	return editKeyList(data, func(items []keyListEntry) ([]keyListEntry, error) {
		for _, k := range keys {
			flags := k.record.Flags
			if flags == 0 {
				flags = defaultKeyFlags(addr)
			}
			items = append(items, entryFor(k.key, false, flags&^keyNotObsolete))
		}
		return items, nil
	})
}

// signKeyList signs a key list as the address, dated by the clock given.
//
// Every signer signs, because which signature a reader checks is not this end's
// to predict: an address that keeps a post-quantum key beside its ordinary one
// is signed by both, which is what Proton's clients write. Each signs separately
// and the packets go up as one block, which is what a several-signature detached
// signature is.
//
// The clock is a parameter because one list is signed for a moment already
// past: a list the address published before its primary changed is signed
// again by the new primary as of when the old one signed it.
func signKeyList(data string, signers []*pgp.Key, at func() time.Time) (string, error) {
	if len(signers) == 0 {
		return "", fmt.Errorf("no key of the address is available to sign its key list")
	}
	config := &packet.Config{
		DefaultHash: crypto.SHA512,
		Time:        at,
		SignatureNotations: []*packet.Notation{{
			Name:            constants.SignatureContextName,
			Value:           []byte(sklSigningContext),
			IsHumanReadable: true,
		}},
	}
	var packets bytes.Buffer
	for _, key := range signers {
		if err := openpgp.DetachSignText(&packets, key.GetEntity(), strings.NewReader(data), config); err != nil {
			return "", fmt.Errorf("sign the address's key list: %w", err)
		}
	}
	return pgp.NewPGPSignature(packets.Bytes()).GetArmored()
}

// addressKey is one of an address's active key records, read far enough to be
// compared with the list Proton publishes and to be told from a forwarding key.
type addressKey struct {
	record Key
	// fingerprints is the key's own SHA256 fingerprint and its subkeys', joined
	// the way a list entry names them.
	fingerprints string
	// forwarding marks a key derived for this address by somebody else, which is
	// exactly what a key list leaves out.
	forwarding bool
	key        *pgp.Key
}

// addressKeys reads the address's active keys. Only the public halves are
// wanted, so nothing here needs a key that opened.
func addressKeys(addr Address) ([]addressKey, error) {
	var out []addressKey
	for _, record := range addr.Keys {
		if record.Active == 0 {
			continue
		}
		key, err := pgp.NewKeyFromArmored(record.PrivateKey)
		if err != nil {
			// Not skipped: a key that will not parse is one this cannot say
			// whether the published list names, and a list signed without
			// knowing that is the one thing this must not do.
			return nil, fmt.Errorf("read one of the address's keys: %w", err)
		}
		out = append(out, addressKey{
			record:       record,
			fingerprints: strings.Join(key.GetSHA256Fingerprints(), ","),
			forwarding:   key.IsForwardingKey(),
			key:          key,
		})
	}
	return out, nil
}

// listable is the keys a list names: the active ones, forwarding keys aside.
func listable(held []addressKey) int {
	n := 0
	for _, k := range held {
		if !k.forwarding {
			n++
		}
	}
	return n
}

// describesAddress reports whether a published list names exactly the keys the
// account holds for the address, forwarding keys aside.
//
// The two ways it can disagree are told apart because they mean different
// things: a key held and not listed is a list that understates the address,
// which is what an interrupted write elsewhere leaves behind, and a key listed
// and not held is a list about some other address or a reading of it this build
// has got wrong. Neither is repaired here, and both stop the write.
func describesAddress(data string, held []addressKey) error {
	var items []keyListEntry
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		return fmt.Errorf("read the published key list: %w", err)
	}
	listed := map[string]int{}
	for _, item := range items {
		listed[strings.Join(item.SHA256Fingerprints, ",")]++
	}
	var unlisted int
	for _, k := range held {
		if k.forwarding {
			continue
		}
		if listed[k.fingerprints] == 0 {
			unlisted++
			continue
		}
		listed[k.fingerprints]--
	}
	var unheld int
	for _, n := range listed {
		unheld += n
	}
	if unlisted > 0 || unheld > 0 {
		return fmt.Errorf(
			"the published key list does not describe the address: of the address's %d listable keys %d are missing from the list of %d, and %d of its entries name keys the account does not hold",
			listable(held), unlisted, len(items), unheld)
	}
	return nil
}
