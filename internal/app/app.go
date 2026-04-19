// Package app wires the Proton services, renderer and session together for
// the CLI. One App instance per invocation.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/roman-16/proton-cli/internal/api"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/keys"
	"github.com/roman-16/proton-cli/internal/render"
	"github.com/roman-16/proton-cli/internal/services/calendar"
	"github.com/roman-16/proton-cli/internal/services/contacts"
	"github.com/roman-16/proton-cli/internal/services/drive"
	"github.com/roman-16/proton-cli/internal/services/mail"
	"github.com/roman-16/proton-cli/internal/services/pass"
	"github.com/roman-16/proton-cli/internal/session"
)

// Credentials is the resolved set of auth material for this invocation.
type Credentials struct {
	User     string
	Password string
	TOTP     string
}

// App is the runtime container shared across commands.
type App struct {
	Profile string
	Creds   Credentials

	API *api.Client

	Mail     *mail.Service
	Drive    *drive.Service
	Calendar *calendar.Service
	Contacts *contacts.Service
	Pass     *pass.Service

	R *render.Renderer

	DryRun bool

	mu    sync.Mutex
	cache *keys.Unlocked
}

// Options configures New.
type Options struct {
	Profile    string
	User       string
	Password   string
	TOTP       string
	APIURL     string
	AppVersion string
	Output     render.Format
	LogLevel   slog.Level
	Quiet      bool
	DryRun     bool
}

// New constructs an App: loads the config, resolves the profile, installs any
// saved session, and wires up the services.
func New(opts Options) (*App, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	profileName, prof := cfg.Resolve(opts.Profile)

	user := firstNonEmpty(opts.User, os.Getenv("PROTON_USER"), prof.User)
	password := firstNonEmpty(opts.Password, os.Getenv("PROTON_PASSWORD"), prof.Password)
	totp := firstNonEmpty(opts.TOTP, os.Getenv("PROTON_TOTP"), prof.TOTP)
	apiURL := firstNonEmpty(opts.APIURL, os.Getenv("PROTON_API_URL"), prof.APIURL)
	appVer := firstNonEmpty(opts.AppVersion, os.Getenv("PROTON_APP_VERSION"), prof.AppVersion)

	r := render.New(opts.Output, os.Stdout, os.Stderr, opts.LogLevel, opts.Quiet)
	c := api.New(api.Options{
		BaseURL: apiURL, AppVersion: appVer, Profile: profileName,
		Logger: r.Log,
	})

	// Install a saved session for this profile if we have one.
	if sess, err := session.Load(profileName); err == nil && sess != nil {
		c.SetTokens(sess.UID, sess.AccessToken, sess.RefreshToken, sess.SaltedKeyPass)
	}

	return &App{
		Profile:  profileName,
		Creds:    Credentials{User: user, Password: password, TOTP: totp},
		API:      c,
		Mail:     mail.New(c),
		Drive:    drive.New(c),
		Calendar: calendar.New(c),
		Contacts: contacts.New(c),
		Pass:     pass.New(c),
		R:        r,
		DryRun:   opts.DryRun,
	}, nil
}

// Authenticate ensures the client has valid tokens, logging in if needed.
func (a *App) Authenticate(ctx context.Context) error {
	// If we already have a session, just trust it; Do will refresh on 401.
	if a.API.Session().UID != "" {
		return nil
	}
	if a.Creds.User == "" {
		return fmt.Errorf("user is required (set --user, PROTON_USER, or configure a profile)")
	}
	if a.Creds.Password == "" {
		return fmt.Errorf("password is required (set --password, PROTON_PASSWORD, or configure a profile)")
	}
	a.R.Info(fmt.Sprintf("Authenticating as %s...", a.Creds.User))
	if err := a.API.Login(ctx, a.Creds.User, []byte(a.Creds.Password), a.Creds.TOTP); err != nil {
		return err
	}
	a.R.Success("Authenticated.")
	return session.Save(a.Profile, a.API.Session())
}

// Unlock returns the unlocked keys for this session, computing them on first
// call and caching subsequently.
func (a *App) Unlock(ctx context.Context) (*keys.Unlocked, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache != nil {
		return a.cache, nil
	}
	u, err := keys.Unlock(ctx, a.API, a.Creds.Password)
	if err != nil {
		return nil, err
	}
	a.cache = u
	return u, nil
}

// ClearSession wipes the session file for the current profile.
func (a *App) ClearSession() error {
	return session.Clear(a.Profile)
}

// ExitError signals an exit with a specific code.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }
func (e *ExitError) Unwrap() error { return e.Err }

// Exit wraps err with a specific exit code.
func Exit(code int, err error) error {
	if err == nil {
		return nil
	}
	return &ExitError{Code: code, Err: err}
}

// ExitCodeFor classifies an error into one of the CLI's exit codes.
func ExitCodeFor(err error) int {
	if err == nil {
		return 0
	}
	var ee *ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	var apiErr *api.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.HTTPStatus {
		case 401, 403:
			return 2
		case 404:
			return 3
		case 409, 422:
			return 4
		}
		if apiErr.HTTPStatus >= 500 {
			return 5
		}
	}
	if errors.Is(err, api.ErrUnauthorized) {
		return 2
	}
	return 1
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
