package self

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/runlog"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// IssuesPage is where a report is meant to end up.
const IssuesPage = "https://github.com/roman-16/proton-cli/issues/new"

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
		Use:   "report",
		Short: "Collect what a bug report needs",
		Long: `Collect what a bug report needs: this build, your settings, and a
redacted trace of the run that failed.

The last run that failed, or --all for every run still on disk.
One file per day; the last 16 are kept.

Addresses, IDs and file paths are replaced by stable stand-ins before
anything is written, so the same address reads as the same name
throughout and as nothing at all to anybody else. Nothing here can be
turned back into an address, a password, a subject or a filename.

Reads only what is already on this machine: no account, no network.`,
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			doc, err := collect(c, version, all)
			if err != nil {
				return err
			}
			return deliver(c, doc, dest, force)
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

	Runs []ReportedRun `json:"runs"`
}

// ReportedRun is one past invocation, as the report carries it.
type ReportedRun struct {
	ID      string   `json:"id"`
	Command string   `json:"command,omitempty"`
	Exit    int      `json:"exit"`
	Records []string `json:"records"`
}

func collect(c *kit.Invocation, version string, all bool) (Report, error) {
	a := c.App
	configPath, _, err := config.Path("")
	if err != nil {
		configPath = ""
	}
	r := Report{
		Version:  version,
		Revision: app.Revision(),
		Go:       runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH,
		Install:  selfmanage.Source(),
		Config:   configPath,
		Output:   string(a.UI.Format),
		Confirm:  a.Confirm.Describe(),
		Zone:     a.LocalZone(),
		Profile:  a.Profile.String(),
		Logging:  a.Log() != nil,
	}
	r.Runs = pastRuns(a, all)
	return r, nil
}

// pastRuns is what the report says about what happened.
//
// The run doing the reporting is never one of them: it is a run in which nothing
// went wrong, and including it would bury the one that did.
func pastRuns(a *app.App, all bool) []ReportedRun {
	dir, err := config.LogDir()
	if err != nil {
		return nil
	}
	mine := ""
	if a.Log() != nil {
		mine = a.Log().ID
	}
	if !all {
		latest, ok := runlog.Latest(dir, mine)
		if !ok {
			return nil
		}
		return []ReportedRun{reported(latest)}
	}
	entries, err := runlog.List(dir)
	if err != nil {
		return nil
	}
	out := make([]ReportedRun, 0, len(entries))
	for _, e := range entries {
		if e.ID == mine {
			continue
		}
		out = append(out, reported(e))
	}
	return out
}

func reported(e runlog.Entry) ReportedRun {
	return ReportedRun{ID: e.ID, Command: e.Command, Exit: e.Exit, Records: e.Lines}
}

// deliver writes the report where it was asked for: standard output by default,
// so the person sending it reads every byte before it leaves the machine.
func deliver(c *kit.Invocation, r Report, dest string, force bool) error {
	body := r.Text()
	if dest != "" && dest != "-" {
		path, err := kit.WriteNamed(dest, []byte(body), force)
		if err != nil {
			return err
		}
		c.UI().Notef("Wrote %s. Attach it to an issue: %s", path, IssuesPage)
		return nil
	}
	return kit.Read(c, ui.DocumentSpec{
		BodyOnly: true,
		Parts:    []ui.Part{{Body: body}},
		Object:   r,
	})
}

// Text renders the report as the plain text a person pastes into an issue.
//
// Plain text rather than markdown: the issue form renders it as a preformatted
// block, so a heading written with hashes would be shown as hashes, and the
// aligned columns are what make it readable either way.
func (r Report) Text() string {
	var b strings.Builder

	b.WriteString(kit.Program + " report\n\n")
	b.WriteString("Fill in these three lines, then paste the whole thing into an issue:\n")
	b.WriteString(IssuesPage + "\n\n")
	b.WriteString("  What I ran:\n")
	b.WriteString("  What I expected:\n")
	b.WriteString("  What happened instead:\n")

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

	if !r.Logging {
		b.WriteString("\nLog\n")
		b.WriteString("  Turned off on this machine, so there is no trace of what ran.\n")
		b.WriteString("  Unset " + config.NoLogVar + " and no-log, reproduce the problem, then run this again.\n")
		return b.String()
	}
	if len(r.Runs) == 0 {
		b.WriteString("\nLog\n")
		b.WriteString("  No run has been recorded yet.\n")
		return b.String()
	}
	for _, run := range r.Runs {
		b.WriteString("\n" + heading(run) + "\n")
		for _, line := range run.Records {
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}

func heading(run ReportedRun) string {
	label := "Run " + run.ID
	if run.Command != "" {
		label += ": " + run.Command
	}
	return fmt.Sprintf("%s (exit %d)", label, run.Exit)
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
