package keys

import (
	"encoding/json"
	"fmt"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// The account's own statement of which keys an address holds.
//
// Proton serves it beside the keys, Key Transparency audits it, and every client
// that adds a key to an address publishes it again - so it is the one thing this
// package says about somebody's keys that other clients will believe. Two rules
// follow, and everything here is one of them: a list is only composed where
// there is nothing to contradict, and a list that already exists is signed
// again with its bytes untouched - except that turning end-to-end encryption on
// or off flips the one flag bit on every entry that Proton's clients flip, and
// nothing else about it.

// sklSigningContext is the notation Proton's clients sign a key list under,
// which is what stops a signature over one being read as a signature over
// anything else. Mirrors KT_SKL_SIGNING_CONTEXT in WebClients
// (packages/key-transparency/lib/constants/constants.ts).
const sklSigningContext = "key-transparency.key-list"

// mailKeyFlags is what a key that may encrypt and sign mail carries: neither
// obsolete nor compromised. Mirrors getDefaultKeyFlags in WebClients for an
// address with end-to-end encryption on, which is every address this creates.
const mailKeyFlags = 3

// keyEncryptionOff is the bit an entry carries when mail arriving at the
// address cannot be encrypted to that key. Mirrors KEY_FLAG.FLAG_EMAIL_NO_ENCRYPT
// in WebClients (packages/shared/lib/constants.ts).
const keyEncryptionOff = 4

// keyListEntry is one key as a key list names it. The field order is the order
// Proton's clients write, since the list is stored as the text that was signed.
type keyListEntry struct {
	Primary            int      `json:"Primary"`
	Flags              int      `json:"Flags"`
	Fingerprint        string   `json:"Fingerprint"`
	SHA256Fingerprints []string `json:"SHA256Fingerprints"`
}

// composeKeyList states that an address holds one key, which is the only claim
// this package composes rather than reads.
//
// It is for an address that has just come into being: Proton holds no list for
// it, the key named here is the one being published, and there is therefore
// nothing the statement could disagree with.
func composeKeyList(key *pgp.Key) (string, error) {
	data, err := json.Marshal([]keyListEntry{{
		Primary:            1,
		Flags:              mailKeyFlags,
		Fingerprint:        key.GetFingerprint(),
		SHA256Fingerprints: key.GetSHA256Fingerprints(),
	}})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// signKeyList signs a key list as the address.
//
// Every signer signs, because which signature a reader checks is not this end's
// to predict: an address that keeps a post-quantum key beside its ordinary one
// is signed by both, which is what Proton's clients write. gopenpgp signs with
// one entity at a time, so each signs separately and the packets go up as one
// block, which is what a several-signature detached signature is.
func signKeyList(data string, signers []*pgp.Key) (string, error) {
	if len(signers) == 0 {
		return "", fmt.Errorf("no key of the address is available to sign its key list")
	}
	message := pgp.NewPlainMessageFromString(data)
	context := pgp.NewSigningContext(sklSigningContext, false)

	var packets []byte
	for _, key := range signers {
		ring, err := pgp.NewKeyRing(key)
		if err != nil {
			return "", err
		}
		signature, err := ring.SignDetachedWithContext(message, context)
		if err != nil {
			return "", fmt.Errorf("sign the address's key list: %w", err)
		}
		packets = append(packets, signature.GetBinary()...)
	}
	return pgp.NewPGPSignature(packets).GetArmored()
}

// withEncryption is the published list saying that mail to the address is
// end-to-end encrypted, or that it is not.
//
// It is the one thing here that rewrites a list rather than re-signing it, and
// it rewrites exactly what Proton's own clients rewrite: one flag bit, on every
// entry. No key is added, removed or reordered, so the list still names the keys
// the address holds; the whole of what it says differently is whether what
// arrives can be encrypted to them.
func withEncryption(data string, on bool) (string, error) {
	var items []keyListEntry
	if err := json.Unmarshal([]byte(data), &items); err != nil {
		return "", fmt.Errorf("read the published key list: %w", err)
	}
	if len(items) == 0 {
		return "", fmt.Errorf("the published key list names no key")
	}
	for i := range items {
		if on {
			items[i].Flags &^= keyEncryptionOff
			continue
		}
		items[i].Flags |= keyEncryptionOff
	}
	out, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(out), nil
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
	var listable, unlisted int
	for _, k := range held {
		if k.forwarding {
			continue
		}
		listable++
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
			listable, unlisted, len(items), unheld)
	}
	return nil
}
