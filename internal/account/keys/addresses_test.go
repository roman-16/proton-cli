package keys

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ProtonMail/go-crypto/openpgp/ecdh"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Giving an address its first key is the one place this package makes key
// material, so what it makes is pinned here: a key the account's own password
// opens, and a list that says one true thing about the address.

// newAddress serves the two requests publishing a key makes - the key itself, and
// the read-back that follows - and keeps what it was given.
type newAddress struct {
	published map[string]any
	// addresses is what /core/v4/addresses answers with after the key went up.
	addresses []Address
	requests  []proton.Request
}

func (a *newAddress) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *newAddress) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	switch req.Path {
	case "/core/v4/keys/address":
		a.published, _ = req.Body.(map[string]any)
		a.remember()
		return json.Unmarshal([]byte(`{"Code":1000,"Key":{"ID":"published-key"}}`), out)
	case "/core/v4/addresses":
		answer, err := json.Marshal(map[string]any{"Code": 1000, "Addresses": a.addresses})
		if err != nil {
			return err
		}
		return json.Unmarshal(answer, out)
	}
	return nil
}

// remember files the published key against the address, which is what Proton
// answers with afterwards.
func (a *newAddress) remember() {
	armored, _ := a.published["PrivateKey"].(string)
	token, _ := a.published["Token"].(string)
	signature, _ := a.published["Signature"].(string)
	skl, _ := a.published["SignedKeyList"].(map[string]string)
	for i, addr := range a.addresses {
		if addr.ID != a.published["AddressID"] {
			continue
		}
		addr.Keys = append(addr.Keys, Key{
			ID: "published-key", PrivateKey: armored,
			Token: token, Signature: signature, Primary: 1, Active: 1,
		})
		addr.SignedKeyList = &SignedKeyList{Data: skl["Data"], Signature: skl["Signature"]}
		a.addresses[i] = addr
	}
}

// addressWithoutKeys is an account holding a user key and one address with no keys of
// its own, which is what Proton answers with the moment an address is made.
func addressWithoutKeys(t *testing.T) (*Unlocked, Address, *newAddress) {
	t.Helper()
	userKey := generated(t, "user")
	userKR, err := pgp.NewKeyRing(userKey)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	addr := Address{ID: "address", Email: "work@example.com"}
	return &Unlocked{
		UserKR: userKR, Addresses: []Address{addr}, AddrKRs: map[string]Rings{},
	}, addr, &newAddress{addresses: []Address{addr}}
}

// The key goes up locked with a passphrase this account can open, and the
// account's own unlocking is what proves it: the token is sealed to the user
// key and signed by it, exactly as every other address key of the account is.
func TestNewAddressKeyLocksWithATokenTheAccountCanOpen(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)

	id, err := u.NewAddressKey(context.Background(), api, addr)
	if err != nil {
		t.Fatalf("NewAddressKey: %v", err)
	}
	if id != "published-key" {
		t.Errorf("published key ID = %q, want the one Proton answered with", id)
	}
	if api.published["Primary"] != 1 {
		t.Errorf("Primary = %v, want 1: the first key of an address is what it writes with", api.published["Primary"])
	}
	if api.published["AddressForwardingID"] != nil {
		t.Error("a key of the address's own is filed against a forwarding")
	}

	token, err := decryptToken(
		api.published["Token"].(string), api.published["Signature"].(string), u.UserKR)
	if err != nil {
		t.Fatalf("the token does not open with the user key: %v", err)
	}
	armored, _ := api.published["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	if _, err := key.Unlock(token); err != nil {
		t.Errorf("the published key does not open with the token published beside it: %v", err)
	}
}

// The new key derives keys the way the account's own do.
//
// OpenPGP leaves that choice to whoever generates a key, and two libraries
// implementing the same standard make it differently - so it is taken from a key
// the account already holds rather than from whatever this build's library
// prefers. A key unlike the account's others is what that prevents.
func TestNewAddressKeyDerivesKeysLikeTheAccountsOwn(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)
	held := generated(t, "held")
	u.Addresses[0].Keys = []Key{{ID: "held", PrivateKey: armoredOf(t, held), Primary: 1, Active: 1}}

	if _, err := u.NewAddressKey(context.Background(), api, addr); err != nil {
		t.Fatalf("NewAddressKey: %v", err)
	}

	armored, _ := api.published["PrivateKey"].(string)
	published, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	made, ok := keyDerivationOf(published)
	if !ok {
		t.Fatal("the published key holds no encryption subkey")
	}
	own, ok := keyDerivationOf(held)
	if !ok {
		t.Fatal("the account's own key holds no encryption subkey")
	}
	if made.Hash.Id() != own.Hash.Id() || made.Cipher.Id() != own.Cipher.Id() {
		t.Errorf("the new key derives with hash %d and cipher %d; the account's own uses %d and %d",
			made.Hash.Id(), made.Cipher.Id(), own.Hash.Id(), own.Cipher.Id())
	}
	// Whatever was rewritten, the key still has to be a whole one: the subkey's
	// binding signature covers a fingerprint that moved with it.
	entity := published.GetEntity()
	if err := entity.PrimaryKey.VerifyKeySignature(entity.Subkeys[0].PublicKey, entity.Subkeys[0].Sig); err != nil {
		t.Errorf("the encryption subkey is not bound to the key it was published with: %v", err)
	}
}

func keyDerivationOf(key *pgp.Key) (ecdh.KDF, bool) {
	for _, sub := range key.GetEntity().Subkeys {
		if public, ok := sub.PublicKey.PublicKey.(*ecdh.PublicKey); ok {
			return public.KDF, true
		}
	}
	return ecdh.KDF{}, false
}

func armoredOf(t *testing.T, key *pgp.Key) string {
	t.Helper()
	armored, err := key.Armor()
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	return armored
}

// The key says which address it belongs to, because everything that later
// compares the two believes it.
func TestNewAddressKeyIsAddressedToTheAddress(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)

	if _, err := u.NewAddressKey(context.Background(), api, addr); err != nil {
		t.Fatalf("NewAddressKey: %v", err)
	}

	armored, _ := api.published["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	if got := key.GetEntity().PrimaryIdentity().UserId.Email; got != addr.Email {
		t.Errorf("the key is addressed to %q, want %q", got, addr.Email)
	}
	if key.IsForwardingKey() {
		t.Error("the key an address writes with reads as a forwarding key")
	}
}

// The list names the one key the address holds, and is signed by it. Nothing
// else could be true of an address that has just come into being, which is why
// this is the only list this package composes.
func TestNewAddressKeyPublishesAListNamingOnlyThatKey(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)

	if _, err := u.NewAddressKey(context.Background(), api, addr); err != nil {
		t.Fatalf("NewAddressKey: %v", err)
	}

	skl, ok := api.published["SignedKeyList"].(map[string]string)
	if !ok {
		t.Fatal("the request carries no key list")
	}
	var entries []keyListEntry
	if err := json.Unmarshal([]byte(skl["Data"]), &entries); err != nil {
		t.Fatalf("the published list is not readable: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the published list names %d keys, want 1", len(entries))
	}
	if entries[0].Primary != 1 || entries[0].Flags != mailKeyFlags {
		t.Errorf("the entry reads %+v, want a primary key that may encrypt and sign", entries[0])
	}

	armored, _ := api.published["PrivateKey"].(string)
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the published key is not readable armour: %v", err)
	}
	if entries[0].Fingerprint != key.GetFingerprint() {
		t.Error("the entry names a key other than the one published")
	}
	assertKeyListSignature(t, key, skl["Data"], skl["Signature"])
}

func assertKeyListSignature(t *testing.T, key *pgp.Key, data, armored string) {
	t.Helper()
	signature, err := pgp.NewPGPSignatureFromArmored(armored)
	if err != nil {
		t.Fatalf("the signature is not readable armour: %v", err)
	}
	public, err := key.ToPublic()
	if err != nil {
		t.Fatalf("take the public half: %v", err)
	}
	kr, err := pgp.NewKeyRing(public)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	if err := kr.VerifyDetachedWithContext(
		pgp.NewPlainMessageFromString(data), signature, pgp.GetUnixTime(),
		pgp.NewVerificationContext(sklSigningContext, true, 0),
	); err != nil {
		t.Errorf("the list is not signed by the key it names, under the key-list context: %v", err)
	}
}

// Having sent a key is not the same as the address having one, so the address is
// read back: what Proton serves has to be the list that was signed and a key
// this account opens.
func TestNewAddressKeyReadsTheAddressBack(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)

	if _, err := u.NewAddressKey(context.Background(), api, addr); err != nil {
		t.Fatalf("NewAddressKey: %v", err)
	}
	var read int
	for _, req := range api.requests {
		if req.Method == "GET" && req.Path == "/core/v4/addresses" {
			read++
		}
	}
	if read != 1 {
		t.Errorf("read the address back %d times, want once", read)
	}
}

// A key list Proton serves differently from the one that was signed is this
// build's fault and says so, while the address is still empty.
func TestNewAddressKeyFailsWhenProtonServesAnotherKeyList(t *testing.T) {
	u, addr, api := addressWithoutKeys(t)
	api.addresses[0].SignedKeyList = &SignedKeyList{Data: `[]`, Signature: "somebody else's"}
	// remember overwrites the list on publish, so hold it back to leave the
	// account disagreeing with what went up.
	api.published = map[string]any{}

	_, err := u.NewAddressKey(context.Background(), withheldKeyList{api}, addr)
	if err == nil {
		t.Fatal("a key list Proton does not serve was accepted")
	}
	if !strings.Contains(err.Error(), "serves a different one") {
		t.Errorf("the failure does not say what disagreed: %v", err)
	}
}

// withheldKeyList answers as an account that filed the key and kept its own key
// list, which is the shape of a write that half happened.
type withheldKeyList struct{ *newAddress }

func (w withheldKeyList) Decode(ctx context.Context, req proton.Request, out any) error {
	if req.Path == "/core/v4/keys/address" {
		w.requests = append(w.requests, req)
		w.published, _ = req.Body.(map[string]any)
		return json.Unmarshal([]byte(`{"Code":1000,"Key":{"ID":"published-key"}}`), out)
	}
	return w.newAddress.Decode(ctx, req, out)
}

// An account that holds post-quantum keys is asked about before an address is
// made, because a v4-only address would leave one address weaker than the rest
// and nothing afterwards would say so.
func TestPostQuantumReadsTheAccountsOwnAnswer(t *testing.T) {
	for _, tc := range []struct {
		flag int
		want bool
	}{{0, false}, {1, true}} {
		t.Run(fmt.Sprint(tc.flag), func(t *testing.T) {
			got, err := PostQuantum(context.Background(), settingsFlag(tc.flag))
			if err != nil {
				t.Fatalf("PostQuantum: %v", err)
			}
			if got != tc.want {
				t.Errorf("PostQuantum = %v, want %v", got, tc.want)
			}
		})
	}
}

type settingsFlag int

func (settingsFlag) Do(context.Context, proton.Request) (*proton.Response, error) { return nil, nil }

func (f settingsFlag) Decode(_ context.Context, req proton.Request, out any) error {
	if req.Path != "/core/v4/settings" {
		return fmt.Errorf("asked %s instead of the account's settings", req.Path)
	}
	return json.Unmarshal([]byte(fmt.Sprintf(
		`{"Code":1000,"UserSettings":{"Flags":{"SupportPgpV6Keys":%d}}}`, int(f))), out)
}
