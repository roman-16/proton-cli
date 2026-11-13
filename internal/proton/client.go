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

	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/units"
)

const (
	DefaultBaseURL    = "https://mail.proton.me/api"
	DefaultAppVersion = "Other"
	DefaultUserAgent  = "proton-cli/dev"

	maxRetryAfter = 30 * time.Second

	sessionRenewals = 3

	transientWaits = 4

	notAnswering = "Proton is not answering"
	rateLimited  = "Proton is rate limiting this run"

	maxInFlight = 8
)

var (
	backoffFloor   = 500 * time.Millisecond
	backoffCeiling = 8 * time.Second
)

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

	renewMu sync.Mutex

	linkMu sync.Mutex

	mu            sync.RWMutex
	uid           string
	acc           string
	ref           string
	offset        time.Duration // this machine's clock against Proton's, from the last response
	link          *linkSession
	encKeyBlob    string
	profile       profile.Name
	dryRun        bool
	hvResolver    HVResolver
	scopeResolver ScopeResolver
	secondFactor  SecondFactorFunc
	store         Store
	savedUID      string
	sessionGuard  func() error
}

type Store interface {
	Hold(ctx context.Context) (release func(), err error)
	Saved() (SavedTokens, error)
	Save() error
}

type SavedTokens struct {
	UID     string
	Access  string
	Refresh string
}

type Options struct {
	BaseURL    string
	AppVersion string
	UserAgent  string
	Profile    profile.Name
	HTTPClient *http.Client
	Logger     *slog.Logger
	DryRun     bool
}

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
	hc := &http.Client{Timeout: 5 * time.Minute}
	if opts.HTTPClient != nil {
		given := *opts.HTTPClient
		hc = &given
	}
	hc.CheckRedirect = SameOrigin
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

var ErrDryRun = errors.New("refusing to change anything under --dry-run")

func (c *Client) dryRunRefuses(req Request) error {
	if !c.dryRun || req.Reads || readOnlyMethod(req.Method) {
		return nil
	}
	return fmt.Errorf("%w: %s %s", ErrDryRun, strings.ToUpper(req.Method), req.Path)
}

func readOnlyMethod(method string) bool {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}

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

func (c *Client) Tokens() (uid, acc, ref string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uid, c.acc, c.ref
}

func (c *Client) AppVersion() string { return c.app }

func (c *Client) BaseURL() string { return c.base }

func (c *Client) SetStore(store Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = store
}

func (c *Client) Resume(saved SavedTokens) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uid, c.acc, c.ref = saved.UID, saved.Access, saved.Refresh
	c.savedUID = saved.UID
}

func (c *Client) Establish(ctx context.Context) error {
	store := c.sessionStore()
	if store == nil {
		return nil
	}
	release := c.hold(ctx, store)
	defer release()
	if err := store.Save(); err != nil {
		return err
	}
	c.mu.Lock()
	c.savedUID = c.uid
	c.mu.Unlock()
	return nil
}

func (c *Client) Persist(ctx context.Context) error {
	store := c.sessionStore()
	if store == nil || !c.sessionSaved() {
		return nil
	}
	release := c.hold(ctx, store)
	defer release()
	if c.reconcile(ctx, store) == standingGone {
		return c.expired()
	}
	return store.Save()
}

func (c *Client) sessionStore() Store {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.store
}

func (c *Client) sessionSaved() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.uid != "" && c.uid == c.savedUID
}

func (c *Client) hold(ctx context.Context, store Store) func() {
	release, err := store.Hold(ctx)
	if err != nil {
		// Recorded and not counted: the session is changed without the lock, and
		// a save that fails for the same reason says so on screen.
		c.log.DebugContext(ctx, "the session is changed without its lock", "error", err.Error())
		return func() {}
	}
	return release
}

type standing int

const (
	standingSame standing = iota
	standingNewer
	standingGone
)

func (c *Client) reconcile(ctx context.Context, store Store) standing {
	saved, err := store.Saved()
	if err != nil {
		// Recorded and not counted: this run carries on with the session it
		// holds, and a save that fails for the same reason says so on screen.
		c.log.DebugContext(ctx, "the saved session could not be read", "error", err.Error())
		return standingSame
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case saved.UID != c.uid:
		return standingGone
	case saved.Access != c.acc:
		c.acc, c.ref = saved.Access, saved.Refresh
		c.log.DebugContext(ctx, "adopted a session refreshed elsewhere")
		return standingNewer
	}
	return standingSame
}

func (c *Client) expired() error { return unauthorized{profile: c.profile} }

func (c *Client) SetSessionGuard(fn func() error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionGuard = fn
}

type Request struct {
	Method string
	Path   string
	Query  url.Values
	Body   any

	ContentType string

	HVToken string
	HVType  string

	// Proton's API is one gateway behind several hosts, and not every host routes
	// every path: the recovery phrase answers only at account.proton.me, Meet's
	// listing only at meet.proton.me, and both are Path not found elsewhere.
	Door Door

	// Proves says that this endpoint wants the account password proved inside the
	// request itself, rather than answered for beforehand.
	//
	// Proton guards the credentials two ways. Most of the endpoints that change one
	// refuse an unelevated session and are answered by elevating and asking again,
	// which is scope.go and which no caller has to know about. A few take the SRP
	// proof in their own body instead - turning the authenticator app off, dropping
	// the recovery phrase - and there is nothing in the answer to those to react to:
	// the proof has to be there the first time. Which endpoints those are is
	// Proton's to decide, so the caller that names one says so.
	//
	// Body is merged into the proof, so it has to be a map.
	Proves bool

	// Reads says that this request changes nothing, whatever its method says.
	//
	// Proton answers some questions with a POST - whether a name is free is one -
	// and a preview that could not ask them could not say what the command would
	// do. It is a claim about the endpoint, so it belongs to the caller that knows
	// which one it is naming.
	Reads bool

	Repeatable bool

	// Transient are the answers this endpoint gives that mean "not now", which are
	// waited out rather than reported.
	//
	// Proton does not always ask for room with a 429. Some endpoints turn a request
	// away with a status that everywhere else means the request itself was wrong,
	// and the only way through is to ask again. Which endpoints do that, and how
	// long it is worth asking, is a fact about the endpoint, so it belongs to the
	// caller that knows which one it is naming.
	Transient []Refusal

	elevated bool

	headers map[string]string

	omitBearer bool

	opensLink bool

	anonymous bool
}

func (r Request) authorisedWithoutASession() bool { return r.opensLink || r.anonymous }

type Refusal struct {
	Status int
	Code   int
	Within time.Duration
	Reason string
}

func (r Refusal) matches(resp *Response) bool {
	return resp != nil && resp.Status == r.Status && (r.Code == 0 || r.Code == errorCode(resp.Body))
}

type credential struct {
	uid   string
	token string
	link  bool
}

func (c credential) nobody() bool { return c.uid == "" && c.token == "" }

func (c *Client) credentialFor(req Request) credential {
	if req.anonymous {
		return credential{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.link != nil && !req.opensLink && c.link.covers(req) {
		return credential{uid: c.link.uid, token: c.link.access, link: true}
	}
	return credential{uid: c.uid, token: c.acc}
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r *Response) retryAfter() string {
	if r == nil {
		return ""
	}
	return r.Header.Get("Retry-After")
}

func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	if err := c.guardRefuses(req); err != nil {
		return nil, err
	}
	if err := c.dryRunRefuses(req); err != nil {
		return nil, err
	}
	if req.Proves {
		return c.prove(ctx, req)
	}
	resp, err := c.send(ctx, req)
	if err != nil {
		return nil, err
	}

	if resp.Status >= 200 && resp.Status < 300 {
		return resp, nil
	}

	if isMissingScope(resp.Status, resp.Body) {
		return c.elevateAndRetry(ctx, req, resp)
	}

	hvErr, apiErr := classifyErrorBody(resp.Status, resp.Body)
	if hvErr != nil {
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
			case rerr != nil:
				return resp, rerr
			}
		}
		return resp, hvErr
	}
	return resp, apiErr
}

func (c *Client) doorFor(req Request) string {
	subdomain, ok := doorSubdomains[req.Door]
	if !ok {
		return c.base
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return c.base
	}
	host, ok := siblingHost(u.Hostname(), subdomain)
	if !ok {
		return c.base
	}
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	u.Host = host
	return u.String()
}

type Door int

const (
	SessionDoor Door = iota
	AccountDoor
	MeetDoor
)

var doorSubdomains = map[Door]string{AccountDoor: "account", MeetDoor: "meet"}

func (c *Client) guardRefuses(req Request) error {
	if !c.credentialFor(req).nobody() || req.authorisedWithoutASession() {
		return nil
	}
	c.mu.RLock()
	guard := c.sessionGuard
	c.mu.RUnlock()
	if guard == nil {
		return nil
	}
	return guard()
}

func (c *Client) send(ctx context.Context, req Request) (*Response, error) {
	for renewals := 0; ; {
		used := c.credentialFor(req)

		resp, err := c.attempt(ctx, req)
		if err != nil {
			return nil, err
		}
		if resp.Status == http.StatusUnauthorized && tokenRejected(resp) &&
			!used.nobody() && renewals < sessionRenewals {
			renewals++
			if rerr := c.renew(ctx, used); rerr != nil {
				return resp, rerr
			}
			continue
		}
		return resp, nil
	}
}

func (c *Client) renew(ctx context.Context, used credential) error {
	if used.link {
		return c.reproveLink(ctx, used)
	}
	return c.renewSession(ctx, used.token)
}

func (c *Client) attempt(ctx context.Context, req Request) (*Response, error) {
	return c.retrying(ctx, req, func() (*Response, error) { return c.doOnce(ctx, req) })
}

func (c *Client) retrying(ctx context.Context, req Request, once func() (*Response, error)) (*Response, error) {
	started := time.Now()
	for attempt := 1; ; attempt++ {
		resp, err := once()
		if resp == nil && err == nil {
			return nil, nil
		}
		pause, again := worthWaiting(req, resp, err)
		if !again || pause.over(attempt, time.Since(started)) {
			return resp, err
		}
		delay := retryDelay(resp.retryAfter(), attempt)
		switch {
		case err != nil:
			c.log.Warn(waiting(pause.reason, delay), "method", req.Method, "path", req.Path,
				"error", err, "wait_ms", delay.Milliseconds(), "attempt", attempt)
		case pause.code != 0:
			c.log.Warn(waiting(pause.reason, delay), "method", req.Method, "path", req.Path,
				"status", resp.Status, "code", pause.code,
				"wait_ms", delay.Milliseconds(), "attempt", attempt)
		default:
			c.log.Warn(waiting(pause.reason, delay), "method", req.Method, "path", req.Path,
				"status", resp.Status, "wait_ms", delay.Milliseconds(), "attempt", attempt)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

type pause struct {
	reason string
	code   int
	waits  int
	within time.Duration
}

func (p pause) over(attempt int, elapsed time.Duration) bool {
	if p.within > 0 {
		return elapsed >= p.within
	}
	return attempt > p.waits
}

func worthWaiting(req Request, resp *Response, err error) (pause, bool) {
	if err != nil {
		if !repeatable(req) || !worthRepeating(err) {
			return pause{}, false
		}
		return pause{reason: notAnswering, waits: transientWaits}, true
	}
	for _, refusal := range req.Transient {
		if refusal.matches(resp) {
			return pause{reason: refusal.Reason, code: refusal.Code, within: refusal.Within}, true
		}
	}
	switch {
	case resp.Status == http.StatusTooManyRequests:
		return pause{reason: rateLimited, waits: transientWaits}, true
	case resp.Status >= 500 && repeatable(req):
		return pause{reason: notAnswering, waits: transientWaits}, true
	}
	return pause{}, false
}

func waiting(reason string, d time.Duration) string {
	return fmt.Sprintf("%s; trying again in %s.", reason, units.Duration(max(d.Round(time.Second), time.Second)))
}

func repeatable(req Request) bool {
	return req.Repeatable || readOnlyMethod(req.Method)
}

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

func (c *Client) renewSession(ctx context.Context, failed string) error {
	c.renewMu.Lock()
	defer c.renewMu.Unlock()

	if _, acc, _ := c.Tokens(); acc != failed {
		return nil
	}
	store := c.sessionStore()
	if store == nil || !c.sessionSaved() {
		if err := c.refreshAuth(ctx); err != nil {
			return c.sessionRefused(err)
		}
		return nil
	}
	release := c.hold(ctx, store)
	defer release()
	switch c.reconcile(ctx, store) {
	case standingNewer:
		return nil
	case standingGone:
		return c.expired()
	}
	if err := c.refreshAuth(ctx); err != nil {
		return c.sessionRefused(err)
	}
	if err := store.Save(); err != nil {
		c.log.WarnContext(ctx, fmt.Sprintf(
			"The renewed session could not be saved (%v), so the next command will have to sign in again.", err))
	}
	return nil
}

// sessionRefused separates Proton ruling a session over from a refresh that
// never got an answer.
//
// The difference is the whole of what a scheduled job branches on: one is fixed
// by signing in again and nothing else, the other by coming back later. A
// connection that failed, an edge that broke and a rate limit still standing
// after the waits all say nothing about the session, and reporting them as an
// expired one sends somebody to re-enter a credential that is fine. Mirrors
// SESSION_INVALID_REFRESH_STATUSES in WebClients
// (packages/shared/lib/api/helpers/refreshHandlers.ts).
func (c *Client) sessionRefused(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.HTTPStatus {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusUnprocessableEntity:
		return c.expired()
	}
	return err
}

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

	cred := c.credentialFor(req)

	u := c.doorFor(req) + req.Path
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
	if cred.uid != "" {
		r.Header.Set("x-pm-uid", cred.uid)
	}
	if cred.token != "" && !req.omitBearer {
		r.Header.Set("Authorization", "Bearer "+cred.token)
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
	c.observeTime(resp.Header.Get("Date"))
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: buf}, nil
}

func (c *Client) observeTime(header string) {
	if header == "" {
		c.log.Debug("api clock", "reason", "undated")
		return
	}
	served, err := http.ParseTime(header)
	if err != nil {
		c.log.Debug("api clock", "reason", "unreadable", "error", err)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.offset = time.Until(served)
}

// Now is the time as Proton keeps it, and this machine's until a response has
// said otherwise.
//
// It is what everything this build writes a date into is dated by. A key is the
// reason: Proton refuses one dated in the future, and a signature made under a
// key is read against the key's own creation time, so a machine whose clock
// runs fast would write keys nobody accepts and signatures nothing verifies.
func (c *Client) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().Add(c.offset)
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

func retryDelay(retryAfter string, attempt int) time.Duration {
	if d, ok := namedDelay(retryAfter); ok {
		return d
	}
	d := backoffFloor
	for i := 1; i < attempt && d < backoffCeiling; i++ {
		d *= 2
	}
	if d > backoffCeiling {
		d = backoffCeiling
	}
	return d/2 + time.Duration(rand.Int64N(int64(d/2)+1))
}

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
	if err := responseError(resp); err != nil {
		return err
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
