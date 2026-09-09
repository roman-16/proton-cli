package mail

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Forwarding hands every message arriving at one of your addresses to another
// Proton account, and keeps it end-to-end encrypted on the way.
//
// The mechanism is OpenPGP proxy re-encryption: a forwardee key is derived from
// the forwarder's, together with a proxy parameter per encryption subkey that
// lets Proton's servers re-wrap a message's session key from one to the other
// without ever holding either plaintext. Proton's own tags of go-crypto carry
// the primitive; the upstream releases do not.
//
// Both ends are here. Setting one up derives that material and sends it;
// accepting one opens the material somebody sent, re-locks the key under this
// account's own passphrase and publishes it as a key of the address - which is
// the only key proton ever writes, and one it did not make.

// Forwarding types, from Proton's ForwardingType. Only the internal encrypted
// one can be set up here: forwarding to an address outside Proton turns the
// address's encryption off and needs the forwardee to answer an email, which a
// command can start and never finish. One that already exists is still listed,
// and asking its forwardee again is still sending that email.
const forwardingInternalEncrypted = 1

// Forwarding states, from Proton's ForwardingState.
const (
	forwardingPending  = 0
	forwardingActive   = 1
	forwardingOutdated = 2
	forwardingPaused   = 3
	forwardingRejected = 4
)

// The words every response uses for a forwarding's state and direction, and the
// words a command judges it by. They are named because both halves of the CLI
// depend on them being the same words.
const (
	DirectionIncoming = "incoming"
	DirectionOutgoing = "outgoing"

	StateActive   = "active"
	StateOutdated = "outdated"
	StatePaused   = "paused"
	StatePending  = "pending"
	StateRejected = "rejected"
)

// Forwarding is one arrangement, in whichever direction it runs.
//
// Both directions are one collection because the question a person asks is the
// same either way - where is my mail going, and what is arriving here that was
// sent somewhere else - and Proton's own settings page shows them together.
type Forwarding struct {
	ID        string `json:"id"`
	Direction string `json:"direction"`
	From      string `json:"from"`
	To        string `json:"to"`
	State     string `json:"state"`
	Encrypted bool   `json:"encrypted"`
	Created   int64  `json:"created"`

	// What accepting one needs, carried from the listing that found it rather
	// than asked for again: the address of this account's that the mail would
	// arrive at, and the keys the forwarder derived for it.
	addressID string
	keys      []apiForwardingKey
}

type apiForwarding struct {
	ID         string
	CreateTime int64
	State      int
	Type       int
	// Outgoing.
	ForwarderAddressID string
	ForwardeeEmail     string
	// Incoming.
	ForwardeeAddressID string
	ForwarderEmail     string
	ForwardingKeys     []apiForwardingKey
}

// apiForwardingKey is one key the forwarder derived for the forwardee, locked
// under a passphrase only the forwardee can read.
//
// There is usually one. A forwarder who changed their primary key while the
// forwarding sat unanswered leaves another, and accepting means publishing every
// one of them: which the server will re-wrap with is its own to decide.
type apiForwardingKey struct {
	ActivationToken string
	PrivateKey      string
}

func stateName(n int) string {
	switch n {
	case forwardingPending:
		return StatePending
	case forwardingActive:
		return StateActive
	case forwardingOutdated:
		return StateOutdated
	case forwardingPaused:
		return StatePaused
	case forwardingRejected:
		return StateRejected
	}
	return fmt.Sprintf("state %d", n)
}

// ForwardingsList lists both directions, outgoing first.
func (s *Service) ForwardingsList(ctx context.Context) ([]Forwarding, error) {
	addrs, err := s.AddressesList(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[string]string{}
	for _, a := range addrs {
		byID[a.ID] = a.Email
	}

	var out []Forwarding
	var outgoing struct{ OutgoingAddressForwardings []apiForwarding }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/mail/v4/forwardings/outgoing",
	}, &outgoing); err != nil {
		return nil, err
	}
	for _, f := range outgoing.OutgoingAddressForwardings {
		out = append(out, Forwarding{
			ID: f.ID, Direction: DirectionOutgoing, From: byID[f.ForwarderAddressID], To: f.ForwardeeEmail,
			State: stateName(f.State), Encrypted: f.Type == forwardingInternalEncrypted, Created: f.CreateTime,
			addressID: f.ForwarderAddressID,
		})
	}

	var incoming struct{ IncomingAddressForwardings []apiForwarding }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/mail/v4/forwardings/incoming",
	}, &incoming); err != nil {
		return nil, err
	}
	for _, f := range incoming.IncomingAddressForwardings {
		out = append(out, Forwarding{
			ID: f.ID, Direction: DirectionIncoming, From: f.ForwarderEmail, To: byID[f.ForwardeeAddressID],
			State: stateName(f.State), Encrypted: f.Type == forwardingInternalEncrypted, Created: f.CreateTime,
			addressID: f.ForwardeeAddressID, keys: f.ForwardingKeys,
		})
	}
	return out, nil
}

// ForwardingCreate asks another Proton account to take mail arriving at one of
// your addresses.
//
// Nothing is forwarded until they accept: what this sends is the derived key,
// sealed under a passphrase only they can open, and the proxy parameters Proton
// needs to re-wrap each message.
func (s *Service) ForwardingCreate(ctx context.Context, forwarder, forwardee string) (string, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return "", err
	}
	from, err := s.forwarderAddress(u, forwarder)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(from.Email, forwardee) {
		return "", errs.Problemf("An address cannot forward to itself.")
	}
	material, err := s.forwardingMaterial(ctx, u, from, forwardee)
	if err != nil {
		return "", err
	}

	var r struct {
		OutgoingAddressForwarding apiForwarding
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/forwardings",
		Body: map[string]any{
			"Type":                forwardingInternalEncrypted,
			"ForwarderAddressID":  from.ID,
			"ForwardeeEmail":      material.forwardee,
			"ForwardeePrivateKey": material.privateKey,
			"ActivationToken":     material.activationToken,
			"ProxyInstances":      material.proxyInstances,
			// No filter: every message arriving at the address is forwarded, which
			// is what the web client's form produces when no condition is added.
			"Tree":    nil,
			"Version": sieveVersion,
		},
	}, &r); err != nil {
		return "", refusedSetup(err)
	}
	return r.OutgoingAddressForwarding.ID, nil
}

// refusedSetup says where a forwarding can be set up, for the request Proton
// will not take from here.
//
// Everything the request carries has been held against what Proton's own client
// sends for the same forwarder and forwardee - the derived key, the sealed
// passphrase, the proxy parameters, every field of the body - and is the same;
// Proton takes theirs and refuses this. Its own answer is left saying whatever
// it says, because it is the only account of the refusal anybody has.
func refusedSetup(err error) error {
	return errs.Problemf("%s", err).
		Hint("Proton takes a forwarding set up in its own clients: add it at account.proton.me",
			"accepting one sent to you works here")
}

// ForwardingAccept publishes the keys somebody derived for one of your
// addresses, which is what starts mail actually arriving.
//
// Every key the forwarding carries is published, and the first that cannot be
// opened stops the run: a forwarding half accepted would leave the address
// holding a key for material the server may not send, and nothing on this end
// could tell afterwards which half it was.
func (s *Service) ForwardingAccept(ctx context.Context, f Forwarding) error {
	if len(f.keys) == 0 {
		return errs.Problemf("The forwarding from %s carries no key, so there is nothing to accept.", f.From).
			Hint("ask them to set it up again")
	}
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	addr, ok := addressByID(u, f.addressID)
	if !ok {
		return errs.Problemf("%s is not one of your addresses any more.", f.To)
	}
	addrKR, ok := u.AddrKR(addr.ID)
	if !ok {
		return errs.Problemf("The keys for %s did not open, so a forwarding to it cannot be accepted.", addr.Email)
	}
	// Every key the forwarder has published, not just their primary: a
	// forwarding that has waited may have been sealed by a key they have since
	// replaced, and the signature over it is checked against whichever it was.
	forwarderKR, err := keys.Signing(ctx, s.C, f.From)
	if err != nil {
		return err
	}
	if forwarderKR == nil {
		return errs.Problemf("Proton publishes no key for %s, so what they sent cannot be checked.", f.From)
	}

	for _, sent := range f.keys {
		key, err := openForwardingKey(sent, addrKR, forwarderKR, f.From, addr.Email)
		if err != nil {
			return err
		}
		if _, err := u.AddForwardingKey(ctx, s.C, addr, key, f.ID); err != nil {
			return err
		}
	}
	return nil
}

// openForwardingKey unseals one forwarding key and makes it this address's.
//
// The passphrase is sealed to this address and signed by the forwarder, so
// opening it proves both that the key was meant for this account and that it
// came from the account the forwarding names.
func openForwardingKey(
	sent apiForwardingKey, addrKR, forwarderKR *pgp.KeyRing, forwarder, forwardee string,
) (*pgp.Key, error) {
	token, err := pgp.NewPGPMessageFromArmored(sent.ActivationToken)
	if err != nil {
		return nil, errs.Problemf("What %s sent is not readable, so the forwarding cannot be accepted.", forwarder).
			Hint("ask them to set it up again")
	}
	passphrase, err := addrKR.Decrypt(token, forwarderKR, pgp.GetUnixTime())
	if err != nil {
		return nil, errs.Problemf(
			"The key %s sent could not be opened as theirs, so the forwarding cannot be accepted.", forwarder).
			Hint("it was sealed to another address, or signed by a key Proton no longer publishes for them",
				"ask them to set it up again")
	}
	locked, err := pgp.NewKeyFromArmored(sent.PrivateKey)
	if err != nil {
		return nil, errs.Problemf("What %s sent is not a readable key.", forwarder)
	}
	key, err := locked.Unlock(passphrase.GetBinary())
	if err != nil {
		return nil, errs.Problemf("The key %s sent does not open with the passphrase they sealed for it.", forwarder)
	}
	if version := key.GetEntity().PrimaryKey.Version; version != 4 {
		return nil, fmt.Errorf("the forwarding key is version %d, which cannot be forwarded to", version)
	}
	return addressedTo(key, forwardee)
}

// addressedTo makes the key say which address it belongs to.
//
// The forwarder typed the address when they set the forwarding up, and Proton
// holds the authoritative spelling of it - capitalisation included. A key whose
// user ID says anything else is rewritten to say this, which is what Proton's
// own clients do, because the address as Proton holds it is what everything
// afterwards compares against.
func addressedTo(key *pgp.Key, email string) (*pgp.Key, error) {
	entity := key.GetEntity()
	if id := entity.PrimaryIdentity(); id != nil && id.UserId != nil && id.UserId.Email == email {
		return key, nil
	}
	for name := range entity.Identities {
		delete(entity.Identities, name)
	}
	if err := entity.AddUserId(email, "", email, nil); err != nil {
		return nil, fmt.Errorf("name the forwarding key after the address: %w", err)
	}
	return pgp.NewKeyFromEntity(entity)
}

// addressByID finds the address record a forwarding arrives at.
func addressByID(u *keys.Unlocked, id string) (keys.Address, bool) {
	for _, a := range u.Addresses {
		if a.ID == id {
			return a, true
		}
	}
	return keys.Address{}, false
}

// forwarderAddress picks which of the account's addresses forwards, refusing one
// whose keys did not open: deriving a forwardee key needs the private half.
func (s *Service) forwarderAddress(u *keys.Unlocked, email string) (keys.Address, error) {
	for _, a := range u.Addresses {
		if !strings.EqualFold(a.Email, email) {
			continue
		}
		if _, ok := u.AddrKR(a.ID); !ok {
			return keys.Address{}, errs.Problemf("The keys for %s did not open, so it cannot forward.", a.Email)
		}
		return a, nil
	}
	return keys.Address{}, errs.Problemf("%s is not one of your addresses.", email).
		Hint("`proton mail settings addresses list` shows them")
}

// forwardingEntity is the address's primary key, as go-crypto sees it.
//
// Deriving forwarding material reaches below gopenpgp: the proxy parameters come
// out of the raw entity, one per encryption subkey.
func forwardingEntity(u *keys.Unlocked, addressID string) (*openpgp.Entity, error) {
	kr, ok := u.AddrKR(addressID)
	if !ok {
		return nil, errs.Problemf("That address has no key that opened.")
	}
	all := kr.GetKeys()
	if len(all) == 0 {
		return nil, errs.Problemf("That address has no key that opened.")
	}
	entity := all[0].GetEntity()
	if entity == nil || entity.PrivateKey == nil {
		return nil, errs.Problemf("That address's key is not one this can forward from.")
	}
	return entity, nil
}

// forwardingMaterial is everything a forwarding request carries that had to be
// computed rather than typed.
type forwardingMaterial struct {
	privateKey string
	// activationToken is the forwardee key's passphrase, sealed to the forwardee
	// and signed by the forwarder. The passphrase itself never leaves this
	// process in the clear.
	activationToken string
	// proxyInstances is what lets Proton re-wrap a session key from the
	// forwarder's subkey to the forwardee's, one per encryption subkey.
	proxyInstances []map[string]any
	// forwardee is the address as the forwardee's own key spells it, which is
	// what the derived key is named after and what the forwarding is filed under.
	forwardee string
}

// forwardingMaterial derives a fresh set for one forwarder and forwardee.
//
// Setting a forwarding up and asking its forwardee again both send exactly this,
// because both are the same offer: a key derived now, sealed to them now. A
// renewal cannot reuse what was sent before - the passphrase was never kept -
// and would not want to, since what makes a forwarding outdated is the key it
// was derived from changing.
func (s *Service) forwardingMaterial(
	ctx context.Context, u *keys.Unlocked, from keys.Address, forwardee string,
) (*forwardingMaterial, error) {
	forwardeeKR, err := keys.Published(ctx, s.C, forwardee)
	if err != nil {
		return nil, err
	}
	if forwardeeKR == nil {
		return nil, errs.Problemf(
			"%s is not a Proton address, so mail cannot be forwarded to it end-to-end.", forwardee).
			Hint("Proton emails an address outside Proton a link its owner must follow, which no command can answer")
	}
	// The address as their own key spells it, not as it was typed: Proton's
	// addresses answer to more spellings than they are stored under, and a key
	// named after the wrong one is a key Proton refuses.
	named := keyEmail(forwardeeKR)
	if named == "" {
		named = forwardee
	}
	entity, err := forwardingEntity(u, from.ID)
	if err != nil {
		return nil, err
	}
	derived, err := deriveForwarding(entity, named)
	if err != nil {
		return nil, err
	}
	token, err := sealPassphrase(derived.passphrase, u.AddrKRs[from.ID], forwardeeKR)
	if err != nil {
		return nil, err
	}
	return &forwardingMaterial{
		privateKey:      derived.armoredKey,
		activationToken: token,
		proxyInstances:  derived.proxyInstances,
		forwardee:       named,
	}, nil
}

// keyEmail is the address a published key says it belongs to.
func keyEmail(kr *pgp.KeyRing) string {
	for _, key := range kr.GetKeys() {
		entity := key.GetEntity()
		if entity == nil {
			continue
		}
		if id := entity.PrimaryIdentity(); id != nil && id.UserId != nil && id.UserId.Email != "" {
			return id.UserId.Email
		}
	}
	return ""
}

// derivedKey is the forwardee key as it comes out of the derivation, before its
// passphrase is sealed to anybody.
type derivedKey struct {
	passphrase     string
	armoredKey     string
	proxyInstances []map[string]any
}

// deriveForwarding builds the forwardee key and the proxy parameters from the
// forwarder's own key.
//
// Not strict: a forwarder holding an encryption subkey of an algorithm the
// scheme cannot proxy still forwards through the ones it can, which is what the
// web client does. A key with none at all fails, and says so.
func deriveForwarding(forwarder *openpgp.Entity, forwardee string) (*derivedKey, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	passphrase := hex.EncodeToString(raw)

	// A configuration of its own, because the derivation writes the algorithm and
	// curve it insists on into whatever it is handed.
	derived, instances, err := forwarder.NewForwardingEntity(
		forwardee, "", forwardee, &packet.Config{}, false)
	if err != nil {
		return nil, errs.Problemf("The key for that address cannot be forwarded from: %v", err).
			Hint("Proton derives a forwarding key from an ECC encryption key, which older keys are not")
	}
	if len(instances) == 0 {
		return nil, errs.Problemf("The key for that address has nothing to forward from.").
			Hint("it holds no encryption subkey a forwarding key can be derived from")
	}
	if err := forwardOnly(derived); err != nil {
		return nil, err
	}

	key, err := pgp.NewKeyFromEntity(derived)
	if err != nil {
		return nil, err
	}
	locked, err := key.Lock([]byte(passphrase))
	if err != nil {
		return nil, err
	}
	armored, err := locked.Armor()
	if err != nil {
		return nil, err
	}

	out := &derivedKey{passphrase: passphrase, armoredKey: armored}
	for _, i := range instances {
		out.proxyInstances = append(out.proxyInstances, proxyInstance(i))
	}
	return out, nil
}

// forwardOnly leaves the derived key saying the one thing about itself that
// Proton's clients say: it may be used for forwarded communications.
//
// The derivation also marks it as a key whose private half was split by a
// secret-sharing scheme, which is a fair description of what forwarding does and
// is not what the accounts on the other side of it write. A key that says more
// about itself than theirs do is a key they did not make.
func forwardOnly(derived *openpgp.Entity) error {
	for _, sub := range derived.Subkeys {
		if sub.Sig == nil || !sub.Sig.FlagForward {
			continue
		}
		sub.Sig.FlagSplitKey = false
		// The flags live in what binds the subkey to the key, so changing them
		// means signing that again.
		if err := sub.Sig.SignKey(sub.PublicKey, derived.PrivateKey, &packet.Config{}); err != nil {
			return fmt.Errorf("sign the forwarding key's encryption subkey: %w", err)
		}
	}
	return nil
}

func proxyInstance(i packet.ForwardingInstance) map[string]any {
	return map[string]any{
		"PgpVersion":              i.KeyVersion,
		"ForwarderKeyFingerprint": hex.EncodeToString(i.ForwarderFingerprint),
		"ForwardeeKeyFingerprint": hex.EncodeToString(i.ForwardeeFingerprint),
		"ProxyParam":              hex.EncodeToString(i.ProxyParameter),
	}
}

// sealPassphrase encrypts the forwardee key's passphrase to the forwardee and
// signs it as the forwarder, which is how they prove where it came from.
func sealPassphrase(passphrase string, forwarderKR, forwardeeKR *pgp.KeyRing) (string, error) {
	msg, err := forwardeeKR.Encrypt(pgp.NewPlainMessageFromString(passphrase), forwarderKR)
	if err != nil {
		return "", err
	}
	return msg.GetArmored()
}

// ForwardingDelete removes an arrangement in either direction.
func (s *Service) ForwardingDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/mail/v4/forwardings/" + id,
	}, nil)
}

// ForwardingPause stops mail being forwarded without taking the arrangement
// down, so resuming it needs nothing from the forwardee.
func (s *Service) ForwardingPause(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/forwardings/" + id + "/pause",
	}, nil)
}

// ForwardingResume starts a paused arrangement again.
func (s *Service) ForwardingResume(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/forwardings/" + id + "/resume",
	}, nil)
}

// ForwardingResend offers a forwarding to its forwardee again.
//
// What that means depends on what the forwardee is. Another Proton account is
// offered fresh material and answers it in their own client, so the offer is the
// material: a key derived now from whatever this address's key is now, which is
// also what makes this the repair for a forwarding left outdated by a key
// change. An address outside Proton has no key and no client, so the only thing
// to send again is the email Proton asks its owner to answer.
func (s *Service) ForwardingResend(ctx context.Context, f Forwarding) error {
	if f.Encrypted {
		return s.renewForwarding(ctx, f)
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/forwardings/" + f.ID + "/reinvite",
	}, nil)
}

func (s *Service) renewForwarding(ctx context.Context, f Forwarding) error {
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	from, err := s.forwarderAddress(u, f.From)
	if err != nil {
		return err
	}
	material, err := s.forwardingMaterial(ctx, u, from, f.To)
	if err != nil {
		return err
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/forwardings/" + f.ID,
		Body: map[string]any{
			"ForwardeePrivateKey": material.privateKey,
			"ActivationToken":     material.activationToken,
			"ProxyInstances":      material.proxyInstances,
		},
	}, nil); err != nil {
		return refusedSetup(err)
	}
	return nil
}
