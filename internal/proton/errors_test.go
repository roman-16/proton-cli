package proton

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
)

func TestNetworkErrorExitCode(t *testing.T) {
	inner := errors.New("dial tcp: connection refused")
	var ne error = &NetworkError{Err: inner}

	// Carries exit code 5 via the errs.ExitCoder interface (how the cli maps it).
	var coder errs.ExitCoder
	if !errors.As(ne, &coder) {
		t.Fatalf("NetworkError should satisfy errs.ExitCoder")
	}
	if coder.ExitCode() != 5 {
		t.Errorf("NetworkError exit = %d, want 5", coder.ExitCode())
	}
	// Unwraps to the transport cause.
	if !errors.Is(ne, inner) {
		t.Error("NetworkError should unwrap to its cause")
	}
}

// TestDoTransportFailureIsNetworkError proves the wiring end to end: a request
// that never reaches an HTTP response (here, a closed server = connection
// refused) surfaces from Client.Do as *NetworkError, so the cli maps it to 5.
func TestDoTransportFailureIsNetworkError(t *testing.T) {
	shrinkBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // connections to url are now refused

	c := New(Options{BaseURL: url})
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/tests/ping"})
	var ne *NetworkError
	if !errors.As(err, &ne) {
		t.Fatalf("err = %v, want *NetworkError", err)
	}
	if ne.ExitCode() != 5 {
		t.Errorf("exit = %d, want 5", ne.ExitCode())
	}
}

// The exit code is a contract scripts branch on, so it follows what went wrong
// rather than which HTTP status Proton happened to wrap it in.
func TestAPIErrorExitCode(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  APIError
		want int
	}{
		{"unauthorized", APIError{HTTPStatus: 401}, 2},
		{"forbidden", APIError{HTTPStatus: 403}, 2},
		{"http not found", APIError{HTTPStatus: 404}, 3},
		{"proton not found under 422", APIError{HTTPStatus: 422, Code: 2501}, 3},
		{"a real conflict", APIError{HTTPStatus: 409}, 4},
		{"unprocessable for another reason", APIError{HTTPStatus: 422, Code: 2011}, 4},
		{"refused credentials", APIError{HTTPStatus: 422, Code: invalidLoginCode}, 2},
		{"rate limited", APIError{HTTPStatus: 429, Code: 85131}, 5},
		{"shut out for a while", APIError{HTTPStatus: 422, Code: jailedCode}, 5},
		{"server error", APIError{HTTPStatus: 503}, 5},
		{"anything else", APIError{HTTPStatus: 400}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.ExitCode(); got != tc.want {
				t.Errorf("got %d want %d", got, tc.want)
			}
		})
	}
}
