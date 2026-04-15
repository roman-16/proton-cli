package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/session"
	"github.com/spf13/cobra"
)

var (
	flagQuery []string
	flagBody  string
)

var apiCmd = &cobra.Command{
	Use:   "api METHOD PATH",
	Short: "Make an authenticated API request",
	Long: `Make an authenticated request to the Proton API.

Examples:
  proton-cli api GET /calendar/v1
  proton-cli api GET /drive/volumes
  proton-cli api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
  proton-cli api GET /calendar/v1/CALENDAR_ID/events --query Start=1700000000 --query End=1700100000 --query Timezone=Europe/Paris --query Type=0
  proton-cli api DELETE /calendar/v1/CALENDAR_ID`,
	Args: cobra.ExactArgs(2),
	RunE: runAPI,
}

func init() {
	apiCmd.Flags().StringArrayVar(&flagQuery, "query", nil, "Query parameters (key=value, repeatable)")
	apiCmd.Flags().StringVar(&flagBody, "body", "", "JSON request body")
	rootCmd.AddCommand(apiCmd)
}

func runAPI(cmd *cobra.Command, args []string) error {
	method := args[0]
	path := args[1]

	apiURL := getFlag(flagAPIURL, "PROTON_API_URL")
	appVersion := getFlag(flagAppVersion, "PROTON_APP_VERSION")

	// Parse query params.
	query := make(map[string]string)
	for _, q := range flagQuery {
		parts := strings.SplitN(q, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid query parameter %q (expected key=value)", q)
		}
		query[parts[0]] = parts[1]
	}

	// Validate body is valid JSON if provided.
	if flagBody != "" {
		if !json.Valid([]byte(flagBody)) {
			return fmt.Errorf("invalid JSON body")
		}
	}

	ctx := context.Background()

	c := client.New(client.Options{
		BaseURL:    apiURL,
		AppVersion: appVersion,
	})

	if err := loadOrLogin(ctx, c); err != nil {
		return err
	}

	body, statusCode, err := c.Do(ctx, method, path, query, flagBody, "", "")

	if errors.Is(err, client.ErrUnauthorized) {
		fmt.Fprintf(os.Stderr, "Session expired, re-authenticating...\n")
		_ = session.Clear()
		if err := doLogin(ctx, c); err != nil {
			return err
		}
		body, statusCode, err = c.Do(ctx, method, path, query, flagBody, "", "")
	}

	var hvErr *client.HumanVerificationError
	if errors.As(err, &hvErr) {
		body, statusCode, err = handleHumanVerification(ctx, c, hvErr, method, path, query, flagBody)
	}

	if err != nil {
		return err
	}

	printJSON(body)

	if statusCode >= 400 {
		os.Exit(1)
	}

	return nil
}
