package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	// The zone database travels with the binary. A machine that names its zone
	// through a variable rather than a file may have no zoneinfo installed at
	// all, and every timed write needs to resolve the name it was given.
	_ "time/tzdata"
)

// ZoneVar is where the zone is read from when no flag or file names one.
//
// It is POSIX's own variable, not one of this CLI's: a machine that has already
// been told which zone it is in should not have to be told again, and inventing
// a second name for the setting every other tool reads would leave the two free
// to disagree.
const ZoneVar = "TZ"

// envZone is the zone the environment names, or "".
//
// TZ may hold a rule rather than a name - "CET-1CEST,M3.5.0,M10.5.0/3" is a
// legal value - and a rule cannot anchor an event, so one is passed over rather
// than rejected. What a person writes for this CLI is held to a higher standard;
// see resolveZone.
func envZone() string {
	tz := strings.TrimSpace(os.Getenv(ZoneVar))
	if plausibleZone(tz) {
		return tz
	}
	return ""
}

// hostZone names the zone this machine is set to, or "" when it cannot be named.
//
// The symlink and the file that the platforms which have them keep the name in
// are read directly, because the standard library discards the name and answers
// "Local", which no other calendar client can resolve.
func hostZone() string {
	if target, err := os.Readlink("/etc/localtime"); err == nil {
		if _, after, found := strings.Cut(filepath.ToSlash(target), "zoneinfo/"); found && plausibleZone(after) {
			return after
		}
	}
	if data, err := os.ReadFile("/etc/timezone"); err == nil {
		if name := strings.TrimSpace(string(data)); plausibleZone(name) {
			return name
		}
	}
	return ""
}

// plausibleZone reports whether a string is an IANA zone name, which is what
// rules out "Local" and the POSIX offset forms.
func plausibleZone(name string) bool {
	if name == "UTC" {
		return true
	}
	if !strings.Contains(name, "/") || strings.HasPrefix(name, "/") {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}
