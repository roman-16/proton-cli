// Package proton is the low-level Proton HTTP client: request building,
// response decoding, typed errors, auth, token refresh, rate-limit and
// human-verification retries. No domain logic lives here.
package proton

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL    = "https://mail.proton.me/api"
	DefaultAppVersion = "Other"
	DefaultUserAgent  = "proton-cli/dev"

	// maxRetryAfter caps how long a delay the server names is honoured for before
	// waiting stops being the civil answer and becomes a hang.
	maxRetryAfter = 30 * time.Second

	// sessionRenewals is how many times one request may have the session renewed
	// under it. More than one is needed because several commands can share a
	// session: by the time this one has taken up the tokens another process wrote,
	// a third may have replaced them again. The count is what stops that becoming a
	// loop when the session is genuinely gone.
	sessionRenewals = 3

	// transientWaits is how many times a request is waited out before what it
	// failed with is reported. Proton throttles a client it considers demanding
	// and its edge has bad moments, and the only civil answer to either is to slow
	// down rather than to fail and be retried by a human immediately.
	transientWaits = 4

	// notAnswering is what a wait on a server that broke or a connection that
	// failed is announced as, for the same reason a rate-limit wait is announced.
	notAnswering = "Proton is not answering; waiting before trying again"

	// maxInFlight bounds how many requests one invocation has outstanding.
	// Independent requests are made at the same time, which is what keeps a
	// command's wall time to the depth of its request graph; this is what keeps
	// that from becoming a way to flood Proton. Uploads already run five blocks
	// at once, so the bound has to leave room for them.
	maxInFlight = 8
)

// backoffFloor and backoffCeiling bound the wait when the server names no
// Retry-After. The wait doubles between them and carries jitter, so several
// requests refused at the same instant do not all come back at the same instant.
//
// They are vars so tests can shrink them; production keeps a real backoff.
var (
	backoffFloor   = 500 * time.Millisecond
	backoffCeiling = 8 * time.Second
)

// Doer is the seam domain services depend on. *Client satisfies it; tests
// supply fakes.
type Doer interface {
	Do(ctx context.Context, r Request) (*Response, error)
	Decode(ctx context.Context, r Request, out any) error
}

type Client struct {
	hc    *http.Client
	base  string
	app   string
	ua    string
	log   *slog.Logger
	slots chan struct{}

	// renewMu makes one process refresh a session once. Independent requests are
	// made at the same time, so a session that has just expired is discovered by
	// several of them at once; a refresh token is single-use, so the second and
	// third refresh would spend a token the first had already replaced.
	renewMu sync.Mutex

	mu            sync.RWMutex
	uid           string
	acc           string
	ref           string
	encKeyBlob    string // persisted (salted key password encrypted with the server-held client key)
	profile       string
	dryRun        bool
	hvResolver    HVResolver
	scopeResolver ScopeResolver
	secondFactor  SecondFactorFunc
	persist       func()
	reload        func() (acc, ref string, ok bool)
	sessionGuard  func() error
}

type Options struct {
	BaseURL    string
	AppVersion string
	UserAgent  string
	Profile    string
	HTTPClient *http.Client
	Logger     *slog.Logger
	// DryRun refuses every request that would change the account's data. See
	// Client.dryRun.
	DryRun bool
}

// New fills empty Options fields with defaults.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	if opts.AppVersion == "" {
		opts.AppVersion = DefaultAppVersion
	}
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute}
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Client{
		hc: hc, base: opts.BaseURL, app: opts.AppVersion, ua: opts.UserAgent,
		profile: opts.Profile, log: log, dryRun: opts.DryRun,
		slots: make(chan struct{}, maxInFlight),
	}
}

// ErrDryRun is returned instead of sending a request that would change something.
var ErrDryRun = errors.New("refusing to change anything under --dry-run")

// dryRun is why --dry-run is a promise rather than a habit.
//
// The CLI states that every command which changes something honours --dry-run.
// Enforcing that inside each command makes it true of the commands that remember
// to; enforcing it here, at the one point every request passes through, makes it
// true of all of them, including the ones not yet written and including `api`,
// whose whole purpose is to send a request nothing else models.
//
// Signing in and refreshing a session do not come through here, so they are
// unaffected: a dry run on an expired session still works, which is the
// difference between a useful preview and an error.
func (c *Client) dryRunRefuses(req Request) error {
	if !c.dryRun || readOnlyMethod(req.Method) {
		return nil
	}
	return fmt.Errorf("%w: %s %s", ErrDryRun, strings.ToUpper(req.Method), req.Path)
}

// readOnlyMethod reports whether a method is defined to leave the resource alone.
func readOnlyMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

func (c *Client) Profile() string { return c.profile }

func (c *Client) SetTokens(uid, acc, ref string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uid, c.acc, c.ref = uid, acc, ref
}

func (c *Client) EncKeyBlob() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.encKeyBlob
}

func (c *Client) SetEncKeyBlob(blob string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.encKeyBlob = blob
}

// Tokens returns the current session auth tokens.
func (c *Client) Tokens() (uid, acc, ref string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uid, c.acc, c.ref
}

// AppVersion returns the app-version header value (a fixed connection setting).
func (c *Client) AppVersion() string { return c.app }

// BaseURL returns the API base URL (a fixed connection setting).
func (c *Client) BaseURL() string { return c.base }

// SetPersistHook installs a callback invoked whenever the client's persistable
// state changes (e.g. a token refresh). The higher layer uses it to write the
// session file, keeping this transport package free of any persistence format.
func (c *Client) SetPersistHook(fn func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.persist = fn
}

// Persist invokes the persist hook if one is installed (best-effort).
func (c *Client) Persist() {
	c.mu.RLock()
	fn := c.persist
	c.mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// SetSessionGuard installs the check that stands between a command and the
// network.
//
// It is asked before every request rather than before every command, which is
// what lets a command judge its own arguments first. A reference shaped like
// nothing, a value outside a declared domain, a selection that names nothing:
// none of those needs an account to be wrong, and answering "you are not signed
// in" to them tells somebody to fix the wrong thing.
func (c *Client) SetSessionGuard(fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionGuard = fn
}

// SetReloadHook installs a callback that re-reads the tokens from where they are
// stored, so a session another process has just refreshed can be picked up rather
// than refreshed again.
//
// Proton hands out a new refresh token each time and stops honouring the old one,
// so two processes refreshing the same session at once leaves one of them holding
// a token that no longer works. Looking first is what makes running two commands
// at the same time safe.
func (c *Client) SetReloadHook(fn func() (acc, ref string, ok bool)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reload = fn
}

// adoptStoredTokens takes up the stored tokens when they are not the ones that
// just failed, and reports whether it did.
func (c *Client) adoptStoredTokens() bool {
	c.mu.RLock()
	fn, current := c.reload, c.acc
	c.mu.RUnlock()
	if fn == nil {
		return false
	}
	acc, ref, ok := fn()
	if !ok || acc == "" || acc == current {
		return false
	}
	c.mu.Lock()
	c.acc, c.ref = acc, ref
	c.mu.Unlock()
	c.log.Debug("adopted a session refreshed elsewhere")
	return true
}

// Request is a typed API request. Body is JSON-encoded when it is not nil and
// not already a []byte / string / io.Reader.
type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any

	// ContentType overrides the request Content-Type. Empty means
	// application/json. Set it (e.g. multipart/form-data with a boundary) when
	// Body is a pre-encoded []byte/io.Reader.
	ContentType string

	// Human-verification state (set by retry logic, not by most callers).
	HVToken string
	HVType  string

	// Repeatable says that sending this twice cannot change what the account ends
	// up in, so a failure that leaves it unclear whether it arrived is worth
	// asking about again.
	//
	// A method defined to leave the resource alone needs no such claim. Anything
	// else does, because Proton has no idempotency keys: whether a POST that may
	// already have been applied is safe to send again is something only the caller
	// knows.
	Repeatable bool

	// elevated marks a request already retried after a scope elevation, so a
	// second refusal cannot restart the cycle.
	elevated bool

	// headers are the request's own, on top of the ones every request carries.
	headers map[string]string

	// omitBearer leaves the access token off. The one request that wants this is
	// the refresh, which authenticates with the refresh token in its body: the
	// token it is replacing has no business on it.
	omitBearer bool
}

type Response struct {
	Status int
	Body   []byte

	// retryHeader carries the raw Retry-After header value for 429 handling.
	retryHeader string
}

// Do sends a request and returns the response. Non-2xx responses return a
// typed error. It transparently handles a transient failure (waiting and asking
// again), 401 (refresh + retry), 429 (Retry-After + retry) and 9001 (human
// verification + retry).
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	c.mu.RLock()
	guard := c.sessionGuard
	c.mu.RUnlock()
	if guard != nil {
		if err := guard(); err != nil {
			return nil, err
		}
	}
	if err := c.dryRunRefuses(req); err != nil {
		return nil, err
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Status >= 200 && resp.Status < 300 {
		return resp, nil
	}

	// The server can refuse because the session is not elevated. Handling that
	// here, once, is why no command has to know which operations are guarded.
	if isMissingScope(resp.Status, resp.Body) {
		return c.elevateAndRetry(ctx, req, resp)
	}

	hvErr, apiErr := classifyErrorBody(resp.Status, resp.Body)
	if hvErr != nil {
		// A request that already carried a verification is not asked to verify a
		// second time: the answer was refused, and offering the same page again
		// would send somebody to solve a challenge that has already been spent.
		if req.HVToken != "" {
			hvErr.Refused = true
			return resp, hvErr
		}
		if resolver := c.getHVResolver(); resolver != nil {
			token, kind, rerr := resolver(hvErr)
			switch {
			case rerr == nil && token != "":
				retry := req
				retry.HVToken = token
				retry.HVType = kind
				return c.Do(ctx, retry)
			case errors.Is(rerr, ErrHVUnavailable):
				// fall through and return the original HV error
			case rerr != nil:
				return resp, rerr
			}
		}
		return resp, hvErr
	}
	return resp, apiErr
}

// send makes the request, renewing a session the server no longer accepts, and
// returns the response it settled on.
func (c *Client) send(ctx context.Context, req Request) (*Response, error) {
	for renewals := 0; ; {
		c.mu.RLock()
		used := c.acc
		c.mu.RUnlock()

		resp, err := c.attempt(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp.Status == http.StatusUnauthorized && tokenRejected(resp) && renewals < sessionRenewals {
			renewals++
			if rerr := c.renewSession(ctx, used); rerr != nil {
				return resp, ErrUnauthorized
			}
			continue
		}
		return resp, nil
	}
}

// attempt sends one request, waiting out what is worth asking about again.
func (c *Client) attempt(ctx context.Context, req Request) (*Response, error) {
	return c.retrying(ctx, req, func() (*Response, error) { return c.doOnce(ctx, req) })
}

// retrying runs one attempt at a time until the request settles: either the
// answer is one worth returning, or asking again cannot change it.
//
// Two things are worth asking about again. A 429 is Proton refusing without
// having done anything, so it is always waited out. A server that broke or a
// connection that failed says nothing about whether the request arrived, so it
// is only sent again when arriving twice would change nothing.
//
// req is the request the attempts are trying to get through, which is not always
// all an attempt sends: an SRP exchange has to ask for fresh parameters each
// time, because the SRPSession the last set carried is spent. Either way once
// yields that request's answer, or the failure that stopped the attempt reaching
// one.
func (c *Client) retrying(ctx context.Context, req Request, once func() (*Response, error)) (*Response, error) {
	repeat := repeatable(req)
	for attempt := 1; ; attempt++ {
		resp, err := once()
		// An attempt that reports neither an answer nor a failure has settled,
		// whatever it did: there is nothing here to weigh and nothing to ask again.
		if resp == nil && err == nil {
			return nil, nil
		}
		if attempt > transientWaits {
			return resp, err
		}
		var delay time.Duration
		// Each wait is said out loud rather than logged at debug: waiting is the right
		// thing to do and it can run to seconds, so a person watching a command sit
		// there deserves to know what it is waiting for and not read it as a hang.
		switch {
		case err != nil:
			if !repeat || !worthRepeating(err) {
				return resp, err
			}
			delay = retryDelay("", attempt)
			c.log.Warn(notAnswering, "method", req.Method, "path", req.Path,
				"error", err, "wait_ms", delay.Milliseconds(), "attempt", attempt)
		case resp.Status == http.StatusTooManyRequests:
			delay = retryDelay(resp.retryHeader, attempt)
			c.log.Warn("rate limited by Proton; waiting before trying again",
				"method", req.Method, "path", req.Path, "wait_ms", delay.Milliseconds(), "attempt", attempt)
		case resp.Status >= 500 && repeat:
			delay = retryDelay(resp.retryHeader, attempt)
			c.log.Warn(notAnswering, "method", req.Method, "path", req.Path,
				"status", resp.Status, "wait_ms", delay.Milliseconds(), "attempt", attempt)
		default:
			return resp, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

// repeatable reports whether sending req twice could not change what the account
// ends up in.
func repeatable(req Request) bool {
	return req.Repeatable || readOnlyMethod(req.Method)
}

// worthRepeating reports whether a failure might not happen again.
//
// A refused connection, a name that did not resolve, a handshake that failed and
// a server that broke are the network or Proton having a bad moment. A deadline
// is not one: the request was given its time and used it, and starting the clock
// again spends it twice. Cancellation is the user changing their mind.
func worthRepeating(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return true
	}
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.HTTPStatus >= 500
}

// tokenRejected reports whether a 401 is about the session rather than about what
// was asked.
//
// Proton answers a refusal it wants understood with a code of its own - a feature
// the plan does not include, an account that is disabled - and no amount of
// refreshing changes that answer. Renewing the session for one of those spends a
// refresh token to ask the same question again, and replaces Proton's reason with
// whatever the last attempt happened to say. A rejected token comes back with
// nothing but the status.
func tokenRejected(resp *Response) bool {
	var env struct{ Code int }
	if json.Unmarshal(resp.Body, &env) != nil {
		return true
	}
	return env.Code == 0 || env.Code == http.StatusUnauthorized
}

// renewSession gets the session working again, preferring whatever somebody else
// has already put in its place.
//
// failed is the access token whose refusal prompted this, so a caller that queued
// behind another's refresh can see that the answer has already arrived and ask
// for nothing. This mirrors how the web clients guard the same route, with a
// once-handler inside a context and a lock between them
// (packages/shared/lib/api/helpers/refreshHandlers.ts).
func (c *Client) renewSession(ctx context.Context, failed string) error {
	c.renewMu.Lock()
	defer c.renewMu.Unlock()

	if _, acc, _ := c.Tokens(); acc != failed {
		return nil
	}
	if c.adoptStoredTokens() {
		return nil
	}
	if err := c.refreshAuth(ctx); err != nil {
		// A refresh token is single-use, so losing this race is not a broken
		// session - it is somebody else's fresher one, already on disk.
		if c.adoptStoredTokens() {
			return nil
		}
		return err
	}
	c.Persist()
	return nil
}

// Decode is Do + JSON unmarshal into out (out may be nil for discard).
func (c *Client) Decode(ctx context.Context, req Request, out any) error {
	resp, err := c.Do(ctx, req)
	if err != nil {
		return err
	}
	if err := responseError(resp); err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(resp.Body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// setClientHeaders applies the identity headers every Proton request carries:
// an honest User-Agent and x-pm-appversion. Per Proton's third-party rules
// these identify the app honestly and must never impersonate an official
// client.
func (c *Client) setClientHeaders(r *http.Request) {
	r.Header.Set("User-Agent", c.ua)
	r.Header.Set("x-pm-appversion", c.app)
}

func (c *Client) doOnce(ctx context.Context, req Request) (*Response, error) {
	select {
	case c.slots <- struct{}{}:
		defer func() { <-c.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	c.mu.RLock()
	uid, acc := c.uid, c.acc
	c.mu.RUnlock()

	u := c.base + req.Path
	if len(req.Query) > 0 {
		u += "?" + req.Query.Encode()
	}

	body, err := encodeBody(req.Body)
	if err != nil {
		return nil, err
	}

	r, err := http.NewRequestWithContext(ctx, strings.ToUpper(req.Method), u, body)
	if err != nil {
		return nil, err
	}
	if req.ContentType != "" {
		r.Header.Set("Content-Type", req.ContentType)
	} else {
		r.Header.Set("Content-Type", "application/json")
	}
	c.setClientHeaders(r)
	if uid != "" {
		r.Header.Set("x-pm-uid", uid)
	}
	if acc != "" && !req.omitBearer {
		r.Header.Set("Authorization", "Bearer "+acc)
	}
	if req.HVToken != "" && req.HVType != "" {
		r.Header.Set("x-pm-human-verification-token", req.HVToken)
		r.Header.Set("x-pm-human-verification-token-type", req.HVType)
	}
	for name, value := range req.headers {
		r.Header.Set(name, value)
	}

	start := time.Now()
	resp, err := c.hc.Do(r)
	if err != nil {
		c.log.Debug("api request failed",
			"method", req.Method, "path", req.Path, "error", err,
			"duration_ms", time.Since(start).Milliseconds())
		return nil, &NetworkError{Err: err}
	}
	defer func() { _ = resp.Body.Close() }()
	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	c.log.Debug("api request",
		"method", req.Method, "path", req.Path, "status", resp.StatusCode,
		"bytes", len(buf), "duration_ms", time.Since(start).Milliseconds())
	return &Response{Status: resp.StatusCode, Body: buf, retryHeader: resp.Header.Get("Retry-After")}, nil
}

func encodeBody(b any) (io.Reader, error) {
	switch v := b.(type) {
	case nil:
		return nil, nil
	case []byte:
		return bytes.NewReader(v), nil
	case string:
		if v == "" {
			return nil, nil
		}
		return strings.NewReader(v), nil
	case io.Reader:
		return v, nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		return bytes.NewReader(raw), nil
	}
}

// retryDelay is how long to wait before asking again.
//
// The server's own Retry-After is the answer when it gives one. Without it the
// wait doubles from a floor to a ceiling, and half of it is random: requests
// refused together would otherwise return together and be refused together.
func retryDelay(retryAfter string, attempt int) time.Duration {
	if d, ok := namedDelay(retryAfter); ok {
		return d
	}
	d := backoffFloor << (attempt - 1)
	if d > backoffCeiling {
		d = backoffCeiling
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

// namedDelay parses the Retry-After header (seconds form) into a bounded delay.
func namedDelay(retryAfter string) (time.Duration, bool) {
	if retryAfter == "" {
		return 0, false
	}
	secs, err := strconv.Atoi(strings.TrimSpace(retryAfter))
	if err != nil || secs < 0 {
		return 0, false
	}
	d := time.Duration(secs) * time.Second
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	return d, true
}

// refreshAuth exchanges the refresh token for a new pair.
//
// A rate-limited refresh is waited out rather than reported: the session is fine,
// the server is busy, and answering "you are not signed in" to that would be both
// wrong and the fastest way to make a person try again immediately. A failure
// that leaves the outcome unknown is reported, because a refresh token is
// single-use: asking again with one Proton may already have replaced is asking
// to be told the session is gone.
func (c *Client) refreshAuth(ctx context.Context) error {
	c.mu.RLock()
	uid, ref := c.uid, c.ref
	c.mu.RUnlock()

	resp, err := c.attempt(ctx, Request{
		Method: "POST",
		Path:   "/auth/v4/refresh",
		Body: map[string]string{
			"GrantType":    "refresh_token",
			"RedirectURI":  "https://protonmail.ch",
			"RefreshToken": ref,
			"ResponseType": "token",
			"State":        strconv.FormatInt(time.Now().UnixNano(), 10),
			"UID":          uid,
		},
		omitBearer: true,
	})
	if err != nil {
		return err
	}
	if resp.Status != http.StatusOK {
		return fmt.Errorf("refresh returned %d: %s", resp.Status, string(resp.Body))
	}
	var result struct {
		AccessToken  string
		RefreshToken string
	}
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return err
	}
	c.mu.Lock()
	c.acc, c.ref = result.AccessToken, result.RefreshToken
	c.mu.Unlock()
	return nil
}

func httpStatusText(status int) string {
	if s := http.StatusText(status); s != "" {
		return s
	}
	return fmt.Sprintf("HTTP %d", status)
}
