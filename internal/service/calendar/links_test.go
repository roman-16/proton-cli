package calendar

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

// A link is handed out once and rebuilt from the account afterwards, so what
// the URL carries has to survive the round trip through what Proton stores.
func TestALinksPassphraseSurvivesTheRoundTripThroughWhatProtonStores(t *testing.T) {
	s := &Service{}
	ck := linkKeys(t)

	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		t.Fatalf("session key: %v", err)
	}
	masked, err := s.maskPassphrase(ck, sk.Key)
	if err != nil {
		t.Fatalf("mask: %v", err)
	}
	if string(masked) == string(sk.Key) {
		t.Error("what is stored is the link's key unchanged")
	}

	back, err := s.unmaskPassphrase(ck, base64.StdEncoding.EncodeToString(masked))
	if err != nil {
		t.Fatalf("unmask: %v", err)
	}
	if string(back) != string(sk.Key) {
		t.Error("the key rebuilt from what Proton stores is not the key the URL carried")
	}
}

// A passphrase of another length cannot be masked, and saying so beats handing
// out a URL that opens nothing.
func TestAPassphraseThatIsNotTheKeysLengthIsRefused(t *testing.T) {
	s := &Service{}
	ck := linkKeys(t)
	ck.passphrase = []byte(base64.StdEncoding.EncodeToString([]byte("eight...")))

	if _, err := s.maskPassphrase(ck, make([]byte, 32)); err == nil {
		t.Error("a passphrase of the wrong length was masked anyway")
	}
}

// Proton checks an arriving key against a hash of the salt and the key, so the
// three have to be made together.
func TestTheCacheKeyHashCoversTheSaltAndTheKey(t *testing.T) {
	key, salt, hash, err := newCacheKey()
	if err != nil {
		t.Fatalf("newCacheKey: %v", err)
	}
	sum := sha256.Sum256([]byte(salt + key))
	if hash != base64.StdEncoding.EncodeToString(sum[:]) {
		t.Error("the hash is not the one Proton will recompute")
	}
	raw, err := base64.URLEncoding.DecodeString(key)
	if err != nil {
		t.Fatalf("the key is not the alphabet a URL carries: %v", err)
	}
	if len(raw) != 16 {
		t.Errorf("the key is %d bytes", len(raw))
	}
	again, _, _, err := newCacheKey()
	if err != nil {
		t.Fatalf("newCacheKey: %v", err)
	}
	if again == key {
		t.Error("two links were given the same key")
	}
}

// A limited link carries one key and a full one carries two: the second is what
// lets Proton read the events to serve them.
func TestOnlyAFullLinkCarriesTheKeyToTheCalendar(t *testing.T) {
	limited := linkURL("8kQ2mXpL", "nR4v+tail==", nil)
	if strings.Contains(limited, "PassphraseKey") {
		t.Errorf("a limited link carries the calendar's key: %s", limited)
	}
	if !strings.HasPrefix(limited, "https://calendar.proton.me/api/calendar/v1/url/8kQ2mXpL/calendar.ics?CacheKey=") {
		t.Errorf("a link is not opened where a calendar app expects: %s", limited)
	}
	escaped, err := url.Parse(limited)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := escaped.Query().Get("CacheKey"); got != "nR4v+tail==" {
		t.Errorf("the key arrived as %q, so it was not escaped into the query string", got)
	}

	full := linkURL("8kQ2mXpL", "nR4v", []byte{0xff, 0xfe, 0xfd})
	parsed, err := url.Parse(full)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	carried := parsed.Query().Get("PassphraseKey")
	key, err := base64.URLEncoding.DecodeString(carried)
	if err != nil {
		t.Fatalf("the key a full link carries is not readable: %v", err)
	}
	if string(key) != string([]byte{0xff, 0xfe, 0xfd}) {
		t.Errorf("the key arrived as %x", key)
	}
}

// The label is the account's own, so it goes out sealed and comes back opened -
// and a link whose label will not open is still a link.
func TestALabelIsSealedToTheCalendarAndOpensAgain(t *testing.T) {
	ck := linkKeys(t)
	sealed, err := sealToCalendar(ck, "Team feed")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(sealed, "Team feed") {
		t.Error("the label left in the clear")
	}

	raw := rawLink{CalendarUrlID: "8kQ2mXpL", EncryptedPurpose: sealed}
	if got := raw.label(context.Background(), ck); got != "Team feed" {
		t.Errorf("label = %q", got)
	}

	unreadable := rawLink{CalendarUrlID: "8kQ2mXpL", EncryptedPurpose: "not a message"}
	if got := unreadable.label(context.Background(), ck); got != "" {
		t.Errorf("label = %q, want it left blank", got)
	}
}

// A listing row cannot hold the URL that opens the calendar, whatever a caller
// does with it.
func TestALinkRowCarriesNoURL(t *testing.T) {
	ck := linkKeys(t)
	row := rawLink{
		CalendarUrlID: "8kQ2mXpL", CalendarID: "cal", AccessLevel: levelFull,
		EncryptedPassphrase: "not the URL", EncryptedCacheKey: "not the URL either",
		CreateTime: 1776000000,
	}.link(context.Background(), "Work", ck)

	if row.Access != AccessFull {
		t.Errorf("access = %q", row.Access)
	}
	if row.Calendar != "Work" || row.ID != "8kQ2mXpL" || row.Created != 1776000000 {
		t.Errorf("row = %+v", row)
	}
}

// linkKeys is a calendar opened: a key ring and the passphrase behind it.
func linkKeys(t *testing.T) *calKeys {
	t.Helper()
	key, err := pgp.GenerateKey("Calendar", "", "x25519", 0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	passphrase := make([]byte, 32)
	for i := range passphrase {
		passphrase[i] = byte(i)
	}
	return &calKeys{
		calKR:      kr,
		passphrase: []byte(base64.StdEncoding.EncodeToString(passphrase)),
	}
}
