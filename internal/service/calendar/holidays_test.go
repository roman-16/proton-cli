package calendar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

// What joining sends has to be a membership the calendar then opens with: the
// key packet beside the calendar's own data packet decrypts to the passphrase,
// and the signature over it verifies - the two checks unlockCalendar makes of
// every member's copy of the passphrase.
func TestJoiningAHolidaysCalendarMakesAMembershipThatOpens(t *testing.T) {
	addrKR := addressKeyRing(t)
	entry, dataPacket := directoryEntryFor(t, "hol1", "Austria", "at", "Deutsch", "de")

	body, err := holidaysMembership(entry, addrKR, "#8080FF")
	if err != nil {
		t.Fatalf("holidaysMembership: %v", err)
	}
	keyPacket, err := base64.StdEncoding.DecodeString(body["PassphraseKeyPacket"].(string))
	if err != nil {
		t.Fatalf("the key packet is not base64: %v", err)
	}
	opened, err := addrKR.Decrypt(pgp.NewPGPSplitMessage(keyPacket, dataPacket).GetPGPMessage(), nil, 0)
	if err != nil {
		t.Fatalf("the membership does not open with the address's key: %v", err)
	}
	if opened.GetString() != entry.Passphrase {
		t.Fatalf("the membership opens to %q, want the calendar's passphrase", opened.GetString())
	}
	signature, err := pgp.NewPGPSignatureFromArmored(body["Signature"].(string))
	if err != nil {
		t.Fatalf("the signature is not armored: %v", err)
	}
	if err := addrKR.VerifyDetached(opened, signature, pgp.GetUnixTime()); err != nil {
		t.Errorf("the signature does not verify over the passphrase: %v", err)
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"DefaultFullDayNotifications":[]`) {
		t.Errorf("a new holidays calendar has to be sent with no reminders, as an empty list: %s", raw)
	}
	if body["Color"] != "#8080FF" {
		t.Errorf("Color = %v, want the one asked for", body["Color"])
	}
}

// The directory is what Proton offers, which leaves out what Proton marks
// hidden, and says which of the rest the account already has.
func TestTheHolidaysDirectoryOffersWhatProtonOffersAndSaysWhatIsAdded(t *testing.T) {
	switzerland, _ := directoryEntryFor(t, "ch-de", "Switzerland", "ch", "Deutsch", "de")
	french, _ := directoryEntryFor(t, "ch-fr", "Switzerland", "ch", "Français", "fr")
	austria, _ := directoryEntryFor(t, "at", "Austria", "at", "Deutsch", "de")
	gone, _ := directoryEntryFor(t, "old", "Atlantis", "xa", "Latin", "la")
	gone.Hidden = true
	d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
		switch r.Path {
		case "/calendar/v1/directory":
			if r.Query.Get("Type") != "2" {
				t.Errorf("the directory was asked for type %q, want the holidays type", r.Query.Get("Type"))
			}
			return json.Marshal(map[string]any{"Calendars": []directoryEntry{french, gone, switzerland, austria}})
		case "/calendar/v1":
			return calendarListing(listed{id: "ch-fr", typ: typeHolidays}), nil
		}
		return []byte(`{}`), nil
	}}

	rows, err := New(d, testKeys(nil)).HolidaysDirectory(context.Background())
	if err != nil {
		t.Fatalf("HolidaysDirectory: %v", err)
	}
	var got []string
	for _, h := range rows {
		got = append(got, h.ID)
	}
	if strings.Join(got, " ") != "at ch-de ch-fr" {
		t.Errorf("offered %v, want Austria, then Switzerland by language, and nothing hidden", got)
	}
	for _, h := range rows {
		if h.Added != (h.ID == "ch-fr") {
			t.Errorf("%s added = %v", h.ID, h.Added)
		}
		if h.CountryCode != strings.ToUpper(h.CountryCode) {
			t.Errorf("%s carries the code %q, want it in capitals", h.ID, h.CountryCode)
		}
	}
}

// Adding one joins it as the account's primary address, and a calendar Proton
// starts out making you look busy is turned back, as the web client leaves one.
func TestAddingAHolidaysCalendarJoinsItAndLeavesYouLookingFree(t *testing.T) {
	for _, tc := range []struct {
		name     string
		busy     int
		wantsPut bool
	}{
		{"Proton starts it busy", 1, true},
		{"Proton starts it free", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addrKR := addressKeyRing(t)
			austria, _ := directoryEntryFor(t, "hol1", "Austria", "at", "Deutsch", "de")
			d := &routeDoer{handler: func(r proton.Request) ([]byte, error) {
				switch {
				case r.Path == "/calendar/v1/directory":
					return json.Marshal(map[string]any{"Calendars": []directoryEntry{austria}})
				case strings.HasSuffix(r.Path, "/join"):
					return json.Marshal(map[string]any{
						"Calendar":         map[string]any{"ID": "hol1"},
						"CalendarSettings": map[string]any{"MakesUserBusy": tc.busy},
					})
				}
				return []byte(`{}`), nil
			}}
			u := &keys.Unlocked{
				Addresses: []keys.Address{{ID: "addr1", Email: "me@proton.me"}},
				AddrKRs:   map[string]keys.Rings{"addr1": {Read: addrKR, Write: addrKR}},
			}

			id, err := New(d, testKeys(u)).HolidaysAdd(context.Background(), "hol1", "#8080FF")
			if err != nil {
				t.Fatalf("HolidaysAdd: %v", err)
			}
			if id != "hol1" {
				t.Errorf("returned %q, want the calendar's ID", id)
			}
			var joined bool
			for _, r := range d.reqs {
				if r.Method == "POST" && r.Path == "/calendar/v1/hol1/invitations/addr1/join" {
					joined = true
				}
			}
			if !joined {
				t.Error("the calendar was not joined as the primary address")
			}
			put, ok := d.putTo("/calendar/v1/hol1/settings")
			if ok != tc.wantsPut {
				t.Fatalf("a settings change was sent = %v, want %v", ok, tc.wantsPut)
			}
			if ok && put.Body.(map[string]any)["MakesUserBusy"] != 0 {
				t.Errorf("the settings change sent %v, want MakesUserBusy 0", put.Body)
			}
		})
	}
}

func addressKeyRing(t *testing.T) *pgp.KeyRing {
	t.Helper()
	key, err := pgp.GenerateKey("Me", "me@proton.me", "x25519", 0)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("key ring: %v", err)
	}
	return kr
}

// directoryEntryFor is a holidays calendar the way Proton's directory hands one
// over, and the data packet its passphrase is stored under, which a member's key
// packet is joined to.
func directoryEntryFor(t *testing.T, id, country, code, language, languageCode string) (directoryEntry, []byte) {
	t.Helper()
	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		t.Fatalf("session key: %v", err)
	}
	passphrase := "passphrase of " + id
	dataPacket, err := sk.Encrypt(pgp.NewPlainMessageFromString(passphrase))
	if err != nil {
		t.Fatalf("encrypt the passphrase: %v", err)
	}
	e := directoryEntry{
		CalendarID: id, Country: country, CountryCode: code,
		Language: language, LanguageCode: languageCode, Passphrase: passphrase,
	}
	e.SessionKey.Key = base64.StdEncoding.EncodeToString(sk.Key)
	e.SessionKey.Algorithm = sk.Algo
	return e, dataPacket
}
