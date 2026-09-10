package keys

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Turning an address's end-to-end encryption on and off.
//
// It is published, which is what makes it delicate: the statement that says so
// is the key list, and everything else about that list has to survive being
// rewritten. So what is pinned here is the difference - one flag bit, on every
// entry - and that the address still holds exactly the keys the list named.

// encrypting is the address whose encryption is being written, and what the
// request carried.
func encrypting(t *testing.T) (*Unlocked, Address, *encryptionAPI) {
	t.Helper()
	key := generated(t, "address")
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	data, err := composeKeyList(key)
	if err != nil {
		t.Fatalf("compose the key list: %v", err)
	}
	addr := Address{
		ID: "address", Email: "me@proton.me", ProtonMX: true,
		Keys:          []Key{{ID: "key", PrivateKey: armoredOf(t, key), Primary: 1, Active: 1}},
		SignedKeyList: &SignedKeyList{Data: data, Signature: "the signature Proton holds"},
	}
	return &Unlocked{
		UserKR: kr, Addresses: []Address{addr}, AddrKRs: map[string]Rings{addr.ID: {Read: kr, Write: kr}},
	}, addr, &encryptionAPI{}
}

type encryptionAPI struct {
	requests []proton.Request
}

func (a *encryptionAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *encryptionAPI) Decode(_ context.Context, req proton.Request, _ any) error {
	a.requests = append(a.requests, req)
	return nil
}

func (a *encryptionAPI) body(t *testing.T) map[string]any {
	t.Helper()
	if len(a.requests) != 1 {
		t.Fatalf("sent %d requests, want the one that writes the encryption", len(a.requests))
	}
	if got := a.requests[0].Method + " " + a.requests[0].Path; got != "PUT /core/v4/addresses/address/encryption" {
		t.Fatalf("sent %s, want the address's encryption", got)
	}
	body, _ := a.requests[0].Body.(map[string]any)
	return body
}

// Off, and back on: the list says which either way, and says nothing else
// differently.
func TestSetEncryptionFlipsOneFlagOnEveryEntry(t *testing.T) {
	for _, tc := range []struct {
		on    bool
		flags int
	}{{false, mailKeyFlags | keyEncryptionOff}, {true, mailKeyFlags}} {
		u, addr, api := encrypting(t)
		if !tc.on {
			// Coming from an address that is encrypted, which is what turning it
			// off starts from.
			addr.Flags = 0
		}
		if err := u.SetEncryption(context.Background(), api, addr, tc.on); err != nil {
			t.Fatalf("SetEncryption(%v): %v", tc.on, err)
		}

		body := api.body(t)
		if body["Encrypt"] != boolBit(tc.on) {
			t.Errorf("Encrypt = %v, want %v", body["Encrypt"], boolBit(tc.on))
		}
		skl, _ := body["SignedKeyList"].(map[string]string)
		var written []keyListEntry
		if err := json.Unmarshal([]byte(skl["Data"]), &written); err != nil {
			t.Fatalf("the published list is not readable: %v", err)
		}
		var served []keyListEntry
		if err := json.Unmarshal([]byte(addr.SignedKeyList.Data), &served); err != nil {
			t.Fatalf("the served list is not readable: %v", err)
		}
		if len(written) != len(served) {
			t.Fatalf("the published list names %d keys, want the %d Proton serves", len(written), len(served))
		}
		for i, entry := range written {
			if entry.Flags != tc.flags {
				t.Errorf("entry %d reads flags %d, want %d", i, entry.Flags, tc.flags)
			}
			if entry.Primary != served[i].Primary || entry.Fingerprint != served[i].Fingerprint {
				t.Errorf("entry %d names %+v, want the key the served list named: %+v", i, entry, served[i])
			}
		}
	}
}

// Whether a signature is expected on what arrives is a second thing the same
// request carries, and no business of this one.
func TestSetEncryptionLeavesTheOtherFlagAsItFoundIt(t *testing.T) {
	for _, tc := range []struct {
		flags int
		sign  int
	}{{0, 1}, {addressExpectSignedOff, 0}} {
		u, addr, api := encrypting(t)
		addr.Flags = tc.flags

		if err := u.SetEncryption(context.Background(), api, addr, false); err != nil {
			t.Fatalf("SetEncryption: %v", err)
		}
		if got := api.body(t)["Sign"]; got != tc.sign {
			t.Errorf("an address with flags %d was written with Sign = %v, want %v", tc.flags, got, tc.sign)
		}
	}
}

// The list is signed as the address, under the context a key list is read in.
func TestSetEncryptionSignsTheListItPublishes(t *testing.T) {
	u, addr, api := encrypting(t)

	if err := u.SetEncryption(context.Background(), api, addr, false); err != nil {
		t.Fatalf("SetEncryption: %v", err)
	}

	skl, _ := api.body(t)["SignedKeyList"].(map[string]string)
	assertKeyListSignature(t, u.AddrKRs[addr.ID].Write.GetKeys()[0], skl["Data"], skl["Signature"])
}

// A list that does not describe the address is not one to re-publish under a
// fresh signature, whatever it says about encryption.
func TestSetEncryptionRefusesAListThatDoesNotDescribeTheAddress(t *testing.T) {
	u, addr, api := encrypting(t)
	addr.SignedKeyList = &SignedKeyList{
		Data:      `[{"Primary":1,"Flags":3,"Fingerprint":"nobody","SHA256Fingerprints":["nobody"]}]`,
		Signature: "the signature Proton holds",
	}

	err := u.SetEncryption(context.Background(), api, addr, false)
	if err == nil {
		t.Fatal("a list naming keys the account does not hold was re-published")
	}
	if !strings.Contains(err.Error(), "does not describe the address") {
		t.Errorf("the failure does not say what disagreed: %v", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("sent %d requests despite refusing", len(api.requests))
	}
}

// An address Proton publishes no list for cannot have one written from here:
// there would be nothing to derive it from but this build's own opinion of what
// the address holds.
func TestSetEncryptionRefusesAnAddressWithNoPublishedList(t *testing.T) {
	u, addr, api := encrypting(t)
	addr.SignedKeyList = nil

	err := u.SetEncryption(context.Background(), api, addr, false)
	if err == nil {
		t.Fatal("an address with no published key list had one written")
	}
	if !strings.Contains(err.Error(), "publishes no key list") {
		t.Errorf("the refusal does not say what is missing: %v", err)
	}
	if len(api.requests) != 0 {
		t.Errorf("sent %d requests despite refusing", len(api.requests))
	}
}
