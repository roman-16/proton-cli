package app

import (
	"context"
	"sync"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
)

// An event, a scheduled send and a range of days are all written against a
// named time zone, and it is the same zone for all of them: the one the person
// running the command is in. So it is settled once per invocation rather than
// per command, and nothing below here asks the machine a second time.
//
// A name is what is needed, not an offset. A weekly 09:00 meeting anchored to
// Europe/Vienna stays at 09:00 when the clocks change; the same meeting stored
// as a UTC instant moves to 08:00.

// LocalZone is the zone this machine and its configuration name, and nothing
// more: the answer that cost no request. It is what the diagnostic log and a
// report state, neither of which may reach the network to say what they know.
func (a *App) LocalZone() string { return a.zone.name }

// Zone is the IANA time zone this invocation works in.
//
// Configuration answers first, from the flag, the variable, the file and the
// host in that order. Only when this machine cannot name its own zone is the
// account asked, which covers the platforms that keep no zone file and the
// containers that ship none - and only then, because it costs a request.
//
// Nothing is guessed. A run that reaches the end of the chain fails, because the
// alternative is an event stored without an anchor, which is a silent hour's
// error twice a year rather than a message now.
func (a *App) Zone(ctx context.Context) (string, error) {
	a.zone.once.Do(func() {
		if a.zone.name == "" {
			a.zone.name = a.Calendar.PrimaryZone(ctx)
		}
		adoptZone(a.zone.name)
	})
	if a.zone.name == "" {
		return "", errs.Problemf("Nothing here names your time zone, and an event has to be anchored to one.").
			Hint("set TZ, put `zone:` in your config, or pass --zone Europe/Vienna")
	}
	return a.zone.name, nil
}

// Location is the zone this invocation works in, as the frame a wall-clock time
// is read and printed in.
func (a *App) Location(ctx context.Context) (*time.Location, error) {
	name, err := a.Zone(ctx)
	if err != nil {
		return nil, err
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, errs.Problemf("%q is not a time zone this machine knows.", name)
	}
	return loc, nil
}

// adoptZone makes the resolved zone this process's own.
//
// The zone is the frame every wall-clock reading is written in and read back
// out of, and those two have to agree: an occurrence reference the CLI prints
// in one zone and parses in another is a reference nobody can use. Naming the
// frame once, where the standard library keeps it, is what makes them agree by
// construction rather than by every caller remembering to pass a location.
//
// It is the same thing TZ does, for the same reason, and --zone is that setting
// for one run.
func adoptZone(name string) {
	if name == "" {
		return
	}
	if loc, err := time.LoadLocation(name); err == nil {
		time.Local = loc
	}
}

// zoneCache holds the answer for the invocation, so the account is asked at most
// once however many times a command needs the zone.
type zoneCache struct {
	once sync.Once
	name string
}
