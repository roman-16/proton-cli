package app

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/redact"
	"github.com/roman-16/proton-cli/internal/runlog"
	"github.com/roman-16/proton-cli/internal/selfmanage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// openLog starts this run's diagnostic file, unless something said not to.
//
// It never fails outwards. A home directory that cannot be written, a disk that
// is full, a directory somebody made read-only: none of those is a reason to
// refuse to send mail, so the run carries on with nowhere to record what it did.
// The salt is read either way, because the handles it keys are also what makes
// the commentary stream safe to paste.
func openLog(noLog bool) (*runlog.Run, []byte) {
	dir, err := config.LogDir()
	if err != nil {
		return nil, nil
	}
	if noLog {
		return nil, redact.Salt(dir)
	}
	run, err := runlog.Open(dir)
	if err != nil {
		return nil, redact.Salt(dir)
	}
	// Pruned once this run's own file exists, so that what is kept is a count of
	// the runs on the disk rather than that many plus whichever one is happening.
	// It is the newest, so it is never what gets removed.
	runlog.Prune(dir)
	return run, redact.Salt(dir)
}

// Log is this run's file, or nil when nothing is being recorded. The report
// command asks, so that it can say so rather than printing an empty section.
func (a *App) Log() *runlog.Run { return a.run }

// Began records what this run is, as its first line.
//
// The command path and the names of the flags that were set, and nothing else: a
// flag's value is a subject, a recipient, a path or a search term, and the names
// alone already answer the question a reader has, which is which of two ways of
// invoking the thing was taken.
func (a *App) Began(cmd *cobra.Command) {
	if a.run == nil {
		return
	}
	a.started = time.Now()
	a.UI.Trace.Info("run",
		"run", a.run.ID,
		"command", cmd.CommandPath(),
		"flags", strings.Join(flagsSet(cmd), " "),
		"version", a.version,
		"revision", Revision(),
		"go", runtime.Version(),
		"platform", runtime.GOOS+"/"+runtime.GOARCH,
		"install", selfmanage.Source(),
		"profile", a.Profile.String(),
		"output", string(a.UI.Format),
		"tty", a.UI.IsTTY(),
		"dry_run", a.DryRun,
		"zone", a.zone.name,
	)
}

// Ended records how this run came out, as its last line, and closes the file.
//
// It is called on the way to os.Exit rather than deferred, because os.Exit runs
// no deferred function and the whole value of the record is that it is there
// when something went wrong.
// Every attribute is spelled out, including the ones with nothing to say, so
// that the names in the record are names something can check.
func (a *App) Ended(code int, err error) {
	if a.run == nil {
		return
	}
	var elapsed int64
	if !a.started.IsZero() {
		elapsed = time.Since(a.started).Milliseconds()
	}
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	if code == 0 {
		a.UI.Trace.Info("run finished",
			"run", a.run.ID, "exit", code, "duration", elapsed, "error", detail)
	} else {
		a.UI.Trace.Error("run failed",
			"run", a.run.ID, "exit", code, "duration", elapsed, "error", detail)
	}
	_ = a.run.Close()
}

// Crashed records a panic, with the stack, before anything else happens to the
// process. A crash is the failure a report is most needed for and the one the
// person who hit it can say least about.
func (a *App) Crashed(value any, stack []byte) {
	if a.run == nil {
		return
	}
	a.UI.Trace.Error("panic",
		"run", a.run.ID,
		"panic", fmt.Sprint(value),
		"stack", string(stack),
	)
}

// flagsSet is the names of the flags this run set, in the order pflag keeps
// them, which is alphabetical.
//
// Whether a flag was given is read off the flag itself rather than off the set
// that parsed it. A global is parsed by the root on the way down, and every
// derived set - inherited, local - is a fresh set of copies that never saw it
// happen; the flag it copied is the same flag, and it remembers.
func flagsSet(cmd *cobra.Command) []string {
	seen := map[string]bool{}
	var names []string
	note := func(f *pflag.Flag) {
		if !f.Changed || seen[f.Name] {
			return
		}
		seen[f.Name] = true
		names = append(names, "--"+f.Name)
	}
	cmd.Flags().VisitAll(note)
	cmd.Root().PersistentFlags().VisitAll(note)
	sort.Strings(names)
	return names
}

// Revision is the commit the binary was built from, when the toolchain stamped
// one in.
func Revision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			if len(s.Value) > 12 {
				return s.Value[:12]
			}
			return s.Value
		}
	}
	return ""
}
