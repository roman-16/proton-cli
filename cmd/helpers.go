package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/roman-16/proton-cli/internal/client"
	"github.com/roman-16/proton-cli/internal/session"
)

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

func handleHumanVerification(ctx context.Context, c *client.Client, hvErr *client.HumanVerificationError, method, path string, query map[string]string, body string) ([]byte, int, error) {
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

	return c.Do(ctx, method, path, query, body, hvErr.Token, tokenType)
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
