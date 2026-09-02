package ical

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Proton stores a recurring event as one VEVENT carrying a rule. There is no
// endpoint that expands it: every client works out the occurrences itself, which
// is why the same arithmetic lives here.

// maxExpansion caps how many instances a rule may generate before the walk gives
// up. An unbounded rule with a far-future window would otherwise iterate for as
// long as it is asked to.
const maxExpansion = 10_000

// ErrTooManyOccurrences is what a walk that ran out of room reports.
//
// Reaching the cap silently was the same mistake in three places: a walk that
// stops early and returns nothing has told its caller the series ended, and
// every count taken from it went on to be printed as though it were the whole
// truth.
var ErrTooManyOccurrences = fmt.Errorf("a rule generating more than %d occurrences", maxExpansion)

// Occurrence is one instance of an event.
type Occurrence struct {
	// Number is the 1-based position among the instances the rule generates.
	// Excluded dates still take their number, because that is what COUNT counts
	// and therefore what truncating a counted rule has to reason about.
	Number int
	Start  DateTime
	End    DateTime
}

// Occurrences returns the instances the window covers.
func (v VEvent) Occurrences(w Window) ([]Occurrence, error) {
	var out []Occurrence
	err := v.walk(skipExcluded, func(o Occurrence) (bool, error) {
		if w.Ended(o.Start) {
			return false, nil
		}
		if w.Covers(o.Start, o.End) {
			out = append(out, o)
		}
		return true, nil
	})
	return out, err
}

// OccurrenceAt finds the instance starting at the given value.
//
// It looks at every instance the rule generates, cancelled ones included. A
// reference keeps naming the same instance after it is cancelled, which is what
// makes cancelling it again a no-op instead of a complaint that it is not there.
func (v VEvent) OccurrenceAt(at DateTime) (Occurrence, bool, error) {
	var found Occurrence
	ok := false
	err := v.walk(includeExcluded, func(o Occurrence) (bool, error) {
		if o.Start.Equal(at) {
			found, ok = o, true
			return false, nil
		}
		return !o.Start.Time.After(at.Time), nil
	})
	return found, ok, err
}

// Walk visits every instance in order until the visitor stops, which is what a
// caller listing a whole series needs.
func (v VEvent) Walk(visit func(Occurrence) bool) error {
	return v.walk(skipExcluded, func(o Occurrence) (bool, error) { return visit(o), nil })
}

// Endless reports a series that never stops.
//
// A rule saying neither how many times it repeats nor when it stops repeats for
// ever, and says so in its own text - so this is read rather than counted, and
// is the one honest answer to "how many occurrences" for such a series.
func (v VEvent) Endless() bool {
	return v.Recurring() && RuleValue(v.RRule, "COUNT") == "" && RuleValue(v.RRule, "UNTIL") == ""
}

// CountOccurrences is how many instances the event has, or nil when it has no
// last one.
//
// A count is either the whole truth or not a count. Where the rule states how
// many times it repeats, that is the answer without walking anything; where it
// states when it stops, the instances are counted; and where it states neither,
// there is no number to give and saying so is the only thing that is true.
func (v VEvent) CountOccurrences() (*int, error) {
	if v.Endless() {
		return nil, nil
	}
	// COUNT counts the instances the rule generates, cancellations included, so it
	// answers for a series nobody has cancelled anything out of and the rest are
	// counted properly.
	if n := ruleCount(v.RRule); n > 0 && len(v.ExDates) == 0 {
		return &n, nil
	}
	n := 0
	if err := v.walk(skipExcluded, func(Occurrence) (bool, error) {
		n++
		return true, nil
	}); err != nil {
		return nil, err
	}
	return &n, nil
}

// ParseOccurrence reads the occurrence half of a reference.
//
// It reads local, which is the frame the reference was printed in, and the value
// it produces is compared by the instant it names rather than by how it is
// written - so a reference matches the occurrence a person read off the row
// whatever zone the series itself is anchored to.
func (v VEvent) ParseOccurrence(s string) (DateTime, error) {
	if v.Start.AllDay {
		t, err := time.ParseInLocation(refDateLayout, s, time.UTC)
		if err != nil {
			return DateTime{}, fmt.Errorf("%q is not a date (expected YYYY-MM-DD)", s)
		}
		return Day(t), nil
	}
	t, err := ParseWallTime(s, time.Local)
	if err != nil {
		return DateTime{}, fmt.Errorf("%q is not a time (expected YYYY-MM-DDTHH:MM)", s)
	}
	return DateTime{Time: t, TZID: v.Start.TZID}, nil
}

// Whether a walk reports the instances somebody has cancelled. Listing a window
// must not show them; finding the one a reference names must, or a reference stops
// resolving the moment its instance is cancelled.
const (
	skipExcluded    = true
	includeExcluded = false
)

// walk visits every instance in order, stopping when the visitor says so.
// Excluded dates always consume their number, whether or not they are reported,
// because that is what a counted rule counts.
func (v VEvent) walk(skip bool, visit func(Occurrence) (bool, error)) error {
	if v.Start.IsZero() {
		return fmt.Errorf("event %s has no start", v.UID)
	}
	start, end := v.Span()
	duration := end.Time.Sub(start.Time)
	if !v.Recurring() {
		_, err := visit(Occurrence{Number: 1, Start: start, End: end})
		return err
	}

	next, err := v.iterator()
	if err != nil {
		return err
	}
	for n := 1; ; n++ {
		if n > maxExpansion {
			return ErrTooManyOccurrences
		}
		t, ok := next()
		if !ok {
			return nil
		}
		at := v.Start.At(t)
		if skip && slices.ContainsFunc(v.ExDates, at.Equal) {
			continue
		}
		carryOn, err := visit(Occurrence{
			Number: n,
			Start:  at,
			End:    v.Start.At(t.Add(duration)),
		})
		if err != nil || !carryOn {
			return err
		}
	}
}

// iterator builds the rule's instance generator, anchored so that the rule
// advances the wall clock in the event's own zone. That is what keeps a 09:00
// weekly event at 09:00 across a daylight-saving change.
func (v VEvent) iterator() (func() (time.Time, bool), error) {
	opt, err := rrule.StrToROption(v.RRule)
	if err != nil {
		return nil, fmt.Errorf("recurrence rule %q: %w", v.RRule, err)
	}
	opt.Dtstart = v.Start.Wall()
	r, err := rrule.NewRRule(*opt)
	if err != nil {
		return nil, fmt.Errorf("recurrence rule %q: %w", v.RRule, err)
	}
	return r.Iterator(), nil
}

// ── the six things a scoped change does ──

// ExcludeOccurrence removes one instance from the series by excluding its
// original start.
//
// The exclusion is written in the series' own value type and zone: a bare UTC
// exclusion does not cancel an instance of a zone-anchored series, so the
// "deleted" occurrence would keep coming back.
//
// It reports false when the instance is already excluded, so retrying a delete
// does not accumulate duplicates.
func (v VEvent) ExcludeOccurrence(occ DateTime) (VEvent, bool) {
	exdate := v.Start.At(occ.Time)
	if slices.ContainsFunc(v.ExDates, exdate.Equal) {
		return v, false
	}
	v.ExDates = append(slices.Clone(v.ExDates), exdate)
	v.sortExDates()
	return v, true
}

// TruncateBefore ends the series just before the given instance.
//
// A counted rule loses the instances from here on; a dated or unbounded rule
// gains an UNTIL. It reports false when nothing would be left, which is the
// caller's cue to delete the event rather than store an empty series.
func (v VEvent) TruncateBefore(occ DateTime, ordinal int) (VEvent, bool) {
	if count := ruleCount(v.RRule); count > 0 {
		remaining := ordinal - 1
		if remaining < 1 {
			return v, false
		}
		v.RRule = RuleWith(v.RRule, "COUNT", strconv.Itoa(remaining))
		return v, true
	}
	if ordinal <= 1 {
		return v, false
	}
	v.RRule = RuleWith(RuleWithout(v.RRule, "COUNT"), "UNTIL", UntilValue(v.Start.At(occ.Time)))
	return v, true
}

// AsOverride turns the event into the single-occurrence override of a series: it
// keeps the series UID, names the instance it replaces, and carries no rule of
// its own.
func (v VEvent) AsOverride(series VEvent, occ DateTime) VEvent {
	id := series.Start.At(occ.Time)
	v.UID = series.UID
	v.RecurrenceID = &id
	v.RRule = ""
	v.ExDates = nil
	return v
}

// AsFutureSeries turns the event into the remainder of a split series: a new
// series, with a UID derived from the old one so the chain stays discoverable,
// carrying whatever is left of a counted rule.
func (v VEvent) AsFutureSeries(series VEvent, occ DateTime, ordinal int) VEvent {
	v.UID = FutureUID(series.UID, series.Start.At(occ.Time))
	v.RecurrenceID = nil
	v.ExDates = nil
	v.Sequence = 0
	if count := ruleCount(v.RRule); count > 0 {
		remaining := count - (ordinal - 1)
		if remaining < 1 {
			v.RRule = ""
		} else {
			v.RRule = RuleWith(v.RRule, "COUNT", strconv.Itoa(remaining))
		}
	}
	return v
}

// recurrenceOffset matches the suffix FutureUID adds, so splitting a series that
// was already split does not stack offsets.
var recurrenceOffset = regexp.MustCompile(`(?:_R\d{8}(?:T\d{6})?)+$`)

// FutureUID derives the identifier for the remainder of a series split at occ.
func FutureUID(uid string, occ DateTime) string {
	offset := "_R" + occ.Wall().Format(dateLayout)
	if !occ.AllDay {
		offset += "T" + occ.Wall().Format("150405")
	}
	pre, post := uid, ""
	if at := strings.LastIndexByte(uid, '@'); at >= 0 {
		pre, post = uid[:at], uid[at:]
	}
	return recurrenceOffset.ReplaceAllString(pre, "") + offset + post
}

// NextSequence is the sequence an update writes.
//
// Proton refuses a sequence that goes backwards, and an override may never sit
// below the series it belongs to, so the number only ever rises. It rises only
// for a change attendees have to be told about - the times, whether it is all-day,
// or a rule that is not a narrowing of the old one - because bumping it otherwise
// re-notifies everybody about a corrected spelling.
func NextSequence(old, updated VEvent) int {
	breaking := !old.Start.Equal(updated.Start) ||
		!old.End.Equal(updated.End) ||
		old.Start.AllDay != updated.Start.AllDay ||
		!ruleIsNarrowing(old.RRule, updated.RRule)
	if breaking {
		return old.Sequence + 1
	}
	return old.Sequence
}

// ruleIsNarrowing reports whether updated recurs on the same pattern as old, or
// on a subset of it. Dropping or shortening the tail of a series does not change
// when its remaining occurrences happen, so it is not a breaking change.
func ruleIsNarrowing(old, updated string) bool {
	if old == updated {
		return true
	}
	if old == "" || updated == "" {
		return false
	}
	return RuleWithout(old, "COUNT", "UNTIL") == RuleWithout(updated, "COUNT", "UNTIL")
}
