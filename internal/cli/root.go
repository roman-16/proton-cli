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
	"slices"
	"strings"
	"syscall"

	"github.com/roman-16/proton-cli/internal/app"
	accountcmd "github.com/roman-16/proton-cli/internal/cli/account"
	apicmd "github.com/roman-16/proton-cli/internal/cli/api"
	calendarcmd "github.com/roman-16/proton-cli/internal/cli/calendar"
	contactscmd "github.com/roman-16/proton-cli/internal/cli/contacts"
	drivecmd "github.com/roman-16/proton-cli/internal/cli/drive"
	indexcmd "github.com/roman-16/proton-cli/internal/cli/index"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailcmd "github.com/roman-16/proton-cli/internal/cli/mail"
	meetcmd "github.com/roman-16/proton-cli/internal/cli/meet"
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
	forceColor bool
	fullIDs    bool
	noColor    bool
	noInput    bool
	noLog      bool
	verified   string
	zone       string
}

func (g *globalFlags) settings(pf *pflag.FlagSet) config.Flags {
	said := func(name string, value bool) *bool {
		if !pf.Changed(name) {
			return nil
		}
		return &value
	}
	return config.Flags{
		Config:     g.configPath,
		Profile:    g.profile,
		Output:     g.output,
		LogLevel:   g.logLevel,
		Confirm:    g.confirm,
		Zone:       g.zone,
		Quiet:      said("quiet", g.quiet),
		FullIDs:    said("full-ids", g.fullIDs),
		ForceColor: said("force-color", g.forceColor),
		NoColor:    said("no-color", g.noColor),
		NoInput:    said("no-input", g.noInput),
		NoLog:      said("no-log", g.noLog),
	}
}

func newRoot() *cobra.Command {
	var g globalFlags

	root := &cobra.Command{
		Use:   kit.Program,
		Short: "Unofficial CLI for Proton Mail, Drive, Calendar, Pass, Meet and Contacts",
		Long: "Proton, in your terminal.\n\n" +
			"Unofficial, end-to-end encrypted CLI for Proton Mail, Drive, Calendar, Pass, Meet and Contacts.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

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
	pf.BoolVar(&g.forceColor, "force-color", false, "Force colored output, even when piped (env: FORCE_COLOR)")
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
		if completing(cmd) {
			return nil
		}
		// A help screen is in this binary already. Asked for with --help it is
		// written before any of this runs, because cobra answers the flag first, so
		// the same screen asked for by name must not additionally require a
		// configuration that parses and a profile that resolves.
		if cmd.Name() == helpName {
			return nil
		}
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
		return kit.Judge(cmd)
	}

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
	add(kit.GroupApps, mailcmd.New(), drivecmd.New(), calendarcmd.New(), contactscmd.New(), meetcmd.New(), passcmd.New())
	add(kit.GroupAccount, accountcmd.New(), apicmd.New(), indexcmd.New())
	add(kit.GroupSelf, selfcmd.ChangelogCmd(), selfcmd.ReportCmd(version), selfcmd.UpdateCmd(version),
		selfcmd.UninstallCmd(), selfcmd.VersionCmd(version), completionCmd(root), skillCmd(root, version))

	attachExamples(root)
	installHelp(root)
	kit.InstallArguments(root)

	return root
}

func completing(cmd *cobra.Command) bool {
	return cmd.Name() == cobra.ShellCompRequestCmd
}

func Execute() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	context.AfterFunc(ctx, func() {
		impatient := make(chan os.Signal, 1)
		signal.Notify(impatient, os.Interrupt, syscall.SIGTERM)
		<-impatient
		os.Exit(exitCancelled)
	})

	os.Args = preprocessArgs(os.Args)

	if err := unknownSubcommand(newRoot(), os.Args[1:]); err != nil {
		ui.WriteError(os.Stderr, err, ui.StyleFor(os.Stderr, config.EnvColor()), false)
		os.Exit(exitCode(err))
	}

	root := newRoot()
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
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		os.Exit(finish(root, cmd, exitCancelled, err))
	}
	var hvErr *proton.HumanVerificationError
	if errors.As(err, &hvErr) {
		err = hvFinalError(hvErr, app.FromOrNil(root.Context()))
	}
	ui.WriteError(os.Stderr, err, errorStyle(root), shortIDs(root))
	invite(os.Stderr, root, err)
	os.Exit(finish(root, cmd, exitCode(err), err))
}

func finish(root *cobra.Command, cmd *cobra.Command, code int, err error) int {
	announce(cmd)
	if a := app.FromOrNil(root.Context()); a != nil {
		a.Ended(code, err)
	}
	return code
}

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

func invite(w io.Writer, root *cobra.Command, err error) {
	if !ourFault(err) {
		return
	}
	_, _ = fmt.Fprintln(w, errorStyle(root).Paint(ui.Muted,
		kit.Program+" report  (this looks like a bug in "+kit.Program+", not something you did)"))
}

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
	// Cobra defines these two inside Execute, which a probe never reaches. Left
	// undefined they are unknown flags, and an unknown flag stops this check -
	// so `proton drive files download --help` would answer with the help for
	// drive instead of saying that there is no `files`.
	cmd.InitDefaultHelpFlag()
	cmd.InitDefaultVersionFlag()
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

func errorStyle(root *cobra.Command) ui.Style {
	if a := app.FromOrNil(root.Context()); a != nil {
		return a.UI.ErrStyle()
	}
	return ui.StyleFor(os.Stderr, config.EnvColor())
}

func shortIDs(root *cobra.Command) bool {
	if a := app.FromOrNil(root.Context()); a != nil {
		return a.UI.ShortIDs()
	}
	return false
}

func hvFinalError(hv *proton.HumanVerificationError, a *app.App) error {
	if hv.Refused {
		return errs.Problemf("Proton did not accept the verification.").
			Hint(retryVerification(hv.Methods)).Exit(2)
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

func retryVerification(methods []string) string {
	if len(methods) == 0 || slices.Contains(methods, proton.MethodCaptcha) {
		return "solve the CAPTCHA fully, then run the command again"
	}
	return "check the code Proton sent, then run the command again"
}

func verificationPage(hv *proton.HumanVerificationError, a *app.App) (string, error) {
	if a == nil {
		return "", fmt.Errorf("no client to build a verification address with")
	}
	return a.API.VerifyURL(hv)
}

func Root() *cobra.Command { return newRoot() }
