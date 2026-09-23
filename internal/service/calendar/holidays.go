package calendar

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Holidays is one of the holidays calendars Proton offers: a country's public
// holidays, in one language.
//
// Its ID is the ID the calendar has once it is added, because adding one joins
// the calendar Proton keeps rather than making a copy of it.
type Holidays struct {
	ID           string `json:"id"`
	Country      string `json:"country"`
	CountryCode  string `json:"country_code"`
	Language     string `json:"language"`
	LanguageCode string `json:"language_code"`
	// Added says whether the calendar is already one of the account's.
	Added bool `json:"added"`
}

// directoryEntry is one holidays calendar as Proton's directory hands it over.
//
// The passphrase and the session key are what joining the calendar takes, and
// Proton gives them to every account that asks. They stay here rather than in
// what a listing shows.
type directoryEntry struct {
	CalendarID   string
	Country      string
	CountryCode  string
	Hidden       bool
	Language     string
	LanguageCode string
	Passphrase   string
	SessionKey   struct {
		Key       string
		Algorithm string
	}
}

// directoryEntries reads the holidays calendars Proton offers.
//
// Proton marks some of them hidden, and its clients offer none of those, so
// neither does this.
func (s *Service) directoryEntries(ctx context.Context) ([]directoryEntry, error) {
	return s.directory.Do("", func() ([]directoryEntry, error) {
		var r struct{ Calendars []directoryEntry }
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/calendar/v1/directory",
			Query: url.Values{"Type": {strconv.Itoa(typeHolidays)}},
		}, &r); err != nil {
			return nil, err
		}
		offered := make([]directoryEntry, 0, len(r.Calendars))
		for _, e := range r.Calendars {
			if !e.Hidden {
				offered = append(offered, e)
			}
		}
		// Recorded and not counted: a hidden calendar is one no client offers, so
		// no answer is short for leaving it out - but a country somebody expected
		// and cannot find is explained by this line.
		slog.DebugContext(ctx, "read the holidays directory",
			"count", len(offered), "hidden", len(r.Calendars)-len(offered))
		return offered, nil
	})
}

// HolidaysDirectory lists the holidays calendars Proton offers, by country and
// then by language, and says which of them the account already has.
func (s *Service) HolidaysDirectory(ctx context.Context) ([]Holidays, error) {
	var entries []directoryEntry
	var cals []Calendar
	if err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			entries, err = s.directoryEntries(ctx)
			return err
		},
		func(ctx context.Context) error {
			var err error
			cals, err = s.CalendarsList(ctx)
			return err
		},
	); err != nil {
		return nil, err
	}
	added := make(map[string]bool, len(cals))
	for _, c := range cals {
		added[c.ID] = true
	}
	out := make([]Holidays, 0, len(entries))
	for _, e := range entries {
		out = append(out, Holidays{
			ID: e.CalendarID, Country: e.Country, CountryCode: strings.ToUpper(e.CountryCode),
			Language: e.Language, LanguageCode: strings.ToLower(e.LanguageCode),
			Added: added[e.CalendarID],
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := strings.ToLower(out[i].Country), strings.ToLower(out[j].Country); a != b {
			return a < b
		}
		return strings.ToLower(out[i].Language) < strings.ToLower(out[j].Language)
	})
	return out, nil
}

// HolidaysAdd adds one of the holidays calendars Proton offers to the account,
// and returns the ID it has there.
//
// Adding one is joining the calendar Proton keeps: the key its passphrase is
// sealed with is sealed again to the account's primary address, and the
// passphrase is signed with that address's key, which is the membership Proton's
// own clients make. It starts the way they start one - with no reminders, and
// without making you look busy.
func (s *Service) HolidaysAdd(ctx context.Context, calendarID, color string) (string, error) {
	var entries []directoryEntry
	u, err := s.keys.Alongside(ctx, func(ctx context.Context) error {
		var err error
		entries, err = s.directoryEntries(ctx)
		return err
	})
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(entries, func(e directoryEntry) bool { return e.CalendarID == calendarID })
	if i < 0 {
		return "", &errs.NotFound{Kind: "holidays calendar", Ref: calendarID}
	}
	rings, addr, err := u.PrimaryAddr()
	if err != nil {
		return "", err
	}
	body, err := holidaysMembership(entries[i], rings.Write, color)
	if err != nil {
		return "", err
	}
	var r struct {
		Calendar         struct{ ID string }
		CalendarSettings struct{ MakesUserBusy int }
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/calendar/v1/" + calendarID + "/invitations/" + addr.ID + "/join",
		Body: body,
	}, &r); err != nil {
		return "", err
	}
	id := r.Calendar.ID
	if id == "" {
		id = calendarID
	}
	if r.CalendarSettings.MakesUserBusy != 0 {
		off := false
		if err := s.CalendarDefaultsUpdate(ctx, id, DefaultsPatch{Busy: &off}); err != nil {
			slog.WarnContext(ctx, "The holidays calendar was added, but it still makes you look busy to others.",
				"calendar", id, "error", err)
		}
	}
	return id, nil
}

// holidaysMembership is what joining a holidays calendar sends: the key that
// opens its passphrase, sealed to the address joining, and the passphrase signed
// by that address - exactly what a member's copy of the passphrase is checked
// against when the calendar is opened.
func holidaysMembership(e directoryEntry, addrKR *pgp.KeyRing, color string) (map[string]any, error) {
	token, err := base64.StdEncoding.DecodeString(e.SessionKey.Key)
	if err != nil {
		return nil, fmt.Errorf("read the holidays calendar's session key: %w", err)
	}
	keyPacket, err := addrKR.EncryptSessionKey(pgp.NewSessionKeyFromToken(token, e.SessionKey.Algorithm))
	if err != nil {
		return nil, fmt.Errorf("seal the holidays calendar's key: %w", err)
	}
	signature, err := addrKR.SignDetached(pgp.NewPlainMessageFromString(e.Passphrase))
	if err != nil {
		return nil, fmt.Errorf("sign the holidays calendar's passphrase: %w", err)
	}
	armored, err := signature.GetArmored()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"PassphraseKeyPacket":         base64.StdEncoding.EncodeToString(keyPacket),
		"Signature":                   armored,
		"Color":                       color,
		"DefaultFullDayNotifications": []map[string]any{},
	}, nil
}
