package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/ui"
)

// write puts a configuration file somewhere and returns its path.
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), Name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, body string) *File {
	t.Helper()
	f, _, err := Load(write(t, body), Source{Named: "--config"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return f
}

// clean removes every variable this package reads, so a developer's own
// environment cannot decide what a test sees.
func clean(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		PathVar, ConfirmVar, "PROTON_PROFILE", "PROTON_LOG_LEVEL",
		"FORCE_COLOR", "NO_COLOR", "PROTON_NO_INPUT", "PROTON_NO_UPDATE_CHECK",
	} {
		prev, had := os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
		if had {
			t.Cleanup(func() { _ = os.Setenv(key, prev) })
		}
	}
}

func resolve(t *testing.T, f *File, flags Flags) Resolved {
	t.Helper()
	got, err := Resolve(f, flags)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	return got
}

// The ordinary case: no file at the default path is no configuration, not a
// failure.
func TestAMissingFileIsNoConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), Name)
	f, from, err := Load(path, Source{})
	if f != nil || err != nil {
		t.Errorf("Load = (%v, %v), want (nil, nil)", f, err)
	}
	if got := from.Describe(); got != "none" {
		t.Errorf("a machine with no file reads as %q, want %q", got, "none")
	}
}

// A file somebody named and that is not there is a mistake worth reporting: they
// meant to configure something and nothing was configured.
func TestANamedFileThatIsMissingIsAnError(t *testing.T) {
	_, from, err := Load(filepath.Join(t.TempDir(), "nope.yaml"), Source{Named: "--config"})
	if err == nil {
		t.Error("a named file that does not exist should be reported")
	}
	if got := from.Describe(); got != "named by --config, ignored: not there" {
		t.Errorf("it reads as %q", got)
	}
}

// What a report says about the file is what went wrong with it and never where
// it was: a configuration path holds a home directory, and a report is pasted in
// public.
func TestWhatIsSaidAboutTheFileNamesNoPath(t *testing.T) {
	path := write(t, "outpu: json\n")
	_, from, err := Load(path, Source{})
	if err == nil {
		t.Fatal("a file with a key that is not one should be refused")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("the error %q does not name the file the reader has to fix", err)
	}
	got := from.Describe()
	if strings.Contains(got, path) || strings.Contains(got, filepath.Dir(path)) {
		t.Errorf("the report would carry the path: %q", got)
	}
	if !strings.Contains(got, `unknown field "outpu"`) {
		t.Errorf("the report would not say what was wrong: %q", got)
	}
}

// Nothing runs on a file that does not parse. It carries the confirmation
// policy, and a policy that quietly fails to load is one that fails open.
//
// Every refusal names the file and the position in it, because the reader has to
// find the line before they can fix it.
func TestAFileThatCannotBeReadStopsEverything(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"a key that is not one", "loglevel: warn\n"},
		{"a value of the wrong shape", "per-profile: work\n"},
		{"a profile named inside a section", "per-profile:\n  work:\n    profile: other\n"},
		{"not YAML at all", "output: [json\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := write(t, tc.body)
			_, _, err := Load(path, Source{Named: "--config"})
			if err == nil {
				t.Fatal("want an error")
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not name the file", err)
			}
			if !strings.Contains(err.Error(), "[") {
				t.Errorf("error %q carries no position", err)
			}
		})
	}
}

// Which profile is in force has to be settled before the section for it can be
// chosen, so it is the one setting that only means something at the top level.
func TestProfileIsSettledBeforeItsSection(t *testing.T) {
	clean(t)
	f := load(t, "profile: work\noutput: json\nper-profile:\n  work:\n    output: yaml\n")

	got := resolve(t, f, Flags{})
	if got.Profile.String() != "work" || got.Output != ui.FormatYAML {
		t.Errorf("the file's profile picks its own section: got %s/%s", got.Profile, got.Output)
	}

	t.Setenv("PROTON_PROFILE", "personal")
	if got := resolve(t, f, Flags{}); got.Profile.String() != "personal" || got.Output != ui.FormatJSON {
		t.Errorf("the environment picks a different section: got %s/%s", got.Profile, got.Output)
	}
	if got := resolve(t, f, Flags{Profile: "work"}); got.Profile.String() != "work" || got.Output != ui.FormatYAML {
		t.Errorf("the flag beats the environment: got %s/%s", got.Profile, got.Output)
	}
}

// An ordinary setting takes the nearest answer, which is the order every other
// tool uses and the one people expect.
func TestTheNearestSourceWinsForAnOrdinarySetting(t *testing.T) {
	clean(t)
	f := load(t, "log-level: error\nper-profile:\n  work:\n    log-level: info\n")

	if got := resolve(t, f, Flags{}); got.LogLevel != slog.LevelError {
		t.Errorf("with no section in play the top level applies: got %v", got.LogLevel)
	}
	if got := resolve(t, f, Flags{Profile: "work"}); got.LogLevel != slog.LevelInfo {
		t.Errorf("a section beats the top level: got %v", got.LogLevel)
	}
	t.Setenv("PROTON_LOG_LEVEL", "warn")
	if got := resolve(t, f, Flags{Profile: "work"}); got.LogLevel != slog.LevelWarn {
		t.Errorf("the environment beats a section: got %v", got.LogLevel)
	}
	if got := resolve(t, f, Flags{Profile: "work", LogLevel: "debug"}); got.LogLevel != slog.LevelDebug {
		t.Errorf("the flag beats everything: got %v", got.LogLevel)
	}
}

// A boolean the file sets is still a preference, so the flag has to be able to
// say no as well as yes. Nothing else tells "left alone" from "set to false".
func TestAFileBooleanIsOverridableForOneRun(t *testing.T) {
	clean(t)
	f := load(t, "quiet: true\n")
	no, yes := false, true

	if !resolve(t, f, Flags{}).Quiet {
		t.Error("the file should apply when the flag is left alone")
	}
	if resolve(t, f, Flags{Quiet: &no}).Quiet {
		t.Error("--quiet=false has to beat the file")
	}
	if !resolve(t, nil, Flags{Quiet: &yes}).Quiet {
		t.Error("--quiet has to work with no file at all")
	}
}

// Colour has three answers rather than two, because whether it is wanted and
// whether the destination can show it are different questions - and only the
// person running the command knows the first when the answer is going somewhere
// this program cannot ask about.
//
// The nearest source that says anything decides, as everywhere else. Inside the
// environment forcing outranks refusing: NO_COLOR is set once for everything a
// person runs, FORCE_COLOR for one job by whoever knows what reads its output.
func TestColourTakesTheNearestSourceThatSaysAnything(t *testing.T) {
	no, yes := false, true
	for _, tc := range []struct {
		name  string
		env   map[string]string
		file  string
		flags Flags
		want  ui.Color
	}{
		{name: "nothing said", want: ui.ColorAuto},
		{name: "the file", file: "no-color: true\n", want: ui.ColorNever},
		{name: "NO_COLOR", env: map[string]string{"NO_COLOR": ""}, want: ui.ColorNever},
		{name: "FORCE_COLOR", env: map[string]string{"FORCE_COLOR": ""}, want: ui.ColorAlways},
		{
			name: "forcing outranks refusing",
			env:  map[string]string{"NO_COLOR": "1", "FORCE_COLOR": "1"},
			want: ui.ColorAlways,
		},
		{
			name:  "--force-color beats NO_COLOR",
			env:   map[string]string{"NO_COLOR": "1"},
			flags: Flags{ForceColor: &yes},
			want:  ui.ColorAlways,
		},
		{
			name:  "--no-color beats FORCE_COLOR",
			env:   map[string]string{"FORCE_COLOR": "1"},
			flags: Flags{NoColor: &yes},
			want:  ui.ColorNever,
		},
		{
			name:  "--no-color=false takes the file's answer back",
			file:  "no-color: true\n",
			flags: Flags{NoColor: &no},
			want:  ui.ColorAuto,
		},
		{
			name:  "--force-color=false forces nothing",
			env:   map[string]string{"FORCE_COLOR": "1"},
			flags: Flags{ForceColor: &no},
			want:  ui.ColorAuto,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clean(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			var file *File
			if tc.file != "" {
				file = load(t, tc.file)
			}
			if got := resolve(t, file, tc.flags).Color; got != tc.want {
				t.Errorf("got colour %d, want %d", got, tc.want)
			}
		})
	}
}

// Asking for both at once is a sentence that contradicts itself. Resolved either
// way, half of what was typed would be passed over in silence.
func TestPaintingAndNotPaintingIsRefused(t *testing.T) {
	clean(t)
	yes := true
	_, err := Resolve(nil, Flags{ForceColor: &yes, NoColor: &yes})
	if err == nil {
		t.Fatal("--no-color with --force-color has to be refused")
	}
	for _, want := range []string{"--no-color", "--force-color"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not mention %s: %v", want, err)
		}
	}
}

// Presence is what counts for these, whatever the value - the convention
// NO_COLOR set and PROTON_NO_INPUT follows.
func TestPresenceIsWhatCountsForTheConventionalVariables(t *testing.T) {
	for _, key := range []string{"NO_COLOR", "FORCE_COLOR", "PROTON_NO_INPUT", "PROTON_NO_UPDATE_CHECK"} {
		for _, value := range []string{"1", "0", "false", ""} {
			clean(t)
			t.Setenv(key, value)
			got := resolve(t, nil, Flags{})
			set := map[string]bool{
				"NO_COLOR": got.Color == ui.ColorNever, "FORCE_COLOR": got.Color == ui.ColorAlways,
				"PROTON_NO_INPUT": got.NoInput, "PROTON_NO_UPDATE_CHECK": got.NoUpdateCheck,
			}[key]
			if !set {
				t.Errorf("%s=%q is set, so it applies", key, value)
			}
		}
	}
}

// Every place a policy may be written is a source of its own, and the most
// cautious of them decides. Adding one can tighten the policy and can never
// loosen it.
func TestTheConfirmationPolicyOnlyEverTightens(t *testing.T) {
	clean(t)
	f := load(t, "confirm:\n  ask:\n    \"*\": mutations\nper-profile:\n  work:\n    confirm:\n      deny:\n        \"*\": deletions\n")
	send := confirm.Command{Path: []string{"mail", "drafts", "send"}, Mutating: true}
	del := confirm.Command{Path: []string{"mail", "messages", "delete"}, Mutating: true, Irreversible: true}

	global := resolve(t, f, Flags{}).Confirm
	if got := global.Require(send).Outcome; got != confirm.Ask {
		t.Errorf("the top level asks about mutations: got %v", got)
	}
	if got := global.Require(del).Outcome; got == confirm.Deny {
		t.Error("a section's deny must not apply outside that section")
	}

	scoped := resolve(t, f, Flags{Profile: "work"}).Confirm
	if got := scoped.Require(del).Outcome; got != confirm.Deny {
		t.Errorf("a section tightens what the top level said: got %v", got)
	}
	if got := scoped.Require(send).Outcome; got != confirm.Ask {
		t.Errorf("and leaves the rest of it standing: got %v", got)
	}

	// A flag adds a fourth source. It cannot stand any of the others down.
	loosened := resolve(t, f, Flags{Profile: "work", Confirm: "default"}).Confirm
	if got := loosened.Require(del).Outcome; got != confirm.Deny {
		t.Errorf("--confirm default must not lift a deny: got %v", got)
	}
	if got := loosened.Require(send).Outcome; got != confirm.Ask {
		t.Errorf("--confirm default must not lift an ask: got %v", got)
	}
}

// The variable and the flag are read the same way the file is, so a policy moves
// between them unchanged.
func TestThePolicyCanComeFromTheEnvironmentOrAFlag(t *testing.T) {
	clean(t)
	list := confirm.Command{Path: []string{"mail", "messages", "list"}}

	t.Setenv(ConfirmVar, "reads")
	if got := resolve(t, nil, Flags{}).Confirm.Require(list).Outcome; got != confirm.Ask {
		t.Errorf("%s should apply: got %v", ConfirmVar, got)
	}
	clean(t)
	if got := resolve(t, nil, Flags{Confirm: "reads"}).Confirm.Require(list).Outcome; got != confirm.Ask {
		t.Errorf("--confirm should apply: got %v", got)
	}
}

// A directive that cannot mean anything stops the command where it was written.
func TestABadPolicyIsRefused(t *testing.T) {
	clean(t)
	if _, err := Resolve(nil, Flags{Confirm: "nonsense"}); err == nil {
		t.Error("--confirm nonsense should be refused")
	}
	f := load(t, "confirm:\n  ask:\n    \"*\": everything\n")
	if _, err := Resolve(f, Flags{}); err == nil {
		t.Error("a class the file invents should be refused")
	}
}

// The file to read is chosen before anything in it can have an opinion.
func TestWhichFileToReadIsChosenFirst(t *testing.T) {
	clean(t)
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}

	path, from, err := Path("")
	if err != nil || from.Named != "" || path != filepath.Join(dir, Name) {
		t.Errorf(`Path("") = (%q, %v, %v), want the default path, unnamed`, path, from, err)
	}
	t.Setenv(PathVar, "/from/env.yaml")
	if path, from, _ := Path(""); path != "/from/env.yaml" || from.Named != PathVar {
		t.Errorf("%s should be honoured: got %q from %q", PathVar, path, from.Named)
	}
	if path, from, _ := Path("/from/flag.yaml"); path != "/from/flag.yaml" || from.Named != "--config" {
		t.Errorf("--config should beat %s: got %q from %q", PathVar, path, from.Named)
	}
}

// With no file and no variables, the defaults are the ones the CLI has always
// had.
func TestNoConfigurationLeavesTheDefaults(t *testing.T) {
	clean(t)
	got := resolve(t, nil, Flags{})
	if got.Profile.String() != "default" || got.Output != ui.FormatText || got.LogLevel != slog.LevelWarn {
		t.Errorf("got %s/%s/%v", got.Profile, got.Output, got.LogLevel)
	}
	if got.Quiet || got.FullIDs || got.Color != ui.ColorAuto || got.NoInput || got.NoUpdateCheck || len(got.Confirm) != 0 {
		t.Errorf("nothing should be switched on: %+v", got)
	}
}
