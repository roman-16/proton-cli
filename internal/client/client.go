package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/roman-16/proton-cli/internal/session"
)

const (
	defaultBaseURL    = "https://mail.proton.me/api"
	defaultAppVersion = "web-account@5.0.364.0"
)

// Client is an authenticated Proton API client.
type Client struct {
	rc *resty.Client

	uid           string
	acc           string
	ref           string
	saltedKeyPass string
	mu            sync.RWMutex

	baseURL    string
	appVersion string
}

// Options configures the client.
type Options struct {
	BaseURL    string
	AppVersion string
}

// New creates a new client.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.AppVersion == "" {
		opts.AppVersion = defaultAppVersion
	}

	rc := resty.New()
	rc.SetBaseURL(opts.BaseURL)
	rc.SetHeader("Content-Type", "application/json")
	rc.SetRetryCount(3)
	rc.SetRetryMaxWaitTime(time.Minute)
	rc.AddRetryCondition(func(r *resty.Response, err error) bool {
		return r != nil && r.StatusCode() == http.StatusTooManyRequests
	})

	return &Client{
		rc:         rc,
		baseURL:    opts.BaseURL,
		appVersion: opts.AppVersion,
	}
}

// SetTokens sets the auth tokens directly (e.g. from a saved session).
func (c *Client) SetTokens(uid, acc, ref, saltedKeyPass string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.uid = uid
	c.acc = acc
	c.ref = ref
	c.saltedKeyPass = saltedKeyPass
}

// SaltedKeyPass returns the cached salted key password.
func (c *Client) SaltedKeyPass() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.saltedKeyPass
}

// SetSaltedKeyPass stores the salted key password.
func (c *Client) SetSaltedKeyPass(skp string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.saltedKeyPass = skp
}

// Session returns the current auth state for persistence.
func (c *Client) Session() *session.Session {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &session.Session{
		UID:           c.uid,
		AccessToken:   c.acc,
		RefreshToken:  c.ref,
		SaltedKeyPass: c.saltedKeyPass,
		AppVersion:    c.appVersion,
		BaseURL:       c.baseURL,
	}
}

// HumanVerificationError is returned when the API requires human verification.
type HumanVerificationError struct {
	Token   string
	Methods []string
	WebURL  string
}

func (e *HumanVerificationError) Error() string {
	return fmt.Sprintf("human verification required: %s", e.WebURL)
}

// ErrUnauthorized signals that auth failed and refresh also failed.
var ErrUnauthorized = fmt.Errorf("unauthorized: session expired")

// Do executes an authenticated API request and returns the raw JSON response body.
// Automatically refreshes the token on 401 and saves updated tokens.
func (c *Client) Do(ctx context.Context, method, path string, query map[string]string, body string, hvToken string, hvTokenType string) ([]byte, int, error) {
	respBody, statusCode, err := c.doOnce(ctx, method, path, query, body, hvToken, hvTokenType)
	if err != nil {
		return nil, 0, err
	}

	// Handle 401 — try token refresh then retry.
	if statusCode == http.StatusUnauthorized {
		if refreshErr := c.refreshAuth(ctx); refreshErr != nil {
			return respBody, statusCode, ErrUnauthorized
		}
		// Save refreshed tokens.
		session.Save(c.Session())

		respBody, statusCode, err = c.doOnce(ctx, method, path, query, body, hvToken, hvTokenType)
		if err != nil {
			return nil, 0, err
		}
	}

	// Check for human verification (code 9001).
	var apiResp struct {
		Code    int
		Details *struct {
			HumanVerificationToken   string
			HumanVerificationMethods []string
			WebUrl                   string
		}
	}
	if json.Unmarshal(respBody, &apiResp) == nil && apiResp.Code == 9001 && apiResp.Details != nil {
		return respBody, statusCode, &HumanVerificationError{
			Token:   apiResp.Details.HumanVerificationToken,
			Methods: apiResp.Details.HumanVerificationMethods,
			WebURL:  apiResp.Details.WebUrl,
		}
	}

	return respBody, statusCode, nil
}

func (c *Client) doOnce(ctx context.Context, method, path string, query map[string]string, body string, hvToken string, hvTokenType string) ([]byte, int, error) {
	c.mu.RLock()
	uid := c.uid
	acc := c.acc
	c.mu.RUnlock()

	r := c.rc.R().
		SetContext(ctx).
		SetHeader("x-pm-uid", uid).
		SetHeader("x-pm-appversion", c.appVersion).
		SetAuthToken(acc)

	if hvToken != "" && hvTokenType != "" {
		r.SetHeader("x-pm-human-verification-token", hvToken).
			SetHeader("x-pm-human-verification-token-type", hvTokenType)
	}

	if len(query) > 0 {
		r.SetQueryParams(query)
	}

	if body != "" {
		r.SetBody(body)
	}

	var resp *resty.Response
	var err error

	switch strings.ToUpper(method) {
	case "GET":
		resp, err = r.Get(path)
	case "POST":
		resp, err = r.Post(path)
	case "PUT":
		resp, err = r.Put(path)
	case "DELETE":
		resp, err = r.Delete(path)
	case "PATCH":
		resp, err = r.Patch(path)
	default:
		return nil, 0, fmt.Errorf("unsupported HTTP method: %s", method)
	}

	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}

	return resp.Body(), resp.StatusCode(), nil
}

// refreshAuth refreshes the access token using the refresh token.
func (c *Client) refreshAuth(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	reqBody, err := json.Marshal(map[string]string{
		"UID":          c.uid,
		"RefreshToken": c.ref,
		"ResponseType": "token",
		"GrantType":    "refresh_token",
		"RedirectURI":  "https://protonmail.ch",
		"State":        fmt.Sprintf("%d", time.Now().UnixNano()),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/auth/v4/refresh", strings.NewReader(string(reqBody)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-pm-uid", c.uid)
	req.Header.Set("x-pm-appversion", c.appVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("refresh returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AccessToken  string
		RefreshToken string
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return err
	}

	c.acc = result.AccessToken
	c.ref = result.RefreshToken

	return nil
}
