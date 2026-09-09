package proton

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// newHeaderCaptureServer records the headers of every request and replies with
// a minimal Proton-OK body.
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

// The clock everything this build dates is dated by comes off the responses,
// because a key dated by a machine whose clock is out is a key Proton refuses.
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

// An answer that says nothing about the time leaves this machine's clock in
// charge, and says so in the log: it is the difference between a key dated by a
// clock somebody else keeps right and one dated by this.
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

// shrinkBackoff makes the waits negligible for the duration of a test, so a test
// of what gets asked again does not also spend the backoff.
func shrinkBackoff(t *testing.T) {
	t.Helper()
	floor, ceiling := backoffFloor, backoffCeiling
	backoffFloor, backoffCeiling = time.Millisecond, 2*time.Millisecond
	t.Cleanup(func() { backoffFloor, backoffCeiling = floor, ceiling })
}

// A server that broke says nothing about whether the request arrived. Asking
// again is right for something that changes nothing and wrong for anything else:
// Proton has no idempotency keys, so a POST it may already have applied would be
// applied twice.
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

// A deadline is not a bad moment: the request was given its time and used it, so
// starting the clock again would spend it over and over.
func TestARequestOutOfTimeIsNotAskedAgain(t *testing.T) {
	shrinkBackoff(t)

	var asked int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked++
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
	if asked != 1 {
		t.Errorf("the request was made %d times, want once", asked)
	}
}

// An attempt is weighed on what it reports, and an attempt that reports nothing
// at all is over. Reading a status off an answer that is not there is how a
// successful scope elevation once took the whole command down with it.
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
	c.SetTokens("uid", "stale", "stale-refresh")
	c.SetReloadHook(func() (string, string, bool) { return "fresh", "fresh-refresh", true })

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

// Concurrent requests discover an expired session at the same moment. A refresh
// token is single-use, so the second and third refresh would spend one the first
// had already replaced: the web clients guard this with a once-handler, and so
// does this.
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

// A rate-limited refresh means the server is busy, not that the session is gone.
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

// Several commands can share one session, so by the time this one has taken up
// the tokens another process wrote, a third may have replaced them again. One
// renewal is not always enough.
func TestASessionReplacedTwiceUnderARequestStillSucceeds(t *testing.T) {
	stored := []string{"second", "third"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer third" {
			_, _ = w.Write([]byte(`{"Code":1000}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "first", "first-refresh")
	// Each look finds what somebody else has since written.
	c.SetReloadHook(func() (string, string, bool) {
		if len(stored) == 0 {
			return "", "", false
		}
		next := stored[0]
		stored = stored[1:]
		return next, next + "-refresh", true
	})

	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != http.StatusOK {
		t.Errorf("status = %d, want the request to succeed once the session settled", resp.Status)
	}
}

// What a failed refresh says about the session is what a scheduled job branches
// on: one answer means sign in again and nothing else will do, the other means
// come back later. Proton refuses a session it has ended; anything else is the
// refresh not getting through, and reading that as an expired session sends
// somebody to re-enter a credential that is fine.
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

// A network that never reached the refresh says nothing about the session
// either, and a job that waits on 5 has to be able to tell.
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

// A session that is genuinely gone is reported rather than renewed forever.
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

// Proton answers some refusals with 401 and a code of its own - a feature the
// plan does not include, for one. Refreshing the session cannot change that
// answer, and doing so spends a refresh token and loses Proton's reason.
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
