package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/spf13/cobra"
)

func newAPICmd() *cobra.Command {
	var query []string
	var body string
	cmd := &cobra.Command{
		Use:   "api METHOD PATH",
		Short: "Make an authenticated raw API request",
		Long: `Send a raw authenticated request to the Proton API.

Examples:
  proton-cli api GET /calendar/v1
  proton-cli api GET /drive/volumes
  proton-cli api POST /calendar/v1 --body '{"Name":"Work","Color":"#7272a7","Display":1,"AddressID":"..."}'
  proton-cli api GET /mail/v4/messages --query Page=0 --query PageSize=10`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			a := app.From(cmd.Context())
			if err := a.Authenticate(cmd.Context()); err != nil {
				return err
			}
			method := args[0]
			path := args[1]

			q := make(map[string][]string)
			for _, kv := range query {
				parts := strings.SplitN(kv, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("invalid --query %q (expected key=value)", kv)
				}
				q[parts[0]] = append(q[parts[0]], parts[1])
			}
			if body != "" && !json.Valid([]byte(body)) {
				return fmt.Errorf("invalid JSON --body")
			}
			req := api.Request{Method: method, Path: path, Query: q, Body: body}

			resp, err := a.API.Do(cmd.Context(), req)
			if e := errHumanVerify(err); e != nil {
				if err = handleHumanVerification(cmd, a, &req, e); err == nil {
					resp, err = a.API.Do(cmd.Context(), req)
				}
			}
			if err != nil {
				// Still emit the server body if we have one, matching the old `api` behaviour.
				var apiErr *api.APIError
				if errors.As(err, &apiErr) {
					_ = a.R.JSON(apiErr.RawBody)
					return app.Exit(app.ExitCodeFor(err), err)
				}
				return err
			}
			return a.R.JSON(resp.Body)
		},
	}
	cmd.Flags().StringArrayVar(&query, "query", nil, "Query parameter (key=value, repeatable)")
	cmd.Flags().StringVar(&body, "body", "", "JSON request body")
	return cmd
}

func errHumanVerify(err error) *api.HumanVerificationError {
	var hv *api.HumanVerificationError
	if errors.As(err, &hv) {
		return hv
	}
	return nil
}

func handleHumanVerification(cmd *cobra.Command, a *app.App, req *api.Request, hv *api.HumanVerificationError) error {
	fmt.Fprintf(os.Stderr, "\nHuman verification required.\nOpening: %s\nMethods: %s\n", hv.WebURL, strings.Join(hv.Methods, ", "))
	openBrowser(hv.WebURL)
	fmt.Fprintf(os.Stderr, "\nComplete verification in your browser, then press Enter to retry...")
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
	req.HVToken = hv.Token
	if len(hv.Methods) > 0 {
		req.HVType = hv.Methods[0]
	} else {
		req.HVType = "ownership-email"
	}
	return nil
}

func openBrowser(url string) {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("open", url)
	case "windows":
		c = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		c = exec.Command("xdg-open", url)
	}
	_ = c.Start()
}
