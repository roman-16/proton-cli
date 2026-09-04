package self

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/redact"
	"github.com/roman-16/proton-cli/internal/runlog"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// ReportName is the command, named here because the root has to know which one
// it is: a configuration too broken to run on is what a report is for, so this
// is the one command a failure to settle the settings may not stop.
const ReportName = "report"

// IssuesPage is where a report is meant to end up. It opens the bug form rather
// than the chooser in front of it, because a report is what nearly every one of
// these is.
const IssuesPage = "https://github.com/roman-16/proton-cli/issues/new?template=bug.yml"

// head and tail are how much of a long run the pasteable form keeps: what it set
// out to do, and what it was doing when it stopped.
const (
	head = 20
	tail = 80
)

// timestamp is how a past run says when it happened, to the second, because a
// failure is matched against what Proton's service was doing at that moment.
const timestamp = "2006-01-02 15:04:05"

// ReportCmd assembles everything needed to debug a failure.
//
// It answers the question a maintainer has to ask and a user cannot easily
// answer: what actually happened. A bug report saying only what the error said
// is unactionable, and asking somebody to reproduce the failure with a flag set
// only works for failures that reproduce - which the ones worth reporting often
// do not. So every run records what it did, and this reads the last one back.
//
// It reaches nothing: no account, no network, no keys. A command for reporting
// that something is broken must not be able to fail because something is
// broken.
func ReportCmd(version string) *cobra.Command {
	var (
		all   bool
		dest  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   ReportName,
		Short: "Collect what a bug report needs",
		Long: `Collect what a bug report needs: this build, your settings, and a
redacted trace of the run that failed.

The last run that failed, or --all for every run still on disk.
One file per day; the last 16 are kept.

Addresses, IDs and file paths are replaced by stable stand-ins before
anything is written, so the same address reads as the same name
throughout and as nothing at all to anybody else. Nothing here can be
turned back into an address, a password, a subject or a filename.

A long run is shortened to what an issue form takes: its first and last
records, and everything above debug in between. --dest writes the whole
of it to a file instead.

Reads only what is already on this machine: no account, no network.`,
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			return deliver(c, collect(c, version, all), dest, force)
		}),
	}
	f := cmd.Flags()
	kit.All(f, &all)
	f.StringVar(&dest, "dest", "", "Write to this path, or - for stdout")
	f.BoolVar(&force, "force", false, "Overwrite a file that already exists")
	return cmd
}

// Report is the bundle, in the shape a machine format writes it.
type Report struct {
	Version  string `json:"version"`
	Revision string `json:"revision,omitempty"`
	Go       string `json:"go"`
	Platform string `json:"platform"`
	Install  string `json:"install"`

	Config  string `json:"config"`
	Output  string `json:"output"`
	Confirm string `json:"confirm"`
	Zone    string `json:"zone,omitempty"`
	Profile string `json:"profile"`
	Logging bool   `json:"logging"`

	// Kept is how many runs are on disk over how many Days, so that a reader can
	// ask for one this report did not carry, and Unread is what stopped them
	// being counted at all.
	Kept   int           `json:"kept"`
	Days   int           `json:"days"`
	Unread string        `json:"unread,omitempty"`
	Runs   []ReportedRun `json:"runs"`
}

// ReportedRun is one past invocation, as the report carries it.
type ReportedRun struct {
	ID      string    `json:"id"`
	Command string    `json:"command,omitempty"`
	Version string    `json:"version,omitempty"`
	Started time.Time `json:"started"`
	Took    string    `json:"took,omitempty"`
	Exit    int       `json:"exit"`
	Ended   bool      `json:"ended"`
	Records []string  `json:"records"`
}

// Failed reports whether this is a run something went wrong in.
func (r ReportedRun) Failed() bool { return r.Ended && r.Exit != 0 }

func collect(c *kit.Invocation, version string, all bool) Report {
	a := c.App
	r := Report{
		Version:  version,
		Revision: app.Revision(),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Install:  selfmanage.Source(),
		Config:   a.Settings.Describe(),
		Output:   string(a.UI.Format),
		Confirm:  a.Confirm.Describe(),
		Zone:     a.LocalZone(),
		Profile:  a.Profile.String(),
		Logging:  a.Log() != nil,
	}
	r.Runs, r.Kept, r.Days, r.Unread = pastRuns(a, all)
	return r
}

// pastRuns is what the report says about what happened, and how much else there
// is to ask for.
//
// A log this run cannot read is said rather than swallowed: "nothing has been
// recorded yet" and "the recording could not be opened" are different problems
// with different fixes, and only one of them is about the run being reported.
func pastRuns(a *app.App, all bool) (runs []ReportedRun, kept, days int, unread string) {
	dir, err := config.LogDir()
	if err != nil {
		return nil, 0, 0, err.Error()
	}
	entries, err := runlog.List(dir)
	if err != nil {
		return nil, 0, 0, redact.WithoutPath(err).Error()
	}
	mine := ""
	if a.Log() != nil {
		mine = a.Log().ID
	}
	runs, kept, days = chosen(others(entries, mine), all)
	return runs, kept, days, ""
}

// others is every run on the disk but the one doing the reporting.
//
// That one is a run in which nothing went wrong, and carrying it would bury the
// one that did. It goes by ID rather than by position, because "the last one" is
// whichever run happened to be slowest to finish and that is not a thing to
// guess about.
func others(entries []runlog.Entry, mine string) []runlog.Entry {
	kept := make([]runlog.Entry, 0, len(entries))
	for _, e := range entries {
		if e.ID != mine {
			kept = append(kept, e)
		}
	}
	return kept
}

// chosen is which of them the report carries, and how much there is to ask for
// that it did not.
func chosen(entries []runlog.Entry, all bool) (runs []ReportedRun, kept, days int) {
	files := map[string]bool{}
	for _, e := range entries {
		files[e.Path] = true
	}
	if all {
		for _, e := range entries {
			runs = append(runs, reported(e))
		}
	} else if newest, ok := runlog.Newest(entries); ok {
		runs = append(runs, reported(newest))
	}
	return runs, len(entries), len(files)
}

func reported(e runlog.Entry) ReportedRun {
	run := ReportedRun{
		ID:      e.ID,
		Command: e.Command,
		Version: e.Version,
		Started: e.Started,
		Exit:    e.Exit,
		Ended:   e.Ended,
		Records: e.Lines,
	}
	if e.Took > 0 {
		run.Took = e.Took.Round(time.Millisecond).String()
	}
	return run
}

// deliver writes the report where it was asked for: standard output by default,
// so the person sending it reads every byte before it leaves the machine.
//
// A file gets the whole of it and the screen gets what an issue form takes. The
// limit belongs to the form and to nothing else, so it is the only place that
// pays for it.
func deliver(c *kit.Invocation, r Report, dest string, force bool) error {
	if dest != "" && dest != "-" {
		path, err := kit.WriteNamed(dest, []byte(r.Text()), force)
		if err != nil {
			return err
		}
		c.UI().Notef("Wrote %s. Attach it to the bug form: %s", path, IssuesPage)
		return nil
	}
	if err := kit.Read(c, ui.DocumentSpec{
		BodyOnly: true,
		Parts:    []ui.Part{{Body: r.Paste()}},
		Object:   r,
	}); err != nil {
		return err
	}
	if left, whole := r.elided(); left > 0 {
		c.UI().Notef("%d of %d records are left out, so that this fits an issue form.", left, whole)
		c.UI().Notef("%s %s --dest bug.txt writes the whole run to a file you can attach.",
			kit.Program, ReportName)
	}
	c.UI().Notef("Paste this into the bug form: %s", IssuesPage)
	return nil
}

// Text renders the whole report, as a file carries it.
//
// Plain text rather than markdown: the issue form renders it as a preformatted
// block, so a heading written with hashes would be shown as hashes, and the
// aligned columns are what make it readable either way.
func (r Report) Text() string {
	var b strings.Builder

	b.WriteString(kit.Program + " " + ReportName + "\n")

	section(&b, "Build", [][2]string{
		{"Version", r.Version},
		{"Revision", r.Revision},
		{"Go", r.Go},
		{"Platform", r.Platform},
		{"Install", r.Install},
	})
	section(&b, "Settings", [][2]string{
		{"Config", r.Config},
		{"Profile", r.Profile},
		{"Output", r.Output},
		{"Confirm", r.Confirm},
		{"Zone", r.Zone},
	})

	b.WriteString("\nLog\n  " + r.log() + "\n")
	for _, run := range r.Runs {
		b.WriteString("\n" + heading(run) + "\n")
		for _, line := range run.Records {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

// Paste renders it for an issue form, which has a limit a file does not.
func (r Report) Paste() string {
	short := r
	short.Runs = make([]ReportedRun, len(r.Runs))
	for i, run := range r.Runs {
		run.Records, _ = shorten(run.Records)
		short.Runs[i] = run
	}
	return short.Text()
}

// elided is how many records the pasteable form leaves out, of how many there
// are, so that the screen can say so and point at the file that has them all.
func (r Report) elided() (left, whole int) {
	for _, run := range r.Runs {
		_, dropped := shorten(run.Records)
		whole += len(run.Records)
		left += dropped
	}
	return left, whole
}

// shorten is a long run cut down to what is worth pasting: what it set out to
// do, what it was doing when it stopped, and everything in between that was not
// routine.
//
// Nothing above debug is ever dropped. A retry, a request that would not answer,
// a signature that would not verify - those are the middle of a run that a
// reader is looking for, and they are a handful of lines among the hundreds of
// successful requests that make a run long in the first place.
func shorten(records []string) (kept []string, dropped int) {
	if len(records) <= head+tail {
		return records, 0
	}
	kept = slices.Clone(records[:head])
	for _, line := range records[head : len(records)-tail] {
		if routine(line) {
			dropped++
			continue
		}
		kept = append(kept, line)
	}
	if dropped > 0 {
		kept = append(kept, fmt.Sprintf("... %d debug records left out ...", dropped))
	}
	return append(kept, records[len(records)-tail:]...), dropped
}

// routine reports whether a record is one of the many a busy run writes.
func routine(line string) bool {
	var record struct {
		Level string `json:"level"`
	}
	return json.Unmarshal([]byte(line), &record) == nil && record.Level == slog.LevelDebug.String()
}

// log says what there is to read back, and which of it this report carries.
func (r Report) log() string {
	if !r.Logging {
		return "Turned off on this machine, so there is no trace of what ran.\n  Unset " +
			config.NoLogVar + " and no-log, reproduce the problem, then run this again."
	}
	if r.Unread != "" {
		return "Written but not readable back: " + r.Unread + "."
	}
	if r.Kept == 0 {
		return "No run has been recorded yet."
	}
	held := fmt.Sprintf("%s over %s on disk", count(r.Kept, "run"), count(r.Days, "day"))
	if len(r.Runs) == r.Kept {
		return held + "."
	}
	if len(r.Runs) == 1 && r.Runs[0].Failed() {
		return held + "; this is the last one that failed."
	}
	return held + "; nothing failed, so this is the last one."
}

func count(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return fmt.Sprintf("%d %ss", n, thing)
}

// heading names a run the way a reader looks for one: which run, what it was,
// what it ran on, and how it came out.
//
// The build is the run's own and not this one's. Somebody who updates before
// reporting is reporting a run from before they did, and a heading that took the
// version off the machine would say the failure is in a build it never happened
// in.
func heading(run ReportedRun) string {
	parts := []string{"Run " + run.ID, run.Command, run.Version}
	if run.Ended {
		parts = append(parts, fmt.Sprintf("exit %d", run.Exit))
	} else {
		parts = append(parts, "no exit recorded")
	}
	if run.Took != "" {
		parts = append(parts, "after "+run.Took)
	}
	if !run.Started.IsZero() {
		parts = append(parts, run.Started.Format(timestamp))
	}
	return strings.Join(slices.DeleteFunc(parts, func(s string) bool { return s == "" }), "  ")
}

// section writes a labelled block, skipping the facts this machine had no
// answer for rather than printing an empty one.
func section(b *strings.Builder, title string, rows [][2]string) {
	b.WriteString("\n" + title + "\n")
	width := 0
	for _, row := range rows {
		if row[1] != "" && len(row[0]) > width {
			width = len(row[0])
		}
	}
	for _, row := range rows {
		if row[1] == "" {
			continue
		}
		_, _ = fmt.Fprintf(b, "  %-*s  %s\n", width, row[0], row[1])
	}
}
