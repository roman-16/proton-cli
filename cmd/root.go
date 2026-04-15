package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagUser       string
	flagPassword   string
	flagTOTP       string
	flagAPIURL     string
	flagAppVersion string
	flagJSON       bool
)

var rootCmd = &cobra.Command{
	Use:   "proton-cli",
	Short: "CLI for the Proton API",
	Long:  "A command-line tool for interacting with the Proton API (Drive, Calendar, Mail, Contacts). Handles SRP authentication and end-to-end encryption automatically.",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&flagUser, "user", "", "Proton account email (env: PROTON_USER)")
	rootCmd.PersistentFlags().StringVar(&flagPassword, "password", "", "Account password (env: PROTON_PASSWORD)")
	rootCmd.PersistentFlags().StringVar(&flagTOTP, "totp", "", "TOTP 2FA code (env: PROTON_TOTP)")
	rootCmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "API base URL (env: PROTON_API_URL, default: https://mail.proton.me/api)")
	rootCmd.PersistentFlags().StringVar(&flagAppVersion, "app-version", "", "App version header (env: PROTON_APP_VERSION)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output raw JSON instead of human-readable format")
	rootCmd.CompletionOptions.DisableDefaultCmd = true
}

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// getFlag returns the flag value, falling back to the environment variable.
func getFlag(flag, envKey string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv(envKey)
}
