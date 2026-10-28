package meet

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v3/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

// The states a new meeting declares, as Proton's useSaveMeeting fills them: no
// password beside the link's, and whether Calendar made it.
const (
	noCustomPassword      = 0
	notFromProtonCalendar = 0
	fromProtonCalendar    = 1
)

// isoTime is how Proton's web client writes a meeting's times, Date.toISOString.
const isoTime = "2006-01-02T15:04:05.000Z"

type createBody struct {
	Name           string
	Password       string
	Salt           string
	SessionKey     string
	SRPModulusID   string
	SRPSalt        string
	SRPVerifier    string
	AddressID      string
	StartTime      *string
	EndTime        *string
	RRule          *string
	Timezone       *string
	CustomPassword int
	Type           int
	ProtonCalendar int
}

type rawMeeting struct {
	ID              string
	MeetingLinkName string
	MeetingName     string
	Password        string
	Salt            string
	SessionKey      string
	StartTime       unixTime
	EndTime         unixTime
	RRule           string
	Timezone        string
	Type            int
	ProtonCalendar  int
	CalendarID      string
	CalendarEventID string
}

// Proton declares a meeting's times as strings of epoch seconds and sends some
// as numbers, which the web client reads alike with Number.
type unixTime struct{ t *time.Time }

func (u *unixTime) UnmarshalJSON(b []byte) error {
	b = bytes.Trim(b, `"`)
	if string(b) == "null" || len(b) == 0 {
		u.t = nil
		return nil
	}
	secs, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("read a meeting's time: %w", err)
	}
	t := time.Unix(secs, 0).UTC()
	u.t = &t
	return nil
}

func (s *Service) Create(ctx context.Context, nm NewMeeting) (*Meeting, error) {
	return s.send(ctx, nm, func(body createBody) proton.Request {
		return proton.Request{Method: "POST", Path: "/meet/v1/meetings", Body: body, Door: proton.MeetDoor}
	})
}

// RotatePersonal gives the personal room a new link. Proton allows it once every
// 24 hours, and says so with this code.
func (s *Service) RotatePersonal(ctx context.Context, title string) (*Meeting, error) {
	m, err := s.send(ctx, NewMeeting{Title: title, Type: Personal}, func(body createBody) proton.Request {
		return proton.Request{Method: "POST", Path: "/meet/v1/meetings/personal/rotate", Body: body, Door: proton.MeetDoor}
	})
	var apiErr *proton.APIError
	switch {
	case errors.As(err, &apiErr) && apiErr.Code == tooMany && strings.Contains(apiErr.Message, "24 hours"):
		return nil, errs.Problemf("Your personal room's link can change once every 24 hours, and the link it has works until then.")
	case proton.DoesNotExist(err):
		return nil, noPersonalRoom()
	}
	return m, err
}

// tooMany is the code Proton refuses a meeting with when an account has made
// too many lately, and a personal room's new link within a day of the last.
const tooMany = 2028

func (s *Service) send(ctx context.Context, nm NewMeeting, request func(createBody) proton.Request) (*Meeting, error) {
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	body, sec, err := s.sealed(ctx, u, nm)
	if err != nil {
		return nil, err
	}
	var r struct{ Meeting rawMeeting }
	if err := s.C.Decode(ctx, request(body), &r); err != nil {
		return nil, err
	}
	m := meeting(r.Meeting, time.Now())
	m.Title = nm.Title
	m.URL = Link(r.Meeting.MeetingLinkName, sec.password)
	m.sessionKey = sec.sessionKey
	return m, nil
}

func (s *Service) sealed(ctx context.Context, u *keys.Unlocked, nm NewMeeting) (createBody, secret, error) {
	primary, err := u.PrimaryUserKey()
	if err != nil {
		return createBody{}, secret{}, err
	}
	addressID := nm.AddressID
	if addressID == "" {
		if addressID, err = hostAddress(u); err != nil {
			return createBody{}, secret{}, err
		}
	}
	sec, err := newSecret()
	if err != nil {
		return createBody{}, secret{}, err
	}
	locked, err := sec.lockedSessionKey()
	if err != nil {
		return createBody{}, secret{}, err
	}
	title, err := sealTitle(sec.sessionKey, nm.Title)
	if err != nil {
		return createBody{}, secret{}, err
	}
	copied, err := sealPassword(primary, sec.password)
	if err != nil {
		return createBody{}, secret{}, err
	}
	modulus, err := proton.FetchModulus(ctx, s.C)
	if err != nil {
		return createBody{}, secret{}, err
	}
	verifier, err := modulus.Verifier([]byte(sec.password))
	if err != nil {
		return createBody{}, secret{}, err
	}
	calendar := notFromProtonCalendar
	if nm.FromCalendar {
		calendar = fromProtonCalendar
	}
	return createBody{
		Name:           title,
		Password:       copied,
		Salt:           base64.StdEncoding.EncodeToString(sec.salt),
		SessionKey:     base64.StdEncoding.EncodeToString(locked),
		SRPModulusID:   verifier.ModulusID,
		SRPSalt:        verifier.Salt,
		SRPVerifier:    verifier.Value,
		AddressID:      addressID,
		StartTime:      isoOrNull(nm.Start),
		EndTime:        isoOrNull(nm.End),
		RRule:          orNull(nm.RRule),
		Timezone:       orNull(nm.Timezone),
		CustomPassword: noCustomPassword,
		Type:           int(nm.Type),
		ProtonCalendar: calendar,
	}, sec, nil
}

func (s *Service) List(ctx context.Context) ([]Meeting, error) {
	var r struct{ Meetings []rawMeeting }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/meet/v1/meetings/upcoming", Door: proton.MeetDoor,
	}, &r); err != nil {
		return nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	out := make([]Meeting, 0, len(r.Meetings))
	for _, raw := range r.Meetings {
		m, err := open(u, raw, "", now)
		if err != nil {
			skip.Record(ctx, skip.KindMeeting, raw.ID, shut(u, raw), err)
			continue
		}
		out = append(out, *m)
	}
	return out, nil
}

func (s *Service) Personal(ctx context.Context) (*Meeting, error) {
	all, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Type == Personal {
			return &all[i], nil
		}
	}
	return nil, noPersonalRoom()
}

func noPersonalRoom() error {
	return &errs.NotFound{
		Kind: "personal room", Where: "on this account yet",
		Try: []string{"open https://" + linkHost + " once: Proton Meet makes your personal room on the first visit"},
	}
}

func (s *Service) ByLink(ctx context.Context, name, password string) (*Meeting, error) {
	var r struct{ Meeting rawMeeting }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/meet/v1/meetings/by-link/" + name, Door: proton.MeetDoor,
	}, &r); err != nil {
		if proton.DoesNotExist(err) {
			return nil, &errs.NotFound{Kind: "meeting", Ref: name}
		}
		return nil, err
	}
	u, err := s.keys(ctx)
	if err != nil {
		return nil, err
	}
	return open(u, r.Meeting, password, time.Now())
}

func open(u *keys.Unlocked, raw rawMeeting, password string, now time.Time) (*Meeting, error) {
	if password == "" {
		if raw.Password == "" {
			return nil, errors.New("open the meeting: Proton keeps no copy of its password for this account")
		}
		var err error
		if password, err = openPassword(u.UserKR, raw.Password); err != nil {
			return nil, err
		}
	}
	salt, err := base64.StdEncoding.DecodeString(raw.Salt)
	if err != nil {
		return nil, fmt.Errorf("read the meeting's salt: %w", err)
	}
	locked, err := base64.StdEncoding.DecodeString(raw.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("read the meeting's session key: %w", err)
	}
	sk, err := unlockSessionKey(locked, password, salt)
	if err != nil {
		return nil, err
	}
	title := ""
	if raw.MeetingName != "" {
		if title, err = openTitle(sk, raw.MeetingName); err != nil {
			return nil, err
		}
	}
	m := meeting(raw, now)
	m.Title = title
	m.URL = Link(raw.MeetingLinkName, password)
	m.sessionKey = sk
	return m, nil
}

func (s *Service) Rename(ctx context.Context, m *Meeting, title string) error {
	sealed, err := m.SealTitle(title)
	if err != nil {
		return err
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/meet/v1/meetings/" + m.ID + "/name",
		Body: map[string]string{"Name": sealed}, Door: proton.MeetDoor,
	}, nil); err != nil {
		return gone(m, err)
	}
	m.Title = title
	return nil
}

func (s *Service) Reschedule(ctx context.Context, m *Meeting, start, end time.Time, rrule, zone string) error {
	var r struct{ Meeting *rawMeeting }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/meet/v1/meetings/" + m.ID + "/schedule",
		Body: map[string]any{
			"StartTime": start.UTC().Format(isoTime),
			"EndTime":   end.UTC().Format(isoTime),
			"Timezone":  zone,
			"RRule":     orNull(rrule),
		},
		Door: proton.MeetDoor,
	}, &r); err != nil {
		return gone(m, err)
	}
	if r.Meeting == nil {
		return fmt.Errorf("reschedule meeting %s: Proton's answer carries no meeting", m.Name)
	}
	moved := meeting(*r.Meeting, time.Now())
	m.Type, m.Start, m.End, m.Next = moved.Type, moved.Start, moved.End, moved.Next
	m.RRule, m.Timezone = moved.RRule, moved.Timezone
	return nil
}

func (s *Service) Delete(ctx context.Context, m *Meeting) error {
	return gone(m, s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: "/meet/v1/meetings/" + m.ID, Door: proton.MeetDoor,
	}, nil))
}

func gone(m *Meeting, err error) error {
	if proton.DoesNotExist(err) {
		return &errs.NotFound{Kind: "meeting", Ref: m.Name}
	}
	return err
}

func shut(u *keys.Unlocked, raw rawMeeting) skip.Reason {
	if raw.Password == "" {
		return skip.NoKey
	}
	msg, err := pgp.NewPGPMessageFromArmored(raw.Password)
	if err != nil {
		return skip.Malformed
	}
	return u.Shut(msg)
}

func meeting(raw rawMeeting, now time.Time) *Meeting {
	m := &Meeting{
		ID:              raw.ID,
		Name:            raw.MeetingLinkName,
		Type:            Type(raw.Type),
		Start:           raw.StartTime.t,
		End:             raw.EndTime.t,
		Timezone:        raw.Timezone,
		RRule:           raw.RRule,
		CalendarID:      raw.CalendarID,
		CalendarEventID: raw.CalendarEventID,
		madeByCalendar:  raw.ProtonCalendar == fromProtonCalendar,
	}
	m.Next = next(*m, now)
	return m
}

// next is getNextOccurrence in Proton's Meet web app: a recurring meeting's
// first occurrence that has not ended yet, and any other meeting's own start.
func next(m Meeting, now time.Time) *time.Time {
	if m.Start == nil || m.Type != Recurring || m.RRule == "" {
		return m.Start
	}
	zone := m.Timezone
	if zone == "" {
		zone = "UTC"
	}
	end := m.Start.Add(time.Hour)
	if m.End != nil {
		end = *m.End
	}
	series := ical.VEvent{Start: ical.Timed(*m.Start, zone), End: ical.Timed(end, zone), RRule: m.RRule}
	var found *time.Time
	if err := series.Walk(func(occ ical.Occurrence) bool {
		if occ.End.Time.After(now) {
			at := occ.Start.Time.UTC()
			found = &at
			return false
		}
		return true
	}); err != nil || found == nil {
		return m.Start
	}
	return found
}

// hostAddress is activeAddresses[0] in Proton's useGetMeetingDependencies.
func hostAddress(u *keys.Unlocked) (string, error) {
	var first *keys.Address
	for i, a := range u.Addresses {
		if a.CanSend() && (first == nil || a.Order < first.Order) {
			first = &u.Addresses[i]
		}
	}
	if first == nil {
		return "", errs.Problemf("This account has no active address to host a meeting from.")
	}
	return first.ID, nil
}

func isoOrNull(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(isoTime)
	return &s
}

func orNull(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// Allowed is useHasMeetProductAccess in Proton's web client: an organization may
// keep Meet from its members, and an account it has not is free to use it.
func (s *Service) Allowed(ctx context.Context) bool {
	var r struct{ AllowedProducts []string }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/organizations/settings"}, &r); err != nil {
		// Recorded and not counted: an account with no organization has nothing
		// to read here, and is allowed Meet like any other.
		slog.DebugContext(ctx, "meet: the organization's allowed products could not be read",
			"reason", "unreadable", "error", err)
		return true
	}
	return len(r.AllowedProducts) == 0 || slices.Contains(r.AllowedProducts, "All") ||
		slices.Contains(r.AllowedProducts, "Meet")
}
