package ical

import (
	"fmt"
	"strings"
	"time"

	// A value read here is anchored to an IANA zone name, and resolving one is
	// what tells a wall-clock reading that names two instants from one that names
	// none. The database travels with the package rather than with the binary, so
	// the checks are exercised wherever this code is built and not only where the
	// host happens to ship a zoneinfo directory.
	_ "time/tzdata"

	"github.com/roman-16/proton-cli/internal/contentline"
)

// Layouts for the three shapes an iCalendar date value takes.
const (
	dateLayout      = "20060102"
	dateTimeLayout  = "20060102T150405"
	utcDateTime     = "20060102T150405Z"
	stampLayout     = "20060102T150405Z"
	refTimeLayout   = "2006-01-02T15:04"
	refDateLayout   = "2006-01-02"
	clockLayout     = "15:04"
	untilDayEndHour = 23
)

// DateTime is an iCalendar date or date-time value together with the anchor it
// was written against.
//
// The anchor is not decoration. A weekly event at 09:00 anchored to
// Europe/Vienna stays at 09:00 across a daylight-saving change; the same event
// stored as a UTC instant moves to 08:00. Proton's clients always write an
// anchor, so the CLI has to carry one.
type DateTime struct {
	// Time is the instant. For an all-day value it is midnight UTC on that day,
	// because an all-day date has no time of day and no zone to have one in.
	Time time.Time
	// TZID is the IANA zone the value is anchored to. Empty means the value is
	// written as a UTC instant.
	TZID string
	// AllDay marks a VALUE=DATE value: a whole day, with no time of day.
	AllDay bool
}

// Timed builds a date-time anchored to an IANA zone. An empty zone anchors to
// UTC.
func Timed(t time.Time, tzid string) DateTime {
	return DateTime{Time: t, TZID: tzid}
}

// Day builds an all-day value for the calendar day t falls on in its own
// location.
func Day(t time.Time) DateTime {
	y, m, d := t.Date()
	return DateTime{Time: time.Date(y, m, d, 0, 0, 0, 0, time.UTC), AllDay: true}
}

// IsZero reports whether the value is absent.
func (d DateTime) IsZero() bool { return d.Time.IsZero() }

// Equal compares two values by the instant they name.
func (d DateTime) Equal(o DateTime) bool { return d.Time.Equal(o.Time) }

// Location resolves the anchor, falling back to UTC when the zone is empty or
// unknown to this system.
func (d DateTime) Location() *time.Location {
	if d.TZID == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(d.TZID)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Wall is the value's wall-clock reading in its own anchor, which is what the
// serialized form carries and what recurrence arithmetic has to advance.
func (d DateTime) Wall() time.Time {
	if d.AllDay {
		return d.Time
	}
	return d.Time.In(d.Location())
}

// In returns the instant the value names when it is read in loc.
//
// A date-time names one instant whatever zone reads it, so it is only
// re-expressed. An all-day date names no instant at all: it is a day, and a day
// begins when the reader's own day begins. Reading it anywhere else is what puts a
// whole-day event on the wrong date.
func (d DateTime) In(loc *time.Location) time.Time {
	if !d.AllDay {
		return d.Time.In(loc)
	}
	y, m, day := d.Time.Date()
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

// Anchored returns the value written against an IANA zone.
//
// A day has no time of day, and so no zone for one to be read in; only a value
// naming a clock reading has one.
func (d DateTime) Anchored(tzid string) DateTime {
	if d.AllDay {
		return d
	}
	d.TZID = tzid
	return d
}

// At returns the same anchor and all-day-ness carrying a different instant, so
// a derived value cannot accidentally lose the series' zone.
func (d DateTime) At(t time.Time) DateTime {
	if d.AllDay {
		return Day(t)
	}
	return DateTime{Time: t, TZID: d.TZID}
}

// line renders the value as a content line under the given property name.
func (d DateTime) line(name string) contentline.Line {
	switch {
	case d.AllDay:
		return contentline.Line{
			Name:   name,
			Params: contentline.Params{{Name: "VALUE", Value: "DATE"}},
			Value:  d.Time.Format(dateLayout),
		}
	case d.TZID != "":
		return contentline.Line{
			Name:   name,
			Params: contentline.Params{{Name: "TZID", Value: d.TZID}},
			Value:  d.Time.In(d.Location()).Format(dateTimeLayout),
		}
	default:
		return contentline.Line{Name: name, Value: d.Time.UTC().Format(utcDateTime)}
	}
}

// String renders the value the way a reference names it: the local reading, to
// the minute, or a bare date when it is all-day.
//
// Local rather than the value's own anchor, because this is the string a list
// prints inside an occurrence reference and a person types back, and the columns
// beside it are local too. A reference that read 08:00 next to a row that read
// 10:00 would be one nobody could copy.
func (d DateTime) String() string {
	if d.AllDay {
		return d.Time.Format(refDateLayout)
	}
	return d.Time.Local().Format(refTimeLayout)
}

// parseValues reads every date value on a line. EXDATE carries a comma-separated
// list; DTSTART and RECURRENCE-ID carry one.
func parseValues(l contentline.Line) ([]DateTime, error) {
	tzid := l.Params.Get("TZID")
	allDay := strings.EqualFold(l.Params.Get("VALUE"), "DATE")
	out := make([]DateTime, 0, 1)
	for _, raw := range contentline.SplitList(l.Value) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		d, err := parseValue(raw, tzid, allDay)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", l.Name, err)
		}
		out = append(out, d)
	}
	return out, nil
}

func parseValue(raw, tzid string, allDay bool) (DateTime, error) {
	if allDay || len(raw) == len(dateLayout) {
		t, err := time.ParseInLocation(dateLayout, raw, time.UTC)
		if err != nil {
			return DateTime{}, err
		}
		return DateTime{Time: t, AllDay: true}, nil
	}
	if strings.HasSuffix(raw, "Z") {
		t, err := time.ParseInLocation(utcDateTime, raw, time.UTC)
		if err != nil {
			return DateTime{}, err
		}
		return DateTime{Time: t}, nil
	}
	loc := time.UTC
	if tzid != "" {
		l, err := time.LoadLocation(tzid)
		if err != nil {
			// An unknown zone is a value we can still place, just not anchor.
			// Refusing the whole event over a zone this host has never heard of
			// would make the event unreadable rather than merely unanchored.
			tzid = ""
		} else {
			loc = l
		}
	}
	t, err := time.ParseInLocation(dateTimeLayout, raw, loc)
	if err != nil {
		return DateTime{}, err
	}
	return DateTime{Time: t, TZID: tzid}, nil
}

// ParseTime reads a moment somebody wrote, in the zone they are working in.
//
// What comes back carries the form it was written in. A bare date names a day
// and no time of day, which is the whole difference between an all-day event and
// one that happens to begin at midnight. The value is unanchored: which zone it
// is written against is the caller's to say, with Anchored.
//
// A wall-clock reading is not always one instant. For two hours a year it is
// none, because the clocks went forward over it, and for two hours a year it is
// two, because they went back. Go answers both without complaint - it moves a
// time out of the gap and picks one side of the overlap - so a value the writer
// believed was exact silently becomes a different one.
//
// Neither can be settled from a zone name, which is why an offset is accepted
// alongside: it is the only form that names one instant in the four hours where
// the wall clock cannot.
func ParseTime(s string, loc *time.Location) (DateTime, error) {
	t, form, err := parseTime(s, loc)
	if err != nil {
		return DateTime{}, err
	}
	// An offset names an instant outright, and a bare date names a day, which has
	// no reading of a clock to be uncertain about. Only a wall-clock time read in
	// a zone can be two instants or none.
	if !strings.Contains(form.layout, "15") {
		return Day(t), nil
	}
	if form.offset {
		return Timed(t, ""), nil
	}
	return Timed(t, ""), checkWallClock(s, t, form.layout, loc)
}

// ParseWallTime reads a time the CLI itself printed, without judging whether the
// wall clock it names is unique.
//
// A reference is resolved against the series that generated it, which knows
// which instant a repeated 02:30 is; refusing to parse a string this CLI wrote
// would make the occurrence behind it unaddressable.
func ParseWallTime(s string, loc *time.Location) (time.Time, error) {
	t, _, err := parseTime(s, loc)
	return t, err
}

// timeForm is one way of writing a time, and whether it names an instant on its
// own or a reading of a clock that a zone has to interpret.
type timeForm struct {
	layout string
	offset bool
}

// timeFormats are the forms a person writes, and the ones the CLI prints.
// The two carrying an offset name an instant outright; the rest name a reading
// of a clock, which has to be interpreted in a zone.
var timeFormats = []timeForm{
	{time.RFC3339, true},
	{"2006-01-02T15:04Z07:00", true},
	{"2006-01-02T15:04", false},
	{"2006-01-02T15:04:05", false},
	{"2006-01-02 15:04", false},
	{"2006-01-02 15:04:05", false},
	{refDateLayout, false},
}

func parseTime(s string, loc *time.Location) (time.Time, timeForm, error) {
	if loc == nil {
		loc = time.Local
	}
	for _, f := range timeFormats {
		if t, err := time.ParseInLocation(f.layout, s, loc); err == nil {
			return t, f, nil
		}
	}
	return time.Time{}, timeForm{}, fmt.Errorf("unrecognized time format: %s", s)
}

// checkWallClock reports a wall-clock reading that names no instant in loc, or
// two.
//
// A gap shows up as a reading that will not write itself back: asking for 02:30
// where that hour was skipped yields 03:30, so writing the result out in the
// layout it was read in returns a different string than the one that was given.
//
// An overlap shows up as a twin. The clocks going back repeat the last stretch
// before the change, so the same reading names two instants exactly that shift
// apart, and neither of them is the one that was meant.
func checkWallClock(s string, t time.Time, layout string, loc *time.Location) error {
	if landed := t.Format(layout); landed != s {
		return fmt.Errorf("%s does not exist on %s in %s, where the clocks go forward; "+
			"%s is the same instant, as %s",
			clockOf(s), t.Format(refDateLayout), loc,
			t.Format(clockLayout), t.Format(time.RFC3339))
	}
	if twin, ok := wallClockTwin(t); ok {
		first, second := t, twin
		if twin.Before(t) {
			first, second = twin, t
		}
		return fmt.Errorf("%s happens twice on %s in %s, when the clocks go back; "+
			"say which one, as %s or %s",
			t.Format(clockLayout), t.Format(refDateLayout), loc,
			first.Format(time.RFC3339), second.Format(time.RFC3339))
	}
	return nil
}

// wallClockTwin is the other instant sharing this one's reading of the clock.
//
// The shift is read off the zone either side of the day rather than assumed to
// be an hour, because it is not everywhere: Lord Howe Island moves its clocks
// by thirty minutes, and an overlap only half as long as the search for it is
// one the search would miss.
func wallClockTwin(t time.Time) (time.Time, bool) {
	_, before := t.Add(-24 * time.Hour).Zone()
	_, after := t.Add(24 * time.Hour).Zone()
	shift := time.Duration(before-after) * time.Second
	if shift <= 0 {
		return time.Time{}, false
	}
	for _, twin := range []time.Time{t.Add(-shift), t.Add(shift)} {
		if twin.Format(clockLayout) == t.Format(clockLayout) {
			return twin, true
		}
	}
	return time.Time{}, false
}

// clockOf is the time-of-day half of what was written, for a message about it.
func clockOf(s string) string {
	_, clock, found := strings.Cut(strings.ReplaceAll(s, " ", "T"), "T")
	if !found {
		return s
	}
	return clock
}

// UntilValue is the UNTIL that ends a series just before the given occurrence.
//
// The rule Proton follows: an all-day series gets the floating date of the
// previous day, and a timed series gets the last second of the previous day in
// the series' own zone, expressed as the UTC instant the RFC requires UNTIL to
// be.
func UntilValue(before DateTime) string {
	wall := before.Wall().AddDate(0, 0, -1)
	if before.AllDay {
		return wall.Format(dateLayout)
	}
	loc := before.Location()
	endOfDay := time.Date(wall.Year(), wall.Month(), wall.Day(), untilDayEndHour, 59, 59, 0, loc)
	return endOfDay.UTC().Format(utcDateTime)
}

// stamp renders a DTSTAMP, which is always a UTC instant.
func stamp(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(stampLayout)
}
