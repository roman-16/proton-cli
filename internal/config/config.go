// Package config is the file of durable preferences and the rules for how a
// flag, an environment variable and that file combine.
//
// Everything one invocation runs with is resolved here, once, so that no other
// package has to remember which source outranks which. Two rules do that work,
// and they point in opposite directions on purpose:
//
// An ordinary setting says how the CLI should behave for the convenience of the
// person running it, so the nearest source wins - a flag over a variable, a
// variable over the file, the file over the built-in default.
//
// The confirmation policy says what the CLI must not do by accident, so the most
// cautious source wins instead. See package confirm.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/redact"
	"github.com/roman-16/proton-cli/internal/runlog"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Name is the file this package reads, inside Dir.
const Name = "config.yaml"

// PathVar names a file somewhere other than Dir.
const PathVar = "PROTON_CONFIG"

// Dir is where everything this CLI keeps on disk lives: ~/.config/proton-cli on
// Linux, and the platform's own equivalent elsewhere.
func Dir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "proton-cli"), nil
}

// Settings is every preference that may be written in either scope.
//
// The booleans are pointers so that a file which says nothing about one is not
// the same as a file which turns it off. Without that, every scope would assert
// every setting and a per-profile section could never leave a global one alone.
type Settings struct {
	Output        string           `yaml:"output"`
	LogLevel      string           `yaml:"log-level"`
	Zone          string           `yaml:"zone"`
	Quiet         *bool            `yaml:"quiet"`
	FullIDs       *bool            `yaml:"full-ids"`
	NoColor       *bool            `yaml:"no-color"`
	NoInput       *bool            `yaml:"no-input"`
	NoLog         *bool            `yaml:"no-log"`
	NoUpdateCheck *bool            `yaml:"no-update-check"`
	Confirm       confirm.Document `yaml:"confirm"`
}

// File is the document on disk.
//
// The top level holds the settings that apply whichever profile is in use, and
// per-profile narrows them to one. Which profile that is can only be said at the
// top level, because it has to be known before a section can be chosen - so a
// `profile` key inside a section is not a setting this file has, and the strict
// decode says so.
type File struct {
	Profile    string `yaml:"profile"`
	Settings   `yaml:",inline"`
	PerProfile map[string]Settings `yaml:"per-profile"`
}

// Source is where an invocation's settings came from, in the terms a bug report
// may carry: which of the three places named the file, whether there was one,
// and what stopped it being honoured.
//
// Never the path. A configuration path holds a home directory, which is a
// person's name on every platform this runs on, and a report is pasted in
// public.
type Source struct {
	// Named is what pointed at the file, or empty for the default location.
	Named string
	// Present is whether there was a file there to read.
	Present bool
	// Problem is why the settings in force are not this file's, phrased without
	// the path.
	Problem error
}

// Describe says where the settings came from, in one line, for a report.
func (s Source) Describe() string {
	where := "default location"
	if s.Named != "" {
		where = "named by " + s.Named
	}
	switch {
	case s.Problem != nil:
		return where + ", ignored: " + s.Problem.Error()
	case s.Present:
		return where
	}
	return "none"
}

// Ignoring is this source carrying the reason its settings are not in force.
//
// The first reason stands: what stopped the file being read says more than
// whatever the run made of the settings afterwards, and it is the one already
// phrased without the path.
func (s Source) Ignoring(why error) Source {
	if s.Problem == nil {
		s.Problem = why
	}
	return s
}

// Path is the file to read, and where that answer came from.
//
// A file somebody named and that is not there is a mistake worth reporting; the
// default one being absent is the ordinary case of having written no
// configuration.
func Path(flag string) (path string, from Source, err error) {
	if flag != "" {
		return flag, Source{Named: "--config"}, nil
	}
	if env := os.Getenv(PathVar); env != "" {
		return env, Source{Named: PathVar}, nil
	}
	dir, err := Dir()
	if err != nil {
		return "", Source{}, err
	}
	return filepath.Join(dir, Name), Source{}, nil
}

// Load reads the file, rejecting a key it does not recognise.
//
// A configuration that fails to parse stops the command rather than being
// skipped with a warning. It carries the confirmation policy, and a policy that
// quietly does not load is one that fails open - which is the one outcome a
// guard may never have.
//
// It answers with what it learned about the file as well as with the file, so a
// report can say whether there was one and what was wrong with it. The error
// names the path, because whoever fixes it has to find it; the source carries
// the same fact without it, because a report is read by somebody else.
func Load(path string, from Source) (*File, Source, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if from.Named == "" {
				return nil, from, nil
			}
			return nil, from.Ignoring(errors.New("not there")), fmt.Errorf("could not read %s: %w", path, err)
		}
		return nil, from.Ignoring(redact.WithoutPath(err)), fmt.Errorf("could not read %s: %w", path, err)
	}
	from.Present = true
	var f File
	if err := yaml.NewDecoder(bytes.NewReader(b), yaml.Strict()).Decode(&f); err != nil {
		detail := yaml.FormatError(err, false, false)
		return nil, from.Ignoring(errors.New(detail)), fmt.Errorf("%s: %s", path, detail)
	}
	return &f, from, nil
}

// Flags is what the command line said. A nil boolean is a flag left alone.
type Flags struct {
	Config   string
	Profile  string
	Output   string
	LogLevel string
	Confirm  string
	Zone     string
	Quiet    *bool
	FullIDs  *bool
	NoColor  *bool
	NoInput  *bool
	NoLog    *bool
}

// Resolved is the settled answer for one invocation.
type Resolved struct {
	Profile profile.Name
	Output  ui.Format
	// Zone is the IANA zone this invocation works in, or "" when nothing on this
	// machine names one.
	Zone          string
	LogLevel      slog.Level
	Quiet         bool
	FullIDs       bool
	NoColor       bool
	NoInput       bool
	NoLog         bool
	NoUpdateCheck bool
	Confirm       confirm.Policy
	// Source is where these came from, which a report says and nothing else
	// reads.
	Source Source
}

// Defaults is what a run gets when it has been told nothing: no file, no flags,
// no variables, and for the zone whatever the host itself answers.
//
// It is spelled out rather than resolved, because it is what a report falls back
// to when resolving is the thing that failed, and a fallback that can fail is
// not one.
func Defaults() Resolved {
	return Resolved{Output: ui.FormatText, LogLevel: slog.LevelWarn, Zone: hostZone()}
}

// Profile settles which profile an invocation acts as: what was typed, then the
// environment, then the file.
//
// It stands apart from Resolve because a shell completion needs this answer and
// none of the others: which cache to read is the whole of what it asks, and
// having it settle the log level on the way would let a setting it never uses
// refuse it an answer.
func Profile(f *File, flag string) (profile.Name, error) {
	name := ""
	if f != nil {
		name = f.Profile
	}
	p, err := profile.Parse(firstNonEmpty(flag, os.Getenv("PROTON_PROFILE"), name))
	if err != nil {
		return profile.Name{}, errs.Problemf("%v.", err).Hint("profiles are named like `work` or `my-work.2`")
	}
	return p, nil
}

// Resolve settles every setting from the file, the environment and the flags.
//
// The profile is settled first, because which per-profile section applies
// depends on it and nothing about it depends on the section.
func Resolve(f *File, flags Flags) (Resolved, error) {
	var global, scoped Settings
	if f != nil {
		global = f.Settings
	}

	profileName, err := Profile(f, flags.Profile)
	if err != nil {
		return Resolved{}, err
	}
	if f != nil {
		scoped = f.PerProfile[profileName.String()]
	}

	format, err := ui.ParseFormat(firstNonEmpty(flags.Output, scoped.Output, global.Output))
	if err != nil {
		return Resolved{}, err
	}
	level, err := ui.ParseLogLevel(firstNonEmpty(
		flags.LogLevel, os.Getenv("PROTON_LOG_LEVEL"), scoped.LogLevel, global.LogLevel))
	if err != nil {
		return Resolved{}, err
	}
	policy, err := resolveConfirm(global, scoped, flags.Confirm)
	if err != nil {
		return Resolved{}, err
	}
	zone, err := resolveZone(global, scoped, flags.Zone)
	if err != nil {
		return Resolved{}, err
	}

	return Resolved{
		Profile:       profileName,
		Output:        format,
		Zone:          zone,
		LogLevel:      level,
		Quiet:         firstSet(flags.Quiet, nil, scoped.Quiet, global.Quiet),
		FullIDs:       firstSet(flags.FullIDs, nil, scoped.FullIDs, global.FullIDs),
		NoColor:       firstSet(flags.NoColor, present("NO_COLOR"), scoped.NoColor, global.NoColor),
		NoInput:       firstSet(flags.NoInput, present("PROTON_NO_INPUT"), scoped.NoInput, global.NoInput),
		NoLog:         firstSet(flags.NoLog, present(NoLogVar), scoped.NoLog, global.NoLog),
		NoUpdateCheck: firstSet(nil, present("PROTON_NO_UPDATE_CHECK"), scoped.NoUpdateCheck, global.NoUpdateCheck),
		Confirm:       policy,
	}, nil
}

// resolveZone settles the zone this invocation works in.
//
// A zone written for this CLI has to be a real one, so a flag or a file naming
// an unknown zone stops the command rather than being passed over - a silent
// fallback there would anchor an event to a zone nobody asked for. The host's
// own answer sits below the file, so `zone:` overrides the machine.
//
// An empty answer is not a failure here. Whether a zone is required depends on
// what the command does, and the account's own calendar setting is a further
// fallback that needs a client to ask; see app.App.Zone.
func resolveZone(global, scoped Settings, flag string) (string, error) {
	for _, named := range [][2]string{{"--zone", flag}, {"zone", scoped.Zone}, {"zone", global.Zone}} {
		source, value := named[0], named[1]
		if value == "" {
			continue
		}
		if _, err := time.LoadLocation(value); err != nil {
			return "", errs.Problemf("%s: %q is not an IANA time zone.", source, value).
				Hint("zones are named like `Europe/Vienna` or `UTC`")
		}
	}
	return firstNonEmpty(flag, envZone(), scoped.Zone, global.Zone, hostZone()), nil
}

// ConfirmVar carries the one-line form of the policy.
const ConfirmVar = "PROTON_CONFIRM"

// NoLogVar turns off the diagnostic log, following the NO_COLOR convention: set
// to anything at all, even nothing, it means no.
const NoLogVar = "PROTON_NO_LOG"

// LogDir is where the diagnostic log lives, inside Dir.
func LogDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, runlog.DirName), nil
}

// resolveConfirm gathers the four places a policy can be declared.
//
// Each stays a source of its own rather than being folded into one list, so
// that none of them can reach into another's scoping. What they add up to is
// settled by package confirm, which takes the most cautious of their answers -
// so a section, a variable or a flag can add a requirement and none of them can
// take one away.
func resolveConfirm(global, scoped Settings, flag string) (confirm.Policy, error) {
	var policy confirm.Policy
	add := func(source confirm.Source, err error) error {
		if err != nil {
			return err
		}
		if len(source) > 0 {
			policy = append(policy, source)
		}
		return nil
	}
	for _, doc := range []confirm.Document{global.Confirm, scoped.Confirm} {
		if err := add(doc.Source()); err != nil {
			return nil, err
		}
	}
	for _, line := range []string{os.Getenv(ConfirmVar), flag} {
		if err := add(confirm.Parse(line)); err != nil {
			return nil, err
		}
	}
	return policy, nil
}

// present reports an environment variable set to anything at all, even nothing.
//
// NO_COLOR made the convention and PROTON_NO_INPUT follows it: a variable that
// switches a behaviour off should not need a second mental model for its value,
// and `NO_COLOR=` in a CI environment file reads as intent either way.
func present(name string) *bool {
	if _, ok := os.LookupEnv(name); !ok {
		return nil
	}
	yes := true
	return &yes
}

// firstSet takes the nearest source that said anything, and false when none did.
func firstSet(vs ...*bool) bool {
	for _, v := range vs {
		if v != nil {
			return *v
		}
	}
	return false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
