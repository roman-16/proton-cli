package config

import (
	"strings"
	"testing"
)

// The zone is one setting with four places to say it, and they rank the way
// every ordinary setting ranks: the flag, then the variable, then the file, then
// what the machine says about itself.
func TestTheNearestSourceNamesTheZone(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flag   string
		env    string
		global Settings
		scoped Settings
		want   string
	}{
		{name: "the flag over everything", flag: "Asia/Tokyo", env: "America/New_York",
			global: Settings{Zone: "Europe/Vienna"}, want: "Asia/Tokyo"},
		{name: "the variable over the file", env: "America/New_York",
			global: Settings{Zone: "Europe/Vienna"}, want: "America/New_York"},
		{name: "a section over the top level", global: Settings{Zone: "Europe/Vienna"},
			scoped: Settings{Zone: "Asia/Tokyo"}, want: "Asia/Tokyo"},
		{name: "the file when nothing else says", global: Settings{Zone: "Europe/Vienna"},
			want: "Europe/Vienna"},
	} {
		t.Setenv(ZoneVar, tc.env)
		got, err := resolveZone(tc.global, tc.scoped, tc.flag)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: zone = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The file sits above the machine, so writing a zone down is how somebody
// working in one place from a laptop set to another says which they meant.
func TestTheFileOverridesTheMachine(t *testing.T) {
	t.Setenv(ZoneVar, "")
	got, err := resolveZone(Settings{Zone: "Pacific/Auckland"}, Settings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Pacific/Auckland" {
		t.Errorf("zone = %q, want the one in the file", got)
	}
}

// A zone written for this CLI has to be a real one. Passing over an unknown name
// would anchor an event to a zone nobody asked for, and say nothing about it.
func TestAZoneNobodyHasIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name   string
		flag   string
		global Settings
		source string
	}{
		{name: "from the flag", flag: "Europe/Viena", source: "--zone"},
		{name: "from the file", global: Settings{Zone: "Mars/Olympus"}, source: "zone"},
	} {
		t.Setenv(ZoneVar, "")
		_, err := resolveZone(tc.global, Settings{}, tc.flag)
		if err == nil {
			t.Fatalf("%s: an unknown zone was accepted", tc.name)
		}
		if !strings.Contains(err.Error(), tc.source) {
			t.Errorf("%s: error %q does not name the source", tc.name, err)
		}
	}
}

// TZ is POSIX's, and may hold a rule rather than a name. A rule cannot anchor an
// event, so it is passed over for whatever names one - it is not a mistake
// somebody made for this CLI's benefit.
func TestARuleInTheVariableIsPassedOver(t *testing.T) {
	t.Setenv(ZoneVar, "CET-1CEST,M3.5.0,M10.5.0/3")
	got, err := resolveZone(Settings{Zone: "Europe/Vienna"}, Settings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "Europe/Vienna" {
		t.Errorf("zone = %q, want the file's", got)
	}
}

// Nothing naming a zone is not a failure here. Whether one is required depends
// on the command, and the account is a further place to ask.
func TestNothingNamingAZoneIsNotAnError(t *testing.T) {
	t.Setenv(ZoneVar, "")
	got, err := resolveZone(Settings{}, Settings{}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" && !plausibleZone(got) {
		t.Errorf("zone = %q, want the host's or nothing", got)
	}
}

// "Local" is what Go answers when it has read a zone file but kept no name for
// it, and no other calendar client can resolve it.
func TestTheNamelessAnswersAreNotZones(t *testing.T) {
	for _, name := range []string{"Local", "", "CET-1CEST", "/etc/localtime", "+02:00"} {
		if plausibleZone(name) {
			t.Errorf("%q was taken for a zone name", name)
		}
	}
	for _, name := range []string{"UTC", "Europe/Vienna", "America/Argentina/Salta"} {
		if !plausibleZone(name) {
			t.Errorf("%q is a zone name and was not taken for one", name)
		}
	}
}
