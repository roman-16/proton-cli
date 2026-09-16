package main

import (
	"testing"
	"time"
)

func at(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return parsed
}

func TestClassify(t *testing.T) {
	tests := []struct {
		asset string
		want  kind
	}{
		{asset: "checksums.txt", want: ignored},
		{asset: "proton-cli_4.1.0_darwin_arm64.tar.gz", want: packaged},
		{asset: "proton-cli_4.1.0_linux_amd64.apk", want: packaged},
		{asset: "proton-cli_4.1.0_linux_amd64.deb", want: packaged},
		{asset: "proton-cli_4.1.0_linux_amd64.rpm", want: packaged},
		{asset: "proton-cli_4.1.0_windows_amd64.zip", want: packaged},
		{asset: "proton-cli_darwin_arm64", want: binary},
		{asset: "proton-cli_linux_amd64", want: binary},
		{asset: "proton-cli_windows_amd64.exe", want: binary},
		{asset: "source-code.tar.gz", want: ignored},
		{asset: "proton-cli_4.1.0_darwin_amd64", want: binary},
		{asset: "proton-cli_4.1.0_windows_amd64.exe", want: binary},
	}
	for _, test := range tests {
		t.Run(test.asset, func(t *testing.T) {
			if got := classify(test.asset, "4.1.0"); got != test.want {
				t.Errorf("classify(%q) = %d, want %d", test.asset, got, test.want)
			}
		})
	}
}

func TestPlatform(t *testing.T) {
	tests := []struct {
		asset string
		want  string
	}{
		{asset: "proton-cli_4.1.0_darwin_amd64", want: "darwin_amd64"},
		{asset: "proton-cli_4.1.0_linux_amd64.deb", want: "linux_amd64"},
		{asset: "proton-cli_4.1.0_windows_arm64.zip", want: "windows_arm64"},
		{asset: "proton-cli_darwin_arm64", want: "darwin_arm64"},
		{asset: "proton-cli_windows_amd64.exe", want: "windows_amd64"},
	}
	for _, test := range tests {
		t.Run(test.asset, func(t *testing.T) {
			if got := platform(test.asset, "4.1.0"); got != test.want {
				t.Errorf("platform(%q) = %q, want %q", test.asset, got, test.want)
			}
		})
	}
}

// The two channels are counted from the assets themselves, so a scripted
// install counts once - as the binary it fetched - and the checksums.txt it
// fetched alongside counts for nothing.
func TestSpend(t *testing.T) {
	releases := []release{{
		Tag: "v4.1.0",
		Assets: []asset{
			{Name: "checksums.txt", Downloads: 30},
			{Name: "proton-cli_4.1.0_linux_amd64.deb", Downloads: 7},
			{Name: "proton-cli_4.1.0_linux_amd64.tar.gz", Downloads: 40},
			{Name: "proton-cli_linux_amd64", Downloads: 120},
		},
	}}
	previous := state{
		CapturedAt: at(t, "2026-09-15T00:00:12Z"),
		Assets: map[string]int{
			"v4.1.0/checksums.txt":                       20,
			"v4.1.0/proton-cli_4.1.0_linux_amd64.tar.gz": 35,
			"v4.1.0/proton-cli_linux_amd64":              100,
		},
	}
	current := state{CapturedAt: at(t, "2026-09-16T03:00:12Z"), Assets: counters(releases)}

	day := spend(previous, current, releases)

	if day.Date != "2026-09-15" {
		t.Errorf("date = %q, want 2026-09-15", day.Date)
	}
	if day.Hours != 27 {
		t.Errorf("hours = %v, want 27", day.Hours)
	}
	if day.Binary != 20 {
		t.Errorf("binary = %d, want 20", day.Binary)
	}
	// Five of the archive, and the whole of a package first seen inside the
	// interval.
	if day.Packaged != 12 {
		t.Errorf("packaged = %d, want 12", day.Packaged)
	}
	if day.Platform["linux_amd64"] != 32 {
		t.Errorf("platform linux_amd64 = %d, want 32", day.Platform["linux_amd64"])
	}
	if day.Version["4.1.0"] != 32 {
		t.Errorf("version 4.1.0 = %d, want 32", day.Version["4.1.0"])
	}
}

func TestSpendNamesTheDayTheIntervalBegan(t *testing.T) {
	previous := state{CapturedAt: at(t, "2026-09-15T23:50:00Z")}
	current := state{CapturedAt: at(t, "2026-09-16T00:10:00Z")}

	if day := spend(previous, current, nil); day.Date != "2026-09-15" {
		t.Errorf("date = %q, want 2026-09-15", day.Date)
	}
}

func TestRecord(t *testing.T) {
	series := []downloadDay{{Date: "2026-09-15", Hours: 1, Binary: 2, Packaged: 3}}
	day := downloadDay{
		Date: "2026-09-15", Hours: 2, Binary: 5, Packaged: 7,
		Platform: map[string]int{}, Version: map[string]int{},
	}

	series = record(series, day)

	if len(series) != 1 {
		t.Fatalf("rows = %d, want 1", len(series))
	}
	if got := series[0]; got.Hours != 3 || got.Binary != 7 || got.Packaged != 10 {
		t.Errorf("merged row = %+v, want hours 3, binary 7, packaged 10", got)
	}
}

func TestLatest(t *testing.T) {
	tests := []struct {
		name     string
		releases []release
		want     string
	}{
		{
			name: "nothing published",
			want: "",
		},
		{
			name:     "the highest version rather than the longest string",
			releases: []release{{Tag: "v3.8.0"}, {Tag: "v3.10.0"}, {Tag: "v3.9.0"}},
			want:     "3.10.0",
		},
		{
			name: "a draft and a prerelease are not published",
			releases: []release{
				{Tag: "v4.1.0"},
				{Tag: "v4.2.0", Draft: true},
				{Tag: "v5.0.0-rc.1", Prerelease: true},
			},
			want: "4.1.0",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := latest(test.releases); got != test.want {
				t.Errorf("latest = %q, want %q", got, test.want)
			}
		})
	}
}

func TestCounted(t *testing.T) {
	daily := []countDay{
		{Date: "2026-09-12", Count: 0},
		{Date: "2026-09-13", Count: 90},
		{Date: "2026-09-14", Count: 70},
		{Date: "2026-09-15", Count: 0},
		{Date: "2026-09-16", Count: 0},
	}

	got := counted(daily)

	if len(got) != 3 {
		t.Fatalf("days = %d, want 3", len(got))
	}
	if last := got[len(got)-1]; last.Date != "2026-09-14" {
		t.Errorf("last day = %q, want 2026-09-14", last.Date)
	}
	if counted(nil) != nil {
		t.Error("counted(nil) is not nil")
	}
}
