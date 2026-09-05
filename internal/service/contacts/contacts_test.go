package contacts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	gopenpgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/vcard"
)

// testKeyRing generates a throwaway keyring to sign/verify contact cards.
func testKeyRing(t *testing.T) *gopenpgp.KeyRing {
	t.Helper()
	key, err := gopenpgp.GenerateKey("test", "test@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kr, err := gopenpgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return kr
}

// armoredPubKey returns a fresh armored public key plus its raw KEY-property value.
func armoredPubKey(t *testing.T) (armored, keyValue string) {
	t.Helper()
	key, err := gopenpgp.GenerateKey("pin", "pin@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	armored, err = key.GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("GetArmoredPublicKey: %v", err)
	}
	bin, err := key.GetPublicKey()
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	return armored, "data:application/pgp-keys;base64," + base64.StdEncoding.EncodeToString(bin)
}

// signedCard builds a Type-2 signed card from vCard text using kr.
func signedCard(t *testing.T, kr *gopenpgp.KeyRing, data string) map[string]any {
	t.Helper()
	card, err := pgp.SignCard(data, kr)
	if err != nil {
		t.Fatalf("SignCard: %v", err)
	}
	return map[string]any{"Type": card.Type, "Data": card.Data, "Signature": card.Signature}
}

// contactDoer fakes the contacts API: it serves the emails lookup and a single
// contact's raw cards, and captures the PUT body.
type contactDoer struct {
	emails  []map[string]any
	cards   []map[string]any
	putBody map[string]any
}

func (d *contactDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *contactDoer) Decode(_ context.Context, r proton.Request, out any) error {
	if r.Method == "PUT" {
		d.putBody, _ = r.Body.(map[string]any)
		return nil
	}
	var payload any
	switch {
	case strings.HasSuffix(r.Path, "/contacts/emails"):
		payload = map[string]any{"ContactEmails": d.emails}
	default: // GET a contact by id
		payload = map[string]any{"Contact": map[string]any{"Cards": d.cards}}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// putCard returns the card of one type out of the captured PUT, so a test asks
// what a write sent rather than where in the list it landed.
func putCard(t *testing.T, d *contactDoer, cardType int) *pgp.Card {
	t.Helper()
	cards, ok := d.putBody["Cards"].([]any)
	if !ok || len(cards) == 0 {
		t.Fatalf("no Cards in PUT body: %#v", d.putBody)
	}
	for _, raw := range cards {
		if c, ok := raw.(*pgp.Card); ok && c.Type == cardType {
			return c
		}
	}
	t.Fatalf("the PUT carried no card of type %d: %#v", cardType, cards)
	return nil
}

// putSignedCardText returns the signed card's Data from the captured PUT.
func putSignedCardText(t *testing.T, d *contactDoer) string {
	t.Helper()
	return putCard(t, d, pgp.CardSigned).Data
}

// twoKeyRing is an account that has been rotating its user keys: both open what
// they sealed, and the first is the one Proton would call primary.
func twoKeyRing(t *testing.T) *gopenpgp.KeyRing {
	t.Helper()
	ring, err := gopenpgp.NewKeyRing(nil)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	for _, name := range []string{"primary", "retired"} {
		key, err := gopenpgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		if err := ring.AddKey(key); err != nil {
			t.Fatalf("AddKey: %v", err)
		}
	}
	return ring
}

// oneKeyRing isolates one of a ring's keys, to ask which of them a card was
// sealed to by trying to open it.
func oneKeyRing(t *testing.T, ring *gopenpgp.KeyRing, i int) *gopenpgp.KeyRing {
	t.Helper()
	kr, err := gopenpgp.NewKeyRing(ring.GetKeys()[i])
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return kr
}

// A write seals the encrypted card to the primary user key and to nothing else.
//
// Every key that ever was primary has to open what it sealed, so reading uses
// the whole ring - but sealing something new under all of them puts it under
// keys the owner has retired, and is not what Proton's own client sends.
func TestUpdateSealsTheEncryptedCardToThePrimaryUserKeyAlone(t *testing.T) {
	ring := twoKeyRing(t)
	base := vcard.BuildSigned(vcard.Signed{
		Name: "Bob", UID: "u",
		Emails: []vcard.SignedEmail{{Address: "bob@example.com"}},
	})
	d := &contactDoer{cards: []map[string]any{signedCard(t, ring, base)}}

	svc := New(d, testKeys(&keys.Unlocked{UserKR: ring}))
	if _, err := svc.Update(context.Background(), "c1", NewContact{Note: "Likes tea"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	sealed := putCard(t, d, pgp.CardEncryptedSigned)
	msg, err := gopenpgp.NewPGPMessageFromArmored(sealed.Data)
	if err != nil {
		t.Fatalf("the encrypted card is not armored PGP: %v", err)
	}
	if ids, ok := msg.GetHexEncryptionKeyIDs(); !ok || len(ids) != 1 {
		t.Fatalf("the card is sealed to %v, want exactly one key", ids)
	}
	if _, err := oneKeyRing(t, ring, 0).Decrypt(msg, nil, gopenpgp.GetUnixTime()); err != nil {
		t.Errorf("the primary user key cannot open the card it should have sealed: %v", err)
	}
	if _, err := oneKeyRing(t, ring, 1).Decrypt(msg, nil, gopenpgp.GetUnixTime()); err == nil {
		t.Error("the card was sealed to a key that is no longer primary")
	}
}

// An edit reads a contact off every card it is stored as and writes them out
// again, so anything it claims twice is stored twice - one more copy of the
// address and of every pinned key on every edit.
func TestUpdateDoesNotDuplicateWhatTheSignedCardHolds(t *testing.T) {
	ring := twoKeyRing(t)
	_, keyValue := armoredPubKey(t)
	base := vcard.BuildSigned(vcard.Signed{
		Name: "Bob", UID: "u",
		Emails: []vcard.SignedEmail{{
			Address: "bob@example.com", KeyValues: []string{keyValue}, Encrypt: ptr(true),
		}},
	})
	d := &contactDoer{cards: []map[string]any{signedCard(t, ring, base)}}

	svc := New(d, testKeys(&keys.Unlocked{UserKR: ring}))
	if _, err := svc.Update(context.Background(), "c1", NewContact{Name: "Bobby"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// A rename touches nothing encrypted, so there is no encrypted card to send.
	if cards := d.putBody["Cards"].([]any); len(cards) != 1 {
		t.Errorf("a rename sent %d cards, want only the signed one", len(cards))
	}
	signed := putSignedCardText(t, d)
	if got := len(vcard.Values(signed, "EMAIL")); got != 1 {
		t.Errorf("the contact now holds %d copies of its address:\n%s", got, signed)
	}
	if got := len(vcard.Values(signed, "KEY")); got != 1 {
		t.Errorf("the contact now holds %d copies of its pinned key:\n%s", got, signed)
	}
}

func TestPinnedKeysForReadsSignedCard(t *testing.T) {
	kr := testKeyRing(t)
	_, keyValue := armoredPubKey(t)
	card := vcard.BuildSigned(vcard.Signed{
		Name: "Bob", UID: "uid-1",
		Emails: []vcard.SignedEmail{{
			Address:   "bob@example.com",
			KeyValues: []string{keyValue},
			Encrypt:   ptr(true),
		}},
	})
	d := &contactDoer{
		emails: []map[string]any{{"ContactID": "c1", "Defaults": 0}},
		cards:  []map[string]any{signedCard(t, kr, card)},
	}
	u := &keys.Unlocked{UserKR: kr}

	cc, err := New(d, testKeys(u)).PinnedKeysFor(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("PinnedKeysFor: %v", err)
	}
	if cc == nil || len(cc.ArmoredKeys) != 1 {
		t.Fatalf("expected one pinned key, got %+v", cc)
	}
	if cc.Encrypt == nil || !*cc.Encrypt {
		t.Errorf("Encrypt = %v, want true", cc.Encrypt)
	}
	if !cc.SignatureVerified {
		t.Error("SignatureVerified should be true for a card signed by the user key")
	}
	if !strings.Contains(cc.ArmoredKeys[0], "PGP PUBLIC KEY BLOCK") {
		t.Errorf("pinned key is not armored: %q", cc.ArmoredKeys[0])
	}
}

// A contact that will not open is not a contact with no pin. The answer says so,
// and leaves what to do about it to the send.
func TestPinnedKeysForSaysWhenItCannotTell(t *testing.T) {
	kr := testKeyRing(t)
	d := &contactDoer{
		emails: []map[string]any{{"ContactID": "c1", "Defaults": 0}},
		// An encrypted card sealed to a key this account does not hold.
		cards: []map[string]any{{"Type": float64(pgp.CardEncryptedSigned), "Data": "not a message", "Signature": ""}},
	}
	cc, err := New(d, testKeys(&keys.Unlocked{UserKR: kr})).PinnedKeysFor(context.Background(), "bob@example.com")
	if err != nil {
		t.Fatalf("PinnedKeysFor: %v", err)
	}
	if cc == nil || !cc.Unknown {
		t.Fatalf("a contact that would not open came back as %+v, want Unknown", cc)
	}
}

// A pin is read off the signed card alone: a KEY anywhere else was vouched for by
// nobody, and Proton's own clients do not treat it as a pin either.
func TestPinnedKeysIgnoreAKeyOutsideTheSignedCard(t *testing.T) {
	_, keyValue := armoredPubKey(t)
	ct := &Contact{
		ID: "c1", Signature: pgp.Verified,
		signed: vcard.BuildSigned(vcard.Signed{Name: "Bob", UID: "u", Emails: []vcard.SignedEmail{{Address: "bob@example.com"}}}),
		clear:  "BEGIN:VCARD\r\nVERSION:4.0\r\nitem1.EMAIL:bob@example.com\r\nitem1.KEY:" + keyValue + "\r\nEND:VCARD",
	}
	if cc := PinnedKeys(context.Background(), ct, "bob@example.com"); cc != nil {
		t.Errorf("a KEY in the clear card was taken as a pin: %+v", cc)
	}
}

// A pinned key that will not decode is a pin that cannot be seen, and the
// answer says so alongside whatever did decode.
func TestPinnedKeysSayWhenAKeyWillNotDecode(t *testing.T) {
	_, keyValue := armoredPubKey(t)
	ct := &Contact{
		ID: "c1", Signature: pgp.Verified,
		signed: vcard.BuildSigned(vcard.Signed{Name: "Bob", UID: "u", Emails: []vcard.SignedEmail{{
			Address: "bob@example.com", KeyValues: []string{keyValue, "data:application/pgp-keys;base64,bm9wZQ=="},
		}}}),
	}
	cc := PinnedKeys(context.Background(), ct, "bob@example.com")
	if cc == nil || len(cc.ArmoredKeys) != 1 {
		t.Fatalf("PinnedKeys = %+v, want the one key that decoded", cc)
	}
	if !cc.Unknown {
		t.Error("a key that would not decode was passed over in silence")
	}
}

func TestPinnedKeysForNoConfigIsMiss(t *testing.T) {
	d := &contactDoer{emails: []map[string]any{{"ContactID": "c1", "Defaults": 1}}}
	cc, err := New(d, testKeys(&keys.Unlocked{UserKR: testKeyRing(t)})).PinnedKeysFor(context.Background(), "x@example.com")
	if err != nil || cc != nil {
		t.Errorf("Defaults==1 should be a clean miss; got %+v, %v", cc, err)
	}
}

func TestPinKeyAddsKeyAndPreservesOtherCards(t *testing.T) {
	kr := testKeyRing(t)
	base := vcard.BuildSigned(vcard.Signed{
		Name: "Bob", UID: "uid-1",
		Emails: []vcard.SignedEmail{{Address: "bob@example.com"}},
	})
	// A verbatim encrypted card that must survive the edit untouched.
	encrypted := map[string]any{"Type": float64(pgp.CardEncryptedSigned), "Data": "ENC", "Signature": "SIG"}
	d := &contactDoer{cards: []map[string]any{signedCard(t, kr, base), encrypted}}
	u := &keys.Unlocked{UserKR: kr}

	armored, keyValue := armoredPubKey(t)
	verdict, err := New(d, testKeys(u)).PinKey(context.Background(), "c1", "bob@example.com", armored, nil, nil, "")
	if err != nil {
		t.Fatalf("PinKey: %v", err)
	}
	if verdict != pgp.Verified {
		t.Errorf("a card signed by this account came back %q", verdict)
	}

	newSigned := putSignedCardText(t, d)
	model := vcard.ParseSigned(newSigned)
	e := model.FindEmail("bob@example.com")
	if e == nil || len(e.KeyValues) != 1 || e.KeyValues[0] != keyValue {
		t.Fatalf("pinned key not written: %+v", e)
	}
	if e.Encrypt == nil || !*e.Encrypt || e.Sign == nil || !*e.Sign {
		t.Errorf("encrypt/sign should default to true: enc=%v sign=%v", e.Encrypt, e.Sign)
	}
	// The encrypted card must be re-attached verbatim.
	cards := d.putBody["Cards"].([]any)
	if len(cards) != 2 {
		t.Fatalf("expected 2 cards in PUT, got %d", len(cards))
	}
	if got := cards[1].(map[string]any); got["Data"] != "ENC" || got["Signature"] != "SIG" {
		t.Errorf("encrypted card not preserved verbatim: %#v", got)
	}
}

// A card that does not verify is far more often one signed by a key this
// account has since retired than one somebody altered, and the two cannot be
// told apart. So the write goes ahead, as Proton's own client's does, and the
// verdict comes back for the caller to say - which is the one thing a refusal
// could not do.
func TestAWriteOverAnUnverifiedCardGoesAheadAndSaysSo(t *testing.T) {
	kr := testKeyRing(t)
	other := testKeyRing(t) // signs with a key this account no longer holds
	base := vcard.BuildSigned(vcard.Signed{Name: "Bob", UID: "u", Emails: []vcard.SignedEmail{{Address: "bob@example.com"}}})
	u := &keys.Unlocked{UserKR: kr}

	t.Run("pin", func(t *testing.T) {
		d := &contactDoer{cards: []map[string]any{signedCard(t, other, base)}}
		armored, _ := armoredPubKey(t)
		verdict, err := New(d, testKeys(u)).PinKey(context.Background(), "c1", "bob@example.com", armored, nil, nil, "")
		if err != nil {
			t.Fatalf("PinKey refused a card it could not verify: %v", err)
		}
		if verdict != pgp.Unverified {
			t.Errorf("verdict = %q, want %q", verdict, pgp.Unverified)
		}
		if d.putBody == nil {
			t.Error("nothing was written")
		}
	})
	t.Run("update", func(t *testing.T) {
		d := &contactDoer{cards: []map[string]any{signedCard(t, other, base)}}
		verdict, err := New(d, testKeys(u)).Update(context.Background(), "c1", NewContact{Note: "Likes tea"})
		if err != nil {
			t.Fatalf("Update: %v", err)
		}
		if verdict != pgp.Unverified {
			t.Errorf("verdict = %q, want %q", verdict, pgp.Unverified)
		}
	})
}

func TestUnpinKeyRemovesKeys(t *testing.T) {
	kr := testKeyRing(t)
	_, keyValue := armoredPubKey(t)
	base := vcard.BuildSigned(vcard.Signed{
		Name: "Bob", UID: "u",
		Emails: []vcard.SignedEmail{{Address: "bob@example.com", KeyValues: []string{keyValue}, Encrypt: ptr(true)}},
	})
	d := &contactDoer{cards: []map[string]any{signedCard(t, kr, base)}}
	u := &keys.Unlocked{UserKR: kr}

	if _, err := New(d, testKeys(u)).UnpinKey(context.Background(), "c1", "bob@example.com"); err != nil {
		t.Fatalf("UnpinKey: %v", err)
	}
	model := vcard.ParseSigned(putSignedCardText(t, d))
	if e := model.FindEmail("bob@example.com"); e == nil {
		t.Error("email should remain after unpin")
	} else if len(e.KeyValues) != 0 || e.Encrypt != nil {
		t.Errorf("keys/flags should be gone: %+v", e)
	}
}

func encodePinnedKeyValue(t *testing.T, armored string) string {
	t.Helper()
	v, err := encodePinnedKey(armored)
	if err != nil {
		t.Fatalf("encodePinnedKey: %v", err)
	}
	return v
}

func TestEncodePinnedKeyRoundTrip(t *testing.T) {
	armored, want := armoredPubKey(t)
	if got := encodePinnedKeyValue(t, armored); got != want {
		t.Errorf("encodePinnedKey mismatch:\n got %q\nwant %q", got, want)
	}
	if _, err := encodePinnedKey("not-a-key"); err == nil {
		t.Error("expected an error for a non-key input")
	}
}

func TestPrependUnique(t *testing.T) {
	got := prependUnique([]string{"b", "a"}, "a")
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("prependUnique = %v, want [a b] (a moved to front, deduped)", got)
	}
}

func ptr[T any](v T) *T { return &v }

// testKeys hands a service the key hierarchy a test wants it to decrypt with.
// A test that decrypts nothing passes nil, which is never asked for.
func testKeys(u *keys.Unlocked) keys.Get {
	return func(context.Context) (*keys.Unlocked, error) {
		if u == nil {
			return nil, errors.New("this test has no keys")
		}
		return u, nil
	}
}

// Two entries reachable at the same address are one person. Two merely sharing a
// name are not - people are routinely called the same thing, and folding them
// together would lose one of them.
func TestDuplicatesGroupByAddressNotByName(t *testing.T) {
	all := []Contact{
		{ID: "1", Name: "Jane Roe", Emails: []string{"jane@example.com"}},
		{ID: "2", Name: "J. Roe", Emails: []string{"JANE@example.com", "j@work.com"}},
		{ID: "3", Name: "Jane Roe", Emails: []string{"different@example.com"}},
		{ID: "4", Name: "Nobody", Emails: nil},
	}
	groups := Duplicates(all)
	if len(groups) != 1 {
		t.Fatalf("found %d duplicate sets, want 1: %+v", len(groups), groups)
	}
	if groups[0].Email != "jane@example.com" {
		t.Errorf("grouped on %q, want the shared address", groups[0].Email)
	}
	if len(groups[0].Contacts) != 2 {
		t.Fatalf("set holds %d contacts, want 2", len(groups[0].Contacts))
	}
	// The two Jane Roes that share no address stay apart.
	for _, ct := range groups[0].Contacts {
		if ct.ID == "3" {
			t.Error("a contact sharing only a name was folded in")
		}
	}
}

// A contact already folded into one set is not offered in another; merging it
// twice would move it out from under the first merge.
func TestDuplicatesClaimAContactOnlyOnce(t *testing.T) {
	all := []Contact{
		{ID: "1", Emails: []string{"a@example.com"}},
		{ID: "2", Emails: []string{"a@example.com", "b@example.com"}},
		{ID: "3", Emails: []string{"b@example.com"}},
	}
	groups := Duplicates(all)
	seen := map[string]bool{}
	for _, g := range groups {
		for _, ct := range g.Contacts {
			if seen[ct.ID] {
				t.Errorf("contact %s appears in two merge sets", ct.ID)
			}
			seen[ct.ID] = true
		}
	}
}

// The kept contact wins every scalar, and every list keeps the union: a merge
// adds what the others had and overwrites nothing.
func TestMergeCardsKeepsTheFirstAndUnionsTheRest(t *testing.T) {
	first := Contact{ID: "1", Name: "Jane Roe", Cards: []string{
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane Roe\r\nEMAIL:jane@example.com\r\n" +
			"NOTE:Original note\r\nTEL:+43 1 111\r\nEND:VCARD",
	}}
	second := Contact{ID: "2", Name: "J. Roe", Cards: []string{
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:J. Roe\r\nEMAIL:j@work.com\r\n" +
			"NOTE:Different note\r\nTEL:+43 1 222\r\nORG:Acme\r\nEND:VCARD",
	}}

	got := mergeCards([]Contact{first, second})
	if got.name != "Jane Roe" {
		t.Errorf("name = %q, want the kept contact's", got.name)
	}
	if got.encrypted.Note != "Original note" {
		t.Errorf("note = %q; the kept contact's value must not be overwritten", got.encrypted.Note)
	}
	if got.encrypted.Org != "Acme" {
		t.Errorf("org = %q; a field the kept contact lacked should be filled in", got.encrypted.Org)
	}
	if len(got.emails) != 2 {
		t.Errorf("emails = %v, want both", got.emails)
	}
	if len(got.encrypted.Phones) != 2 {
		t.Errorf("phones = %v, want both", got.encrypted.Phones)
	}
}

// The same mailbox written two ways is one mailbox, so a merge does not keep it
// twice.
func TestMergeCardsDeduplicatesAddressesByCase(t *testing.T) {
	got := mergeCards([]Contact{
		{ID: "1", Cards: []string{"BEGIN:VCARD\r\nVERSION:4.0\r\nEMAIL:jane@example.com\r\nEND:VCARD"}},
		{ID: "2", Cards: []string{"BEGIN:VCARD\r\nVERSION:4.0\r\nEMAIL:JANE@Example.com\r\nEND:VCARD"}},
	})
	if len(got.emails) != 1 {
		t.Errorf("emails = %v, want one; the same mailbox written twice is one mailbox", got.emails)
	}
}

// bookDoer fakes the parts of the contacts API an import and an export touch:
// the labels, the address listing, the batch add, and the labelling. It records
// what was labelled so a test can ask where an import put the addresses.
type bookDoer struct {
	groups   []map[string]any
	emails   []map[string]any
	contact  []map[string]any
	created  []string
	labelled map[string][]string
}

func (d *bookDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *bookDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var payload any
	switch {
	case r.Method == "GET" && r.Path == "/core/v4/labels":
		payload = map[string]any{"Labels": d.groups}
	case r.Method == "POST" && r.Path == "/core/v4/labels":
		name, _ := r.Body.(map[string]any)["Name"].(string)
		id := "group-" + name
		d.groups = append(d.groups, map[string]any{"ID": id, "Name": name, "Color": "#8080FF"})
		d.created = append(d.created, name)
		payload = map[string]any{"Label": map[string]any{"ID": id}}
	case r.Method == "GET" && strings.HasSuffix(r.Path, "/contacts/emails"):
		payload = map[string]any{"ContactEmails": d.emails}
	case r.Method == "POST" && r.Path == "/contacts/v4/contacts":
		var responses []map[string]any
		for i, raw := range r.Body.(map[string]any)["Contacts"].([]map[string]any) {
			var addresses []map[string]any
			for _, c := range raw["Cards"].([]any) {
				card, ok := c.(*pgp.Card)
				if !ok || card.Type != pgp.CardSigned {
					continue
				}
				for j, e := range vcard.Values(card.Data, "EMAIL") {
					addresses = append(addresses, map[string]any{"ID": fmt.Sprintf("e%d-%d", i, j), "Email": e})
				}
			}
			responses = append(responses, map[string]any{"Index": i, "Response": map[string]any{
				"Code": 1000, "Contact": map[string]any{"ID": fmt.Sprintf("c%d", i), "ContactEmails": addresses},
			}})
		}
		payload = map[string]any{"Responses": responses}
	case r.Method == "PUT" && r.Path == "/contacts/v4/contacts/emails/label":
		body := r.Body.(map[string]any)
		if d.labelled == nil {
			d.labelled = map[string][]string{}
		}
		d.labelled[body["LabelID"].(string)] = body["ContactEmailIDs"].([]string)
		return nil
	case r.Method == "GET":
		payload = map[string]any{"Contact": map[string]any{"ID": "c1", "Cards": d.contact}}
	default:
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(b, out)
}

// An import puts each address in the groups the file gave it - the group of a
// CATEGORIES under the address's own group, every address for one under none -
// creating the groups the account does not have and reporting which.
func TestImportPutsAddressesInTheGroupsTheFileNames(t *testing.T) {
	d := &bookDoer{groups: []map[string]any{{"ID": "g-work", "Name": "Work", "Color": "#8080FF"}}}
	svc := New(d, testKeys(&keys.Unlocked{UserKR: testKeyRing(t)}))
	file := []string{
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane Roe\r\nUID:u1\r\n" +
			"item1.EMAIL:jane@example.com\r\nitem1.CATEGORIES:Climbing\r\n" +
			"item2.EMAIL:jane@work.example\r\nitem2.CATEGORIES:Work\r\nEND:VCARD",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Bob\r\nUID:u2\r\nEMAIL:bob@example.com\r\nCATEGORIES:Work,Climbing\r\nEND:VCARD",
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Nobody\r\nUID:u3\r\nCATEGORIES:Work\r\nEND:VCARD",
	}
	res, err := svc.Import(context.Background(), file, ImportOptions{Groups: true, GroupColor: "#8080FF"})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(res.Imported) != 3 || len(res.Skipped) != 0 {
		t.Fatalf("imported %d, skipped %v", len(res.Imported), res.Skipped)
	}
	if strings.Join(d.created, ",") != "Climbing" {
		t.Errorf("created %v, want only the group the account did not have", d.created)
	}
	if got := strings.Join(d.labelled["g-work"], ","); got != "e0-1,e1-0" {
		t.Errorf("Work holds %q, want jane@work.example and bob@example.com", got)
	}
	if got := strings.Join(d.labelled["group-Climbing"], ","); got != "e0-0,e1-0" {
		t.Errorf("Climbing holds %q, want jane@example.com and bob@example.com", got)
	}
	if res.Grouped != 4 || strings.Join(res.GroupsUsed, ",") != "Climbing,Work" || strings.Join(res.GroupsCreated, ",") != "Climbing" {
		t.Errorf("the result does not say what happened: %+v", res)
	}
	// A contact with no address has no address to put anywhere, as in Proton's
	// own importer, and says nothing about it.
	if len(res.GroupsFailed) != 0 {
		t.Errorf("a card with no address was reported as a failure: %v", res.GroupsFailed)
	}
}

func TestImportLeavesGroupsOutWhenAsked(t *testing.T) {
	d := &bookDoer{}
	svc := New(d, testKeys(&keys.Unlocked{UserKR: testKeyRing(t)}))
	file := []string{"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Bob\r\nUID:u2\r\nEMAIL:bob@example.com\r\nCATEGORIES:Work\r\nEND:VCARD"}
	res, err := svc.Import(context.Background(), file, ImportOptions{})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(d.created) != 0 || len(d.labelled) != 0 || res.Grouped != 0 {
		t.Errorf("groups were applied under --no-groups: created %v, labelled %v", d.created, d.labelled)
	}
}

// Membership is read where it lives - the labels - and joined to the names.
func TestMembershipJoinsLabelsToNames(t *testing.T) {
	d := &bookDoer{
		groups: []map[string]any{{"ID": "g-work", "Name": "Work"}, {"ID": "g-climb", "Name": "Climbing"}},
		emails: []map[string]any{
			{"ID": "e1", "ContactID": "c1", "Email": "Jane@Example.com", "LabelIDs": []string{"g-work", "g-climb"}},
			{"ID": "e2", "ContactID": "c1", "Email": "jane@work.example", "LabelIDs": []string{}},
			{"ID": "e3", "ContactID": "c2", "Email": "bob@example.com", "LabelIDs": []string{"g-climb", "gone"}},
		},
	}
	got, err := New(d, testKeys(&keys.Unlocked{UserKR: testKeyRing(t)})).Membership(context.Background())
	if err != nil {
		t.Fatalf("Membership: %v", err)
	}
	if strings.Join(got["c1"]["jane@example.com"], ",") != "Work,Climbing" {
		t.Errorf("c1's groups = %v", got["c1"])
	}
	if _, ok := got["c1"]["jane@work.example"]; ok {
		t.Error("an address in no group has a membership")
	}
	if strings.Join(got["c2"]["bob@example.com"], ",") != "Climbing" {
		t.Errorf("c2's groups = %v, want a label with no group left out", got["c2"])
	}
}

// An edit keeps the stored copy of the groups, by address: the signed card
// numbers its addresses afresh, and the copy has to follow the person.
func TestUpdateKeepsTheGroupsWithTheirAddresses(t *testing.T) {
	kr := testKeyRing(t)
	signed := vcard.BuildSigned(vcard.Signed{Name: "Jane", UID: "u1", Emails: []vcard.SignedEmail{
		{Address: "a@example.com"}, {Address: "b@example.com"},
	}})
	clear := map[string]any{"Type": float64(pgp.CardClear), "Data": "BEGIN:VCARD\r\nVERSION:4.0\r\nitem1.CATEGORIES:Work\r\nEND:VCARD"}
	d := &contactDoer{cards: []map[string]any{signedCard(t, kr, signed), clear}}

	svc := New(d, testKeys(&keys.Unlocked{UserKR: kr}))
	if _, err := svc.Update(context.Background(), "c1", NewContact{Emails: []string{"b@example.com", "a@example.com"}}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	written := putCard(t, d, pgp.CardClear).Data
	if !strings.Contains(written, "item2.CATEGORIES:Work") || strings.Contains(written, "item1.CATEGORIES") {
		t.Errorf("Work did not follow a@example.com to its new place:\n%s", written)
	}
}
