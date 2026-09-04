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
// What is built here is setting one up and running it. Accepting one somebody
// sent you is not: it means writing a new address key and re-signing the
// address's Signed Key List, and proton changes no key material. See
// docs/help/limits.md.

// Forwarding types, from Proton's ForwardingType. Only the internal encrypted
// one is reachable here: forwarding to an address outside Proton turns the
// address's encryption off and needs the forwardee to answer an email, which a
// command can start and never finish.
const forwardingInternalEncrypted = 1

// Forwarding states, from Proton's ForwardingState.
const (
	forwardingPending  = 0
	forwardingActive   = 1
	forwardingOutdated = 2
	forwardingPaused   = 3
	forwardingRejected = 4
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
}

func stateName(n int) string {
	switch n {
	case forwardingPending:
		return "pending"
	case forwardingActive:
		return "active"
	case forwardingOutdated:
		return "outdated"
	case forwardingPaused:
		return "paused"
	case forwardingRejected:
		return "rejected"
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
			ID: f.ID, Direction: "outgoing", From: byID[f.ForwarderAddressID], To: f.ForwardeeEmail,
			State: stateName(f.State), Encrypted: f.Type == forwardingInternalEncrypted, Created: f.CreateTime,
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
			ID: f.ID, Direction: "incoming", From: f.ForwarderEmail, To: byID[f.ForwardeeAddressID],
			State: stateName(f.State), Encrypted: f.Type == forwardingInternalEncrypted, Created: f.CreateTime,
		})
	}
	return out, nil
}

// ForwardingCreate asks another Proton account to take mail arriving at one of
// your addresses.
//
// Nothing is forwarded until they accept, which they do in their own Proton
// client: what this sends is the derived key, sealed under a passphrase only
// they can open, and the proxy parameters Proton needs to re-wrap each message.
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
	forwardeeKR, err := keys.Published(ctx, s.C, forwardee)
	if err != nil {
		return "", err
	}
	if forwardeeKR == nil {
		return "", errs.Problemf("%s is not a Proton address, so mail cannot be forwarded to it end-to-end.", forwardee).
			Hint("Proton emails an address outside Proton a link its owner must follow, which no command can answer")
	}

	entity, err := forwardingEntity(u, from.ID)
	if err != nil {
		return "", err
	}
	material, err := deriveForwarding(entity, forwardee)
	if err != nil {
		return "", err
	}
	token, err := sealPassphrase(material.passphrase, u.AddrKRs[from.ID], forwardeeKR)
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
			"ForwardeeEmail":      forwardee,
			"ForwardeePrivateKey": material.armoredKey,
			"ActivationToken":     token,
			"ProxyInstances":      material.proxyInstances,
			// No filter: every message arriving at the address is forwarded, which
			// is what the web client's form produces when no condition is added.
			"Tree":    nil,
			"Version": sieveVersion,
		},
	}, &r); err != nil {
		return "", err
	}
	return r.OutgoingAddressForwarding.ID, nil
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
	// passphrase locks the forwardee key and is what the activation token
	// carries. It never leaves this process in the clear.
	passphrase string
	armoredKey string
	// proxyInstances is what lets Proton re-wrap a session key from the
	// forwarder's subkey to the forwardee's, one per encryption subkey.
	proxyInstances []map[string]any
}

// deriveForwarding builds the forwardee key and the proxy parameters from the
// forwarder's own key.
//
// Not strict: a forwarder holding an encryption subkey of an algorithm the
// scheme cannot proxy still forwards through the ones it can, which is what the
// web client does. A key with none at all fails, and says so.
func deriveForwarding(forwarder *openpgp.Entity, forwardee string) (*forwardingMaterial, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	passphrase := hex.EncodeToString(raw)

	derived, instances, err := forwarder.NewForwardingEntity(forwardee, "", forwardee, nil, false)
	if err != nil {
		return nil, errs.Problemf("The key for that address cannot be forwarded from: %v", err).
			Hint("Proton derives a forwarding key from an ECC encryption key, which older keys are not")
	}
	if len(instances) == 0 {
		return nil, errs.Problemf("The key for that address has nothing to forward from.").
			Hint("it holds no encryption subkey a forwarding key can be derived from")
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

	out := &forwardingMaterial{passphrase: passphrase, armoredKey: armored}
	for _, i := range instances {
		out.proxyInstances = append(out.proxyInstances, proxyInstance(i))
	}
	return out, nil
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

// ForwardingDelete removes an arrangement in either direction. On an incoming
// one this is how it is refused.
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

// ForwardingResend asks the forwardee again, for one they have not answered.
func (s *Service) ForwardingResend(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/forwardings/" + id + "/reinvite",
	}, nil)
}
