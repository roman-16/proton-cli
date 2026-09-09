package proton

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --dry-run is a promise the CLI makes about every command that changes
// something. Checking it here, where every request passes, is what makes it hold
// for commands that forget to check it themselves.
func TestDryRunRefusesEveryRequestThatWouldChangeSomething(t *testing.T) {
	var reached []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = append(reached, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, DryRun: true})
	c.SetTokens("uid", "acc", "ref")

	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "post"} {
		_, err := c.Do(context.Background(), Request{Method: method, Path: "/core/v4/labels"})
		if !errors.Is(err, ErrDryRun) {
			t.Errorf("%s under --dry-run returned %v, want ErrDryRun", method, err)
		}
	}
	if len(reached) != 0 {
		t.Errorf("requests reached the server under --dry-run: %v", reached)
	}

	for _, method := range []string{"GET", "HEAD", "OPTIONS"} {
		if _, err := c.Do(context.Background(), Request{Method: method, Path: "/core/v4/users"}); err != nil {
			t.Errorf("%s under --dry-run failed: %v", method, err)
		}
	}
	if len(reached) != 3 {
		t.Errorf("read-only requests reached the server %d times, want 3", len(reached))
	}

	// Proton answers some questions with a POST, and a preview that could not ask
	// them could not say what the command would do.
	if _, err := c.Do(context.Background(), Request{
		Method: "POST", Path: "/drive/shares/x/links/y/checkAvailableHashes", Reads: true,
	}); err != nil {
		t.Errorf("a POST that only reads was refused under --dry-run: %v", err)
	}
	if len(reached) != 4 {
		t.Errorf("the request that only reads reached the server %d times, want 1", len(reached)-3)
	}
}

func TestWithoutDryRunEveryMethodIsSent(t *testing.T) {
	var reached []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = append(reached, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL})
	c.SetTokens("uid", "acc", "ref")
	for _, method := range []string{"GET", "POST", "PUT", "DELETE"} {
		if _, err := c.Do(context.Background(), Request{Method: method, Path: "/x"}); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}
	if len(reached) != 4 {
		t.Errorf("%d requests reached the server, want 4", len(reached))
	}
}
