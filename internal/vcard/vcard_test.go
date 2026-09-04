package vcard

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/contentline"
	"testing"
)

const signedCard = "BEGIN:VCARD\r\nVERSION:4.0\r\n" +
	"FN:Jane Doe\r\n" +
	"UID:proton-cli-1\r\n" +
	"item1.EMAIL;PREF=1:jane@example.test\r\n" +
	"item1.KEY;PREF=2:data:application/pgp-keys;base64,SECOND\r\n" +
	"item1.KEY;PREF=1:data:application/pgp-keys;base64,FIRST\r\n" +
	"item1.X-PM-ENCRYPT:true\r\n" +
	"item1.X-PM-SIGN:false\r\n" +
	"item1.X-PM-SCHEME:pgp-mime\r\n" +
	"item2.EMAIL;PREF=2:JANE@work.test\r\n" +
	"END:VCARD"

func TestParseSignedKeepsEachAddressSettings(t *testing.T) {
	c := ParseSigned(signedCard)
	if c.Name != "Jane Doe" || c.UID != "proton-cli-1" {
		t.Errorf("ParseSigned = %+v", c)
	}
	if len(c.Emails) != 2 {
		t.Fatalf("got %d addresses, want 2", len(c.Emails))
	}
	first := c.FindEmail("jane@example.test")
	if first == nil {
		t.Fatal("FindEmail did not find the address")
	}
	if len(first.KeyValues) != 2 || !strings.HasSuffix(first.KeyValues[0], "FIRST") {
		t.Errorf("pinned keys are not in preference order: %+v", first.KeyValues)
	}
	if first.Encrypt == nil || !*first.Encrypt || first.Sign == nil || *first.Sign {
		t.Errorf("crypto flags = %+v %+v", first.Encrypt, first.Sign)
	}
	if first.Scheme != "pgp-mime" {
		t.Errorf("scheme = %q", first.Scheme)
	}
}

func TestFindEmailIgnoresCaseAndSurroundingSpace(t *testing.T) {
	c := ParseSigned(signedCard)
	if c.FindEmail(" jane@WORK.test ") == nil {
		t.Error("FindEmail is case- or space-sensitive")
	}
}

// Rebuilding a contact from its addresses alone would silently unpin its keys, so
// a round trip has to carry them.
func TestBuildSignedRoundTripsPinnedKeysAndFlags(t *testing.T) {
	c := ParseSigned(signedCard)
	back := ParseSigned(BuildSigned(c))
	if len(back.Emails) != len(c.Emails) {
		t.Fatalf("round trip lost addresses: %+v", back.Emails)
	}
	got := back.FindEmail("jane@example.test")
	if got == nil || len(got.KeyValues) != 2 {
		t.Fatalf("round trip lost pinned keys: %+v", got)
	}
	if got.Scheme != "pgp-mime" || got.Encrypt == nil || !*got.Encrypt {
		t.Errorf("round trip lost the crypto settings: %+v", got)
	}
}

func TestEmailGroupFindsTheGroupAnAddressSettingsHangOff(t *testing.T) {
	if g := EmailGroup(signedCard, "JANE@example.test"); g != "item1" {
		t.Errorf("EmailGroup = %q, want item1", g)
	}
	if g := EmailGroup(signedCard, "nobody@example.test"); g != "" {
		t.Errorf("EmailGroup = %q, want empty", g)
	}
}

func TestValuesReturnsEveryValueInDocumentOrder(t *testing.T) {
	card := "BEGIN:VCARD\r\nTEL;PREF=1:+431\r\nTEL;PREF=2:+432\r\nEND:VCARD"
	got := Values(card, "TEL")
	if len(got) != 2 || got[0] != "+431" || got[1] != "+432" {
		t.Errorf("Values = %q", got)
	}
}

func TestBuildEncryptedEscapesTextAndOmitsEmptyProperties(t *testing.T) {
	out := BuildEncrypted(Encrypted{Note: "line one\nline two, with a comma", Org: "Acme"})
	if !strings.Contains(out, `NOTE:line one\nline two\, with a comma`) {
		t.Errorf("note was not escaped:\n%s", out)
	}
	if strings.Contains(out, "BDAY") || strings.Contains(out, "URL") {
		t.Errorf("empty properties were written:\n%s", out)
	}
	if got := Field(out, "NOTE"); got != "line one\nline two, with a comma" {
		t.Errorf("note did not survive a round trip: %q", got)
	}
}

func TestUIDDoesNotRepeat(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		uid := UID()
		if seen[uid] {
			t.Fatal("UID repeated itself")
		}
		seen[uid] = true
		if !strings.HasPrefix(uid, "proton-cli-") {
			t.Errorf("UID = %q, want the proton prefix", uid)
		}
	}
}

// A contact is stored as several cards, each a complete vCard carrying a slice
// of the properties. A file has to be one card with all of them.
func TestDocumentMergesTheCardsIntoOne(t *testing.T) {
	signed := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane Roe\r\nUID:u1\r\nEMAIL:jane@example.com\r\nEND:VCARD"
	encrypted := "BEGIN:VCARD\r\nVERSION:4.0\r\nUID:u1\r\nTEL:+43 1 234567\r\nNOTE:Likes tea\r\nEND:VCARD"

	doc := Document([]string{signed, encrypted})
	for _, want := range []string{"FN:Jane Roe", "EMAIL:jane@example.com", "TEL:+43 1 234567", "NOTE:Likes tea"} {
		if !strings.Contains(doc, want) {
			t.Errorf("merged card is missing %q:\n%s", want, doc)
		}
	}
	// A property vCard allows one of must appear once, however many cards carry it.
	for _, once := range []string{"UID:u1", "VERSION:4.0", "BEGIN:VCARD", "END:VCARD"} {
		if got := strings.Count(doc, once); got != 1 {
			t.Errorf("%q appears %d times, want 1:\n%s", once, got, doc)
		}
	}
}

// A file may hold any number of contacts, and two cards in one file are two
// people rather than one merged person.
func TestParseDocumentsKeepsContactsApart(t *testing.T) {
	file := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:One\r\nEND:VCARD\r\n" +
		"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Two\r\nEND:VCARD\r\n"
	cards := ParseDocuments(file)
	if len(cards) != 2 {
		t.Fatalf("read %d cards, want 2", len(cards))
	}
	if Field(cards[0], "FN") != "One" || Field(cards[1], "FN") != "Two" {
		t.Errorf("cards came back as %q and %q", Field(cards[0], "FN"), Field(cards[1], "FN"))
	}
	if ParseDocuments("") != nil {
		t.Error("an empty file should hold no cards")
	}
}

// The split is Proton's: identity is signed so it can be verified, everything
// else is encrypted because it is nobody's business.
func TestSplitForStoragePutsIdentityInTheSignedCard(t *testing.T) {
	card := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane\r\nUID:u1\r\nEMAIL:jane@example.com\r\n" +
		"TEL:+43 1 234567\r\nBDAY:1990-01-31\r\nX-CUSTOM:whatever\r\nEND:VCARD"
	signed, encrypted := SplitForStorage(card)

	for _, want := range []string{"FN:Jane", "UID:u1", "EMAIL:jane@example.com"} {
		if !strings.Contains(signed, want) {
			t.Errorf("signed card is missing %q:\n%s", want, signed)
		}
	}
	for _, private := range []string{"TEL:", "BDAY:", "X-CUSTOM:"} {
		if strings.Contains(signed, private) {
			t.Errorf("%q belongs in the encrypted card, not the signed one:\n%s", private, signed)
		}
		if !strings.Contains(encrypted, private) {
			t.Errorf("encrypted card is missing %q:\n%s", private, encrypted)
		}
	}
	// VERSION is written fresh by each card and carried by neither.
	if strings.Count(signed, "VERSION:") != 1 || strings.Count(encrypted, "VERSION:") != 1 {
		t.Error("each card writes its own VERSION exactly once")
	}
}

// A contact with nothing private has no encrypted card to store.
func TestAContactWithNothingPrivateHasNoEncryptedCard(t *testing.T) {
	_, encrypted := SplitForStorage("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane\r\nUID:u1\r\nEND:VCARD")
	if HasProperties(encrypted) {
		t.Errorf("a contact with nothing private should have no encrypted card, got:\n%s", encrypted)
	}
}

// The signed card's properties are the signed card's. An edit reads a contact
// back off every card it is stored as, so a property this claimed as its own
// would be written into the encrypted card as well - and into it again on the
// next edit, and again on the one after that.
func TestParseEncryptedClaimsNothingAnotherCardHolds(t *testing.T) {
	signed := BuildSigned(Signed{
		Name: "Jane Roe", UID: "u1",
		Emails: []SignedEmail{{
			Address:   "jane@example.com",
			KeyValues: []string{"data:application/pgp-keys;base64,AAAA"},
			Encrypt:   func() *bool { b := true; return &b }(),
			Scheme:    "pgp-mime",
		}},
	})
	if rest := ParseEncrypted(signed).Rest; len(rest) != 0 {
		t.Errorf("the signed card's properties were claimed as the encrypted card's: %+v", rest)
	}
}

// Editing a contact over and over leaves it the size it was.
func TestRewritingAContactDoesNotGrowIt(t *testing.T) {
	signed := BuildSigned(Signed{
		Name: "Jane Roe", UID: "u1",
		Emails: []SignedEmail{{
			Address:   "jane@example.com",
			KeyValues: []string{"data:application/pgp-keys;base64,AAAA"},
		}},
	})
	encrypted := BuildEncrypted(Encrypted{Note: "Likes tea"})

	for round := 1; round <= 3; round++ {
		// What an edit does: read the contact off every card it is stored as,
		// then write the cards out again.
		joined := signed + "\n" + encrypted
		signed = BuildSigned(ParseSigned(joined))
		encrypted = BuildEncrypted(ParseEncrypted(joined))

		doc := Document([]string{signed, encrypted})
		if got := len(Values(doc, "EMAIL")); got != 1 {
			t.Fatalf("after %d edits the contact holds %d copies of its address:\n%s", round, got, doc)
		}
		if got := len(Values(doc, "KEY")); got != 1 {
			t.Fatalf("after %d edits the contact holds %d copies of its pinned key:\n%s", round, got, doc)
		}
		if got := len(Values(doc, "NOTE")); got != 1 {
			t.Fatalf("after %d edits the contact holds %d copies of its note:\n%s", round, got, doc)
		}
	}
}

// Files from other address books often lack the two things Proton requires.
// Falling back beats dropping the row.
func TestEnsureIdentityFillsInWhatProtonRequires(t *testing.T) {
	// Google writes N and no FN.
	card, ok := EnsureIdentity("BEGIN:VCARD\r\nVERSION:4.0\r\nN:Roe;Jane;;;\r\nEND:VCARD")
	if !ok {
		t.Fatal("a card with a structured name should be importable")
	}
	if got := Field(card, "FN"); got != "Roe Jane" {
		t.Errorf("display name = %q, want it derived from N", got)
	}
	if Field(card, "UID") == "" {
		t.Error("a card with no UID should be given one")
	}

	// No name at all, but an address is enough to file it under.
	card, ok = EnsureIdentity("BEGIN:VCARD\r\nVERSION:4.0\r\nEMAIL:jane@example.com\r\nEND:VCARD")
	if !ok || Field(card, "FN") != "jane@example.com" {
		t.Errorf("an address should stand in for a name, got %q (ok=%v)", Field(card, "FN"), ok)
	}

	// Nothing to file it under at all.
	if _, ok := EnsureIdentity("BEGIN:VCARD\r\nVERSION:4.0\r\nNOTE:orphan\r\nEND:VCARD"); ok {
		t.Error("a card with no name and no address should be refused")
	}

	// A card that already has both is left exactly as it was.
	whole := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane\r\nUID:u1\r\nEND:VCARD"
	if got, _ := EnsureIdentity(whole); got != whole {
		t.Errorf("a complete card should be untouched, got:\n%s", got)
	}
}

// Export and import are each other's inverse at the card level.
func TestDocumentAndSplitRoundTrip(t *testing.T) {
	original := "BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Jane Roe\r\nUID:u1\r\n" +
		"EMAIL:jane@example.com\r\nTEL:+43 1 234567\r\nNOTE:Likes tea\r\nEND:VCARD"
	signed, encrypted := SplitForStorage(original)
	back := Document([]string{signed, encrypted})
	for _, want := range []string{"FN:Jane Roe", "UID:u1", "EMAIL:jane@example.com", "TEL:+43 1 234567", "NOTE:Likes tea"} {
		if !strings.Contains(back, want) {
			t.Errorf("round trip lost %q:\n%s", want, back)
		}
	}
}

// Proton stores an address's key settings under that address's vCard group, so
// an EMAIL with no group has nowhere to hang them and the server refuses the
// whole card. A file written anywhere but Proton ordinarily has no groups at all.
func TestStoringACardGivesEveryAddressAGroup(t *testing.T) {
	signed, _ := SplitForStorage(strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0", "FN:Jane Roe",
		"EMAIL:jane@example.org", "EMAIL:jane@work.example", "END:VCARD",
	}, "\r\n"))

	var groups []string
	for _, l := range contentline.ParseAll(signed) {
		if l.Name == "EMAIL" {
			groups = append(groups, l.Group)
		}
	}
	if len(groups) != 2 {
		t.Fatalf("two addresses went in, %d came out", len(groups))
	}
	if groups[0] == "" || groups[1] == "" {
		t.Errorf("an address was stored with no group: %q", groups)
	}
	if groups[0] == groups[1] {
		t.Errorf("both addresses were put in group %q; settings under it could not say which they meant", groups[0])
	}
}

// A card that already names its groups keeps them, since the settings stored
// alongside refer to them by name.
func TestStoringACardKeepsGroupsItAlreadyHas(t *testing.T) {
	signed, _ := SplitForStorage(strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0", "FN:Jane Roe",
		"item3.EMAIL:jane@example.org", "item3.X-PM-SCHEME:pgp-mime", "END:VCARD",
	}, "\r\n"))

	if g := EmailGroup(signed, "jane@example.org"); g != "item3" {
		t.Errorf("the address moved from item3 to %q, orphaning its scheme", g)
	}
}

// A group cannot be handed out twice: the second address needs one of its own,
// and it must not be a group the card is already using for something else.
func TestStoringACardResolvesAGroupClash(t *testing.T) {
	signed, _ := SplitForStorage(strings.Join([]string{
		"BEGIN:VCARD", "VERSION:4.0", "FN:Jane Roe",
		"item1.EMAIL:jane@example.org", "item1.EMAIL:jane@work.example", "END:VCARD",
	}, "\r\n"))

	first := EmailGroup(signed, "jane@example.org")
	second := EmailGroup(signed, "jane@work.example")
	if first == "" || second == "" || first == second {
		t.Errorf("groups %q and %q; each address needs one of its own", first, second)
	}
}
