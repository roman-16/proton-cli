package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Forwarding, from both ends: what one account derives, the other has to be
// able to open. Every test here plays both parts with real keys, because the
// two halves only agree if the derivation, the sealing, the signature over it
// and the re-locking all line up - and nothing short of doing it proves that.

const (
	forwarderEmail = "jane@proton.me"
	forwardeeEmail = "me@proton.me"
)

// party is one account's keys: the user key its address keys hang under, the
// address itself, and the passphrase the address's keys are locked with.
type party struct {
	unlocked *keys.Unlocked
	addr     keys.Address
	kr       *pgp.KeyRing
	token    string
}

func newParty(t *testing.T, email string) *party {
	t.Helper()
	userKey := freshKey(t, "user")
	userKR, err := pgp.NewKeyRing(userKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	token := "the-address-key-token-of-" + email
	sealed, signature := sealedToken(t, userKR, token)

	addrKey := freshKey(t, email)
	addrKR, err := pgp.NewKeyRing(addrKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	addr := keys.Address{
		ID: "address-of-" + email, Email: email, Status: 1, Send: 1, Receive: 1,
		Keys: []keys.Key{{
			ID: "key-of-" + email, PrivateKey: armoredLocked(t, addrKey, token),
			Token: sealed, Signature: signature, Primary: 1, Active: 1,
		}},
	}
	addr.SignedKeyList = &keys.SignedKeyList{
		Data: publishedKeyList(t, addrKey), Signature: "the signature Proton holds",
	}
	return &party{
		unlocked: &keys.Unlocked{
			UserKR: userKR, Addresses: []keys.Address{addr},
			AddrKRs: map[string]*pgp.KeyRing{addr.ID: addrKR},
		},
		addr:  addr,
		kr:    addrKR,
		token: token,
	}
}

func freshKey(t *testing.T, name string) *pgp.Key {
	t.Helper()
	key, err := pgp.GenerateKey(name, name, "x25519", 0)
	if err != nil {
		t.Fatalf("generate a key for %s: %v", name, err)
	}
	return key
}

func armoredLocked(t *testing.T, key *pgp.Key, passphrase string) string {
	t.Helper()
	shut, err := key.Lock([]byte(passphrase))
	if err != nil {
		t.Fatalf("lock: %v", err)
	}
	armored, err := shut.Armor()
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	return armored
}

func sealedToken(t *testing.T, userKR *pgp.KeyRing, token string) (sealed, signature string) {
	t.Helper()
	message := pgp.NewPlainMessageFromString(token)
	encrypted, err := userKR.Encrypt(message, nil)
	if err != nil {
		t.Fatalf("seal the token: %v", err)
	}
	if sealed, err = encrypted.GetArmored(); err != nil {
		t.Fatalf("armor the token: %v", err)
	}
	sig, err := userKR.SignDetached(message)
	if err != nil {
		t.Fatalf("sign the token: %v", err)
	}
	if signature, err = sig.GetArmored(); err != nil {
		t.Fatalf("armor the token's signature: %v", err)
	}
	return sealed, signature
}

func publishedKeyList(t *testing.T, key *pgp.Key) string {
	t.Helper()
	data, err := json.Marshal([]map[string]any{{
		"Primary": 1, "Flags": 3,
		"Fingerprint": key.GetFingerprint(), "SHA256Fingerprints": key.GetSHA256Fingerprints(),
	}})
	if err != nil {
		t.Fatalf("marshal the key list: %v", err)
	}
	return string(data)
}

func publicArmor(t *testing.T, kr *pgp.KeyRing) string {
	t.Helper()
	public, err := kr.GetKeys()[0].ToPublic()
	if err != nil {
		t.Fatalf("take the public half: %v", err)
	}
	armored, err := public.Armor()
	if err != nil {
		t.Fatalf("armor the public key: %v", err)
	}
	return armored
}

// forwardingAPI answers the requests both ends of a forwarding make: what
// Proton publishes for an address, and the writes each end sends.
type forwardingAPI struct {
	published map[string]string
	requests  []proton.Request
}

func (a *forwardingAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *forwardingAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	answer := `{"Code":1000}`
	switch req.Path {
	case "/core/v4/keys/all":
		armored, ok := a.published[strings.ToLower(req.Query.Get("Email"))]
		if !ok {
			return &proton.APIError{HTTPStatus: 422, Code: 33103, Message: "no such address"}
		}
		answer = fmt.Sprintf(`{"Code":1000,"Address":{"Keys":[{"PublicKey":%q,"Primary":1}]}}`, armored)
	case "/core/v4/keys/address":
		answer = `{"Code":1000,"Key":{"ID":"published-key"}}`
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(answer), out)
}

func (a *forwardingAPI) sent(method, path string) []proton.Request {
	var out []proton.Request
	for _, req := range a.requests {
		if req.Method == method && req.Path == path {
			out = append(out, req)
		}
	}
	return out
}

func serviceFor(p *party, api *forwardingAPI) *Service {
	return New(api, func(context.Context) (*keys.Unlocked, error) { return p.unlocked, nil })
}

// offered is what a forwarder sends: the derived key, and its passphrase sealed
// to the forwardee and signed as the forwarder.
func offered(t *testing.T, forwarder, forwardee *party, signAs *pgp.KeyRing, addressed string) apiForwardingKey {
	t.Helper()
	entity := forwarder.kr.GetKeys()[0].GetEntity()
	derived, err := deriveForwarding(entity, addressed)
	if err != nil {
		t.Fatalf("derive the forwarding key: %v", err)
	}
	token, err := sealPassphrase(derived.passphrase, signAs, forwardee.kr)
	if err != nil {
		t.Fatalf("seal the passphrase: %v", err)
	}
	return apiForwardingKey{ActivationToken: token, PrivateKey: derived.armoredKey}
}

func incoming(forwardee *party, sent ...apiForwardingKey) Forwarding {
	return Forwarding{
		ID: "forwarding-id", Direction: DirectionIncoming, From: forwarderEmail, To: forwardee.addr.Email,
		State: StatePending, Encrypted: true, addressID: forwardee.addr.ID, keys: sent,
	}
}

// The whole round trip: what one account derived opens on the other, and goes
// up locked under the forwardee's own token.
func TestForwardingAcceptPublishesWhatTheForwarderDerived(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{
		forwarderEmail: publicArmor(t, forwarder.kr),
		forwardeeEmail: publicArmor(t, forwardee.kr),
	}}
	sent := offered(t, forwarder, forwardee, forwarder.kr, forwardeeEmail)

	if err := serviceFor(forwardee, api).ForwardingAccept(context.Background(), incoming(forwardee, sent)); err != nil {
		t.Fatalf("ForwardingAccept: %v", err)
	}

	writes := api.sent("POST", "/core/v4/keys/address")
	if len(writes) != 1 {
		t.Fatalf("published %d keys, want 1", len(writes))
	}
	published, _ := writes[0].Body.(map[string]any)
	if published["AddressForwardingID"] != "forwarding-id" {
		t.Errorf("the published key names %v, not the forwarding", published["AddressForwardingID"])
	}
	armored, _ := published["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	if _, err := key.Unlock([]byte(forwardee.token)); err != nil {
		t.Errorf("the published key does not open with the forwardee's own token: %v", err)
	}
}

// A forwarder who changed their primary key while the forwarding waited leaves
// a key per primary, and accepting publishes every one of them.
func TestForwardingAcceptPublishesEveryKeyTheForwardingCarries(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{forwarderEmail: publicArmor(t, forwarder.kr)}}
	first := offered(t, forwarder, forwardee, forwarder.kr, forwardeeEmail)
	second := offered(t, forwarder, forwardee, forwarder.kr, forwardeeEmail)

	if err := serviceFor(forwardee, api).ForwardingAccept(
		context.Background(), incoming(forwardee, first, second),
	); err != nil {
		t.Fatalf("ForwardingAccept: %v", err)
	}
	if writes := api.sent("POST", "/core/v4/keys/address"); len(writes) != 2 {
		t.Fatalf("published %d keys, want 2", len(writes))
	}
}

// The signature over the passphrase is what says who sent the key. One by
// anybody else is refused, and nothing is published.
func TestForwardingAcceptRefusesAKeySignedBySomebodyElse(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	stranger := newParty(t, "stranger@proton.me")
	api := &forwardingAPI{published: map[string]string{forwarderEmail: publicArmor(t, forwarder.kr)}}
	sent := offered(t, forwarder, forwardee, stranger.kr, forwardeeEmail)

	err := serviceFor(forwardee, api).ForwardingAccept(context.Background(), incoming(forwardee, sent))
	if err == nil {
		t.Fatal("a key signed by a stranger was accepted")
	}
	if !strings.Contains(err.Error(), forwarderEmail) {
		t.Errorf("the refusal does not name who it was expecting: %v", err)
	}
	if writes := api.sent("POST", "/core/v4/keys/address"); len(writes) != 0 {
		t.Errorf("published %d keys despite refusing", len(writes))
	}
}

// The forwarder typed the address when they set it up, and Proton holds the
// spelling that counts. A key naming anything else is renamed before it is
// published, capitalisation included.
func TestForwardingAcceptNamesThePublishedKeyAfterTheAddress(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{forwarderEmail: publicArmor(t, forwarder.kr)}}
	sent := offered(t, forwarder, forwardee, forwarder.kr, strings.ToUpper(forwardeeEmail))

	if err := serviceFor(forwardee, api).ForwardingAccept(context.Background(), incoming(forwardee, sent)); err != nil {
		t.Fatalf("ForwardingAccept: %v", err)
	}

	published, _ := api.sent("POST", "/core/v4/keys/address")[0].Body.(map[string]any)
	armored, _ := published["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	identities := key.GetEntity().Identities
	if len(identities) != 1 {
		t.Fatalf("the published key carries %d user IDs, want 1", len(identities))
	}
	if got := key.GetEntity().PrimaryIdentity().UserId.Email; got != forwardeeEmail {
		t.Errorf("the published key is addressed to %q, want %q", got, forwardeeEmail)
	}
}

// A forwarding that carries no key is nothing to accept, and says so without
// asking Proton anything.
func TestForwardingAcceptRefusesAForwardingWithNoKey(t *testing.T) {
	forwardee := newParty(t, forwardeeEmail)
	api := &forwardingAPI{}

	err := serviceFor(forwardee, api).ForwardingAccept(context.Background(), incoming(forwardee))
	if err == nil {
		t.Fatal("a forwarding carrying no key was accepted")
	}
	if len(api.requests) != 0 {
		t.Errorf("sent %d requests despite there being nothing to accept", len(api.requests))
	}
}

// Asking a Proton forwardee again is a fresh offer, not a nudge: the material
// is derived now, so the one their client answers is the one this address's key
// can serve.
func TestForwardingResendDerivesFreshMaterialForAProtonForwardee(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{forwardeeEmail: publicArmor(t, forwardee.kr)}}
	f := Forwarding{
		ID: "forwarding-id", Direction: DirectionOutgoing, From: forwarderEmail, To: forwardeeEmail,
		State: StateRejected, Encrypted: true, addressID: forwarder.addr.ID,
	}

	if err := serviceFor(forwarder, api).ForwardingResend(context.Background(), f); err != nil {
		t.Fatalf("ForwardingResend: %v", err)
	}

	writes := api.sent("PUT", "/mail/v4/forwardings/forwarding-id")
	if len(writes) != 1 {
		t.Fatalf("sent %d updates, want 1", len(writes))
	}
	body, _ := writes[0].Body.(map[string]any)
	for _, field := range []string{"ActivationToken", "ForwardeePrivateKey", "ProxyInstances"} {
		if body[field] == nil {
			t.Errorf("the update carries no %s", field)
		}
	}
	if len(api.sent("PUT", "/mail/v4/forwardings/forwarding-id/reinvite")) != 0 {
		t.Error("a Proton forwardee was sent the email meant for an address outside Proton")
	}
}

// A forwardee outside Proton has no key and no client, so the only thing to
// send again is the email Proton asks its owner to answer.
func TestForwardingResendEmailsAForwardeeOutsideProton(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	api := &forwardingAPI{}
	f := Forwarding{
		ID: "forwarding-id", Direction: DirectionOutgoing, From: forwarderEmail, To: "somebody@example.com",
		State: StatePending, Encrypted: false, addressID: forwarder.addr.ID,
	}

	if err := serviceFor(forwarder, api).ForwardingResend(context.Background(), f); err != nil {
		t.Fatalf("ForwardingResend: %v", err)
	}
	if len(api.sent("PUT", "/mail/v4/forwardings/forwarding-id/reinvite")) != 1 {
		t.Error("the confirmation email was not sent again")
	}
	if len(api.sent("PUT", "/mail/v4/forwardings/forwarding-id")) != 0 {
		t.Error("material was derived for an address that has no Proton key")
	}
}

// Setting one up and asking again both send the same three fields, because both
// are the same offer. The derivation is what produces them, and it produces one
// for every encryption subkey the address has.
func TestForwardingMaterialCarriesWhatARequestNeeds(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{forwardeeEmail: publicArmor(t, forwardee.kr)}}

	material, err := serviceFor(forwarder, api).forwardingMaterial(
		context.Background(), forwarder.unlocked, forwarder.addr, forwardeeEmail)
	if err != nil {
		t.Fatalf("forwardingMaterial: %v", err)
	}
	if material.privateKey == "" || material.activationToken == "" {
		t.Error("the material carries no key or no token")
	}
	if len(material.proxyInstances) == 0 {
		t.Fatal("the material carries no proxy parameters, so Proton could re-wrap nothing")
	}
	for _, field := range []string{"ForwarderKeyFingerprint", "ForwardeeKeyFingerprint", "ProxyParam", "PgpVersion"} {
		if material.proxyInstances[0][field] == nil {
			t.Errorf("a proxy parameter carries no %s", field)
		}
	}
}

// An address with no Proton key on the other end is refused where the material
// would be derived, so the same sentence answers setting one up and asking
// again.
func TestForwardingMaterialRefusesAnAddressOutsideProton(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	api := &forwardingAPI{}

	_, err := serviceFor(forwarder, api).forwardingMaterial(
		context.Background(), forwarder.unlocked, forwarder.addr, "somebody@example.com")
	if err == nil {
		t.Fatal("an address outside Proton was forwarded to end-to-end")
	}
	if !strings.Contains(err.Error(), "not a Proton address") {
		t.Errorf("the refusal does not name the address as the problem: %v", err)
	}
}

// A forwardee key says it may be used for forwarded communications and nothing
// else, which is what the accounts that answer one write. A key that also called
// itself split - as the derivation leaves it - is one Proton refuses.
func TestDerivedForwardingKeySaysOnlyThatItForwards(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)

	derived, err := deriveForwarding(forwarder.kr.GetKeys()[0].GetEntity(), forwardeeEmail)
	if err != nil {
		t.Fatalf("deriveForwarding: %v", err)
	}
	// Read back as it is sent, since that is the only form anybody else sees.
	key, err := pgp.NewKeyFromArmored(derived.armoredKey)
	if err != nil {
		t.Fatalf("the derived key is not readable armour: %v", err)
	}
	if !key.IsForwardingKey() {
		t.Fatal("the derived key does not read as a forwarding key")
	}
	var forwarding int
	for _, sub := range key.GetEntity().Subkeys {
		if sub.Sig == nil || !sub.Sig.FlagForward {
			continue
		}
		forwarding++
		if sub.Sig.FlagSplitKey {
			t.Error("the derived key says its private half was split, which no other client writes")
		}
		if sub.Sig.FlagEncryptCommunications || sub.Sig.FlagEncryptStorage || sub.Sig.FlagSign {
			t.Errorf("the derived key claims more than forwarding: %+v", sub.Sig)
		}
	}
	if forwarding == 0 {
		t.Error("the derived key carries no subkey for forwarded communications")
	}
}
