// Package api is the escape hatch: a raw authenticated request to any Proton
// endpoint.
//
// It is the one command whose output contract is Proton's own rather than this
// CLI's, because passing the response through unchanged is the entire point.
// Everything else speaks snake_case; this speaks whatever the API said.
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	var query []string
	var body string
	c := &cobra.Command{
		Use:   "api METHOD ENDPOINT",
		Short: "Send a raw authenticated request to the Proton API",
		Long: `Send a raw authenticated request to the Proton API.

The response is passed through as the API returned it, so this is where to reach
anything the commands do not cover.`,
		Args: cobra.ExactArgs(2),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			method := strings.ToUpper(c.Args[0])
			q := make(map[string][]string)
			for _, kv := range query {
				key, value, found := strings.Cut(kv, "=")
				if !found {
					return kit.Fail("--query expects key=value, but got %q.", kv)
				}
				q[key] = append(q[key], value)
			}
			if body != "" && !json.Valid([]byte(body)) {
				return kit.Fail("--body is not valid JSON.")
			}
			req := proton.Request{Method: method, Path: c.Args[1], Query: q, Body: body}

			send := func() error {
				resp, err := c.App.API.Do(c.Ctx, req)
				if err != nil {
					// An API error carries a body that explains itself, and that body
					// is what someone reaching for this command wants to read.
					var apiErr *proton.APIError
					if errors.As(err, &apiErr) && len(apiErr.RawBody) > 0 {
						_ = ui.Raw(c.UI(), apiErr.RawBody)
					}
					return err
				}
				return ui.Raw(c.UI(), resp.Body)
			}

			if readOnly(method) {
				return send()
			}

			// An unmodelled write can communicate externally, change security
			// settings, or be irreversible. Treat that uncertainty as a consent
			// boundary, while keeping the API response as the command's one answer.
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated.WithConsent(), Kind: "API requests", Count: 1,
				Name: method + " " + c.Args[1], AnswerFollows: true,
			}, send)
		}),
	}
	c.Flags().StringArrayVar(&query, "query", nil, "Query parameter as key=value (repeatable)")
	c.Flags().StringVar(&body, "body", "", "JSON request body")
	return c
}

// readOnly reports whether a method is defined to leave the resource alone.
func readOnly(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
