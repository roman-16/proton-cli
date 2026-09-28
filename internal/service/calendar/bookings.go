package calendar

import (
	"bytes"
	"context"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/plan"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
	"github.com/roman-16/proton-cli/internal/units"
)

const (
	NoticeNone     = "none"
	NoticeTwoHours = "2h"
	NoticeTwoDays  = "48h"
	NoticeNextDay  = "next-day"
)

var noticeModes = []string{NoticeNone, NoticeTwoHours, NoticeTwoDays, NoticeNextDay}

func NoticeWords() []string { return slices.Clone(noticeModes) }

func noticeWord(mode int) string {
	if mode >= 0 && mode < len(noticeModes) {
		return noticeModes[mode]
	}
	return fmt.Sprintf("mode %d", mode)
}

var bookingDurations = []time.Duration{
	15 * time.Minute, 30 * time.Minute, time.Hour, 90 * time.Minute, 2 * time.Hour,
}

func BookingDurationWords() []string {
	out := make([]string, 0, len(bookingDurations))
	for _, d := range bookingDurations {
		out = append(out, units.Duration(d))
	}
	return out
}

func BookingDurationOffered(d time.Duration) bool { return slices.Contains(bookingDurations, d) }

const (
	MaxBookingSlots         = 200
	BookingTitleLimit       = 255
	BookingLocationLimit    = 255
	BookingDescriptionLimit = 3000
	bookingPagesMost        = 25
	weeklyRule              = "FREQ=WEEKLY"
	endOfDay                = "24:00"
	dateLayout              = "2006-01-02"
	clockLayout             = "15:04"
)

var bookingAllowances = map[string]int{
	"bundle2022":    1,
	"bundlebiz2025": bookingPagesMost,
	"bundlepro2022": bookingPagesMost,
	"bundlepro2024": bookingPagesMost,
	"duo2024":       1,
	"family2022":    1,
	"mail2022":      1,
	"mailbiz2024":   bookingPagesMost,
	"mailpro2022":   1,
	"meet2026":      bookingPagesMost,
	"meetbiz2025":   bookingPagesMost,
	"visionary2022": bookingPagesMost,
}

var ErrNoCalendarTakesBookings = errors.New("no calendar takes bookings")

type Window struct {
	Day   string `json:"day"`
	Start string `json:"start"`
	End   string `json:"end"`
}

func (w Window) String() string { return w.Day + "=" + w.Start + "-" + w.End }

func (w Window) Weekly() bool {
	_, err := time.Parse(dateLayout, w.Day)
	return err != nil
}

type Slot struct {
	Start  int64
	End    int64
	Zone   string
	Weekly bool
}

type BookingPage struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	CalendarID  string `json:"calendar_id"`
	Calendar    string `json:"calendar"`
	Meet        bool   `json:"meet"`
	Location    string `json:"location,omitempty"`
	Description string `json:"description,omitempty"`
	Notice      string `json:"notice"`
	Created     int64  `json:"created"`
	Modified    int64  `json:"modified"`
}

type BookingPageDetail struct {
	BookingPage
	URL       string                 `json:"url"`
	Contact   string                 `json:"contact"`
	Duration  string                 `json:"duration"`
	Weekly    bool                   `json:"weekly"`
	Available []Window               `json:"available"`
	Zone      string                 `json:"zone"`
	BlockedBy []string               `json:"blocked_by"`
	Signature pgphelper.VerifyResult `json:"signature,omitempty"`
}

type NewBookingPage struct {
	Title       string
	Description string
	Location    string
	Slots       []Slot
	Notice      string
	BlockedBy   []string
}

type BookingPatch struct {
	Title       *string
	Description *string
	Location    *string
	Meet        bool
	Slots       []Slot
	Duration    time.Duration
	Notice      *string
	BlockedBy   *[]string
}

type BookingChange struct {
	raw      rawBookingPage
	ck       *calKeys
	secret   []byte
	verified bool
	content  bookingContent
	slots    []Slot
	notice   int
	blocked  []string
}

type rawBookingPage struct {
	ID                  string
	CalendarID          string
	BookingUID          string
	BookingKeySalt      string
	EncryptedSecret     string
	EncryptedContent    string
	CreateTime          int64
	ModifyTime          int64
	MinimumNoticeMode   int
	ConflictCalendarIDs []string
	Slots               []rawBookingSlot
}

type rawBookingSlot struct {
	StartTime         int64
	EndTime           int64
	Timezone          string
	RRule             *string
	DetachedSignature string
}

type bookingContent struct {
	Description        string `json:"description"`
	Location           string `json:"location"`
	Summary            string `json:"summary"`
	WithProtonMeetLink bool   `json:"withProtonMeetLink"`
}

type openedBookingPage struct {
	raw      rawBookingPage
	ck       *calKeys
	secret   []byte
	content  bookingContent
	verdicts []pgphelper.VerifyResult
}

func (s *Service) BookingPages(ctx context.Context) ([]BookingPage, error) {
	raws, err := s.rawBookingPages(ctx)
	if err != nil {
		return nil, err
	}
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]BookingPage, 0, len(raws))
	for _, raw := range raws {
		opened, reason, err := s.openBookingPage(ctx, raw)
		if err != nil {
			skip.Record(ctx, skip.KindBookingPage, raw.ID, reason, err)
			continue
		}
		out = append(out, opened.row(cals))
	}
	return out, nil
}

func (s *Service) BookingPageOpen(ctx context.Context, id string) (*BookingPageDetail, error) {
	raw, err := s.rawBookingPage(ctx, id)
	if err != nil {
		return nil, err
	}
	opened, _, err := s.openBookingPage(ctx, raw)
	if err != nil {
		return nil, err
	}
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return nil, err
	}
	slots := slotsOf(raw.Slots)
	verdicts := opened.verdicts
	for _, sl := range raw.Slots {
		verdicts = append(verdicts, verifySlot(opened.ck, raw.BookingUID, sl))
	}
	detail := opened.detail(cals, slots)
	detail.Signature = pgphelper.Aggregate(verdicts...)
	return detail, nil
}

func (s *Service) BookingRoom(ctx context.Context) error {
	u, err := s.keys(ctx)
	if err != nil {
		return err
	}
	allowed := bookingPagesMost
	if !u.PaidMeet {
		current, err := plan.Read(ctx, s.C)
		if err != nil {
			return err
		}
		allowed = bookingAllowances[current.Name]
	}
	if allowed == 0 {
		return errs.Problemf("Booking pages need a paid Mail or Meet plan.")
	}
	pages, err := s.BookingPages(ctx)
	if err != nil {
		return err
	}
	if len(pages) < allowed {
		return nil
	}
	if allowed == 1 && len(pages) == 1 {
		return errs.Naming(pages[0].Title, errs.Problemf(
			"Your plan allows one booking page, and \"%s\" is it. Delete it to make another.", pages[0].Title))
	}
	return errs.Problemf("Your plan allows %d booking pages, and you have %d. Delete one to make another.",
		allowed, len(pages))
}

func (s *Service) DefaultBookingCalendar(ctx context.Context) (Calendar, error) {
	cals, settings, err := s.calendarsAndSettings(ctx)
	if err != nil {
		return Calendar{}, err
	}
	takers := inUse(cals, func(c Calendar) bool { return c.Kind == KindPersonal })
	if len(takers) == 0 {
		return Calendar{}, ErrNoCalendarTakesBookings
	}
	if i := slices.IndexFunc(takers, func(c Calendar) bool { return c.ID == settings.DefaultCalendarID }); i >= 0 {
		return takers[i], nil
	}
	return takers[0], nil
}

func (c Calendar) TakesBookings() error {
	switch {
	case c.Kind == KindPersonal && c.active:
		return nil
	case c.Kind == KindPersonal:
		return errs.Naming(c.Name, errs.Problemf("%s is disabled, so it takes no bookings.", c.Name))
	case c.Kind == KindShared:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is %s's, and only they can take bookings in it.", c.Name, c.Owner))
	}
	return errs.Naming(c.Name, errs.Problemf("%s is read-only, so it takes no bookings.", c.Name))
}

func (c Calendar) BlocksBookings() error {
	switch {
	case c.Kind == KindHolidays:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is a holidays calendar, which cannot keep a time from being booked.", c.Name))
	case !c.active:
		return errs.Naming(c.Name, errs.Problemf(
			"%s is disabled, so it cannot keep a time from being booked.", c.Name))
	}
	return nil
}

func (s *Service) BookingPageCreate(ctx context.Context, cal Calendar, n NewBookingPage) (*BookingPageDetail, error) {
	ck, err := s.unlockCalendar(ctx, cal.ID)
	if err != nil {
		return nil, err
	}
	secret, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	salt, err := randomBytes(32)
	if err != nil {
		return nil, err
	}
	uid, err := bookingUID(secret)
	if err != nil {
		return nil, err
	}
	content := bookingContent{
		Description: n.Description, Location: n.Location, Summary: n.Title,
		WithProtonMeetLink: n.Location == "",
	}
	sealedSecret, err := sealBookingSecret(ck, cal.ID, secret)
	if err != nil {
		return nil, err
	}
	signed, err := signBookingPage(ck, cal.ID, uid, secret, salt, content, n.Slots)
	if err != nil {
		return nil, err
	}
	blocked := n.BlockedBy
	if blocked == nil {
		blocked = []string{}
	}
	mode := slices.Index(noticeModes, n.Notice)
	if mode < 0 {
		mode = 0
	}
	body := map[string]any{
		"BookingKeySalt":       base64.StdEncoding.EncodeToString(salt),
		"BookingUID":           uid,
		"CalendarID":           cal.ID,
		"CalendarKeySignature": signed.calendarKey,
		"ConflictCalendarIDs":  blocked,
		"EncryptedContent":     signed.content,
		"EncryptedSecret":      sealedSecret,
		"MinimumNoticeMode":    mode,
		"Slots":                signed.slots,
	}
	var r struct{ BookingPage rawBookingPage }
	if err := s.C.Decode(ctx, proton.Request{Method: "POST", Path: "/calendar/v1/booking", Body: body}, &r); err != nil {
		return nil, err
	}
	cals, err := s.CalendarsList(ctx)
	if err != nil {
		return nil, err
	}
	made := r.BookingPage
	made.CalendarID, made.BookingUID, made.MinimumNoticeMode, made.ConflictCalendarIDs = cal.ID, uid, mode, blocked
	opened := &openedBookingPage{raw: made, ck: ck, secret: secret, content: content}
	return opened.detail(cals, n.Slots), nil
}

func (s *Service) BookingPageChange(ctx context.Context, id string) (*BookingChange, error) {
	raw, err := s.rawBookingPage(ctx, id)
	if err != nil {
		return nil, err
	}
	opened, _, err := s.openBookingPage(ctx, raw)
	if err != nil {
		return nil, err
	}
	return &BookingChange{
		raw: raw, ck: opened.ck, secret: opened.secret,
		verified: len(opened.verdicts) > 0 && opened.verdicts[0] == pgphelper.Verified,
		content:  opened.content, slots: slotsOf(raw.Slots),
		notice: raw.MinimumNoticeMode, blocked: raw.ConflictCalendarIDs,
	}, nil
}

func (ch *BookingChange) Apply(p BookingPatch) error {
	if p.Title != nil {
		ch.content.Summary = *p.Title
	}
	if p.Description != nil {
		ch.content.Description = *p.Description
	}
	if p.Location != nil {
		ch.content.Location, ch.content.WithProtonMeetLink = *p.Location, false
	}
	if p.Meet {
		ch.content.Location, ch.content.WithProtonMeetLink = "", true
	}
	switch {
	case p.Slots != nil:
		ch.slots = p.Slots
	case p.Duration != 0:
		cut, err := recut(ch.slots, p.Duration)
		if err != nil {
			return err
		}
		ch.slots = cut
	}
	if p.Notice != nil {
		ch.notice = max(slices.Index(noticeModes, *p.Notice), 0)
	}
	if p.BlockedBy != nil {
		ch.blocked = *p.BlockedBy
	}
	return nil
}

func (s *Service) BookingPageSave(ctx context.Context, ch *BookingChange) error {
	salt, err := base64.StdEncoding.DecodeString(ch.raw.BookingKeySalt)
	if err != nil {
		return fmt.Errorf("read the booking page's salt: %w", err)
	}
	signed, err := signBookingPage(ch.ck, ch.raw.CalendarID, ch.raw.BookingUID, ch.secret, salt, ch.content, ch.slots)
	if err != nil {
		return err
	}
	sealedSecret := ch.raw.EncryptedSecret
	if !ch.verified {
		if sealedSecret, err = sealBookingSecret(ch.ck, ch.raw.CalendarID, ch.secret); err != nil {
			return err
		}
	}
	blocked := ch.blocked
	if blocked == nil {
		blocked = []string{}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/calendar/v1/booking/" + ch.raw.ID,
		Body: map[string]any{
			"CalendarKeySignature": signed.calendarKey,
			"ConflictCalendarIDs":  blocked,
			"EncryptedContent":     signed.content,
			"EncryptedSecret":      sealedSecret,
			"MinimumNoticeMode":    ch.notice,
			"Slots":                signed.slots,
		},
	}, nil)
}

func (ch *BookingChange) CalendarID() string { return ch.raw.CalendarID }

func (ch *BookingChange) Duration() time.Duration { return windowsOf(ch.slots).duration }

func (s *Service) BookingPageDelete(ctx context.Context, id string) error {
	err := s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/calendar/v1/booking/" + id}, nil)
	if proton.DoesNotExist(err) {
		return &errs.NotFound{Kind: "booking page", Ref: id}
	}
	return err
}

func (s *Service) rawBookingPages(ctx context.Context) ([]rawBookingPage, error) {
	var r struct{ BookingPages []rawBookingPage }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1/booking"}, &r); err != nil {
		return nil, err
	}
	return r.BookingPages, nil
}

func (s *Service) rawBookingPage(ctx context.Context, id string) (rawBookingPage, error) {
	var r struct{ BookingPage rawBookingPage }
	err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/calendar/v1/booking/" + id}, &r)
	if proton.DoesNotExist(err) {
		return rawBookingPage{}, &errs.NotFound{Kind: "booking page", Ref: id}
	}
	return r.BookingPage, err
}

func (s *Service) openBookingPage(ctx context.Context, raw rawBookingPage) (*openedBookingPage, skip.Reason, error) {
	ck, err := s.unlockCalendar(ctx, raw.CalendarID)
	if err != nil {
		return nil, skip.Unreadable, err
	}
	secret, secretVerdict, err := openBookingSecret(ck, raw.CalendarID, raw.EncryptedSecret)
	if err != nil {
		return nil, skip.Undecryptable, err
	}
	salt, err := base64.StdEncoding.DecodeString(raw.BookingKeySalt)
	if err != nil {
		return nil, skip.Malformed, fmt.Errorf("read the booking page's salt: %w", err)
	}
	password, err := bookingKeyPassword(raw.CalendarID, secret, salt)
	if err != nil {
		return nil, skip.Undecryptable, err
	}
	content, contentVerdict, err := openBookingContent(ck, password, raw.BookingUID, raw.EncryptedContent)
	if err != nil {
		return nil, skip.Undecryptable, err
	}
	return &openedBookingPage{
		raw: raw, ck: ck, secret: secret, content: content,
		verdicts: []pgphelper.VerifyResult{secretVerdict, contentVerdict},
	}, "", nil
}

func (o *openedBookingPage) row(cals []Calendar) BookingPage {
	name := o.raw.CalendarID
	if i := slices.IndexFunc(cals, func(c Calendar) bool { return c.ID == o.raw.CalendarID }); i >= 0 {
		name = cals[i].Name
	}
	location := o.content.Location
	if o.content.WithProtonMeetLink {
		location = ""
	}
	return BookingPage{
		ID: o.raw.ID, Title: o.content.Summary, CalendarID: o.raw.CalendarID, Calendar: name,
		Meet: o.content.WithProtonMeetLink, Location: location, Description: o.content.Description,
		Notice: noticeWord(o.raw.MinimumNoticeMode), Created: o.raw.CreateTime, Modified: o.raw.ModifyTime,
	}
}

func (o *openedBookingPage) detail(cals []Calendar, slots []Slot) *BookingPageDetail {
	blocked := make([]string, 0, len(o.raw.ConflictCalendarIDs))
	for _, id := range o.raw.ConflictCalendarIDs {
		name := id
		if i := slices.IndexFunc(cals, func(c Calendar) bool { return c.ID == id }); i >= 0 {
			name = cals[i].Name
		}
		blocked = append(blocked, name)
	}
	offer := windowsOf(slots)
	return &BookingPageDetail{
		BookingPage: o.row(cals),
		URL:         bookingLink(o.secret),
		Contact:     o.ck.email,
		Duration:    units.Duration(offer.duration),
		Weekly:      offer.weekly,
		Available:   offer.windows,
		Zone:        offer.zone,
		BlockedBy:   blocked,
	}
}

func Offer(windows []Window, d time.Duration, loc *time.Location, now time.Time) ([]Slot, error) {
	monday := weekStart(now.In(loc))
	var stretches []stretch
	for _, w := range windows {
		sp, err := w.stretch(loc, monday, now)
		if err != nil {
			return nil, err
		}
		stretches = append(stretches, sp)
	}
	return cutStretches(stretches, d, loc.String())
}

type stretch struct {
	window Window
	start  time.Time
	end    time.Time
	weekly bool
}

func (w Window) stretch(loc *time.Location, monday, now time.Time) (stretch, error) {
	if !w.Weekly() {
		day, _ := time.ParseInLocation(dateLayout, w.Day, loc)
		start, end, err := w.on(day, loc)
		if err != nil {
			return stretch{}, errs.Problemf("%s: %v", w, err)
		}
		if start.Before(now) {
			return stretch{}, errs.Problemf("%s has already begun.", w)
		}
		return stretch{window: w, start: start, end: end}, nil
	}
	weekday, err := units.ParseWeekday(w.Day)
	if err != nil {
		return stretch{}, errs.Problemf("%s: %v", w, err)
	}
	day := monday.AddDate(0, 0, (int(weekday)+6)%7)
	start, end, err := w.on(day, loc)
	if err != nil {
		if start, end, err = w.on(day.AddDate(0, 0, -7), loc); err != nil {
			return stretch{}, errs.Problemf("%s: %v", w, err)
		}
	}
	return stretch{window: w, start: start, end: end, weekly: true}, nil
}

func (w Window) on(day time.Time, loc *time.Location) (time.Time, time.Time, error) {
	date := day.Format(dateLayout)
	start, err := ical.ParseTime(date+"T"+w.Start, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	endAt := date + "T" + w.End
	if w.End == endOfDay {
		endAt = day.AddDate(0, 0, 1).Format(dateLayout) + "T00:00"
	}
	end, err := ical.ParseTime(endAt, loc)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start.Time, end.Time, nil
}

func weekStart(t time.Time) time.Time {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
}

func cutStretches(stretches []stretch, d time.Duration, zone string) ([]Slot, error) {
	if d <= 0 {
		return nil, errors.New("an appointment has to last some time")
	}
	var out []Slot
	for _, sp := range stretches {
		var cut []Slot
		for start := sp.start; !start.Add(d).After(sp.end); start = start.Add(d) {
			cut = append(cut, Slot{Start: start.Unix(), End: start.Add(d).Unix(), Zone: zone, Weekly: sp.weekly})
		}
		if len(cut) == 0 {
			return nil, errs.Problemf("%s is shorter than one %s appointment.", sp.window, units.Duration(d))
		}
		out = append(out, cut...)
	}
	if len(out) > MaxBookingSlots {
		return nil, errs.Problemf("That is %d appointments; a booking page offers at most %d. "+
			"Shorten the windows or make appointments longer.", len(out), MaxBookingSlots)
	}
	return out, nil
}

func recut(slots []Slot, d time.Duration) ([]Slot, error) {
	if len(slots) == 0 {
		return nil, nil
	}
	loc := zoneOf(slots[0].Zone)
	var stretches []stretch
	for _, merged := range merge(slots) {
		stretches = append(stretches, stretch{
			window: windowsIn(merged, loc)[0],
			start:  time.Unix(merged.Start, 0).In(loc), end: time.Unix(merged.End, 0).In(loc),
			weekly: merged.Weekly,
		})
	}
	return cutStretches(stretches, d, slots[0].Zone)
}

func slotsOf(raws []rawBookingSlot) []Slot {
	out := make([]Slot, 0, len(raws))
	for _, r := range raws {
		out = append(out, Slot{Start: r.StartTime, End: r.EndTime, Zone: r.Timezone, Weekly: r.RRule != nil && *r.RRule != ""})
	}
	return out
}

type offered struct {
	weekly   bool
	zone     string
	duration time.Duration
	windows  []Window
}

func windowsOf(slots []Slot) offered {
	if len(slots) == 0 {
		return offered{windows: []Window{}}
	}
	sorted := slices.Clone(slots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	loc := zoneOf(sorted[0].Zone)
	out := offered{
		weekly: sorted[0].Weekly, zone: sorted[0].Zone,
		duration: time.Duration(sorted[0].End-sorted[0].Start) * time.Second,
	}
	for _, merged := range merge(sorted) {
		out.windows = append(out.windows, windowsIn(merged, loc)...)
	}
	if out.weekly {
		sort.SliceStable(out.windows, func(i, j int) bool {
			a, b := weekdayOrder(out.windows[i].Day), weekdayOrder(out.windows[j].Day)
			if a != b {
				return a < b
			}
			return out.windows[i].Start < out.windows[j].Start
		})
	}
	return out
}

func merge(slots []Slot) []Slot {
	sorted := slices.Clone(slots)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Start < sorted[j].Start })
	var out []Slot
	for _, sl := range sorted {
		if n := len(out); n > 0 && out[n-1].End == sl.Start && out[n-1].Weekly == sl.Weekly {
			out[n-1].End = sl.End
			continue
		}
		out = append(out, sl)
	}
	return out
}

func windowsIn(sl Slot, loc *time.Location) []Window {
	start, end := time.Unix(sl.Start, 0).In(loc), time.Unix(sl.End, 0).In(loc)
	var out []Window
	for start.Before(end) {
		midnight := time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, loc)
		stop, clock := end, end.Format(clockLayout)
		if !end.Before(midnight) {
			stop, clock = midnight, endOfDay
		}
		day := start.Format(dateLayout)
		if sl.Weekly {
			day = units.Weekday(start.Weekday())
		}
		out = append(out, Window{Day: day, Start: start.Format(clockLayout), End: clock})
		start = stop
	}
	return out
}

func weekdayOrder(day string) int {
	d, err := units.ParseWeekday(day)
	if err != nil {
		return 7
	}
	return (int(d) + 6) % 7
}

func zoneOf(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func Tokens(windows []Window) []string {
	if len(windows) == 0 || !windows[0].Weekly() {
		out := make([]string, 0, len(windows))
		for _, w := range windows {
			out = append(out, w.String())
		}
		return out
	}
	byDay := make([][]string, 7)
	for _, w := range windows {
		i := weekdayOrder(w.Day)
		if i < 7 {
			byDay[i] = append(byDay[i], w.Start+"-"+w.End)
		}
	}
	var out []string
	for first := 0; first < 7; {
		last := first
		for last+1 < 7 && len(byDay[first]) > 0 && slices.Equal(byDay[last+1], byDay[first]) {
			last++
		}
		days := units.Weekday(time.Weekday((first + 1) % 7))
		if last > first {
			days += "-" + units.Weekday(time.Weekday((last+1)%7))
		}
		for _, hours := range byDay[first] {
			out = append(out, days+"="+hours)
		}
		first = last + 1
	}
	return out
}

func bookingLink(secret []byte) string {
	return linkHost + "/bookings#" + base64.URLEncoding.EncodeToString(secret)
}

func bookingUID(secret []byte) (string, error) {
	uid, err := hkdf.Key(sha256.New, secret, nil, "bookings.booking_id", 32)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(uid), nil
}

func bookingKeyPassword(calendarID string, secret, salt []byte) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, secret, salt, "bookings.booking_key."+calendarID, 32)
	if err != nil {
		return nil, err
	}
	return []byte(base64.StdEncoding.EncodeToString(key)), nil
}

func sealBookingSecret(ck *calKeys, calendarID string, secret []byte) (string, error) {
	msg, err := ck.addr.Write.EncryptWithContext(pgp.NewPlainMessage(secret), ck.addr.Write,
		pgp.NewSigningContext("bookings.secret."+calendarID, true))
	if err != nil {
		return "", fmt.Errorf("seal the booking page's secret: %w", err)
	}
	return base64.StdEncoding.EncodeToString(msg.GetBinary()), nil
}

func openBookingSecret(ck *calKeys, calendarID, sealed string) ([]byte, pgphelper.VerifyResult, error) {
	bin, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return nil, pgphelper.Unverified, fmt.Errorf("read the booking page's secret: %w", err)
	}
	opened, err := ck.addr.Read.DecryptWithContext(pgp.NewPGPMessage(bin), ck.addr.Read, pgp.GetUnixTime(),
		pgp.NewVerificationContext("bookings.secret."+calendarID, true, 0))
	if opened == nil {
		return nil, pgphelper.Unverified, fmt.Errorf("open the booking page's secret: %w", err)
	}
	return opened.GetBinary(), pgphelper.Classify(err), nil
}

func openBookingContent(ck *calKeys, password []byte, uid, sealed string) (bookingContent, pgphelper.VerifyResult, error) {
	bin, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return bookingContent{}, pgphelper.Unverified, fmt.Errorf("read the booking page's content: %w", err)
	}
	split, err := pgp.NewPGPMessage(bin).SplitMessage()
	if err != nil {
		return bookingContent{}, pgphelper.Unverified, fmt.Errorf("read the booking page's content: %w", err)
	}
	sk, err := pgp.DecryptSessionKeyWithPassword(split.KeyPacket, password)
	if err != nil {
		return bookingContent{}, pgphelper.Unverified, fmt.Errorf("open the booking page's content: %w", err)
	}
	opened, err := sk.DecryptAndVerifyWithContext(split.DataPacket, ck.addr.Read, pgp.GetUnixTime(),
		pgp.NewVerificationContext("bookings.content."+uid, true, 0))
	if opened == nil {
		return bookingContent{}, pgphelper.Unverified, fmt.Errorf("open the booking page's content: %w", err)
	}
	var content bookingContent
	if jerr := json.Unmarshal(opened.GetBinary(), &content); jerr != nil {
		return bookingContent{}, pgphelper.Unverified, fmt.Errorf("read the booking page's content: %w", jerr)
	}
	return content, pgphelper.Classify(err), nil
}

type signedBookingPage struct {
	content     string
	calendarKey string
	slots       []map[string]any
}

func signBookingPage(ck *calKeys, calendarID, uid string, secret, salt []byte, content bookingContent,
	slots []Slot) (*signedBookingPage, error) {
	if ck.primary == nil {
		return nil, fmt.Errorf("the calendar %s has no primary key", calendarID)
	}
	password, err := bookingKeyPassword(calendarID, secret, salt)
	if err != nil {
		return nil, err
	}
	sealedContent, err := sealBookingContent(ck, password, uid, content)
	if err != nil {
		return nil, err
	}
	fingerprints := strings.Join(ck.primary.GetSHA256Fingerprints(), ";")
	keySig, err := ck.addr.Write.SignDetachedWithContext(pgp.NewPlainMessageFromString(fingerprints),
		pgp.NewSigningContext("bookings.calendarKey."+uid, true))
	if err != nil {
		return nil, fmt.Errorf("sign the calendar's key: %w", err)
	}
	signed := &signedBookingPage{
		content: sealedContent, calendarKey: base64.StdEncoding.EncodeToString(keySig.GetBinary()),
		slots: make([]map[string]any, 0, len(slots)),
	}
	for _, sl := range slots {
		text, err := slotText(sl.Start, sl.End, sl.Zone, rruleOf(sl))
		if err != nil {
			return nil, err
		}
		sig, err := ck.addr.Write.SignDetachedWithContext(pgp.NewPlainMessageFromString(text),
			pgp.NewSigningContext("bookings.slot."+uid, true))
		if err != nil {
			return nil, fmt.Errorf("sign a booking slot: %w", err)
		}
		signed.slots = append(signed.slots, map[string]any{
			"DetachedSignature": base64.StdEncoding.EncodeToString(sig.GetBinary()),
			"EndTime":           sl.End,
			"RRule":             rruleOf(sl),
			"StartTime":         sl.Start,
			"Timezone":          sl.Zone,
		})
	}
	return signed, nil
}

func sealBookingContent(ck *calKeys, password []byte, uid string, content bookingContent) (string, error) {
	text, err := compactJSON(content)
	if err != nil {
		return "", err
	}
	sk, err := pgp.GenerateSessionKey()
	if err != nil {
		return "", err
	}
	data, err := sk.EncryptAndSignWithContext(pgp.NewPlainMessageFromString(text), ck.addr.Write,
		pgp.NewSigningContext("bookings.content."+uid, true))
	if err != nil {
		return "", fmt.Errorf("seal the booking page's content: %w", err)
	}
	keyPacket, err := pgp.EncryptSessionKeyWithPassword(sk, password)
	if err != nil {
		return "", fmt.Errorf("seal the booking page's content: %w", err)
	}
	return base64.StdEncoding.EncodeToString(append(keyPacket, data...)), nil
}

func verifySlot(ck *calKeys, uid string, raw rawBookingSlot) pgphelper.VerifyResult {
	sig, err := base64.StdEncoding.DecodeString(raw.DetachedSignature)
	if err != nil || len(sig) == 0 {
		return pgphelper.Unsigned
	}
	text, err := slotText(raw.StartTime, raw.EndTime, raw.Timezone, raw.RRule)
	if err != nil {
		return pgphelper.Unverified
	}
	verdict := pgphelper.Classify(ck.addr.Read.VerifyDetachedWithContext(pgp.NewPlainMessageFromString(text),
		pgp.NewPGPSignature(sig), pgp.GetUnixTime(), pgp.NewVerificationContext("bookings.slot."+uid, true, 0)))
	if verdict == pgphelper.Invalid {
		return pgphelper.Unverified
	}
	return verdict
}

func rruleOf(sl Slot) *string {
	if !sl.Weekly {
		return nil
	}
	rule := weeklyRule
	return &rule
}

func slotText(start, end int64, zone string, rrule *string) (string, error) {
	return compactJSON(struct {
		EndTime   int64   `json:"EndTime"`
		RRule     *string `json:"RRule"`
		StartTime int64   `json:"StartTime"`
		Timezone  string  `json:"Timezone"`
	}{end, rrule, start, zone})
}

func compactJSON(v any) (string, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(b.String(), "\n"), nil
}
