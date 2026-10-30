package calendar

import (
	"context"
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
	cal      Calendar
	events   []ical.VEvent
	versions map[string]string
	gone     []string
	changed  int
	version  string
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
		if err != nil {
			return res, err
		}
		if owed {
			due = append(due, f)
			total += len(events)
		}
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

func eventKey(calID string, v ical.VEvent) string {
	key := "event:" + calID + ":" + v.UID
	if v.RecurrenceID != nil {
		key += ":" + v.RecurrenceID.String()
	}
	return key
}

func fileKey(calID string) string { return "calendar:" + calID }

func (copyPart) compare(c *export.Copy, ledger *export.Ledger, cal Calendar, events []ical.VEvent) (calendarFile, bool, error) {
	f := calendarFile{cal: cal, events: events, versions: make(map[string]string, len(events))}
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
	all := make([]string, 0, len(events))
	for _, v := range events {
		key := eventKey(cal.ID, v)
		version := export.Version(ical.Calendar([]ical.VEvent{v}))
		f.versions[key] = version
		all = append(all, version)
		if had, ok := ledger.Get(key); !ok || had.Version != version {
			f.changed++
		}
	}
	prefix := "event:" + cal.ID + ":"
	for _, id := range ledger.IDs() {
		if _, held := f.versions[id]; strings.HasPrefix(id, prefix) && !held {
			f.gone = append(f.gone, id)
		}
	}
	slices.Sort(all)
	f.version = export.Version(all)
	file, _ := ledger.Get(fileKey(cal.ID))
	present := localfile.Exists(c.Path(now))
	if !present {
		f.changed = len(events)
	}
	f.changed += len(f.gone)
	return f, !present || file.Version != f.version, nil
}

func (copyPart) write(c *export.Copy, ledger *export.Ledger, f calendarFile) error {
	file, _ := ledger.Get(fileKey(f.cal.ID))
	if err := localfile.Publish(c.Path(file.Path), []byte(ical.Calendar(f.events)), time.Time{}); err != nil {
		return err
	}
	for key, version := range f.versions {
		if err := ledger.Put(export.Entry{ID: key, Version: version}); err != nil {
			return err
		}
	}
	for _, id := range f.gone {
		if err := ledger.Forget(id); err != nil {
			return err
		}
	}
	return ledger.Put(export.Entry{ID: fileKey(f.cal.ID), Version: f.version, Path: file.Path, Want: file.Want})
}
