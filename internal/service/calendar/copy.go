package calendar

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/ical"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
)

const copyDir = "calendar/"

type copyPart struct{ s *Service }

func (s *Service) CopyPart() export.Part { return copyPart{s: s} }

func (copyPart) App() export.App { return export.AppCalendar }

func (copyPart) Noun() string { return "events" }

func (p copyPart) Holds(ctx context.Context) ([]export.Held, error) {
	cals, err := p.s.CalendarsList(ctx)
	if err != nil {
		return nil, err
	}
	return []export.Held{{Count: len(own(cals)), Noun: "calendars"}}, nil
}

func own(cals []Calendar) []Calendar {
	out := make([]Calendar, 0, len(cals))
	for _, cal := range cals {
		if cal.Kind == KindPersonal {
			out = append(out, cal)
		}
	}
	return out
}

type calendarFile struct {
	cal     Calendar
	events  []ical.VEvent
	changed int
	text    string
}

func (p copyPart) Run(ctx context.Context, c *export.Copy, sink progress.Sink) (res export.Result, err error) {
	ledger, err := c.Ledger(export.AppCalendar)
	if err != nil {
		return res, err
	}
	defer func() {
		if closed := ledger.Close(); err == nil {
			err = closed
		}
	}()
	cals, err := p.s.CalendarsList(ctx)
	if err != nil {
		return res, err
	}
	res.Shared = map[string]int{}
	for _, cal := range cals {
		if cal.Kind == KindShared {
			res.Shared["calendars"]++
		}
	}
	var due []calendarFile
	total := 0
	for _, cal := range own(cals) {
		events, err := p.s.EventsExport(ctx, []string{cal.ID}, nil)
		if err != nil {
			return res, err
		}
		f, owed, err := p.compare(c, ledger, cal, events)
		if errors.Is(err, errUnreadableCopy) {
			slog.WarnContext(ctx, fmt.Sprintf("%s does not read back, so it is left as it is. Move it away and run the export again to write it afresh.",
				c.Path(fileKeyPath(ledger, cal.ID))), "reason", "unreadable")
			continue
		}
		if err != nil {
			return res, err
		}
		if owed {
			due = append(due, f)
			total += len(f.events)
		}
	}
	if err := ledger.Retain(func(id string) bool { return strings.HasPrefix(id, fileKeyPrefix) }); err != nil {
		return res, err
	}
	if len(due) == 0 {
		return res, nil
	}
	progress.Counting(sink, "events")
	sink.Start(int64(total), "Exporting calendar")
	defer sink.Done()
	for _, f := range due {
		res.Owed += f.changed
		if err := p.write(c, ledger, f); err != nil {
			return res, err
		}
		res.Exported += f.changed
		sink.Add(int64(len(f.events)))
	}
	return res, nil
}

func eventKey(v ical.VEvent) string {
	if v.RecurrenceID == nil {
		return v.UID
	}
	return v.UID + ":" + v.RecurrenceID.String()
}

const fileKeyPrefix = "calendar:"

func fileKey(calID string) string { return fileKeyPrefix + calID }

func fileKeyPath(ledger *export.Ledger, calID string) string {
	e, _ := ledger.Get(fileKey(calID))
	return e.Path
}

var errUnreadableCopy = errors.New("calendar: the copy's file does not read back")

func (copyPart) compare(c *export.Copy, ledger *export.Ledger, cal Calendar, events []ical.VEvent) (calendarFile, bool, error) {
	f := calendarFile{cal: cal}
	want := copyDir + localfile.SafeName(cal.Name) + ".ics"
	now, was, err := ledger.Place(fileKey(cal.ID), want)
	if err != nil {
		return f, false, err
	}
	if was != "" && was != now && localfile.Exists(c.Path(was)) {
		if err := os.Rename(c.Path(was), c.Path(now)); err != nil {
			return f, false, err
		}
	}
	before, err := os.ReadFile(c.Path(now))
	present := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return f, false, err
	}
	previous, err := ical.ParseCalendar(string(before))
	if err != nil {
		return f, false, errUnreadableCopy
	}

	written := make(map[string]string, len(previous))
	for _, v := range previous {
		written[eventKey(v)] = ical.Calendar([]ical.VEvent{v})
	}
	held := make(map[string]bool, len(events))
	for _, v := range events {
		held[v.UID] = true
		if was, ok := written[eventKey(v)]; !ok || was != ical.Calendar([]ical.VEvent{v}) {
			f.changed++
		}
	}
	f.events = slices.Clone(events)
	for _, v := range previous {
		if !held[v.UID] {
			f.events = append(f.events, v)
		}
	}
	slices.SortStableFunc(f.events, func(a, b ical.VEvent) int { return a.Start.Time.Compare(b.Start.Time) })
	f.text = ical.Calendar(f.events)
	if !present {
		f.changed = len(events)
	}
	return f, !present || f.text != string(before), nil
}

func (copyPart) write(c *export.Copy, ledger *export.Ledger, f calendarFile) error {
	file, _ := ledger.Get(fileKey(f.cal.ID))
	if err := localfile.Publish(c.Path(file.Path), []byte(f.text), time.Time{}); err != nil {
		return err
	}
	return ledger.Put(export.Entry{ID: fileKey(f.cal.ID), Path: file.Path, Want: file.Want})
}
