// Package calendar provides Proton Calendar operations.
package calendar

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/keys"
	pgphelper "github.com/roman-16/proton-cli/internal/pgp"
)

// Service is the Calendar domain service.
type Service struct{ C *api.Client }

// New constructs a calendar service.
func New(c *api.Client) *Service { return &Service{C: c} }

// Calendar is a Proton calendar entry.
type Calendar struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description,omitempty"`
	MemberCount int    `json:"member_count"`
}

// Event is a decrypted calendar event.
type Event struct {
	ID         string    `json:"id"`
	CalendarID string    `json:"calendar_id"`
	Title      string    `json:"title"`
	Location   string    `json:"location,omitempty"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	AllDay     bool      `json:"all_day"`
	UID        string    `json:"uid,omitempty"`
}

// calKeys holds unlocked calendar material.
type calKeys struct {
	calKR    *pgp.KeyRing
	addrKR   *pgp.KeyRing
	memberID string
}

// CalendarsList returns all calendars on the account.
//
// Proton returns per-user calendar preferences (Name, Color, Description)
// under Members[] rather than on the top-level calendar, so we read from
// the first member.
func (s *Service) CalendarsList(ctx context.Context) ([]Calendar, error) {
	var r struct {
		Calendars []struct {
			ID      string
			Members []struct {
				Name        string
				Color       string
				Description string
				Email       string
			}
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1"}, &r); err != nil {
		return nil, err
	}
	out := make([]Calendar, 0, len(r.Calendars))
	for _, c := range r.Calendars {
		var name, color, desc string
		if len(c.Members) > 0 {
			name = c.Members[0].Name
			color = c.Members[0].Color
			desc = c.Members[0].Description
		}
		out = append(out, Calendar{ID: c.ID, Name: name, Color: color, Description: desc, MemberCount: len(c.Members)})
	}
	return out, nil
}

// CalendarCreate creates a new calendar on the primary address.
func (s *Service) CalendarCreate(ctx context.Context, u *keys.Unlocked, name, color string) ([]byte, error) {
	_, addrID, _, err := u.PrimaryAddrKR()
	if err != nil {
		return nil, err
	}
	resp, err := s.C.Do(ctx, api.Request{
		Method: "POST", Path: "/calendar/v1",
		Body: map[string]any{"Name": name, "Color": color, "Display": 1, "AddressID": addrID},
	})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// CalendarDelete deletes a calendar. Requires scope unlock by the caller.
func (s *Service) CalendarDelete(ctx context.Context, id string) error {
	return s.C.Send(ctx, api.Request{Method: "DELETE", Path: "/calendar/v1/" + id}, nil)
}

// ResolveCalendarID accepts a literal ID or a name.
func (s *Service) ResolveCalendarID(ctx context.Context, nameOrID string) (string, error) {
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return "", err
	}
	if nameOrID == "" {
		if len(cals) == 0 {
			return "", fmt.Errorf("no calendars found")
		}
		return cals[0].ID, nil
	}
	for _, c := range cals {
		if c.ID == nameOrID {
			return c.ID, nil
		}
	}
	for _, c := range cals {
		if strings.EqualFold(c.Name, nameOrID) {
			return c.ID, nil
		}
	}
	return "", fmt.Errorf("calendar %q not found", nameOrID)
}

// EventsList returns decrypted events in the given time range.
func (s *Service) EventsList(ctx context.Context, u *keys.Unlocked, calendarID string, start, end time.Time) ([]Event, error) {
	ck, err := s.unlockCalendar(ctx, u, calendarID)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("Start", fmt.Sprintf("%d", start.Unix()))
	q.Set("End", fmt.Sprintf("%d", end.Unix()))
	q.Set("Timezone", "UTC")
	q.Set("Type", "0")

	var r struct {
		Events []struct {
			ID              string
			CalendarID      string
			StartTime       int64
			EndTime         int64
			FullDay         int
			UID             string
			SharedKeyPacket string
			SharedEvents    []map[string]any
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/events", Query: q}, &r); err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(r.Events))
	for _, e := range r.Events {
		title, location := decryptTitleLocation(e.SharedEvents, e.SharedKeyPacket, ck)
		out = append(out, Event{
			ID: e.ID, CalendarID: e.CalendarID,
			Title: title, Location: location,
			Start: time.Unix(e.StartTime, 0), End: time.Unix(e.EndTime, 0),
			AllDay: e.FullDay == 1, UID: e.UID,
		})
	}
	return out, nil
}

// EventGet returns a single decrypted event.
func (s *Service) EventGet(ctx context.Context, u *keys.Unlocked, calendarID, eventID string) (*Event, error) {
	ck, err := s.unlockCalendar(ctx, u, calendarID)
	if err != nil {
		return nil, err
	}
	var r struct {
		Event struct {
			ID              string
			CalendarID      string
			StartTime       int64
			EndTime         int64
			FullDay         int
			UID             string
			SharedKeyPacket string
			SharedEvents    []map[string]any
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/events/" + eventID}, &r); err != nil {
		return nil, err
	}
	title, location := decryptTitleLocation(r.Event.SharedEvents, r.Event.SharedKeyPacket, ck)
	return &Event{
		ID: r.Event.ID, CalendarID: r.Event.CalendarID,
		Title: title, Location: location,
		Start: time.Unix(r.Event.StartTime, 0), End: time.Unix(r.Event.EndTime, 0),
		AllDay: r.Event.FullDay == 1, UID: r.Event.UID,
	}, nil
}

// EventCreate creates a new event.
func (s *Service) EventCreate(ctx context.Context, u *keys.Unlocked, calendarID, title, location string, start, end time.Time, allDay bool) ([]byte, error) {
	ck, err := s.unlockCalendar(ctx, u, calendarID)
	if err != nil {
		return nil, err
	}
	signed := ical.SignedVEVENT(ical.EventUID(), start, end, allDay, 0)
	encrypted := ical.EncryptedVEVENT(title, location)
	signedCard, encCard, keyPacket, err := pgphelper.EncryptAndSignCardSplit(signed, encrypted, ck.calKR, ck.addrKR, "")
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"MemberID": ck.memberID,
		"Events": []map[string]any{{
			"Overwrite": 0,
			"Event": map[string]any{
				"Permissions":        63,
				"IsOrganizer":        1,
				"SharedKeyPacket":    keyPacket,
				"SharedEventContent": []any{signedCard, encCard},
				"Notifications":      nil,
				"Color":              nil,
			},
		}},
	}
	resp, err := s.C.Do(ctx, api.Request{Method: "PUT", Path: "/calendar/v1/" + calendarID + "/events/sync", Body: body})
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// EventUpdate updates an existing event. Empty fields are left unchanged.
func (s *Service) EventUpdate(ctx context.Context, u *keys.Unlocked, calendarID, eventID, title, location string, start, end time.Time) error {
	ck, err := s.unlockCalendar(ctx, u, calendarID)
	if err != nil {
		return err
	}
	var r struct {
		Event struct {
			UID             string
			StartTime       int64
			EndTime         int64
			FullDay         int
			SharedEvents    []map[string]any
			SharedKeyPacket string
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/events/" + eventID}, &r); err != nil {
		return err
	}

	curTitle, curLoc := decryptTitleLocation(r.Event.SharedEvents, r.Event.SharedKeyPacket, ck)
	if title == "" {
		title = curTitle
	}
	if location == "" {
		location = curLoc
	}
	if start.IsZero() {
		start = time.Unix(r.Event.StartTime, 0)
	}
	if end.IsZero() {
		end = time.Unix(r.Event.EndTime, 0)
	}

	signed := ical.SignedVEVENT(r.Event.UID, start, end, r.Event.FullDay == 1, 1)
	encrypted := ical.EncryptedVEVENT(title, location)
	signedCard, encCard, _, err := pgphelper.EncryptAndSignCardSplit(signed, encrypted, ck.calKR, ck.addrKR, r.Event.SharedKeyPacket)
	if err != nil {
		return err
	}
	body := map[string]any{
		"MemberID": ck.memberID,
		"Events": []map[string]any{{
			"ID": eventID,
			"Event": map[string]any{
				"Permissions":        63,
				"IsOrganizer":        1,
				"SharedEventContent": []any{signedCard, encCard},
				"Notifications":      nil,
				"Color":              nil,
			},
		}},
	}
	return s.C.Send(ctx, api.Request{Method: "PUT", Path: "/calendar/v1/" + calendarID + "/events/sync", Body: body}, nil)
}

// EventDelete deletes an event.
func (s *Service) EventDelete(ctx context.Context, u *keys.Unlocked, calendarID, eventID string) error {
	ck, err := s.unlockCalendar(ctx, u, calendarID)
	if err != nil {
		return err
	}
	return s.C.Send(ctx, api.Request{
		Method: "PUT", Path: "/calendar/v1/" + calendarID + "/events/sync",
		Body: map[string]any{
			"MemberID": ck.memberID,
			"Events":   []map[string]any{{"ID": eventID}},
		},
	}, nil)
}

// ResolveEvent accepts either two args (calendarID eventID) or a single title
// to search across calendars in the next 30 days.
func (s *Service) ResolveEvent(ctx context.Context, u *keys.Unlocked, args []string) (string, string, error) {
	if len(args) == 2 {
		return args[0], args[1], nil
	}
	needle := args[0]
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return "", "", err
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := start.AddDate(0, 0, 30)

	type match struct {
		cal, ev, title string
		when           time.Time
	}
	var matches []match
	for _, c := range cals {
		events, err := s.EventsList(ctx, u, c.ID, start, end)
		if err != nil {
			continue
		}
		for _, e := range events {
			if e.Title != "" && strings.Contains(strings.ToLower(e.Title), strings.ToLower(needle)) {
				matches = append(matches, match{cal: c.ID, ev: e.ID, title: e.Title, when: e.Start})
			}
		}
	}
	switch len(matches) {
	case 0:
		return "", "", fmt.Errorf("no event matching %q in the next 30 days", needle)
	case 1:
		return matches[0].cal, matches[0].ev, nil
	}
	lines := []string{fmt.Sprintf("ambiguous: %d events match %q:", len(matches), needle)}
	for _, m := range matches {
		lines = append(lines, fmt.Sprintf("  %s  %s  (calendar %s, event %s)",
			m.when.Local().Format("2006-01-02 15:04"), m.title, m.cal, m.ev))
	}
	return "", "", fmt.Errorf("%s", strings.Join(lines, "\n"))
}

func (s *Service) unlockCalendar(ctx context.Context, u *keys.Unlocked, calendarID string) (*calKeys, error) {
	var mem struct {
		Members []struct {
			ID, CalendarID, Email, AddressID string
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/members"}, &mem); err != nil {
		return nil, err
	}
	var addrKR *pgp.KeyRing
	var memberID string
	for _, m := range mem.Members {
		if kr, ok := u.AddrKR(m.AddressID); ok {
			addrKR = kr
			memberID = m.ID
			break
		}
	}
	if addrKR == nil {
		return nil, fmt.Errorf("no matching address key for calendar %s", calendarID)
	}

	var pass struct {
		Passphrase struct {
			MemberPassphrases []struct {
				MemberID, Passphrase, Signature string
			}
		}
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/passphrase"}, &pass); err != nil {
		return nil, err
	}
	var calPass []byte
	for _, mp := range pass.Passphrase.MemberPassphrases {
		if mp.MemberID != memberID {
			continue
		}
		msg, err := pgp.NewPGPMessageFromArmored(mp.Passphrase)
		if err != nil {
			return nil, err
		}
		sig, err := pgp.NewPGPSignatureFromArmored(mp.Signature)
		if err != nil {
			return nil, err
		}
		dec, err := addrKR.Decrypt(msg, nil, pgp.GetUnixTime())
		if err != nil {
			return nil, fmt.Errorf("decrypt calendar passphrase: %w", err)
		}
		if err := addrKR.VerifyDetached(dec, sig, pgp.GetUnixTime()); err != nil {
			return nil, err
		}
		calPass = dec.GetBinary()
		break
	}
	if calPass == nil {
		return nil, fmt.Errorf("no passphrase found for member %s", memberID)
	}

	var keyRes struct {
		Keys []struct{ PrivateKey string }
	}
	if err := s.C.Send(ctx, api.Request{Method: "GET", Path: "/calendar/v1/" + calendarID + "/keys"}, &keyRes); err != nil {
		return nil, err
	}
	calKR, err := pgp.NewKeyRing(nil)
	if err != nil {
		return nil, err
	}
	for _, k := range keyRes.Keys {
		locked, err := pgp.NewKeyFromArmored(k.PrivateKey)
		if err != nil {
			continue
		}
		unlocked, err := locked.Unlock(calPass)
		if err != nil {
			continue
		}
		_ = calKR.AddKey(unlocked)
	}
	if calKR.CountEntities() == 0 {
		return nil, fmt.Errorf("failed to unlock calendar keys")
	}
	return &calKeys{calKR: calKR, addrKR: addrKR, memberID: memberID}, nil
}

func decryptTitleLocation(cards []map[string]any, keyPacket string, ck *calKeys) (string, string) {
	kp, _ := base64.StdEncoding.DecodeString(keyPacket)
	decrypted, err := pgphelper.DecryptCardsRaw(cards, ck.calKR, ck.addrKR, kp)
	if err != nil {
		return "", ""
	}
	joined := strings.Join(decrypted, "\n")
	return ical.Field(joined, "SUMMARY"), ical.Field(joined, "LOCATION")
}

// DefaultRange returns start/end of a default 30-day window.
func DefaultRange() (time.Time, time.Time) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start, start.AddDate(0, 0, 30)
}
