// Package cli wires the Cobra command tree. Command bodies are prepared by the
// pipeline (auth/unlock/resolve steps); this file owns the root command, global
// flags, exit-code plumbing and the leading-dash-ID workaround.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/roman-16/proton-cli/internal/app"
	accountcmd "github.com/roman-16/proton-cli/internal/cli/account"
	apicmd "github.com/roman-16/proton-cli/internal/cli/api"
	calendarcmd "github.com/roman-16/proton-cli/internal/cli/calendar"
	contactscmd "github.com/roman-16/proton-cli/internal/cli/contacts"
	drivecmd "github.com/roman-16/proton-cli/internal/cli/drive"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailcmd "github.com/roman-16/proton-cli/internal/cli/mail"
	passcmd "github.com/roman-16/proton-cli/internal/cli/pass"
	selfcmd "github.com/roman-16/proton-cli/internal/cli/self"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// version is overridden at release time via -ldflags -X.
var version = "dev"

type globalFlags struct {
	configPath string
	profile    string
	apiURL     string
	output     string
	quiet      bool
	logLevel   string
	confirm    string
	dryRun     bool
	yes        bool
	fullIDs    bool
	noColor    bool
	noInput    bool
	noLog      bool
	verified   string
	zone       string
}

// settings is what the command line said, with a boolean left alone told apart
// from one set to false.
//
// The difference is what makes a preference in the file overridable for one run:
// `--quiet=false` is a thing somebody said, while no --quiet at all leaves the
// file to answer.
func (g *globalFlags) settings(pf *pflag.FlagSet) config.Flags {
	said := func(name string, value bool) *bool {
		if !pf.Changed(name) {
			return nil
		}
		return &value
	}
	return config.Flags{
		Config:   g.configPath,
		Profile:  g.profile,
		Output:   g.output,
		LogLevel: g.logLevel,
		Confirm:  g.confirm,
		Zone:     g.zone,
		Quiet:    said("quiet", g.quiet),
		FullIDs:  said("full-ids", g.fullIDs),
		NoColor:  said("no-color", g.noColor),
		NoInput:  said("no-input", g.noInput),
		NoLog:    said("no-log", g.noLog),
	}
}

// newRoot assembles the whole command tree and returns it.
//
// Building the tree rather than mutating a package variable is what lets the
// conformance test walk a complete, freshly constructed tree and check the rules
// the interface is meant to obey.
func newRoot() *cobra.Command {
	var g globalFlags

	root := &cobra.Command{
		Use:   kit.Program,
		Short: "Unofficial CLI for Proton Mail, Drive, Calendar, Pass and Contacts",
		Long: "Proton, in your terminal.\n\n" +
			"Unofficial, end-to-end encrypted CLI for Proton Mail, Drive, Calendar, Pass and Contacts.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// A shorthand is a second name for a flag, which cuts against the rule that a
	// name means one thing. Each one here earns that by being typed constantly and
	// by being the letter every other tool already uses for the idea, so it is
	// guessed rather than learned. The rest of the global flags have no shorthand:
	// nobody types --no-input twice a day, and a letter chosen for a flag that
	// does not need one is a letter spent.
	//
	// The whole shorthand namespace belongs to this command. No leaf may take a
	// letter, which is what stops -p meaning `--profile` here and `--page` two
	// words later; the conformance test enforces it.
	pf := root.PersistentFlags()
	pf.StringVar(&g.configPath, "config", "",
		"Settings file to read (env: "+config.PathVar+"; default: "+config.Name+" in the config directory)")
	pf.StringVar(&g.confirm, "confirm", "",
		"Which commands stop for a yes: "+confirm.ClassList()+" (env: "+config.ConfirmVar+")")
	pf.StringVarP(&g.profile, "profile", "p", "", "Profile to act as (env: PROTON_PROFILE; default: default)")
	pf.StringVarP(&g.output, "output", "o", "", "Output format: text, json, yaml (default \"text\")")
	pf.BoolVarP(&g.quiet, "quiet", "q", false, "Suppress non-essential stderr output")
	pf.StringVar(&g.logLevel, "log-level", "",
		"Logging verbosity: "+strings.Join(ui.LogLevels, ", ")+" (env: PROTON_LOG_LEVEL)")
	pf.BoolVarP(&g.dryRun, "dry-run", "n", false, "Preview mutations without applying them")
	pf.BoolVarP(&g.yes, "yes", "y", false, "Answer confirmation prompts with yes")
	pf.BoolVar(&g.fullIDs, "full-ids", false, "Show full IDs in interactive output (default: shortened to 8 chars on TTY)")
	pf.BoolVar(&g.noColor, "no-color", false, "Disable colored output (env: NO_COLOR)")
	pf.BoolVar(&g.noInput, "no-input", false, "Never prompt; a missing credential becomes an error (env: PROTON_NO_INPUT)")
	pf.BoolVar(&g.noLog, "no-log", false,
		"Write no diagnostic log for this run (env: "+config.NoLogVar+")")
	pf.StringVar(&g.verified, "verified", "",
		"A human verification already solved, as the refusal printed it (env: PROTON_VERIFIED)")
	pf.StringVar(&g.zone, "zone", "",
		"IANA time zone to work in (env: "+config.ZoneVar+"; default: your system zone)")

	// Pointing the CLI at something other than Proton is a thing to do while
	// developing this tool and never while using it, so it is hidden rather than
	// spending a line on each of the 164 help screens. It stays a flag as well as
	// PROTON_API_URL because a shell that cannot prefix a variable onto one
	// command still has to be able to do it.
	pf.StringVar(&g.apiURL, "api-url", "", "API base URL (env: PROTON_API_URL)")
	if err := pf.MarkHidden("api-url"); err != nil {
		panic(err)
	}

	// Parse each level's flags while walking to the target command, so an
	// unrecognised flag is reported as one.
	//
	// Without this, cobra fails to route and blames the subcommand instead:
	// `proton --bogus account get` answers `Unknown command "get"`, which
	// points a reader at the wrong thing entirely.
	root.TraverseChildren = true

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// A shell asking what may come next is not an invocation of this CLI. It
		// reads one file and answers, so it needs no client, no session and no
		// settings but which profile is being typed about - and building the rest
		// would put a line in the diagnostic log for every press of the tab key,
		// crowding out the runs somebody would report a bug about.
		if completing(cmd) {
			return nil
		}
		// Everything the configuration decides is settled here, before any command
		// body runs and so before anything reaches the network: a file that does not
		// parse, a format that is not one, a policy naming a command that is not
		// there - none of them should first cost a sign-in to discover.
		//
		// Except for the command whose whole job is a machine that is not working.
		// A configuration too broken to run on is exactly what somebody reports, and
		// a report that refuses to run over it leaves them with nothing to send. It
		// runs on the defaults instead and says what was wrong with theirs.
		settings, err := resolveSettings(root, g.settings(pf))
		if err != nil && cmd.Name() != selfcmd.ReportName {
			return err
		}
		a, err := app.New(app.Options{
			Resolved: settings,
			APIURL:   g.apiURL,
			Version:  version,
			DryRun:   g.dryRun,
			Yes:      g.yes,
			Verified: g.verified,
		})
		if err != nil {
			return err
		}
		a.API.SetHVResolver(cliHVResolver(a))

		newCtx := app.WithApp(cmd.Context(), a)
		cmd.SetContext(newCtx)
		root.SetContext(newCtx)
		a.Began(cmd)
		return nil
	}

	// The groups make `--help` read as a map of the product rather than as an
	// alphabetical list, and they decide which page of the reference a command is
	// published on, which is why they are named in kit.
	root.AddGroup(
		&cobra.Group{ID: kit.GroupApps, Title: "Apps:"},
		&cobra.Group{ID: kit.GroupAccount, Title: "Account:"},
		&cobra.Group{ID: kit.GroupSelf, Title: kit.Program + " itself:"},
	)

	add := func(group string, cmds ...*cobra.Command) {
		for _, c := range cmds {
			c.GroupID = group
			root.AddCommand(c)
		}
	}
	add(kit.GroupApps, mailcmd.New(), drivecmd.New(), calendarcmd.New(), contactscmd.New(), passcmd.New())
	add(kit.GroupAccount, accountcmd.New(), apicmd.New())
	add(kit.GroupSelf, selfcmd.ChangelogCmd(), selfcmd.ReportCmd(version), selfcmd.UpdateCmd(version),
		selfcmd.UninstallCmd(), selfcmd.VersionCmd(version), completionCmd(root))

	attachExamples(root)
	installHelp(root)
	kit.CompleteReferences(root)

	return root
}

// completing reports whether this run is a shell asking what may come next.
// Asking without descriptions is an alias of the same command, so both arrive
// under the one name.
func completing(cmd *cobra.Command) bool {
	return cmd.Name() == cobra.ShellCompRequestCmd
}

func Execute() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	os.Args = preprocessArgs(os.Args)

	// Probed on a tree of its own, because Find and ParseFlags leave state
	// behind and the tree that answers the command has to start clean.
	if err := unknownSubcommand(newRoot(), os.Args[1:]); err != nil {
		ui.WriteError(os.Stderr, err, ui.StyleFor(os.Stderr), false)
		os.Exit(exitCode(err))
	}

	root := newRoot()
	// A crash is the failure a report is most needed for and the one the person
	// who hit it can say least about, so it is caught here: the stack goes to the
	// run's log, where a report will find it, and the screen gets a sentence
	// instead of forty lines of goroutine.
	defer func() {
		if value := recover(); value != nil {
			os.Exit(crashed(os.Stderr, root, value, debug.Stack()))
		}
	}()

	cmd, err := root.ExecuteContextC(ctx)
	if err == nil {
		finish(root, cmd, 0, nil)
		return
	}
	// A signal is the user changing their mind. A deadline is the request running
	// out of time, which is a network failure and reports as one - saying
	// "Cancelled." there would blame the user for a stalled connection.
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		os.Exit(finish(root, cmd, 130, err))
	}
	// A human verification that reaches here was never answered, and what to do
	// about it depends on why. It becomes an ordinary refusal so that it is
	// written by the one thing that writes refusals.
	var hvErr *proton.HumanVerificationError
	if errors.As(err, &hvErr) {
		err = hvFinalError(hvErr, app.FromOrNil(root.Context()))
	}
	ui.WriteError(os.Stderr, rewrapFlagError(err, os.Args), errorStyle(root), shortIDs(root))
	invite(os.Stderr, root, err)
	os.Exit(finish(root, cmd, exitCode(err), err))
}

// finish records how the run came out and returns its exit code.
//
// It is called on the way out rather than deferred, because os.Exit runs no
// deferred function and the whole point of the record is that it survives the
// run that needed it.
func finish(root *cobra.Command, cmd *cobra.Command, code int, err error) int {
	announce(cmd)
	if a := app.FromOrNil(root.Context()); a != nil {
		a.Ended(code, err)
	}
	return code
}

// crashed reports a panic as the bug it is.
//
// The stack is not printed. It says nothing to the person who hit it, and the
// one reader it does help is reached by `report` - which is the only thing this
// screen has to say.
func crashed(w io.Writer, root *cobra.Command, value any, stack []byte) int {
	a := app.FromOrNil(root.Context())
	if a != nil {
		a.Crashed(value, stack)
	}
	ui.WriteError(w, errs.Problemf("%s crashed. This is a bug.", kit.Program).
		Hint(kit.Program+" report").Exit(errs.ExitBug), errorStyle(root), shortIDs(root))
	if a != nil {
		a.Ended(errs.ExitBug, fmt.Errorf("panic: %v", value))
	}
	return errs.ExitBug
}

// invite asks for a report, for the failures that are worth one.
//
// Only for those: a line saying "this might be a bug" under every mistyped flag
// is a line nobody reads by the second week, and the whole value of this one is
// that it appears when something really did go wrong here rather than there.
func invite(w io.Writer, root *cobra.Command, err error) {
	if !ourFault(err) {
		return
	}
	_, _ = fmt.Fprintln(w, errorStyle(root).Paint(ui.Muted,
		kit.Program+" report  (this looks like a bug in "+kit.Program+", not something you did)"))
}

// announce mentions a new release below whatever the command produced.
//
// Below, because it is the least important thing on the screen; and after a
// failure as much as after a success, because running a version from before the
// fix is one of the reasons a command fails.
//
// The two commands that manage the install are left out - they would be saying
// it twice - and so is the completion script, which a shell reads at startup
// where a remark on stderr is noise in a file nobody is watching.
func announce(cmd *cobra.Command) {
	switch cmd.Name() {
	case "update", "uninstall", "completion":
		return
	}
	if a := app.FromOrNil(cmd.Context()); a != nil {
		selfcmd.Notice(cmd.Context(), a.UI, version, a.NoUpdateCheck)
	}
}

// unknownSubcommand reports a group addressed with a word that names none of its
// subcommands, so that a typo fails instead of printing help.
//
// Cobra makes that check only at the root: its default argument validator
// returns early for any command that has a parent, and ValidateArgs is skipped
// entirely for a command that is not Runnable, which every group is. Left to
// cobra, `proton mail mesages list` writes help to stdout and exits 0.
//
// Find's error is the root-level half of the same complaint, phrased by cobra;
// discarding it lets one wording answer for the whole tree.
func unknownSubcommand(root *cobra.Command, args []string) error {
	// A shell asking what may come next is not a person mistyping a command, and
	// cobra answers it itself - out of a tree it completes on the way into
	// Execute, which a tree built to be probed has not been through. Judging that
	// request against this tree finds a command nobody has added yet and refuses
	// the completion every installed shell script asks for.
	if len(args) > 0 && (args[0] == cobra.ShellCompRequestCmd || args[0] == cobra.ShellCompNoDescRequestCmd) {
		return nil
	}
	cmd, rest, _ := root.Find(args)
	if !cmd.HasSubCommands() {
		return nil
	}
	// A malformed flag is cobra's to report, in its own words.
	if cmd.ParseFlags(rest) != nil {
		return nil
	}
	extra := cmd.Flags().Args()
	if len(extra) == 0 {
		return nil
	}
	// Cobra seeds this inside ExecuteC, which a probe never reaches, and a
	// distance of zero suggests nothing.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	problem := errs.Problemf("There is no %s command called %q.", cmd.CommandPath(), extra[0])
	for _, s := range cmd.SuggestionsFor(extra[0]) {
		problem = problem.Hint(cmd.CommandPath() + " " + s)
	}
	return problem.Hint(cmd.CommandPath() + " --help")
}

// errorStyle is how the final error is painted. The app owns one, but a failure
// during flag parsing happens before there is an app, so fall back to asking the
// stream directly.
func errorStyle(root *cobra.Command) ui.Style {
	if a := app.FromOrNil(root.Context()); a != nil {
		return a.UI.ErrStyle()
	}
	return ui.StyleFor(os.Stderr)
}

// shortIDs answers the way every other screen answers it, from the app the
// command ran under. A failure with no app behind it - a flag that did not parse
// - had no listing either, so it names things whole.
func shortIDs(root *cobra.Command) bool {
	if a := app.FromOrNil(root.Context()); a != nil {
		return a.UI.ShortIDs()
	}
	return false
}

// hvFinalError is the refusal a human verification becomes when it reaches the
// top level unanswered.
//
// There are three ways to get here and they want different words. A verification
// that was presented and refused is over: the challenge is spent and a fresh one
// is what the next run will be given. A challenge nobody could be asked about
// carries the two halves an unattended caller needs - the page, and the token to
// repeat the command with - because the proof outlives the run that asked for it
// even though the challenge does not. And a challenge offering no CAPTCHA cannot
// be finished by any client, so it says so rather than pointing at a page.
func hvFinalError(hv *proton.HumanVerificationError, a *app.App) error {
	if hv.Refused {
		return errs.Problemf("Proton did not accept the verification.").
			Hint("solve the CAPTCHA fully, then run the command again").Exit(2)
	}
	page, err := verificationPage(hv, a)
	if err != nil {
		return errs.Problemf("Proton wants to confirm you are human by %s, and that cannot be done from a terminal.",
			strings.Join(hv.Methods, " or ")).
			Hint("sign in to your account in a browser once, then try again").Exit(2)
	}
	return errs.Problemf("Proton wants to confirm you are human, and this run cannot wait while you do.").
		Hint("solve "+page, "then run the same command again with --verified "+hv.Token).Exit(2)
}

// verificationPage is the address to send somebody to, from whichever client is
// at hand. A failure before the app exists has no API to ask, and the address
// depends on the host that raised the challenge.
func verificationPage(hv *proton.HumanVerificationError, a *app.App) (string, error) {
	if a == nil {
		return "", fmt.Errorf("no client to build a verification address with")
	}
	return a.API.VerifyURL(hv)
}

// Root returns the assembled command tree, for the documentation generator and
// for anything else that needs to walk it.
func Root() *cobra.Command { return newRoot() }
