package proton

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/profile"
)

func newHeaderCaptureServer(t *testing.T) (*httptest.Server, *[]http.Header) {
	t.Helper()
	var received []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = append(received, r.Header.Clone())
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &received
}

func TestDoSendsHonestIdentityHeaders(t *testing.T) {
	srv, received := newHeaderCaptureServer(t)
	c := New(Options{BaseURL: srv.URL})
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*received) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*received))
	}
	h := (*received)[0]
	if got := h.Get("User-Agent"); got != DefaultUserAgent {
		t.Errorf("User-Agent = %q, want %q", got, DefaultUserAgent)
	}
	if got := h.Get("x-pm-appversion"); got != DefaultAppVersion {
		t.Errorf("x-pm-appversion = %q, want %q", got, DefaultAppVersion)
	}
}

func TestDoIdentityHeadersHonorOverrides(t *testing.T) {
	srv, received := newHeaderCaptureServer(t)
	c := New(Options{
		BaseURL:    srv.URL,
		AppVersion: "external-proton_cli@9.9.9-beta",
		UserAgent:  "proton-cli/9.9.9",
	})
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(*received) != 1 {
		t.Fatalf("expected 1 request, got %d", len(*received))
	}
	h := (*received)[0]
	if got := h.Get("User-Agent"); got != "proton-cli/9.9.9" {
		t.Errorf("User-Agent = %q, want proton-cli/9.9.9", got)
	}
	if got := h.Get("x-pm-appversion"); got != "external-proton_cli@9.9.9-beta" {
		t.Errorf("x-pm-appversion = %q, want external-proton_cli@9.9.9-beta", got)
	}
}

func TestNowFollowsWhatProtonDatedItsAnswer(t *testing.T) {
	served := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Date", served.UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	t.Cleanup(srv.Close)
	c := New(Options{BaseURL: srv.URL})

	if before := c.Now(); time.Until(before).Abs() > time.Second {
		t.Errorf("before anything was asked the clock reads %s, want this machine's", before)
	}
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := c.Now(); got.Sub(served).Abs() > time.Minute {
		t.Errorf("the clock reads %s, want Proton's at %s", got, served)
	}
}

func TestNowKeepsThisMachinesClockWhenAnAnswerIsUndated(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header()["Date"] = nil
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	t.Cleanup(srv.Close)
	var records strings.Builder
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.NewTextHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug}))})

	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"}); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got := c.Now(); time.Until(got).Abs() > time.Second {
		t.Errorf("the clock reads %s, want this machine's", got)
	}
	if !strings.Contains(records.String(), "reason=undated") {
		t.Errorf("the log does not say the answer was undated:\n%s", records.String())
	}
}

func TestRetryDelayPrefersTheServersAnswer(t *testing.T) {
	got := retryDelay("3", 1)
	if got != 3*time.Second {
		t.Errorf("delay = %v, want the Retry-After the server named", got)
	}
}

func TestRetryDelayBacksOffWithJitterWhenTheServerNamesNothing(t *testing.T) {
	var last time.Duration
	for attempt := 1; attempt <= 6; attempt++ {
		floor := min(backoffFloor<<(attempt-1), backoffCeiling)
		for range 20 {
			got := retryDelay("", attempt)
			if got < floor/2 || got > floor {
				t.Fatalf("attempt %d delay = %v, want between %v and %v", attempt, got, floor/2, floor)
			}
		}
		last = floor
	}
	if last != backoffCeiling {
		t.Errorf("the wait reached %v, want it capped at %v", last, backoffCeiling)
	}
}

func shrinkBackoff(t *testing.T) {
	t.Helper()
	floor, ceiling := backoffFloor, backoffCeiling
	backoffFloor, backoffCeiling = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { backoffFloor, backoffCeiling = floor, ceiling })
}

func TestAServerThatBrokeIsAskedAgainOnlyForWhatMayArriveTwice(t *testing.T) {
	shrinkBackoff(t)

	cases := []struct {
		name     string
		req      Request
		attempts int
	}{
		{"a read", Request{Method: "GET", Path: "/x"}, transientWaits + 1},
		{"a write", Request{Method: "POST", Path: "/x"}, 1},
		{"a write that says it may be repeated", Request{Method: "POST", Path: "/x", Repeatable: true}, transientWaits + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				asked++
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
			_, err := c.Do(context.Background(), tc.req)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusBadGateway {
				t.Fatalf("err = %v, want the 502 reported as what it is", err)
			}
			if apiErr.ExitCode() != 5 {
				t.Errorf("exit = %d, want 5 (a server problem)", apiErr.ExitCode())
			}
			if asked != tc.attempts {
				t.Errorf("the server was asked %d times, want %d", asked, tc.attempts)
			}
		})
	}
}

func TestAReadRidesOutAPassingServerFailure(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		if asked == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("<html>503</html>"))
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the answer after the wait", resp.Status)
	}
	if asked != 2 {
		t.Errorf("the server was asked %d times, want a second attempt", asked)
	}
}

func TestARequestOutOfTimeIsNotAskedAgain(t *testing.T) {
	shrinkBackoff(t)

	var asked atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked.Add(1)
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{
		BaseURL:    srv.URL,
		HTTPClient: &http.Client{Timeout: 10 * time.Millisecond},
		Logger:     slog.New(slog.DiscardHandler),
	})
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("err = %v, want a network failure", err)
	}
	if made := asked.Load(); made != 1 {
		t.Errorf("the request was made %d times, want once", made)
	}
}

func TestAnAttemptThatReportsNothingIsSettled(t *testing.T) {
	c := New(Options{Logger: slog.New(slog.DiscardHandler)})

	var runs int
	resp, err := c.retrying(context.Background(), Request{Method: "POST", Path: "/x", Repeatable: true},
		func() (*Response, error) {
			runs++
			return nil, nil
		})
	if resp != nil || err != nil {
		t.Fatalf("got (%v, %v), want nothing reported either way", resp, err)
	}
	if runs != 1 {
		t.Errorf("the attempt ran %d times, want once", runs)
	}
}

func TestAConnectionThatFailedIsAskedAgainForARead(t *testing.T) {
	shrinkBackoff(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	dead := srv.URL
	srv.Close()

	c := New(Options{BaseURL: dead, Logger: slog.New(slog.DiscardHandler)})
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("err = %v, want a network failure", err)
	}
	if !worthRepeating(err) {
		t.Error("a refused connection is worth another go")
	}
}

func TestRateLimitedRequestIsWaitedOutRatherThanFailed(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the answer after the wait", resp.Status)
	}
	if calls != 2 {
		t.Errorf("the server was asked %d times, want a second attempt", calls)
	}
}

func TestARefusalARequestDeclaredIsWaitedOutRatherThanReported(t *testing.T) {
	shrinkBackoff(t)

	cases := []struct {
		name   string
		req    Request
		status int
		body   string
	}{
		{
			name: "a status on its own",
			req: Request{Method: "DELETE", Path: "/x", Transient: []Refusal{{
				Status: http.StatusConflict, Within: time.Minute,
				Reason: "Proton is still finishing an earlier action on this mailbox",
			}}},
			status: http.StatusConflict,
			body:   `{"Code":2000,"Error":"Another action is currently in progress"}`,
		},
		{
			name: "a status and the code under it",
			req: Request{Method: "PUT", Path: "/x", Transient: []Refusal{{
				Status: http.StatusBadRequest, Code: 2001, Within: time.Minute,
				Reason: "Proton is not taking this contact write yet",
			}}},
			status: http.StatusBadRequest,
			body:   `{"Code":2001,"Error":"Contact update failed"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var asked int
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				asked++
				if asked == 1 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(tc.body))
					return
				}
				_, _ = w.Write([]byte(`{"Code":1000}`))
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
			resp, err := c.Do(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != http.StatusOK {
				t.Errorf("status = %d, want the answer after the wait", resp.Status)
			}
			if asked != 2 {
				t.Errorf("the server was asked %d times, want a second attempt", asked)
			}
		})
	}
}

func TestARefusalNobodyDeclaredIsReportedAtOnce(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"Code":2001,"Error":"Contact update failed"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	_, err := c.Do(context.Background(), Request{Method: "PUT", Path: "/x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != 2001 {
		t.Fatalf("err = %v, want Proton's own refusal reported", err)
	}
	if asked != 1 {
		t.Errorf("the server was asked %d times, want the refusal taken at its word", asked)
	}
}

func TestARefusalThatNeverClearsIsReportedAsItStands(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"Code":2000,"Error":"Another action is currently in progress"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	const within = 30 * time.Millisecond
	start := time.Now()
	_, err := c.Do(context.Background(), Request{Method: "DELETE", Path: "/x", Transient: []Refusal{{
		Status: http.StatusConflict, Within: within,
		Reason: "Proton is still finishing an earlier action on this mailbox",
	}}})

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTPStatus != http.StatusConflict || apiErr.Code != 2000 {
		t.Fatalf("err = %v, want the refusal reported with what Proton said", err)
	}
	if apiErr.ExitCode() != 4 {
		t.Errorf("exit = %d, want 4 (something else holds it)", apiErr.ExitCode())
	}
	if asked < 2 {
		t.Errorf("the server was asked %d times, want the refusal waited out first", asked)
	}
	if elapsed := time.Since(start); elapsed < within {
		t.Errorf("gave up after %v, want the declared %v spent first", elapsed, within)
	}
}

func TestNoMoreThanMaxInFlightRequestsAtOnce(t *testing.T) {
	var mu sync.Mutex
	var inFlight, peak int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	var wg sync.WaitGroup
	for range maxInFlight * 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
		}()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if peak > maxInFlight {
		t.Errorf("%d requests were in flight at once, want at most %d", peak, maxInFlight)
	}
	if peak < 2 {
		t.Errorf("peak was %d; the requests did not overlap at all, so this proves nothing", peak)
	}
}

func TestUnauthorizedAdoptsASessionRefreshedElsewhere(t *testing.T) {
	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked++
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	store := savedAs(c, SavedTokens{UID: "uid", Access: "stale", Refresh: "stale-refresh"})
	store.saved = SavedTokens{UID: "uid", Access: "fresh", Refresh: "fresh-refresh"}

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the request retried with the stored tokens", resp.Status)
	}
	if asked != 2 {
		t.Errorf("the server was asked %d times, want exactly one retry", asked)
	}
	if _, acc, ref := c.Tokens(); acc != "fresh" || ref != "fresh-refresh" {
		t.Errorf("tokens = %q/%q, want the stored pair adopted", acc, ref)
	}
}

func TestARenewedSessionIsSavedForTheNextCommand(t *testing.T) {
	srv := refreshingServer(t)
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	store := savedAs(c, SavedTokens{UID: "uid", Access: "stale", Refresh: "stale-refresh"})

	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.saved != (SavedTokens{UID: "uid", Access: "fresh", Refresh: "fresh-refresh"}) {
		t.Errorf("saved %+v, want the renewed pair", store.saved)
	}
}

func TestASessionThatCouldNotBeSavedSaysSoAndTheRequestStillAnswers(t *testing.T) {
	srv := refreshingServer(t)
	var logged bytes.Buffer
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.NewTextHandler(&logged, nil))})
	store := savedAs(c, SavedTokens{UID: "uid", Access: "stale", Refresh: "stale-refresh"})
	store.saveErr = errors.New("permission denied")

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("err = %v, want the answer the renewed session got", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want success", resp.Status)
	}
	if !strings.Contains(logged.String(), "level=WARN") || !strings.Contains(logged.String(), "permission denied") {
		t.Errorf("log = %q, want a warning naming why the session was not saved", logged.String())
	}
}

func TestASessionSignedOutElsewhereIsNotRenewedBack(t *testing.T) {
	refreshes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			refreshes++
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	for _, tc := range []struct {
		name  string
		saved SavedTokens
	}{
		{"signed out", SavedTokens{}},
		{"signed in again as another session", SavedTokens{UID: "other", Access: "a", Refresh: "r"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Options{BaseURL: srv.URL, Profile: profile.MustParse("work"), Logger: slog.New(slog.DiscardHandler)})
			store := savedAs(c, SavedTokens{UID: "uid", Access: "stale", Refresh: "stale-refresh"})
			store.saved = tc.saved

			_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
			if !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("err = %v, want the session reported as ended", err)
			}
			if store.saves != 0 {
				t.Error("a session signed out elsewhere was written back")
			}
		})
	}
	if refreshes != 0 {
		t.Errorf("the session was refreshed %d times, want not at all", refreshes)
	}
}

func TestASessionNotSavedYetIsRenewedWithoutTouchingTheSavedOne(t *testing.T) {
	srv := refreshingServer(t)
	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	store := savedAs(c, SavedTokens{UID: "saved", Access: "a", Refresh: "r"})
	c.SetTokens("uid", "stale", "stale-refresh")

	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.saves != 0 || store.saved.UID != "saved" {
		t.Errorf("the saved session became %+v after %d saves, want it left alone", store.saved, store.saves)
	}
}

func TestPersistTakesUpTokensRenewedElsewhereBeforeSaving(t *testing.T) {
	c := New(Options{Logger: slog.New(slog.DiscardHandler)})
	store := savedAs(c, SavedTokens{UID: "uid", Access: "old", Refresh: "old-refresh"})
	store.saved = SavedTokens{UID: "uid", Access: "new", Refresh: "new-refresh"}

	if err := c.Persist(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.saved != (SavedTokens{UID: "uid", Access: "new", Refresh: "new-refresh"}) {
		t.Errorf("saved %+v, want the tokens renewed elsewhere kept", store.saved)
	}

	store.saved = SavedTokens{}
	if err := c.Persist(context.Background()); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("Persist after a sign-out elsewhere = %v, want the session reported as ended", err)
	}
	if store.saved != (SavedTokens{}) {
		t.Error("Persist wrote back a session that was signed out")
	}
}

func TestAnExpiredSessionSaysHowToSignIn(t *testing.T) {
	for _, tc := range []struct {
		profile, message, hint string
	}{
		{"", "Your session has expired.", "proton account login"},
		{"work", `The session of profile "work" has expired.`, "proton account login --profile work"},
	} {
		err := error(unauthorized{profile: profile.MustParse(tc.profile)})
		if err.Error() != tc.message {
			t.Errorf("message = %q, want %q", err.Error(), tc.message)
		}
		var hinter errs.Hinter
		if !errors.As(err, &hinter) || len(hinter.Hints()) != 1 || hinter.Hints()[0] != tc.hint {
			t.Errorf("hints = %v, want %q", hinter, tc.hint)
		}
		if !errors.Is(err, ErrUnauthorized) {
			t.Error("an expired session does not match ErrUnauthorized")
		}
	}
}

func TestARedirectIsFollowedOnlyWithinItsHost(t *testing.T) {
	var leaked bool
	elsewhere := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("x-pm-uid") != ""
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer elsewhere.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/here":
			http.Redirect(w, r, "/there", http.StatusTemporaryRedirect)
		case "/there":
			_, _ = w.Write([]byte(`{"Code":1000}`))
		default:
			http.Redirect(w, r, elsewhere.URL+"/x", http.StatusTemporaryRedirect)
		}
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "token", "refresh")

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/here"})
	if err != nil || resp.Status != http.StatusOK {
		t.Fatalf("a redirect within the host = %v, %v; want it followed", resp, err)
	}
	_, err = c.Do(context.Background(), Request{Method: "POST", Path: "/away", Body: map[string]string{"RefreshToken": "secret"}})
	var refused *APIError
	if !errors.As(err, &refused) || refused.HTTPStatus != http.StatusTemporaryRedirect {
		t.Errorf("err = %v, want the redirect itself rather than what is behind it", err)
	}
	if leaked {
		t.Error("the session went to another host")
	}
}

type memoryStore struct {
	mu      sync.Mutex
	client  *Client
	saved   SavedTokens
	saves   int
	saveErr error
}

func savedAs(c *Client, saved SavedTokens) *memoryStore {
	store := &memoryStore{client: c, saved: saved}
	c.SetStore(store)
	c.Resume(saved)
	return store
}

func (m *memoryStore) Hold(context.Context) (func(), error) { return func() {}, nil }

func (m *memoryStore) Saved() (SavedTokens, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saved, nil
}

func (m *memoryStore) Save() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return m.saveErr
	}
	uid, acc, ref := m.client.Tokens()
	m.saved = SavedTokens{UID: uid, Access: acc, Refresh: ref}
	m.saves++
	return nil
}

func refreshingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			_, _ = w.Write([]byte(`{"AccessToken":"fresh","RefreshToken":"fresh-refresh"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestConcurrentUnauthorizedRequestsRefreshOnce(t *testing.T) {
	var mu sync.Mutex
	refreshes := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			mu.Lock()
			refreshes++
			mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			_, _ = w.Write([]byte(`{"AccessToken":"fresh","RefreshToken":"fresh-refresh"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale", "stale-refresh")

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if resp.Status != http.StatusOK {
				t.Errorf("status = %d, want the request to succeed after one refresh", resp.Status)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if refreshes != 1 {
		t.Errorf("the session was refreshed %d times, want once", refreshes)
	}
}

func TestRateLimitedRefreshIsWaitedOutRatherThanReportedAsSignedOut(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"AccessToken":"fresh","RefreshToken":"fresh-refresh"}`))
			return
		}
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"Code":1000}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale", "stale-refresh")

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("err = %v, want the refresh retried rather than the session declared dead", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want success", resp.Status)
	}
	if attempts != 2 {
		t.Errorf("the refresh was attempted %d times, want a second try", attempts)
	}
}

func TestASessionReplacedTwiceUnderARequestStillSucceeds(t *testing.T) {
	c := New(Options{Logger: slog.New(slog.DiscardHandler)})
	var store *memoryStore
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer third":
			_, _ = w.Write([]byte(`{"Code":1000}`))
			return
		case "Bearer second":
			store.mu.Lock()
			store.saved = SavedTokens{UID: "uid", Access: "third", Refresh: "third-refresh"}
			store.mu.Unlock()
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c.base = srv.URL
	store = savedAs(c, SavedTokens{UID: "uid", Access: "first", Refresh: "first-refresh"})
	store.saved = SavedTokens{UID: "uid", Access: "second", Refresh: "second-refresh"}

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the request to succeed once the session settled", resp.Status)
	}
}

func TestARefreshThatFailedIsOnlyTheSessionWhenProtonSaysSo(t *testing.T) {
	shrinkBackoff(t)

	cases := []struct {
		name     string
		status   int
		wantExit int
	}{
		{"a request Proton would not read", http.StatusBadRequest, 2},
		{"a session Proton has ended", http.StatusUnauthorized, 2},
		{"a refresh token Proton will not take", http.StatusUnprocessableEntity, 2},
		{"an edge that broke", http.StatusServiceUnavailable, 5},
		{"a rate limit still standing", http.StatusTooManyRequests, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/auth/v4/refresh" {
					w.WriteHeader(tc.status)
					return
				}
				w.WriteHeader(http.StatusUnauthorized)
			}))
			defer srv.Close()

			c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
			c.SetTokens("uid", "stale", "stale-refresh")

			_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
			var coded interface{ ExitCode() int }
			if !errors.As(err, &coded) {
				t.Fatalf("err = %v, want one that says what to do about it", err)
			}
			if got := coded.ExitCode(); got != tc.wantExit {
				t.Errorf("exit = %d, want %d (err = %v)", got, tc.wantExit, err)
			}
			if tc.wantExit == 2 && !errors.Is(err, ErrUnauthorized) {
				t.Errorf("err = %v, want the session reported as expired", err)
			}
		})
	}
}

func TestARefreshThatNeverArrivedIsANetworkFailure(t *testing.T) {
	shrinkBackoff(t)

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			srv.CloseClientConnections()
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale", "stale-refresh")

	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	var netErr *NetworkError
	if !errors.As(err, &netErr) {
		t.Fatalf("err = %v, want the network failure it was", err)
	}
	if netErr.ExitCode() != 5 {
		t.Errorf("exit = %d, want 5", netErr.ExitCode())
	}
}

func TestAnUnrecoverableSessionIsReportedRatherThanRetriedForever(t *testing.T) {
	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		asked++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "stale", "stale-refresh")
	if _, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"}); err == nil {
		t.Fatal("a dead session should be reported")
	}
	if asked > sessionRenewals+1 {
		t.Errorf("the request was made %d times, want at most %d", asked, sessionRenewals+1)
	}
}

func TestA401WithProtonsOwnCodeIsAnAnswerRatherThanAStaleSession(t *testing.T) {
	var refreshes, asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/auth/v4/refresh" {
			refreshes++
			_, _ = w.Write([]byte(`{"AccessToken":"fresh","RefreshToken":"fresh-refresh"}`))
			return
		}
		asked++
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"Code":2027,"Error":"Cannot edit contact groups"}`))
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "good", "good-refresh")

	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/core/v4/labels"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want Proton's own refusal", err)
	}
	if apiErr.Code != 2027 {
		t.Errorf("code = %d, want 2027 kept intact", apiErr.Code)
	}
	if refreshes != 0 {
		t.Errorf("the session was refreshed %d times for an answer a refresh cannot change", refreshes)
	}
	if asked != 1 {
		t.Errorf("the request was made %d times, want once", asked)
	}
}
