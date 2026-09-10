package keys

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp/packet"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Publishing a key somebody else derived is the one write proton makes to a key
// hierarchy, so what it sends is pinned here: the token it reuses, the key that
// has to open with it, and the key list that has to go up unchanged under a
// signature the account itself can check.

// recorder answers as Proton would and keeps what it was asked.
type recorder struct{ requests []proton.Request }

func (r *recorder) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	r.requests = append(r.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (r *recorder) Decode(_ context.Context, req proton.Request, out any) error {
	r.requests = append(r.requests, req)
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(`{"Code":1000,"Key":{"ID":"published-key"}}`), out)
}

// hierarchy is an account as AddForwardingKey needs it: a user key, an address
// whose keys are locked with a token sealed to it, and the key list Proton
// publishes for that address.
type hierarchy struct {
	u     *Unlocked
	addr  Address
	token string
}

func newHierarchy(t *testing.T, primaries int) *hierarchy {
	t.Helper()
	userKey := generated(t, "user")
	userKR, err := pgp.NewKeyRing(userKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	token := "the-address-key-token"
	sealed, signature := sealToken(t, userKR, token)

	addr := Address{ID: "address", Email: "me@proton.me"}
	addrKR, err := pgp.NewKeyRing(nil)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	for i := range primaries {
		key := generated(t, "address")
		addr.Keys = append(addr.Keys, Key{
			ID: string(rune('a' + i)), PrivateKey: locked(t, key, token),
			Token: sealed, Signature: signature, Primary: 1, Active: 1,
		})
		if err := addrKR.AddKey(key); err != nil {
			t.Fatalf("AddKey: %v", err)
		}
	}
	addr.SignedKeyList = &SignedKeyList{Data: keyList(t, addr), Signature: "the signature Proton holds"}

	return &hierarchy{
		u: &Unlocked{
			UserKR: userKR, Addresses: []Address{addr},
			AddrKRs: map[string]Rings{addr.ID: {Read: addrKR, Write: addrKR}},
		},
		addr:  addr,
		token: token,
	}
}

func generated(t *testing.T, name string) *pgp.Key {
	t.Helper()
	key, err := pgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("generate the %s key: %v", name, err)
	}
	return key
}

func locked(t *testing.T, key *pgp.Key, passphrase string) string {
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

// sealToken seals an address key's passphrase to the user key and signs it as
// the user key, which is the shape decryptToken opens.
func sealToken(t *testing.T, userKR *pgp.KeyRing, token string) (sealed, signature string) {
	t.Helper()
	message := pgp.NewPlainMessageFromString(token)
	encrypted, err := userKR.Encrypt(message, nil)
	if err != nil {
		t.Fatalf("seal the token: %v", err)
	}
	sealed, err = encrypted.GetArmored()
	if err != nil {
		t.Fatalf("armor the token: %v", err)
	}
	sig, err := userKR.SignDetached(message)
	if err != nil {
		t.Fatalf("sign the token: %v", err)
	}
	signature, err = sig.GetArmored()
	if err != nil {
		t.Fatalf("armor the token's signature: %v", err)
	}
	return sealed, signature
}

// keyList is what Proton publishes for the address: one entry per key it holds,
// named by the fingerprints of the key and its subkeys.
func keyList(t *testing.T, addr Address) string {
	t.Helper()
	var items []map[string]any
	for i, record := range addr.Keys {
		key, err := pgp.NewKeyFromArmored(record.PrivateKey)
		if err != nil {
			t.Fatalf("read the address key: %v", err)
		}
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

// body is the request AddForwardingKey sent, or a failure saying it sent none.
func body(t *testing.T, r *recorder) map[string]any {
	t.Helper()
	if len(r.requests) != 1 {
		t.Fatalf("sent %d requests, want 1", len(r.requests))
	}
	req := r.requests[0]
	if req.Method != "POST" || req.Path != "/core/v4/keys/address" {
		t.Fatalf("sent %s %s", req.Method, req.Path)
	}
	out, ok := req.Body.(map[string]any)
	if !ok {
		t.Fatalf("request body is %T, want a map", req.Body)
	}
	return out
}

// The key goes up under the token the address already uses, so the account
// opens it afterwards with what it already has - and the token itself is handed
// back exactly as Proton holds it, because proton seals none of its own.
func TestAddForwardingKeyLocksWithTheAddressKeysOwnToken(t *testing.T) {
	h := newHierarchy(t, 1)
	r := &recorder{}

	id, err := h.u.AddForwardingKey(context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id")
	if err != nil {
		t.Fatalf("AddForwardingKey: %v", err)
	}
	if id != "published-key" {
		t.Errorf("published key ID = %q, want the one Proton answered with", id)
	}

	sent := body(t, r)
	if sent["Token"] != h.addr.Keys[0].Token || sent["Signature"] != h.addr.Keys[0].Signature {
		t.Error("the request does not carry the address key's own token and signature")
	}
	if sent["AddressID"] != h.addr.ID || sent["AddressForwardingID"] != "forwarding-id" {
		t.Errorf("the request names %v and %v", sent["AddressID"], sent["AddressForwardingID"])
	}
	if sent["Primary"] != 0 {
		t.Errorf("Primary = %v, want 0: a forwarding key is not what an address writes with", sent["Primary"])
	}

	armored, _ := sent["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	if _, err := key.Unlock([]byte(h.token)); err != nil {
		t.Errorf("the published key does not open with the address's token: %v", err)
	}
}

// The list goes up as Proton serves it, under a fresh signature: a forwarding
// key is never listed, so adding one leaves the list saying what it said - and
// re-signing bytes this build did not compose is what keeps it from publishing
// its own opinion of somebody's keys.
func TestAddForwardingKeyRepublishesTheServedKeyList(t *testing.T) {
	h := newHierarchy(t, 1)
	r := &recorder{}

	if _, err := h.u.AddForwardingKey(
		context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id",
	); err != nil {
		t.Fatalf("AddForwardingKey: %v", err)
	}

	skl, ok := body(t, r)["SignedKeyList"].(map[string]string)
	if !ok {
		t.Fatalf("the request carries no key list")
	}
	if skl["Data"] != h.addr.SignedKeyList.Data {
		t.Errorf("the published data is not what Proton served:\n got %s\nwant %s",
			skl["Data"], h.addr.SignedKeyList.Data)
	}
	if skl["Signature"] == h.addr.SignedKeyList.Signature {
		t.Error("the list went up under the signature it arrived with, which this account did not make")
	}
	assertSignedByAll(t, h, skl["Data"], skl["Signature"], 1)
}

// Both primary keys sign. One would satisfy an audit; which of them a reader
// checks is not this end's to predict, and Proton's own clients write both.
func TestAddForwardingKeySignsTheKeyListWithEveryPrimaryKey(t *testing.T) {
	h := newHierarchy(t, 2)
	r := &recorder{}

	if _, err := h.u.AddForwardingKey(
		context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id",
	); err != nil {
		t.Fatalf("AddForwardingKey: %v", err)
	}

	skl, _ := body(t, r)["SignedKeyList"].(map[string]string)
	assertSignedByAll(t, h, skl["Data"], skl["Signature"], 2)
}

// assertSignedByAll checks the signature against every key of the address and
// counts the packets in it, which is what a several-signature detached
// signature is.
func assertSignedByAll(t *testing.T, h *hierarchy, data, armored string, want int) {
	t.Helper()
	signature, err := pgp.NewPGPSignatureFromArmored(armored)
	if err != nil {
		t.Fatalf("the signature is not readable armour: %v", err)
	}
	if got := signaturePackets(t, signature.GetBinary()); got != want {
		t.Errorf("the signature holds %d packets, want %d", got, want)
	}
	message := pgp.NewPlainMessageFromString(data)
	context := pgp.NewVerificationContext(sklSigningContext, true, 0)
	for _, key := range h.u.AddrKRs[h.addr.ID].Read.GetKeys() {
		kr, err := pgp.NewKeyRing(key)
		if err != nil {
			t.Fatalf("NewKeyRing: %v", err)
		}
		if err := kr.VerifyDetachedWithContext(message, signature, pgp.GetUnixTime(), context); err != nil {
			t.Errorf("the key list is not signed by one of the address's primary keys: %v", err)
		}
	}
}

func signaturePackets(t *testing.T, binary []byte) int {
	t.Helper()
	reader := packet.NewReader(bytes.NewReader(binary))
	var count int
	for {
		p, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return count
		}
		if err != nil {
			t.Fatalf("read the signature: %v", err)
		}
		if _, ok := p.(*packet.Signature); ok {
			count++
		}
	}
}

// A list that does not name a key the account holds is not a list this may sign.
// Whatever left the two disagreeing, signing it would be this build asserting
// something about somebody's keys that it has no way to check.
func TestAddForwardingKeyRefusesAKeyListThatDoesNotDescribeTheAddress(t *testing.T) {
	h := newHierarchy(t, 2)
	// The list Proton served names one key; the address holds two.
	shortened := h.addr
	shortened.Keys = h.addr.Keys[:1]
	h.addr.SignedKeyList = &SignedKeyList{Data: keyList(t, shortened), Signature: "the signature Proton holds"}
	r := &recorder{}

	_, err := h.u.AddForwardingKey(context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id")
	if err == nil {
		t.Fatal("a key list that understates the address was signed")
	}
	if !strings.Contains(err.Error(), "does not describe the address") {
		t.Errorf("the refusal does not say what disagreed: %v", err)
	}
	if len(r.requests) != 0 {
		t.Errorf("sent %d requests despite refusing", len(r.requests))
	}
}

// A forwarding key already on the address is not in the list and must not be
// expected there, or accepting a second forwarding would refuse on the first.
func TestAddForwardingKeyLeavesForwardingKeysOutOfTheKeyListCheck(t *testing.T) {
	h := newHierarchy(t, 1)
	forwarding := forwardingKeyOf(t, h.u.AddrKRs[h.addr.ID].Read.GetKeys()[0])
	// Published earlier by an accept, and absent from the list by design.
	h.addr.Keys = append(h.addr.Keys, Key{
		ID: "forwarding", PrivateKey: locked(t, forwarding, h.token),
		Token: h.addr.Keys[0].Token, Signature: h.addr.Keys[0].Signature, Primary: 0, Active: 1,
	})
	r := &recorder{}

	if _, err := h.u.AddForwardingKey(
		context.Background(), r, h.addr, generated(t, "forwarding"), "second-forwarding",
	); err != nil {
		t.Fatalf("a second forwarding was refused: %v", err)
	}
	if skl, _ := body(t, r)["SignedKeyList"].(map[string]string); skl["Data"] != h.addr.SignedKeyList.Data {
		t.Error("the published list is not the one Proton served")
	}
}

// forwardingKeyOf derives a real forwarding key from a key, which is the only
// thing IsForwardingKey answers yes to.
func forwardingKeyOf(t *testing.T, from *pgp.Key) *pgp.Key {
	t.Helper()
	derived, instances, err := from.GetEntity().NewForwardingEntity(
		"forwardee@proton.me", "", "forwardee@proton.me", &packet.Config{}, false)
	if err != nil {
		t.Fatalf("derive a forwarding key: %v", err)
	}
	if len(instances) == 0 {
		t.Fatal("the derivation produced no proxy parameters")
	}
	key, err := pgp.NewKeyFromEntity(derived)
	if err != nil {
		t.Fatalf("read the derived key: %v", err)
	}
	if !key.IsForwardingKey() {
		t.Fatal("the derived key is not recognised as a forwarding key")
	}
	return key
}

// An address Proton publishes no list for cannot have one signed for it: there
// is nothing to sign, and composing one here would publish this build's reading
// of the account as the account's own statement.
func TestAddForwardingKeyRefusesAnAddressWithNoPublishedKeyList(t *testing.T) {
	h := newHierarchy(t, 1)
	h.addr.SignedKeyList = nil
	r := &recorder{}

	_, err := h.u.AddForwardingKey(context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id")
	if err == nil {
		t.Fatal("an address with no published key list was written to")
	}
	var problem *errs.Problem
	if !errors.As(err, &problem) || problem.ExitCode() != errs.ExitUnsupported {
		t.Errorf("the refusal exits %v, want the unsupported code", err)
	}
	if len(r.requests) != 0 {
		t.Errorf("sent %d requests despite refusing", len(r.requests))
	}
}

// A key held in the shape that predates address key tokens cannot be added to,
// and says so rather than sending a request Proton would refuse.
func TestAddForwardingKeyRefusesAnAddressKeyWithNoToken(t *testing.T) {
	h := newHierarchy(t, 1)
	h.addr.Keys[0].Token, h.addr.Keys[0].Signature = "", ""
	r := &recorder{}

	_, err := h.u.AddForwardingKey(context.Background(), r, h.addr, generated(t, "forwarding"), "forwarding-id")
	if err == nil {
		t.Fatal("an address key with no token was added to")
	}
	var problem *errs.Problem
	if !errors.As(err, &problem) || problem.ExitCode() != errs.ExitUnsupported {
		t.Errorf("the refusal exits %v, want the unsupported code", err)
	}
	if len(r.requests) != 0 {
		t.Errorf("sent %d requests despite refusing", len(r.requests))
	}
}
