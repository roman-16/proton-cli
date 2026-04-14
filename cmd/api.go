package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

	// Try to load saved session.
	if err := loadOrLogin(ctx, c); err != nil {
		return err
	}

	// Make the API request.
	body, statusCode, err := c.Do(ctx, method, path, query, flagBody, "", "")

	// If unauthorized and refresh failed, re-login and retry.
	if errors.Is(err, client.ErrUnauthorized) {
		fmt.Fprintf(os.Stderr, "Session expired, re-authenticating...\n")
		session.Clear()
		if err := doLogin(ctx, c); err != nil {
			return err
		}
		body, statusCode, err = c.Do(ctx, method, path, query, flagBody, "", "")
	}

	// Handle human verification challenge.
	var hvErr *client.HumanVerificationError
	if errors.As(err, &hvErr) {
		body, statusCode, err = handleHumanVerification(ctx, c, hvErr, method, path, query)
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

// loadOrLogin tries to restore a saved session, falls back to full login.
func loadOrLogin(ctx context.Context, c *client.Client) error {
	sess, err := session.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load session: %v\n", err)
	}

	if sess != nil {
		c.SetTokens(sess.UID, sess.AccessToken, sess.RefreshToken, sess.SaltedKeyPass)
		return nil
	}

	return doLogin(ctx, c)
}

// doLogin performs the full auth flow and saves the session.
func doLogin(ctx context.Context, c *client.Client) error {
	user := getFlag(flagUser, "PROTON_USER")
	password := getFlag(flagPassword, "PROTON_PASSWORD")
	totp := getFlag(flagTOTP, "PROTON_TOTP")

	if user == "" {
		return fmt.Errorf("user is required (set --user or PROTON_USER)")
	}
	if password == "" {
		return fmt.Errorf("password is required (set --password or PROTON_PASSWORD)")
	}

	fmt.Fprintf(os.Stderr, "Authenticating as %s...\n", user)
	if err := c.Login(ctx, user, []byte(password), totp); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Authenticated.\n")

	return session.Save(c.Session())
}

// getAuthenticatedClient creates a client and loads/creates a session.
func getAuthenticatedClient(ctx context.Context) (*client.Client, error) {
	apiURL := getFlag(flagAPIURL, "PROTON_API_URL")
	appVersion := getFlag(flagAppVersion, "PROTON_APP_VERSION")

	c := client.New(client.Options{
		BaseURL:    apiURL,
		AppVersion: appVersion,
	})

	if err := loadOrLogin(ctx, c); err != nil {
		return nil, err
	}

	return c, nil
}

func handleHumanVerification(ctx context.Context, c *client.Client, hvErr *client.HumanVerificationError, method, path string, query map[string]string) ([]byte, int, error) {
	fmt.Fprintf(os.Stderr, "\nHuman verification required.\n")
	fmt.Fprintf(os.Stderr, "Opening: %s\n", hvErr.WebURL)
	fmt.Fprintf(os.Stderr, "Methods: %s\n", strings.Join(hvErr.Methods, ", "))

	openBrowser(hvErr.WebURL)

	fmt.Fprintf(os.Stderr, "\nComplete verification in your browser, then press Enter to retry...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')

	tokenType := "ownership-email"
	if len(hvErr.Methods) > 0 {
		tokenType = hvErr.Methods[0]
	}

	return c.Do(ctx, method, path, query, flagBody, hvErr.Token, tokenType)
}

func printJSON(body []byte) {
	var prettyJSON json.RawMessage
	if json.Unmarshal(body, &prettyJSON) == nil {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.Encode(prettyJSON)
	} else {
		os.Stdout.Write(body)
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
