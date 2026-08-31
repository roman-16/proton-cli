package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
)

type rawAPITransport struct {
	calls int
}

func (t *rawAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`{"Code":1000,"Value":"kept"}`)),
		Request:    req,
	}, nil
}

type rawAPIRun struct {
	out      string
	errOut   string
	err      error
	requests int
}

func runRawAPI(t *testing.T, method, input string, noInput, yes, dryRun bool) rawAPIRun {
	t.Helper()
	previous, hadPrevious := os.LookupEnv("PROTON_NO_INPUT")
	if err := os.Unsetenv("PROTON_NO_INPUT"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv("PROTON_NO_INPUT", previous)
		} else {
			_ = os.Unsetenv("PROTON_NO_INPUT")
		}
	})

	transport := &rawAPITransport{}
	client := proton.New(proton.Options{
		BaseURL:    "https://api.example.test",
		HTTPClient: &http.Client{Transport: transport},
		DryRun:     dryRun,
	})
	client.SetTokens("uid", "access", "refresh")

	var out, errOut bytes.Buffer
	a := &app.App{
		API:    client,
		DryRun: dryRun,
		Yes:    yes,
		UI: ui.New(ui.Options{
			Format:  ui.FormatJSON,
			Out:     &out,
			Err:     &errOut,
			In:      strings.NewReader(input),
			NoInput: noInput,
		}),
	}
	ctx := app.WithApp(context.Background(), a)
	cmd := New()
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{method, "/test"})
	cmd.SetContext(ctx)
	_, err := cmd.ExecuteContextC(ctx)
	return rawAPIRun{out: out.String(), errOut: errOut.String(), err: err, requests: transport.calls}
}

func TestRawWritesRequireConsentBeforeSending(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			got := runRawAPI(t, method, "", true, false, false)
			if got.err == nil {
				t.Fatal("unattended write should be refused")
			}
			if got.requests != 0 {
				t.Fatalf("requests = %d, want 0", got.requests)
			}
			if got.out != "" {
				t.Fatalf("stdout = %q, want empty", got.out)
			}
			if !strings.Contains(got.err.Error(), "Would update API request") {
				t.Fatalf("refusal does not describe the write: %v", got.err)
			}
			hinted, ok := got.err.(interface{ Hints() []string })
			if !ok || !strings.Contains(strings.Join(hinted.Hints(), " "), "--yes") {
				t.Fatalf("refusal does not offer --yes: %v", got.err)
			}
		})
	}
}

func TestYesAllowsEveryRawWriteAndKeepsTheAPIResponse(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			got := runRawAPI(t, method, "", true, true, false)
			if got.err != nil {
				t.Fatalf("write with --yes: %v", got.err)
			}
			if got.requests != 1 {
				t.Fatalf("requests = %d, want 1", got.requests)
			}
			assertOneAPIResponse(t, got.out)
			if got.errOut != "" {
				t.Fatalf("pre-authorized machine write wrote stderr: %q", got.errOut)
			}
		})
	}
}

func TestInteractiveConsentControlsRawWrite(t *testing.T) {
	declined := runRawAPI(t, http.MethodPost, "n\n", false, false, false)
	if declined.err == nil || !strings.Contains(declined.err.Error(), "Cancelled") {
		t.Fatalf("declined write error = %v, want cancellation", declined.err)
	}
	if declined.requests != 0 {
		t.Fatalf("declined write sent %d requests", declined.requests)
	}

	accepted := runRawAPI(t, http.MethodPost, "y\n", false, false, false)
	if accepted.err != nil {
		t.Fatalf("accepted write: %v", accepted.err)
	}
	if accepted.requests != 1 {
		t.Fatalf("accepted write sent %d requests, want 1", accepted.requests)
	}
	if !strings.Contains(accepted.errOut, "Would update API request") {
		t.Fatalf("prompt does not describe the write: %q", accepted.errOut)
	}
	assertOneAPIResponse(t, accepted.out)
}

func TestRawWriteDryRunDoesNotSend(t *testing.T) {
	got := runRawAPI(t, http.MethodDelete, "", true, false, true)
	if got.err != nil {
		t.Fatalf("dry run: %v", got.err)
	}
	if got.requests != 0 {
		t.Fatalf("dry run sent %d requests", got.requests)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(got.out), &result); err != nil {
		t.Fatalf("dry-run output is not JSON: %v\n%s", err, got.out)
	}
	if result["action"] != "updated" || result["dry_run"] != true {
		t.Fatalf("dry-run result = %#v", result)
	}
}

func TestRawReadDoesNotRequireConsent(t *testing.T) {
	got := runRawAPI(t, http.MethodGet, "", true, false, false)
	if got.err != nil {
		t.Fatalf("read: %v", got.err)
	}
	if got.requests != 1 {
		t.Fatalf("read sent %d requests, want 1", got.requests)
	}
	assertOneAPIResponse(t, got.out)
}

func assertOneAPIResponse(t *testing.T, output string) {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(output))
	var response map[string]any
	if err := dec.Decode(&response); err != nil {
		t.Fatalf("API response is not JSON: %v\n%s", err, output)
	}
	if response["Code"] != float64(1000) || response["Value"] != "kept" {
		t.Fatalf("API response = %#v", response)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		t.Fatalf("stdout contains another document: %v\n%s", err, output)
	}
}
