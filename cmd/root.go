// Package cmd wires the Cobra CLI. Command implementations live in the
// per-service subpackages (cmd/mail, cmd/drive, cmd/calendar, cmd/contacts,
// cmd/pass); this package only owns the root command, global flags, and the
// exit-code plumbing.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/roman-16/proton-cli/cmd/calendar"
	"github.com/roman-16/proton-cli/cmd/contacts"
	"github.com/roman-16/proton-cli/cmd/drive"
	"github.com/roman-16/proton-cli/cmd/mail"
	"github.com/roman-16/proton-cli/cmd/pass"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/render"
	"github.com/spf13/cobra"
)

var version = "dev"

// globalFlags holds persistent flag values; parsed in PersistentPreRunE.
type globalFlags struct {
	profile    string
	user       string
	password   string
	totp       string
	apiURL     string
	appVersion string
	output     string
	verbose    bool
	quiet      bool
	logLevel   string
	dryRun     bool
}

var gFlags globalFlags

var rootCmd = &cobra.Command{
	Use:           "proton-cli",
	Short:         "CLI for the Proton API",
	Long:          "An unofficial command-line tool for Proton (Mail, Drive, Calendar, Pass, Contacts). Handles SRP authentication and end-to-end encryption automatically.",
	Version:       version,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&gFlags.profile, "profile", "", "config profile to use (default: default)")
	rootCmd.PersistentFlags().StringVar(&gFlags.user, "user", "", "Proton account email (env: PROTON_USER)")
	rootCmd.PersistentFlags().StringVar(&gFlags.password, "password", "", "Account password (env: PROTON_PASSWORD)")
	rootCmd.PersistentFlags().StringVar(&gFlags.totp, "totp", "", "TOTP 2FA code (env: PROTON_TOTP)")
	rootCmd.PersistentFlags().StringVar(&gFlags.apiURL, "api-url", "", "API base URL (env: PROTON_API_URL)")
	rootCmd.PersistentFlags().StringVar(&gFlags.appVersion, "app-version", "", "App version header (env: PROTON_APP_VERSION)")
	rootCmd.PersistentFlags().StringVar(&gFlags.output, "output", "text", "Output format: text, json, yaml")
	rootCmd.PersistentFlags().BoolVar(&gFlags.verbose, "verbose", false, "Enable debug logging")
	rootCmd.PersistentFlags().BoolVar(&gFlags.quiet, "quiet", false, "Suppress non-essential stderr output")
	rootCmd.PersistentFlags().StringVar(&gFlags.logLevel, "log-level", "", "Log level: debug, info, warn, error")
	rootCmd.PersistentFlags().BoolVar(&gFlags.dryRun, "dry-run", false, "Preview mutations without applying them")

	rootCmd.CompletionOptions.DisableDefaultCmd = true

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		a, err := app.New(app.Options{
			Profile:    gFlags.profile,
			User:       gFlags.user,
			Password:   gFlags.password,
			TOTP:       gFlags.totp,
			APIURL:     gFlags.apiURL,
			AppVersion: gFlags.appVersion,
			Output:     parseFormat(gFlags.output),
			LogLevel:   parseLevel(gFlags.logLevel, gFlags.verbose),
			Quiet:      gFlags.quiet,
			DryRun:     gFlags.dryRun,
		})
		if err != nil {
			return err
		}
		cmd.SetContext(app.WithApp(cmd.Context(), a))
		return nil
	}
}

// Execute runs the root command and exits with the appropriate code.
func Execute() {
	// Cancel context on Ctrl+C / SIGTERM.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	registerSubcommands()

	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return
	}
	// If the root context was cancelled (user hit Ctrl+C), show a clean
	// message instead of whatever error chain bubbled up from the layer
	// that noticed the cancellation first (net/http, etc.).
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		fmt.Fprintln(os.Stderr, "\nCancelled.")
		os.Exit(130)
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(app.ExitCodeFor(err))
}

func registerSubcommands() {
	rootCmd.AddCommand(newAPICmd())
	rootCmd.AddCommand(newSettingsCmd())
	rootCmd.AddCommand(mail.NewCmd())
	rootCmd.AddCommand(drive.NewCmd())
	rootCmd.AddCommand(calendar.NewCmd())
	rootCmd.AddCommand(contacts.NewCmd())
	rootCmd.AddCommand(pass.NewCmd())
}

func parseFormat(s string) render.Format {
	f, err := render.ParseFormat(s)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	return f
}

func parseLevel(s string, verbose bool) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	if verbose {
		return slog.LevelDebug
	}
	return slog.LevelWarn
}
