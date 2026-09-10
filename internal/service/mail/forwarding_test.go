package mail

import (
	"context"
	"crypto"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
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
	return newPartyAt(t, email, time.Now)
}

// newPartyAt is one account whose keys are dated by a clock the test names,
// which is how what a key was written under can be read back off it.
func newPartyAt(t *testing.T, email string, now func() time.Time) *party {
	t.Helper()
	userKey := freshKeyAt(t, "user", now)
	userKR, err := pgp.NewKeyRing(userKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	token := "the-address-key-token-of-" + email
	sealed, signature := sealedToken(t, userKR, token)

	addrKey := freshKeyAt(t, email, now)
	addrKR, err := pgp.NewKeyRing(addrKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	addr := keys.Address{
		ID: "address-of-" + email, Email: email, Status: 1, Send: 1, Receive: 1,
		ProtonMX: true,
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
			AddrKRs:  map[string]keys.Rings{addr.ID: {Read: addrKR, Write: addrKR}},
			PaidMail: true, Now: now,
		},
		addr:  addr,
		kr:    addrKR,
		token: token,
	}
}

func freshKey(t *testing.T, name string) *pgp.Key {
	t.Helper()
	return freshKeyAt(t, name, time.Now)
}

// freshKeyAt is a key of the shape this build writes, dated by a clock the test
// names - which is what makes an account whose keys are older than the run.
func freshKeyAt(t *testing.T, name string, now func() time.Time) *pgp.Key {
	t.Helper()
	entity, err := openpgp.NewEntity(name, "", name, (&keys.Unlocked{Now: now}).Generation())
	if err != nil {
		t.Fatalf("generate a key for %s: %v", name, err)
	}
	key, err := pgp.NewKeyFromEntity(entity)
	if err != nil {
		t.Fatalf("read the key for %s: %v", name, err)
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

func publishedKeyList(t *testing.T, held ...*pgp.Key) string {
	t.Helper()
	var items []map[string]any
	for i, key := range held {
		primary := 0
		if i == 0 {
			primary = 1
		}
		items = append(items, map[string]any{
			"Primary": primary, "Flags": 3,
			"Fingerprint": key.GetFingerprint(), "SHA256Fingerprints": key.GetSHA256Fingerprints(),
		})
	}
	data, err := json.Marshal(items)
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
// Proton publishes for an address, what forwardings it already holds, and the
// writes each end sends.
type forwardingAPI struct {
	published map[string]string
	// outgoing is what the account already forwards, for the tests about what
	// deleting one of several means.
	outgoing []apiForwarding
	// refuse names the paths Proton turns down, so a half-finished run can be
	// watched putting itself back.
	refuse   map[string]bool
	requests []proton.Request
}

func (a *forwardingAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *forwardingAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	if a.refuse[req.Path] {
		return &proton.APIError{HTTPStatus: 422, Code: 2001, Message: "Proton says no"}
	}
	answer := `{"Code":1000}`
	switch req.Path {
	case "/core/v4/addresses":
		answer = `{"Code":1000,"Addresses":[]}`
	case "/core/v4/keys/all":
		armored, ok := a.published[strings.ToLower(req.Query.Get("Email"))]
		if !ok {
			return &proton.APIError{HTTPStatus: 422, Code: 33103, Message: "no such address"}
		}
		answer = fmt.Sprintf(`{"Code":1000,"Address":{"Keys":[{"PublicKey":%q,"Primary":1}]}}`, armored)
	case "/core/v4/keys/address":
		answer = `{"Code":1000,"Key":{"ID":"published-key"}}`
	case "/mail/v4/forwardings":
		answer = `{"Code":1000,"OutgoingAddressForwarding":{"ID":"new-forwarding"}}`
	case "/mail/v4/forwardings/outgoing":
		listing, err := json.Marshal(map[string]any{"OutgoingAddressForwardings": a.outgoing})
		if err != nil {
			return err
		}
		answer = string(listing)
	case "/mail/v4/forwardings/incoming":
		answer = `{"Code":1000,"IncomingAddressForwardings":[]}`
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

// order is where one request came in the run, or -1, for the tests whose
// subject is which of two happened first.
func (a *forwardingAPI) order(method, path string) int {
	for i, req := range a.requests {
		if req.Method == method && req.Path == path {
			return i
		}
	}
	return -1
}

func serviceFor(p *party, api *forwardingAPI) *Service {
	return New(api, func(context.Context) (*keys.Unlocked, error) { return p.unlocked, nil })
}

// offered is what a forwarder sends: the derived key, and its passphrase sealed
// to the forwardee and signed as the forwarder.
func offered(t *testing.T, forwarder, forwardee *party, signAs *pgp.KeyRing, addressed string) apiForwardingKey {
	t.Helper()
	entity := forwarder.kr.GetKeys()[0].GetEntity()
	derived, err := deriveForwarding(entity, addressed, forwarder.unlocked.Generation())
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
		context.Background(), forwarder.unlocked, forwarder.addr, forwardeeEmail, forwardee.kr)
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

// Which kind of forwarding it would be is what the forwardee decides, and it is
// settled before anything is sent: a Proton address has a key to derive from,
// and an address outside Proton has none.
func TestForwardingOfferReadsTheKindOffTheForwardee(t *testing.T) {
	forwarder, forwardee := newParty(t, forwarderEmail), newParty(t, forwardeeEmail)
	api := &forwardingAPI{published: map[string]string{forwardeeEmail: publicArmor(t, forwardee.kr)}}
	svc := serviceFor(forwarder, api)

	for _, want := range []struct {
		to        string
		encrypted bool
	}{{forwardeeEmail, true}, {"somebody@example.com", false}} {
		offer, err := svc.ForwardingOffer(context.Background(), forwarderEmail, want.to)
		if err != nil {
			t.Fatalf("ForwardingOffer(%s): %v", want.to, err)
		}
		if offer.Encrypted != want.encrypted {
			t.Errorf("forwarding to %s reads as encrypted=%v, want %v", want.to, offer.Encrypted, want.encrypted)
		}
		if len(api.sent("POST", "/mail/v4/forwardings")) != 0 {
			t.Fatal("judging what a forwarding would be set one up")
		}
	}
}

// The plan is Proton's to gate and this account's to be asked about, so a free
// account is refused before its address is touched.
func TestForwardingOfferRefusesAnAccountWithoutPaidMail(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	forwarder.unlocked.PaidMail = false
	api := &forwardingAPI{}

	_, err := serviceFor(forwarder, api).ForwardingOffer(
		context.Background(), forwarderEmail, "somebody@example.com")
	if err == nil {
		t.Fatal("a free account was allowed to set a forwarding up")
	}
	if !strings.Contains(err.Error(), "paid Mail plan") {
		t.Errorf("the refusal does not name the plan as the problem: %v", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("sent %d requests for an account that may not forward at all", len(api.requests))
	}
}

// An address forwarding to somewhere outside Proton gives up its own end-to-end
// encryption, and the order is what makes that safe to watch: encryption comes
// off, then the arrangement is asked for.
func TestForwardingCreateTurnsEncryptionOffForAnAddressOutsideProton(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	api := &forwardingAPI{}
	svc := serviceFor(forwarder, api)

	offer, err := svc.ForwardingOffer(context.Background(), forwarderEmail, "somebody@example.com")
	if err != nil {
		t.Fatalf("ForwardingOffer: %v", err)
	}
	if _, err := svc.ForwardingCreate(context.Background(), offer); err != nil {
		t.Fatalf("ForwardingCreate: %v", err)
	}

	encryption := api.order("PUT", "/core/v4/addresses/"+forwarder.addr.ID+"/encryption")
	setup := api.order("POST", "/mail/v4/forwardings")
	if encryption < 0 || setup < 0 || encryption > setup {
		t.Fatalf("encryption came off at %d and the forwarding was asked for at %d", encryption, setup)
	}
	off, _ := api.requests[encryption].Body.(map[string]any)
	if off["Encrypt"] != 0 {
		t.Errorf("the address was left encrypted: %v", off["Encrypt"])
	}
	if off["Sign"] != 1 {
		t.Errorf("whether a signature is expected was changed as well: %v", off["Sign"])
	}
	listed, _ := off["SignedKeyList"].(map[string]string)
	if !strings.Contains(listed["Data"], `"Flags":7`) {
		t.Errorf("the published key list does not say the address is unencrypted: %s", listed["Data"])
	}
	body, _ := api.requests[setup].Body.(map[string]any)
	if body["Type"] != forwardingExternalPlain {
		t.Errorf("the forwarding was set up as type %v, want the unencrypted kind", body["Type"])
	}
	for _, field := range []string{"ActivationToken", "ForwardeePrivateKey", "ProxyInstances"} {
		if _, ok := body[field]; ok {
			t.Errorf("the request carries %s for a forwardee that holds no key", field)
		}
	}
}

// A request Proton refuses would otherwise leave the address unencrypted for
// nothing, so what was turned off is turned back on.
func TestForwardingCreateTurnsEncryptionBackOnWhenProtonRefuses(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	api := &forwardingAPI{refuse: map[string]bool{"/mail/v4/forwardings": true}}
	svc := serviceFor(forwarder, api)

	offer, err := svc.ForwardingOffer(context.Background(), forwarderEmail, "somebody@example.com")
	if err != nil {
		t.Fatalf("ForwardingOffer: %v", err)
	}
	if _, err := svc.ForwardingCreate(context.Background(), offer); err == nil {
		t.Fatal("a refused forwarding was reported as set up")
	}

	writes := api.sent("PUT", "/core/v4/addresses/"+forwarder.addr.ID+"/encryption")
	if len(writes) != 2 {
		t.Fatalf("the address's encryption was written %d times, want it off and back on", len(writes))
	}
	back, _ := writes[1].Body.(map[string]any)
	if back["Encrypt"] != 1 {
		t.Errorf("the address was left unencrypted after the refusal: %v", back["Encrypt"])
	}
}

// Taking down the last forwarding to an address outside Proton is what makes the
// forwarder's address end-to-end encrypted again.
func TestForwardingDeleteTurnsEncryptionBackOn(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	forwarder.unlocked.Addresses[0].Flags = 16
	api := &forwardingAPI{}
	f := Forwarding{
		ID: "forwarding-id", Direction: DirectionOutgoing, From: forwarderEmail, To: "somebody@example.com",
		State: StateActive, Encrypted: false, addressID: forwarder.addr.ID,
	}

	if err := serviceFor(forwarder, api).ForwardingDelete(context.Background(), f); err != nil {
		t.Fatalf("ForwardingDelete: %v", err)
	}

	writes := api.sent("PUT", "/core/v4/addresses/"+forwarder.addr.ID+"/encryption")
	if len(writes) != 1 {
		t.Fatalf("the address's encryption was written %d times, want it turned back on", len(writes))
	}
	body, _ := writes[0].Body.(map[string]any)
	if body["Encrypt"] != 1 {
		t.Errorf("the address was left unencrypted: %v", body["Encrypt"])
	}
	listed, _ := body["SignedKeyList"].(map[string]string)
	if !strings.Contains(listed["Data"], `"Flags":3`) {
		t.Errorf("the published key list does not say the address is encrypted again: %s", listed["Data"])
	}
}

// Only that one deletion does. A forwarding to another Proton account never
// turned encryption off, one sent to you is not this account's to change, and an
// address that still forwards somewhere outside Proton still needs it off.
func TestForwardingDeleteLeavesEncryptionAloneOtherwise(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	forwarder.unlocked.Addresses[0].Flags = 16
	addressID := forwarder.addr.ID
	for _, f := range []Forwarding{{
		ID: "encrypted", Direction: DirectionOutgoing, From: forwarderEmail, To: forwardeeEmail,
		State: StateActive, Encrypted: true, addressID: addressID,
	}, {
		ID: "incoming", Direction: DirectionIncoming, From: forwardeeEmail, To: forwarderEmail,
		State: StatePending, Encrypted: false, addressID: addressID,
	}, {
		ID: "one-of-two", Direction: DirectionOutgoing, From: forwarderEmail, To: "somebody@example.com",
		State: StateActive, Encrypted: false, addressID: addressID,
	}} {
		api := &forwardingAPI{outgoing: []apiForwarding{{
			ID: "the-other-one", Type: forwardingExternalPlain, ForwarderAddressID: addressID,
			ForwardeeEmail: "somebody-else@example.com",
		}}}
		if err := serviceFor(forwarder, api).ForwardingDelete(context.Background(), f); err != nil {
			t.Fatalf("ForwardingDelete(%s): %v", f.ID, err)
		}
		if writes := api.sent("PUT", "/core/v4/addresses/"+addressID+"/encryption"); len(writes) != 0 {
			t.Errorf("deleting the %s forwarding wrote the address's encryption", f.ID)
		}
	}
}

// A forwardee key says it may be used for forwarded communications and nothing
// else, which is what the accounts that answer one write. A key that also called
// itself split - as the derivation leaves it - is one Proton refuses.
func TestDerivedForwardingKeySaysOnlyThatItForwards(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)

	key := derivedKeyOf(t, forwarder, forwardeeEmail)
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

// The rest of what a key says about itself is not the scheme's to choose, and
// Proton refuses a key that chose differently from its own clients: what a
// signature is hashed with, what the key says it prefers, and what it is dated
// by. All three are read back off the key as it is sent.
func TestDerivedForwardingKeyIsShapedLikeAProtonKey(t *testing.T) {
	dated := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	forwarder := newPartyAt(t, forwarderEmail, func() time.Time { return dated })

	key := derivedKeyOf(t, forwarder, forwardeeEmail)
	entity := key.GetEntity()
	if got := entity.PrimaryKey.CreationTime; !got.Equal(dated) {
		t.Errorf("the derived key is dated %s, want Proton's clock at %s", got, dated)
	}
	identity := entity.PrimaryIdentity()
	if identity == nil || identity.SelfSignature == nil {
		t.Fatal("the derived key carries no self-signature to read")
	}
	for what, sig := range map[string]*packet.Signature{
		"the key's own signature": identity.SelfSignature,
		"the subkey binding":      forwardingBinding(t, entity),
	} {
		if sig.Hash != crypto.SHA512 {
			t.Errorf("%s is hashed with %v, want SHA-512 as Proton's clients write", what, sig.Hash)
		}
	}
	if got := identity.SelfSignature.PreferredHash; !slices.Equal(got, []uint8{10, 8}) {
		t.Errorf("the derived key prefers hashes %v, want SHA-512 then SHA-256", got)
	}
	if got := identity.SelfSignature.PreferredSymmetric; !slices.Equal(got, []uint8{9, 7}) {
		t.Errorf("the derived key prefers ciphers %v, want AES-256 then AES-128", got)
	}
	if strings.Contains(armoredKeyOf(t, forwarder, forwardeeEmail), "Comment:") {
		t.Error("the derived key is armoured with headers, which Proton's clients do not write")
	}
}

// A forwarding is derived from the key the address writes with, not from
// whichever of its keys happened to unlock first: the server re-wraps with the
// primary, so a key derived from a retired one would forward nothing.
func TestDerivedForwardingKeyComesFromThePrimaryKey(t *testing.T) {
	forwarder := newParty(t, forwarderEmail)
	retired := freshKey(t, "retired")
	primary := forwarder.kr.GetKeys()[0]
	if err := forwarder.kr.AddKey(retired); err != nil {
		t.Fatalf("add the retired key to the ring: %v", err)
	}
	// Proton serves the records in its own order, and the ring holds the keys in
	// whichever order they opened; only the record says which is primary.
	addr := &forwarder.unlocked.Addresses[0]
	addr.Keys = append([]keys.Key{{
		ID: "retired-key", PrivateKey: armoredLocked(t, retired, forwarder.token),
		Primary: 0, Active: 1,
	}}, addr.Keys...)
	addr.SignedKeyList = &keys.SignedKeyList{
		Data: publishedKeyList(t, primary, retired), Signature: "the signature Proton holds",
	}

	entity, err := forwardingEntity(forwarder.unlocked, *addr)
	if err != nil {
		t.Fatalf("forwardingEntity: %v", err)
	}
	if got := entity.PrimaryKey.KeyIdString(); got != primary.GetEntity().PrimaryKey.KeyIdString() {
		t.Errorf("the forwarding would be derived from %s, want the address's primary key", got)
	}
}

// derivedKeyOf is a forwardee key as anybody else receives it: read back from
// the armour, which is the only form that leaves this process.
func derivedKeyOf(t *testing.T, forwarder *party, forwardee string) *pgp.Key {
	t.Helper()
	key, err := pgp.NewKeyFromArmored(armoredKeyOf(t, forwarder, forwardee))
	if err != nil {
		t.Fatalf("the derived key is not readable armour: %v", err)
	}
	return key
}

func armoredKeyOf(t *testing.T, forwarder *party, forwardee string) string {
	t.Helper()
	derived, err := deriveForwarding(
		forwarder.kr.GetKeys()[0].GetEntity(), forwardee, forwarder.unlocked.Generation())
	if err != nil {
		t.Fatalf("deriveForwarding: %v", err)
	}
	return derived.armoredKey
}

// forwardingBinding is what binds the derived key's encryption subkey to it,
// which is the second signature the key carries and the other place its shape
// shows.
func forwardingBinding(t *testing.T, entity *openpgp.Entity) *packet.Signature {
	t.Helper()
	for _, sub := range entity.Subkeys {
		if sub.Sig != nil && sub.Sig.FlagForward {
			return sub.Sig
		}
	}
	t.Fatal("the derived key carries no subkey for forwarded communications")
	return nil
}
