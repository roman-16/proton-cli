package keys

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Changing the keys an address already has.
//
// Every change goes up with the address's list edited the one way Proton's
// clients edit it for that change, and signed by the keys that are primary once
// it lands - so what is pinned here is the list each request carries, who signed
// it, and what the account believes of the address afterwards.

// managed is an account with one address holding a primary key and a second
// key that reads, both locked with one token sealed to the user key, and the
// list Proton publishes naming both.
type managed struct {
	u               *Unlocked
	primary, second *pgp.Key
	token           *addressKeyToken
	api             *keyAPI
}

func newManaged(t *testing.T) *managed {
	t.Helper()
	user := generated(t, "user")
	userKR, err := pgp.NewKeyRing(user)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	token, err := newAddressKeyToken(userKR)
	if err != nil {
		t.Fatalf("newAddressKeyToken: %v", err)
	}
	primary, second := generated(t, "primary"), generated(t, "second")
	record := func(id string, key *pgp.Key, isPrimary int) Key {
		return Key{
			ID: id, PrivateKey: locked(t, key, token.passphrase), Token: token.sealed, Signature: token.signature,
			Primary: isPrimary, Active: 1, Flags: mailKeyFlags,
		}
	}
	addr := Address{
		ID: "address", Email: "me@proton.me", Status: 1, Order: 1,
		Keys:          []Key{record("primary-key", primary, 1), record("second-key", second, 0)},
		SignedKeyList: &SignedKeyList{Data: listNaming(t, primary, second), Signature: "the signature Proton holds"},
	}
	u := &Unlocked{
		UserKR:    userKR,
		Addresses: []Address{addr},
		AddrKRs: map[string]Rings{addr.ID: {
			Read: ringHolding([]*pgp.Key{primary, second}), Write: ringHolding([]*pgp.Key{primary}),
		}},
		UserKeys: []Key{{ID: "user-key", PrivateKey: armoredOf(t, user), Primary: 1, Active: 1}},
		Username: "me",
		private:  true,
	}
	return &managed{u: u, primary: primary, second: second, token: token, api: &keyAPI{u: u}}
}

func (m *managed) held(t *testing.T, id string) Held {
	t.Helper()
	for _, h := range m.u.Keys(context.Background()) {
		if h.ID == id {
			return h
		}
	}
	t.Fatalf("no key %s among the account's", id)
	return Held{}
}

// keyAPI answers as Proton would for an account whose keys are being changed,
// and keeps what it was asked. What /core/v4/addresses answers is the account as
// the changes so far have left it, which is what reading an address back reads.
type keyAPI struct {
	u        *Unlocked
	requests []proton.Request
	// pastLists is what Key Transparency holds as the address's earlier lists.
	pastLists []pastList
}

func (a *keyAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *keyAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	switch {
	case req.Path == "/core/v4/keys/address" && req.Method == "POST":
		a.file(req)
		return reply(map[string]any{"Key": map[string]any{"ID": "new-key"}}, out)
	case req.Path == "/core/v4/addresses":
		return reply(map[string]any{"Addresses": a.u.Addresses}, out)
	case req.Path == "/core/v4/keys/signedkeylists":
		return reply(map[string]any{"SignedKeyLists": a.pastLists}, out)
	case strings.HasPrefix(req.Path, "/kt/v1/verifiedepoch/"):
		return &proton.APIError{HTTPStatus: 422, Code: 2501, Message: "no verified epoch"}
	}
	return nil
}

// file is Proton filing a published key against its address.
func (a *keyAPI) file(req proton.Request) {
	body, _ := req.Body.(map[string]any)
	skl, _ := body["SignedKeyList"].(map[string]string)
	for i, addr := range a.u.Addresses {
		if addr.ID != body["AddressID"] {
			continue
		}
		armored, _ := body["PrivateKey"].(string)
		token, _ := body["Token"].(string)
		signature, _ := body["Signature"].(string)
		addr.Keys = append(append([]Key(nil), addr.Keys...), Key{
			ID: "new-key", PrivateKey: armored, Token: token, Signature: signature,
			Primary: body["Primary"].(int), Active: 1, Flags: mailKeyFlags,
		})
		addr.SignedKeyList = &SignedKeyList{Data: skl["Data"], Signature: skl["Signature"]}
		a.u.Addresses[i] = addr
	}
}

// sent is the one request to a path, or a failure saying it went elsewhere.
func (a *keyAPI) sent(t *testing.T, method, path string) map[string]any {
	t.Helper()
	for _, req := range a.requests {
		if req.Method == method && req.Path == path {
			body, _ := req.Body.(map[string]any)
			return body
		}
	}
	var seen []string
	for _, req := range a.requests {
		seen = append(seen, req.Method+" "+req.Path)
	}
	t.Fatalf("sent no %s %s; sent %v", method, path, seen)
	return nil
}

func listOf(t *testing.T, body map[string]any) (entries []keyListEntry, data, signature string) {
	t.Helper()
	skl, ok := body["SignedKeyList"].(map[string]string)
	if !ok {
		t.Fatalf("the request carries no key list: %v", body)
	}
	if err := json.Unmarshal([]byte(skl["Data"]), &entries); err != nil {
		t.Fatalf("the published list is not readable: %v", err)
	}
	return entries, skl["Data"], skl["Signature"]
}

// The listing is Proton's settings page: every key, what it is, and what it is
// used for - with a key the list names described by the list, as Proton's
// clients read it.
func TestKeysSaysWhatEachKeyIs(t *testing.T) {
	m := newManaged(t)
	m.u.Addresses[0].Keys = append(m.u.Addresses[0].Keys,
		Key{ID: "locked-key", PrivateKey: locked(t, generated(t, "old"), "a password from before"), Flags: mailKeyFlags})
	data, err := withFlags(m.u.Addresses[0].SignedKeyList.Data, m.second.GetFingerprint(), keyNotCompromised)
	if err != nil {
		t.Fatalf("withFlags: %v", err)
	}
	m.u.Addresses[0].SignedKeyList.Data = data

	for _, want := range []struct {
		id      string
		status  Status
		usedFor string
	}{
		{"primary-key", StatusPrimary, "encryption, decryption, signing, verification"},
		{"second-key", StatusObsolete, "decryption, verification"},
		{"locked-key", StatusLocked, "nothing until it is reactivated"},
		{"user-key", StatusPrimary, "encryption, decryption, signing, verification"},
	} {
		h := m.held(t, want.id)
		if h.Status != want.status || h.UsedFor != want.usedFor {
			t.Errorf("%s is %s, used for %q; want %s, used for %q", want.id, h.Status, h.UsedFor, want.status, want.usedFor)
		}
	}
	if h := m.held(t, "primary-key"); h.Fingerprint != m.primary.GetFingerprint() || h.Algorithm != "ECC (Curve25519)" {
		t.Errorf("the primary key reads as %q, %q", h.Fingerprint, h.Algorithm)
	}
	if h := m.held(t, "user-key"); h.Kind != KindAccount || h.Email != "" {
		t.Errorf("the account key reads as kind %q of %q", h.Kind, h.Email)
	}
}

// A new primary: the list names it first and it signs, and the key it replaces
// stays as one that reads.
func TestSetPrimarySignsAsTheNewPrimary(t *testing.T) {
	m := newManaged(t)
	if err := m.u.SetPrimary(context.Background(), m.api, m.held(t, "second-key")); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	body := m.api.sent(t, "PUT", "/core/v4/keys/second-key/primary")
	if body["Primary"] != 1 {
		t.Errorf("Primary = %v, want 1", body["Primary"])
	}
	entries, data, signature := listOf(t, body)
	if len(entries) != 2 || entries[0].Fingerprint != m.second.GetFingerprint() || entries[0].Primary != 1 {
		t.Fatalf("the list reads %+v, want the new primary first", entries)
	}
	if entries[1].Fingerprint != m.primary.GetFingerprint() || entries[1].Primary != 0 {
		t.Errorf("the key it replaced reads %+v, want it kept as one that reads", entries[1])
	}
	assertKeyListSignature(t, m.second, data, signature)
	if h := m.held(t, "second-key"); h.Status != StatusPrimary {
		t.Errorf("afterwards the account believes the new primary is %s", h.Status)
	}
}

// When the primary changes, the address's earlier lists are signed again by the
// new one wherever the old one signed them, dated when it did.
func TestSetPrimarySignsTheEarlierListsAgain(t *testing.T) {
	m := newManaged(t)
	then := time.Now().Add(time.Hour)
	earlier, err := signKeyList(m.u.Addresses[0].SignedKeyList.Data, []*pgp.Key{m.primary},
		func() time.Time { return then })
	if err != nil {
		t.Fatalf("sign the earlier list: %v", err)
	}
	stranger, err := signKeyList(m.u.Addresses[0].SignedKeyList.Data, []*pgp.Key{generated(t, "stranger")},
		func() time.Time { return then })
	if err != nil {
		t.Fatalf("sign a list by somebody else: %v", err)
	}
	m.api.pastLists = []pastList{
		{Data: m.u.Addresses[0].SignedKeyList.Data, Signature: earlier, Revision: 3},
		{Data: m.u.Addresses[0].SignedKeyList.Data, Signature: stranger, Revision: 4},
	}

	if err := m.u.SetPrimary(context.Background(), m.api, m.held(t, "second-key")); err != nil {
		t.Fatalf("SetPrimary: %v", err)
	}
	var resigned []map[string]any
	for _, req := range m.api.requests {
		if req.Path == "/core/v4/keys/signedkeylists/signature" {
			resigned = append(resigned, req.Body.(map[string]any))
		}
		if req.Path == "/core/v4/keys/signedkeylists" && req.Query.Get("Identifier") != "me@proton.me" {
			t.Errorf("asked for the lists of %q", req.Query.Get("Identifier"))
		}
	}
	if len(resigned) != 1 || resigned[0]["Revision"] != 3 {
		t.Fatalf("signed again %v, want revision 3 alone", resigned)
	}
	signature := resigned[0]["Signature"].(string)
	when, ok := signedAt(openpgp.EntityList{m.second.GetEntity()}, pastList{Data: m.api.pastLists[0].Data, Signature: signature})
	if !ok || !when.Equal(then.Truncate(time.Second)) {
		t.Errorf("the new signature is by the new primary at %v (%v), want at %v", when, ok, then)
	}
}

// One key's flags change, and nothing else in the list does.
func TestSetFlagsChangesOneEntry(t *testing.T) {
	m := newManaged(t)
	if err := m.u.SetFlags(context.Background(), m.api, m.held(t, "second-key"), 0); err != nil {
		t.Fatalf("SetFlags: %v", err)
	}
	body := m.api.sent(t, "PUT", "/core/v4/keys/second-key/flags")
	if body["Flags"] != 0 {
		t.Errorf("Flags = %v, want 0", body["Flags"])
	}
	entries, data, signature := listOf(t, body)
	if entries[0].Flags != mailKeyFlags || entries[1].Flags != 0 {
		t.Errorf("the list reads %+v, want the second key's flags alone changed", entries)
	}
	assertKeyListSignature(t, m.primary, data, signature)
	if h := m.held(t, "second-key"); h.Status != StatusCompromised {
		t.Errorf("afterwards the account believes the key is %s", h.Status)
	}
}

// Deleting a key takes it out of the list and out of the address.
func TestDeleteTakesTheKeyOutOfTheList(t *testing.T) {
	m := newManaged(t)
	if err := m.u.Delete(context.Background(), m.api, m.held(t, "second-key")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	entries, data, signature := listOf(t, m.api.sent(t, "POST", "/core/v4/keys/address/second-key/delete"))
	if len(entries) != 1 || entries[0].Fingerprint != m.primary.GetFingerprint() {
		t.Errorf("the list reads %+v, want the primary alone", entries)
	}
	assertKeyListSignature(t, m.primary, data, signature)
	if len(m.u.Addresses[0].Keys) != 1 {
		t.Errorf("afterwards the address holds %d keys, want 1", len(m.u.Addresses[0].Keys))
	}
}

// A list that does not describe the keys the account holds is not one to sign
// again with a change in it, and nothing is sent.
func TestAChangeRefusesAListThatDoesNotDescribeTheAddress(t *testing.T) {
	m := newManaged(t)
	m.u.Addresses[0].SignedKeyList.Data = listNaming(t, m.primary, generated(t, "somebody else's"))
	if err := m.u.SetFlags(context.Background(), m.api, m.held(t, "second-key"), 0); err == nil {
		t.Fatal("a list naming a key the account does not hold was signed again")
	}
	if len(m.api.requests) != 0 {
		t.Errorf("sent %d requests, want none", len(m.api.requests))
	}
}

// A key the account made joins as the primary, under the token the address's
// keys already share.
func TestPublishAddressKeyReplacesThePrimary(t *testing.T) {
	m := newManaged(t)
	key := generated(t, "new")
	if _, err := m.u.PublishAddressKey(context.Background(), m.api, m.u.Addresses[0], key); err != nil {
		t.Fatalf("PublishAddressKey: %v", err)
	}
	body := m.api.sent(t, "POST", "/core/v4/keys/address")
	if body["Primary"] != 1 || body["Token"] != m.token.sealed {
		t.Errorf("published as primary %v under a token of its own: %v", body["Primary"], body["Token"] != m.token.sealed)
	}
	entries, data, signature := listOf(t, body)
	if len(entries) != 3 || entries[0].Fingerprint != key.GetFingerprint() || entries[0].Primary != 1 {
		t.Fatalf("the list reads %+v, want the new key first as primary", entries)
	}
	for _, e := range entries[1:] {
		if e.Primary != 0 {
			t.Errorf("an old key is still primary: %+v", e)
		}
	}
	assertKeyListSignature(t, key, data, signature)
}

// An organization's member does not change keys the organization holds.
func TestChangesAreRefusedForKeysAnOrganizationHolds(t *testing.T) {
	m := newManaged(t)
	m.u.private = false
	if err := m.u.SetFlags(context.Background(), m.api, m.held(t, "second-key"), 0); err == nil ||
		!strings.Contains(err.Error(), "organization") {
		t.Errorf("SetFlags = %v, want the organization's refusal", err)
	}
}

// Marking a key follows Proton's own flag arithmetic, including that lifting
// compromised leaves a key obsolete.
func TestMarkedFollowsProtonsFlags(t *testing.T) {
	yes, no := true, false
	active := Held{flags: mailKeyFlags}
	compromised := Held{flags: 0}
	for _, tc := range []struct {
		name                  string
		h                     Held
		compromised, obsolete *bool
		want                  int
	}{
		{"compromised", active, &yes, nil, 0},
		{"obsolete", active, nil, &yes, keyNotCompromised},
		{"not compromised", compromised, &no, nil, keyNotCompromised},
		{"in use again", compromised, &no, &no, mailKeyFlags},
		{"keeps the encryption bit", Held{flags: mailKeyFlags | keyEncryptionOff}, &yes, nil, keyEncryptionOff},
	} {
		if got := tc.h.Marked(tc.compromised, tc.obsolete); got != tc.want {
			t.Errorf("%s: flags %d, want %d", tc.name, got, tc.want)
		}
	}
}

// A version 6 primary stays primary beside a new version 4 one, and comes second.
func TestPrimariesFirstKeepsOnePrimaryOfEachVersion(t *testing.T) {
	v4 := strings.Repeat("a", 40)
	v6 := strings.Repeat("b", 64)
	other := strings.Repeat("c", 40)
	items := primariesFirst([]keyListEntry{
		{Primary: 0, Fingerprint: other}, {Primary: 1, Fingerprint: v6}, {Primary: 1, Fingerprint: v4},
	})
	if items[0].Fingerprint != v4 || items[1].Fingerprint != v6 || items[2].Fingerprint != other {
		t.Errorf("ordered %+v, want the v4 primary, the v6 primary, then the rest", items)
	}
}

func TestCanonicalEmailIsHowKeyTransparencyFilesAnAddress(t *testing.T) {
	for in, want := range map[string]string{
		"Me.Myself+news@Proton.me": "memyself@proton.me",
		"a_b-c@example.com":        "abc@example.com",
	} {
		if got := canonicalEmail(in); got != want {
			t.Errorf("canonicalEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
