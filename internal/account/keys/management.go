package keys

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// The keys an account holds, as a person manages them.
//
// Proton's settings show every key of the account - the account's own, and each
// address's - with what it is used for, and offer the same few things to do with
// one: take a copy out, make it the key an address writes with, stop encrypting
// to it, stop trusting what it signed, and remove it. Everything here is one of
// those, and each change to an address key is a change to the list the address
// publishes, made and signed the way Proton's clients make and sign it.

// Status is what a key is to the account now, in the words Proton's settings
// badge it with.
type Status string

const (
	// StatusPrimary is the key an address writes with: what mail to it is
	// encrypted to and what it signs with.
	StatusPrimary Status = "primary"
	// StatusActive is a key that opens and verifies what reached it earlier.
	StatusActive Status = "active"
	// StatusObsolete is a key nothing new is encrypted to, whose signatures are
	// still trusted.
	StatusObsolete Status = "obsolete"
	// StatusCompromised is a key nothing is encrypted to and whose signatures
	// are not trusted: it only opens what was sealed to it.
	StatusCompromised Status = "compromised"
	// StatusLocked is a key a password reset left shut.
	StatusLocked Status = "locked"
	// StatusForwarding is a key another account derived so a forwarding to this
	// one can be read.
	StatusForwarding Status = "forwarding"
	// StatusDisabled is a key of an address that is disabled.
	StatusDisabled Status = "disabled"
	// StatusUnreadable is a key Proton holds as in use that did not open here.
	StatusUnreadable Status = "unreadable"
)

// Held is one key of the account's.
type Held struct {
	ID string `json:"id"`
	// Kind is account for a key of the account itself, and address for a key of
	// one of its addresses.
	Kind      string `json:"kind"`
	AddressID string `json:"address_id,omitempty"`
	Email     string `json:"address,omitempty"`
	// Fingerprint is empty for a key whose record this build could not read.
	Fingerprint string `json:"fingerprint"`
	Algorithm   string `json:"algorithm"`
	Created     int64  `json:"created"`
	Version     int    `json:"version"`
	Primary     bool   `json:"primary"`
	Status      Status `json:"status"`
	UsedFor     string `json:"used_for"`

	flags    int
	disabled bool
	record   Key
	public   *pgp.Key
	opened   *pgp.Key
}

// The two kinds a key can be.
const (
	KindAccount = "account"
	KindAddress = "address"
)

// Account reports whether the key is the account's own rather than an
// address's.
func (h Held) Account() bool { return h.Kind == KindAccount }

// Opened reports whether the key opened on this machine, which is what an
// unlocked copy of it takes.
func (h Held) Opened() bool { return h.opened != nil }

// Locked reports whether a password reset left the key shut.
func (h Held) Locked() bool { return h.record.Active == 0 }

// Forwarding reports whether the key is one another account derived for a
// forwarding to this one.
func (h Held) Forwarding() bool { return h.Status == StatusForwarding }

// AddressDisabled reports whether the key's address is disabled.
func (h Held) AddressDisabled() bool { return h.disabled }

// Compromised reports whether the key is marked compromised: neither encrypted
// to nor trusted as a signer. Mirrors getDisplayKey in WebClients
// (packages/components/containers/keys/shared/getDisplayKey.ts).
func (h Held) Compromised() bool { return h.flags&(keyNotCompromised|keyNotObsolete) == 0 }

// Obsolete reports whether the key is no longer encrypted to while its
// signatures are still trusted.
func (h Held) Obsolete() bool { return h.flags&keyNotObsolete == 0 && !h.Compromised() }

// Flags is what the key's entry in its address's list carries.
func (h Held) Flags() int { return h.flags }

// Marked is the flags the key carries once it is marked compromised or
// obsolete, or cleared of either; nil leaves a mark as it is. Clearing
// compromised trusts its signatures again and leaves it obsolete, as Proton's
// clients do (getNewAddressKeyFlags, packages/shared/lib/keys).
func (h Held) Marked(compromised, obsolete *bool) int {
	flags := h.flags
	if compromised != nil {
		if *compromised {
			flags &^= keyNotCompromised | keyNotObsolete
		} else {
			flags |= keyNotCompromised
		}
	}
	if obsolete != nil {
		if *obsolete {
			flags &^= keyNotObsolete
		} else {
			flags |= keyNotObsolete
		}
	}
	return flags
}

// StatusFor is the status a key of the same standing would have under these
// flags, which is how a change is described before it is made.
func (h Held) StatusFor(flags int) Status {
	h.flags = flags
	return h.status()
}

// Keys is every key the account holds: each address's, in the order the
// addresses are kept and primary first, then the account's own.
func (u *Unlocked) Keys(ctx context.Context) []Held {
	addrs := append([]Address(nil), u.Addresses...)
	sort.SliceStable(addrs, func(i, j int) bool { return addrs[i].Order < addrs[j].Order })
	var out []Held
	for _, addr := range addrs {
		listed := listedEntries(ctx, addr)
		for _, record := range addr.Keys {
			out = append(out, u.addressHeld(ctx, addr, record, listed))
		}
	}
	for _, record := range u.UserKeys {
		out = append(out, u.accountHeld(ctx, record))
	}
	return out
}

// listedEntries is an address's published list, by fingerprint. A key its list
// names is described by the list rather than by its record, which is what
// Proton's clients believe of it too.
func listedEntries(ctx context.Context, addr Address) map[string]keyListEntry {
	listed := map[string]keyListEntry{}
	if addr.SignedKeyList == nil || addr.SignedKeyList.Data == "" {
		return listed
	}
	var items []keyListEntry
	if err := json.Unmarshal([]byte(addr.SignedKeyList.Data), &items); err != nil {
		// Recorded and not counted: every key is still listed, described by its
		// record instead, and this is what says why.
		slog.DebugContext(ctx, "keys: an address's key list is not readable",
			"kind", string(skip.KindAddress), "reason", string(skip.Malformed), "ref", addr.ID, "error", err.Error())
		return listed
	}
	for _, item := range items {
		listed[strings.ToLower(item.Fingerprint)] = item
	}
	return listed
}

func (u *Unlocked) addressHeld(ctx context.Context, addr Address, record Key, listed map[string]keyListEntry) Held {
	h := Held{
		ID: record.ID, Kind: KindAddress, AddressID: addr.ID, Email: addr.Email,
		Primary: record.Primary == 1, flags: record.Flags, disabled: addr.Status == 0, record: record,
	}
	if h.flags == 0 {
		h.flags = defaultKeyFlags(addr)
	}
	if !h.read(ctx, record) {
		return h.settled()
	}
	if entry, ok := listed[strings.ToLower(h.Fingerprint)]; ok {
		h.Primary, h.flags = entry.Primary == 1, entry.Flags
	}
	if rings, ok := u.AddrKRs[addr.ID]; ok {
		h.opened = openedIn(rings.Read, h.Fingerprint)
	}
	return h.settled()
}

func (u *Unlocked) accountHeld(ctx context.Context, record Key) Held {
	h := Held{
		ID: record.ID, Kind: KindAccount, Primary: record.Primary == 1,
		flags: mailKeyFlags, record: record,
	}
	if h.read(ctx, record) {
		h.opened = openedIn(u.UserKR, h.Fingerprint)
	}
	return h.settled()
}

// read fills in what the record's own key says: its fingerprint, algorithms,
// age and version. A record whose key will not parse is still one of the
// account's keys, shown as unreadable.
func (h *Held) read(ctx context.Context, record Key) bool {
	key, err := pgp.NewKeyFromArmored(record.PrivateKey)
	if err != nil {
		// Recorded and not counted: the key is listed as unreadable, and this is
		// what says why.
		slog.DebugContext(ctx, "keys: a key record could not be read",
			"kind", string(skip.KindKey), "reason", string(skip.Malformed), "ref", record.ID, "error", err.Error())
		return false
	}
	entity := key.GetEntity()
	h.public = key
	h.Fingerprint = key.GetFingerprint()
	h.Algorithm = algorithms(entity)
	h.Created = entity.PrimaryKey.CreationTime.Unix()
	h.Version = entity.PrimaryKey.Version
	if key.IsForwardingKey() {
		h.Status = StatusForwarding
	}
	return true
}

func (h Held) settled() Held {
	h.Status = h.status()
	h.UsedFor = h.usedFor()
	return h
}

// status is the one badge a key is shown with. A key can deserve several in
// Proton's settings; this is the one that most changes what the key does.
func (h Held) status() Status {
	switch {
	case h.Status == StatusForwarding:
		return StatusForwarding
	case !h.Account() && h.Compromised():
		return StatusCompromised
	case h.Locked():
		return StatusLocked
	case h.opened == nil:
		return StatusUnreadable
	case h.disabled:
		return StatusDisabled
	case h.Primary:
		return StatusPrimary
	case !h.Account() && h.Obsolete():
		return StatusObsolete
	}
	return StatusActive
}

// usedFor says what the key does for the account now. Mirrors getKeyFunction in
// WebClients (packages/components/containers/keys/KeysStatus.tsx).
func (h Held) usedFor() string {
	switch {
	case h.Locked():
		return "nothing until it is reactivated"
	case h.opened == nil:
		return "nothing, since it did not open here"
	case h.disabled:
		return "nothing while its address is disabled"
	case h.Primary:
		return "encryption, decryption, signing, verification"
	case h.Status == StatusForwarding, h.Compromised():
		return "decryption"
	}
	return "decryption, verification"
}

// algorithms names the algorithms a key is made of, the way Proton's settings
// name them. Mirrors getFormattedAlgorithmNames in WebClients
// (packages/shared/lib/keys/keyAlgorithm.ts).
func algorithms(entity *openpgp.Entity) string {
	var names []string
	seen := map[string]bool{}
	add := func(pk *packet.PublicKey) {
		name := algorithmName(pk)
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	add(entity.PrimaryKey)
	for _, sub := range entity.Subkeys {
		add(sub.PublicKey)
	}
	return strings.Replace(strings.Join(names, ", "),
		"PQC (ML-DSA) + ECC (Curve25519) hybrid, PQC (ML-KEM) + ECC (Curve25519) hybrid",
		"PQC (ML-DSA, ML-KEM) + ECC (Curve25519) hybrid", 1)
}

func algorithmName(pk *packet.PublicKey) string {
	bits := func() uint16 {
		n, err := pk.BitLength()
		if err != nil {
			return 0
		}
		return n
	}
	switch pk.PubKeyAlgo {
	case packet.PubKeyAlgoRSA, packet.PubKeyAlgoRSAEncryptOnly, packet.PubKeyAlgoRSASignOnly:
		return fmt.Sprintf("RSA (%d)", bits())
	case packet.PubKeyAlgoElGamal:
		return fmt.Sprintf("ElGamal (%d)", bits())
	case packet.PubKeyAlgoDSA:
		return fmt.Sprintf("DSA (%d)", bits())
	case packet.PubKeyAlgoECDH, packet.PubKeyAlgoECDSA, packet.PubKeyAlgoEdDSA:
		curve, err := pk.Curve()
		if err != nil {
			return "ECC"
		}
		return "ECC (" + curveName(curve) + ")"
	case packet.PubKeyAlgoX25519, packet.PubKeyAlgoEd25519:
		return "ECC (Curve25519, new format)"
	case packet.PubKeyAlgoX448, packet.PubKeyAlgoEd448:
		return "ECC (Curve448, new format)"
	case packet.PubKeyAlgoMldsa65Ed25519:
		return "PQC (ML-DSA) + ECC (Curve25519) hybrid"
	case packet.PubKeyAlgoMlkem768X25519:
		return "PQC (ML-KEM) + ECC (Curve25519) hybrid"
	}
	return fmt.Sprintf("algorithm %d", pk.PubKeyAlgo)
}

func curveName(curve packet.Curve) string {
	name := string(curve)
	switch {
	case strings.HasPrefix(name, "P"):
		return "NIST " + name
	case strings.HasPrefix(name, "Brainpool"):
		return "Brainpool " + strings.TrimPrefix(name, "Brainpool") + "r1"
	}
	return name
}

// openedIn is the opened copy of a key in a ring, by fingerprint.
func openedIn(ring *pgp.KeyRing, fingerprint string) *pgp.Key {
	if ring == nil || fingerprint == "" {
		return nil
	}
	for _, key := range ring.GetKeys() {
		if key.GetFingerprint() == fingerprint {
			return key
		}
	}
	return nil
}

// ── changing a key ──

// Manages refuses a change to keys the account does not hold alone. An
// organization can hold the keys of a member it made, and changing them is the
// administrator's.
func (u *Unlocked) Manages() error {
	if !u.private {
		return errs.Problemf("An organization holds this account's keys, so only its administrator changes them.")
	}
	return nil
}

// SetPrimary makes a key the one its address writes with.
//
// The key it replaces stays, as one the address reads with. The list the address
// publishes is signed by the new primary, and the address's earlier lists are
// signed again by it where the old primary signed them.
func (u *Unlocked) SetPrimary(ctx context.Context, c proton.Doer, h Held) error {
	if err := u.Manages(); err != nil {
		return err
	}
	addr, err := u.Address(h.AddressID)
	if err != nil {
		return err
	}
	if h.opened == nil {
		return fmt.Errorf("the key being made primary did not open")
	}
	former, err := u.writers(addr)
	if err != nil {
		return err
	}
	current := replacingPrimary(former, h.opened)
	list, err := u.relisted(addr, noKeyList(addr, "changing its primary key"),
		func(data string) (string, error) { return withPrimary(data, h.Fingerprint) }, current)
	if err != nil {
		return err
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/keys/" + h.ID + "/primary",
		Body: map[string]any{"Primary": 1, "SignedKeyList": list.body()},
	}, nil); err != nil {
		return err
	}
	version := h.Version
	for i := range addr.Keys {
		record, err := pgp.NewKeyFromArmored(addr.Keys[i].PrivateKey)
		if err == nil && record.GetEntity().PrimaryKey.Version != version {
			continue
		}
		addr.Keys[i].Primary = boolBit(addr.Keys[i].ID == h.ID)
	}
	addr.SignedKeyList = &list
	rings := u.AddrKRs[addr.ID]
	u.put(addr, Rings{Read: rings.Read, Write: ringHolding(current)})
	u.resignHistory(ctx, c, addr, former, current)
	return nil
}

// SetFlags publishes what a key of an address may be used for: whether it is
// encrypted to, and whether its signatures are trusted.
func (u *Unlocked) SetFlags(ctx context.Context, c proton.Doer, h Held, flags int) error {
	if err := u.Manages(); err != nil {
		return err
	}
	addr, err := u.Address(h.AddressID)
	if err != nil {
		return err
	}
	signers, err := u.writers(addr)
	if err != nil {
		return err
	}
	list, err := u.relisted(addr, noKeyList(addr, "marking one of its keys"),
		func(data string) (string, error) { return withFlags(data, h.Fingerprint, flags) }, signers)
	if err != nil {
		return err
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/core/v4/keys/" + h.ID + "/flags",
		Body: map[string]any{"Flags": flags, "SignedKeyList": list.body()},
	}, nil); err != nil {
		return err
	}
	for i := range addr.Keys {
		if addr.Keys[i].ID == h.ID {
			addr.Keys[i].Flags = flags
		}
	}
	addr.SignedKeyList = &list
	u.put(addr, u.AddrKRs[addr.ID])
	return nil
}

// Delete removes a key from its address for good. Whatever was sealed to it
// alone does not open again.
func (u *Unlocked) Delete(ctx context.Context, c proton.Doer, h Held) error {
	if err := u.Manages(); err != nil {
		return err
	}
	addr, err := u.Address(h.AddressID)
	if err != nil {
		return err
	}
	signers, err := u.writers(addr)
	if err != nil {
		return err
	}
	list, err := u.relisted(addr, noKeyList(addr, "deleting one of its keys"),
		func(data string) (string, error) { return without(data, h.Fingerprint) }, signers)
	if err != nil {
		return err
	}
	if err := c.Decode(ctx, proton.Request{
		Method: "POST", Path: "/core/v4/keys/address/" + h.ID + "/delete",
		Body: map[string]any{"SignedKeyList": list.body()},
	}, nil); err != nil {
		return err
	}
	kept := addr.Keys[:0]
	for _, record := range addr.Keys {
		if record.ID != h.ID {
			kept = append(kept, record)
		}
	}
	addr.Keys = kept
	addr.SignedKeyList = &list
	rings := u.AddrKRs[addr.ID]
	u.put(addr, Rings{Read: ringHolding(keysBut(rings.Read, h.Fingerprint)), Write: rings.Write})
	return nil
}

// ExportPublic is the public half of a key, armoured as Proton's clients write
// it. A key that did not open still has one.
func (h Held) ExportPublic() (string, error) {
	if h.public == nil {
		return "", errs.Problemf("The key %s could not be read, so it has no public half to export.", h.ID)
	}
	return h.public.GetArmoredPublicKeyWithCustomHeaders("", "")
}

// ExportPrivate is the private key, locked under a passphrase the person
// chose. It is never handed out unlocked.
func (h Held) ExportPrivate(passphrase string) (string, error) {
	if h.opened == nil {
		return "", fmt.Errorf("the key being exported did not open")
	}
	return LockAndArmor(h.opened, []byte(passphrase))
}

// ── the account's state, as the changes leave it ──

// Address is the account's record of one of its addresses, as the changes made
// so far in this run have left it.
func (u *Unlocked) Address(id string) (Address, error) {
	for _, addr := range u.Addresses {
		if addr.ID == id {
			addr.Keys = append([]Key(nil), addr.Keys...)
			return addr, nil
		}
	}
	return Address{}, fmt.Errorf("the address is not among the %d the account holds", len(u.Addresses))
}

// put records an address as a change left it, so a second change in the same
// run starts from the first one's result.
func (u *Unlocked) put(addr Address, rings Rings) {
	for i := range u.Addresses {
		if u.Addresses[i].ID == addr.ID {
			u.Addresses[i] = addr
		}
	}
	if rings.Read != nil {
		u.AddrKRs[addr.ID] = rings
	}
}

// adopt records a key that joined an address, as Proton now holds the address.
func (u *Unlocked) adopt(addr Address, key *pgp.Key, primary bool) {
	rings := u.AddrKRs[addr.ID]
	var read, write []*pgp.Key
	if rings.Read != nil {
		read = rings.Read.GetKeys()
	}
	if rings.Write != nil {
		write = rings.Write.GetKeys()
	}
	if primary {
		write = replacingPrimary(write, key)
	}
	if u.AddrKRs == nil {
		u.AddrKRs = map[string]Rings{}
	}
	u.put(addr, Rings{Read: ringHolding(append(read, key)), Write: ringHolding(write)})
}

// writers are the keys an address signs with now: its primaries, as they opened.
func (u *Unlocked) writers(addr Address) ([]*pgp.Key, error) {
	rings, ok := u.AddrRings(addr.ID)
	if !ok || rings.Write == nil || len(rings.Write.GetKeys()) == 0 {
		return nil, errs.Problemf("The keys for %s did not open, so its key list cannot be signed.", addr.Email)
	}
	return rings.Write.GetKeys(), nil
}

// replacingPrimary is the keys an address signs with once key becomes its
// primary: key, and the primary of the other version, which it does not replace.
func replacingPrimary(primaries []*pgp.Key, key *pgp.Key) []*pgp.Key {
	version := key.GetEntity().PrimaryKey.Version
	out := []*pgp.Key{key}
	for _, k := range primaries {
		if k.GetEntity().PrimaryKey.Version != version {
			out = append(out, k)
		}
	}
	return out
}

// keysBut is the keys of a ring but one.
func keysBut(ring *pgp.KeyRing, fingerprint string) []*pgp.Key {
	if ring == nil {
		return nil
	}
	var out []*pgp.Key
	for _, k := range ring.GetKeys() {
		if k.GetFingerprint() != fingerprint {
			out = append(out, k)
		}
	}
	return out
}

// ringHolding is a ring holding exactly the keys given. Rings are built rather than
// added to, because an address's read and write rings may be the same one.
func ringHolding(keys []*pgp.Key) *pgp.KeyRing {
	ring, _ := pgp.NewKeyRing(nil)
	for _, k := range keys {
		_ = ring.AddKey(k)
	}
	return ring
}

// relisted is the list Proton serves for an address, changed by edit and signed
// again by signers.
//
// The served list has to describe the keys the account holds before anything is
// changed in it. If it does, the signature asserts nothing but the change; if it
// does not, something is wrong that this is not the place to paper over. missing
// is the refusal for an address Proton publishes no list for, which says what
// was being attempted.
func (u *Unlocked) relisted(
	addr Address, missing error, edit func(string) (string, error), signers []*pgp.Key,
) (SignedKeyList, error) {
	if addr.SignedKeyList == nil || addr.SignedKeyList.Data == "" {
		return SignedKeyList{}, missing
	}
	held, err := addressKeys(addr)
	if err != nil {
		return SignedKeyList{}, err
	}
	if err := describesAddress(addr.SignedKeyList.Data, held); err != nil {
		return SignedKeyList{}, err
	}
	data, err := edit(addr.SignedKeyList.Data)
	if err != nil {
		return SignedKeyList{}, err
	}
	signature, err := signKeyList(data, signers, u.clock())
	if err != nil {
		return SignedKeyList{}, err
	}
	return SignedKeyList{Data: data, Signature: signature}, nil
}

// noKeyList is the refusal for changing the keys of an address Proton publishes
// no list for.
func noKeyList(addr Address, doing string) error {
	return errs.Unsupportedf("Proton publishes no key list for %s, and %s has to sign one.", addr.Email, doing).
		Hint("a Proton client writes the list the next time it opens the account")
}

func (l SignedKeyList) body() map[string]string {
	return map[string]string{"Data": l.Data, "Signature": l.Signature}
}
