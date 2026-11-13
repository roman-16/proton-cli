package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/roman-16/proton-cli/internal/account/fork"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/account/localkey"
	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/idcache"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/runlog"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/service/meet"
	"github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

type App struct {
	Profile profile.Name
	Creds   *Credentials

	API *proton.Client

	Account  *account.Service
	Mail     *mail.Service
	Drive    *drive.Service
	Calendar *calendar.Service
	Meet     *meet.Service
	Contacts *contacts.Service
	Pass     *pass.Service

	Index *search.Store

	UI *ui.UI

	DryRun        bool
	FullIDs       bool
	Yes           bool
	Confirm       confirm.Policy
	NoUpdateCheck bool
	Settings      config.Source

	IDCache *idcache.Cache

	Verified string

	run     *runlog.Run
	started time.Time
	version string

	zone  zoneCache
	mu    sync.Mutex
	cache *keys.Unlocked

	stdinMu    sync.Mutex
	stdinClaim string

	sessionMu  sync.Mutex
	userID     string
	email      string
	sessionErr error
	foreign    bool
}

type Options struct {
	config.Resolved
	APIURL   string
	Version  string
	DryRun   bool
	Yes      bool
	Verified string
}

func New(opts Options) (*App, error) {
	profileName := opts.Profile

	apiURL := firstNonEmpty(opts.APIURL, os.Getenv("PROTON_API_URL"))
	verified := firstNonEmpty(opts.Verified, os.Getenv("PROTON_VERIFIED"))
	userAgent := defaultUserAgent(opts.Version)

	run, salt := openLog(opts.NoLog)
	var logFile io.Writer
	runID := ""
	if run != nil {
		logFile, runID = run.Writer(), run.ID
	}
	u := ui.New(ui.Options{
		Format:   opts.Output,
		LogLevel: opts.LogLevel,
		Quiet:    opts.Quiet,
		Color:    opts.Color,
		NoInput:  opts.NoInput,
		FullIDs:  opts.FullIDs,
		Log:      logFile,
		Run:      runID,
		Salt:     salt,
	})
	slog.SetDefault(u.Log)
	c := proton.New(proton.Options{
		BaseURL: apiURL, Logger: u.Log, Profile: profileName,
		UserAgent: userAgent, DryRun: opts.DryRun,
	})

	var userID, email string
	sess, sessionErr := session.Load(profileName)
	foreign := false
	switch {
	case sessionErr != nil:
		sessionErr = unreadableSession(profileName, sessionErr)
	case sess == nil:
	case !sameAPI(sess.BaseURL, c.BaseURL()):
		sessionErr, foreign = foreignSession(profileName, sess.BaseURL, c.BaseURL()), true
	default:
		c.Resume(proton.SavedTokens{UID: sess.UID, Access: sess.AccessToken, Refresh: sess.RefreshToken})
		c.SetEncKeyBlob(sess.EncKeyBlob)
		userID, email = sess.UserID, sess.Email
	}

	a := &App{
		Profile:       profileName,
		run:           run,
		version:       opts.Version,
		Creds:         newCredentials(u, email),
		API:           c,
		UI:            u,
		DryRun:        opts.DryRun,
		FullIDs:       opts.FullIDs,
		Yes:           opts.Yes,
		Confirm:       opts.Confirm,
		NoUpdateCheck: opts.NoUpdateCheck,
		Settings:      opts.Source,
		Verified:      verified,
		IDCache:       Seen(profileName),
		userID:        userID,
		email:         email,
		sessionErr:    sessionErr,
		foreign:       foreign,
	}
	a.zone.name = opts.Zone
	adoptZone(opts.Zone)
	a.Index = search.New(indexDir(profileName), a.UserID, a.indexKeys)
	a.Account = account.New(c, a.Unlock)
	a.Mail = mail.New(c, a.Unlock)
	a.Mail.SetIndex(a.Index)
	a.Drive = drive.New(c, a.Unlock)
	a.Drive.SetIndex(a.Index)
	a.Calendar = calendar.New(c, a.Unlock)
	a.Calendar.SetIndex(a.Index)
	a.Meet = meet.New(c, a.Unlock)
	a.Calendar.SetMeetings(a.Meet)
	a.Contacts = contacts.New(c, a.Unlock)
	a.Pass = pass.New(c, a.Unlock)
	c.SetStore(profileStore{a})
	c.SetSessionGuard(func() error { return a.Authenticate(context.Background()) })
	a.Creds.stdinOwner = a.Stdin
	a.installScopeResolver()
	a.installSecondFactorResolver()
	return a, nil
}

func (a *App) Stdin(claim string) (io.Reader, error) {
	a.stdinMu.Lock()
	defer a.stdinMu.Unlock()
	if a.stdinClaim != "" {
		return nil, errs.Problemf("%s and %s both read standard input, which can only be read once.",
			a.stdinClaim, claim).
			Hint(elsewhere(a.stdinClaim, claim))
	}
	a.stdinClaim = claim
	if a.UI.InIsTTY() {
		a.UI.Instruct(fmt.Sprintf("Reading %s from the terminal. Finish with %s.",
			strings.TrimSuffix(claim, " -"), ui.EOFKey))
	}
	return a.UI.In, nil
}

func elsewhere(claims ...string) string {
	for _, claim := range claims {
		flag, _, ok := strings.Cut(claim, " ")
		if ok && strings.HasSuffix(flag, "-file") {
			return "pass it with " + flag + " FILE instead"
		}
	}
	return "read one of them from a path rather than -"
}

type profileStore struct{ a *App }

func (s profileStore) Hold(ctx context.Context) (func(), error) {
	return session.Hold(ctx, s.a.Profile)
}

func (s profileStore) Saved() (proton.SavedTokens, error) {
	sess, err := session.Load(s.a.Profile)
	if err != nil || sess == nil {
		return proton.SavedTokens{}, err
	}
	return proton.SavedTokens{UID: sess.UID, Access: sess.AccessToken, Refresh: sess.RefreshToken}, nil
}

func (s profileStore) Save() error {
	uid, acc, refresh := s.a.API.Tokens()
	s.a.sessionMu.Lock()
	defer s.a.sessionMu.Unlock()
	return session.Save(s.a.Profile, &session.Session{
		UID:          uid,
		AccessToken:  acc,
		RefreshToken: refresh,
		UserID:       s.a.userID,
		Email:        s.a.email,
		EncKeyBlob:   s.a.API.EncKeyBlob(),
		BaseURL:      s.a.API.BaseURL(),
	})
}

func sameAPI(a, b string) bool {
	ua, errA := url.Parse(a)
	ub, errB := url.Parse(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host) &&
		strings.TrimSuffix(ua.Path, "/") == strings.TrimSuffix(ub.Path, "/")
}

func unreadableSession(name profile.Name, err error) error {
	if errors.Is(err, session.ErrDamaged) {
		if name.IsDefault() {
			return errs.Problemf("Your saved session is damaged.").Hint(name.SignIn()).Exit(2)
		}
		return errs.Problemf("The saved session of profile %q is damaged.", name).Hint(name.SignIn()).Exit(2)
	}
	if name.IsDefault() {
		return errs.Problemf("Your saved session could not be read: %v", err).Exit(2)
	}
	return errs.Problemf("The saved session of profile %q could not be read: %v", name, err).Exit(2)
}

func foreignSession(name profile.Name, saved, asked string) error {
	hint := "proton --profile NAME account login"
	if name.IsDefault() {
		return errs.Problemf("Your saved session was signed in at %s, not %s.", saved, asked).Hint(hint).Exit(2)
	}
	return errs.Problemf("The saved session of profile %q was signed in at %s, not %s.", name, saved, asked).
		Hint(hint).Exit(2)
}

func (a *App) rememberIdentity(userID, email string) {
	a.sessionMu.Lock()
	a.userID, a.email = userID, email
	a.sessionMu.Unlock()
}

func Seen(name profile.Name) *idcache.Cache { return idcache.New(idCachePath(name)) }

func idCachePath(name profile.Name) string {
	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "idcache", name.FileName(".json"))
}

func (a *App) Forget(ctx context.Context, name profile.Name, local bool) error {
	if err := a.SignOut(ctx, name, local); err != nil {
		return err
	}
	if err := session.DropLock(name); err != nil {
		return err
	}
	if err := Seen(name).Clear(); err != nil {
		return err
	}
	if err := os.RemoveAll(indexDir(name)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (a *App) SignOut(ctx context.Context, name profile.Name, local bool) error {
	release, err := session.Hold(ctx, name)
	if err != nil {
		return err
	}
	defer release()
	if !local {
		if err := a.revokeSaved(ctx, name); err != nil {
			return err
		}
	}
	return session.Clear(name)
}

type RevokeError struct {
	Profile profile.Name
	Err     error
}

func (e *RevokeError) Error() string {
	return fmt.Sprintf("revoke the session of profile %q: %v", e.Profile, e.Err)
}

func (e *RevokeError) Unwrap() error { return e.Err }

func (a *App) revokeSaved(ctx context.Context, name profile.Name) error {
	sess, err := session.Load(name)
	if errors.Is(err, session.ErrDamaged) {
		// Recorded and not counted: a damaged file holds no session to revoke,
		// and discarding it is what signing out asks for.
		slog.DebugContext(ctx, "a damaged session was discarded without revoking", "error", err.Error())
		return nil
	}
	if err != nil {
		return err
	}
	if sess == nil {
		return nil
	}
	c := proton.New(proton.Options{
		BaseURL: sess.BaseURL, Profile: name, Logger: a.UI.Log,
		UserAgent: defaultUserAgent(a.version),
	})
	c.SetTokens(sess.UID, sess.AccessToken, sess.RefreshToken)
	err = c.Revoke(ctx)
	if err == nil || errors.Is(err, proton.ErrUnauthorized) {
		return nil
	}
	return &RevokeError{Profile: name, Err: err}
}

func indexDir(name profile.Name) string {
	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "index", name.FileName(""))
}

func (a *App) indexKeys(ctx context.Context) (search.Keys, error) {
	u, err := a.Unlock(ctx)
	if err != nil {
		return search.Keys{}, err
	}
	primary, err := u.PrimaryUserKey()
	if err != nil {
		return search.Keys{}, err
	}
	return search.Keys{Seal: primary, Open: u.UserKR}, nil
}

func (a *App) UserID() string { return a.userID }

func (a *App) Email() string { return a.email }

func (a *App) SignedIn() bool {
	uid, _, _ := a.API.Tokens()
	return uid != ""
}

func (a *App) Authenticate(context.Context) error {
	if a.SignedIn() {
		return nil
	}
	if a.sessionErr != nil {
		return a.sessionErr
	}
	if a.Profile.IsDefault() {
		return errs.Problemf("You are not signed in.").Hint(a.Profile.SignIn()).Exit(2)
	}
	return errs.Problemf("Profile %q is not signed in.", a.Profile).Hint(a.Profile.SignIn()).Exit(2)
}

func (a *App) Login(ctx context.Context, user string) error {
	if resumed, err := a.resume(ctx, user); resumed || err != nil {
		return err
	}
	if user == "" {
		var err error
		if user, err = a.Creds.User(); err != nil {
			return err
		}
	}
	password, err := a.Creds.Password("sign in")
	if err != nil {
		return err
	}
	if err := a.API.Login(ctx, user, []byte(password)); err != nil {
		return err
	}
	if err := a.attach(ctx); err != nil {
		return err
	}
	return a.settle(ctx)
}

func (a *App) resume(ctx context.Context, user string) (bool, error) {
	if a.foreign {
		return true, a.sessionErr
	}
	if !a.SignedIn() {
		return false, nil
	}
	acct, err := a.Account.Get(ctx)
	if err != nil {
		slog.DebugContext(ctx, "the saved session did not answer, signing in again", "error", err.Error())
		return false, nil
	}
	if user != "" && !acct.AnswersTo(user) {
		slog.DebugContext(ctx, "sign-in refused", "reason", "not an address of the saved account")
		return true, a.refuseAnotherAccount()
	}
	a.rememberIdentity(acct.ID, acct.Email)
	return true, a.settle(ctx)
}

func (a *App) attach(ctx context.Context) error {
	acct, err := a.Account.Get(ctx)
	if err != nil {
		return err
	}
	if a.userID != "" && acct.ID != a.userID {
		slog.DebugContext(ctx, "sign-in refused", "reason", "another account")
		if err := a.API.Revoke(ctx); err != nil {
			slog.DebugContext(ctx, "the refused session could not be revoked", "error", err.Error())
		}
		return a.refuseAnotherAccount()
	}
	a.rememberIdentity(acct.ID, acct.Email)
	return a.API.Establish(ctx)
}

func (a *App) settle(ctx context.Context) error {
	if _, err := a.Unlock(ctx); err != nil {
		return err
	}
	if err := a.unlockPass(ctx); err != nil {
		return err
	}
	return a.API.Establish(ctx)
}

// forkPoll is how often an unapproved fork is asked about again, and forkLife is
// how long it is worth asking for.
//
// Both are what Proton's own sign-in screen uses
// (packages/account/signInWithAnotherDevice/signInWithAnotherDevicePull.ts): it
// asks every three seconds and throws its code away after nine minutes. A code
// nobody has approved in nine minutes is a code somebody walked away from, and
// asking for ever would be a run that never ends.
const (
	forkPoll = 3 * time.Second
	forkLife = 9 * time.Minute
)

func (a *App) LoginQR(ctx context.Context, show func(code string) error) error {
	if resumed, err := a.resume(ctx, ""); resumed || err != nil {
		return err
	}
	if err := a.API.AnonymousSession(ctx); err != nil {
		return err
	}
	opened, err := a.API.NewFork(ctx)
	if err != nil {
		return err
	}
	code, err := fork.New(opened.UserCode, a.API.AppVersion())
	if err != nil {
		return err
	}
	if err := show(code.String()); err != nil {
		return err
	}
	approved, err := a.awaitFork(ctx, opened.Selector)
	if err != nil {
		return err
	}
	keyPass, err := code.Open(approved.Payload)
	if err != nil {
		return err
	}
	a.API.AdoptSession(approved.UID, approved.AccessToken, approved.RefreshToken)
	if err := a.attach(ctx); err != nil {
		return err
	}
	if keyPass != "" {
		if err := keys.Seal(ctx, a.API, keyPass); err != nil {
			return err
		}
	}
	return a.settle(ctx)
}

func (a *App) AccessInto(ctx context.Context, target profile.Name, sess account.AccessSession) (string, error) {
	c := proton.New(proton.Options{
		BaseURL: a.API.BaseURL(), AppVersion: a.API.AppVersion(),
		Profile: target, Logger: a.UI.Log, UserAgent: defaultUserAgent(a.version),
	})
	c.SetTokens(sess.UID, sess.AccessToken, sess.RefreshToken)
	acct, err := account.New(c, nil).Get(ctx)
	if err != nil {
		return "", err
	}
	key, err := localkey.Generate()
	if err != nil {
		return "", err
	}
	if err := localkey.Put(ctx, c, key); err != nil {
		return "", err
	}
	blob, err := localkey.Wrap(sess.KeyPassword, key)
	if err != nil {
		return "", err
	}
	uid, acc, ref := c.Tokens()
	release, err := session.Hold(ctx, target)
	if err != nil {
		return "", err
	}
	defer release()
	if err := session.Save(target, &session.Session{
		UID: uid, AccessToken: acc, RefreshToken: ref,
		UserID: acct.ID, Email: acct.Email, EncKeyBlob: blob,
		BaseURL: c.BaseURL(),
	}); err != nil {
		return "", err
	}
	return acct.Email, nil
}

func (a *App) awaitFork(ctx context.Context, selector string) (*proton.ForkedSession, error) {
	deadline := time.Now().Add(forkLife)
	for attempt := 1; ; attempt++ {
		approved, err := a.API.PullFork(ctx, selector)
		switch {
		case err == nil:
			slog.DebugContext(ctx, "the code was approved", "attempt", attempt)
			return approved, nil
		case !errors.Is(err, proton.ErrForkWaiting):
			return nil, err
		case time.Now().After(deadline):
			slog.DebugContext(ctx, "nobody approved the code", "attempt", attempt)
			return nil, errs.Problemf("The code expired.").
				Hint("run the command again for a new one").Exit(2)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(forkPoll):
		}
	}
}

func (a *App) SignInDevice(ctx context.Context, code fork.Code) error {
	unlocked, err := a.Unlock(ctx)
	if err != nil {
		return err
	}
	payload, err := code.Seal(unlocked.KeyPassword())
	if err != nil {
		return err
	}
	return a.API.PushFork(ctx, code.UserCode, code.ClientID, payload)
}

func (a *App) unlockPass(ctx context.Context) error {
	if !a.Creds.ExtraPasswordOffered() {
		return nil
	}
	reaches, err := a.ReachesPass(ctx)
	if err != nil {
		return err
	}
	if reaches {
		a.UI.Note("This account has no Pass extra password, so nothing needed the one you gave.")
		return nil
	}
	extra, err := a.Creds.ExtraPassword()
	if err != nil {
		return err
	}
	return a.UnlockPass(ctx, extra)
}

// ReachesPass reports whether this session may reach Pass as it stands.
//
// The session is asked what it holds rather than the account what it has,
// because the scope is the thing that decides: Proton withholds it from a session
// that has not proved an extra password, and grants it for the life of one that
// has.
func (a *App) ReachesPass(ctx context.Context) (bool, error) {
	scopes, err := proton.Scopes(ctx, a.API)
	if err != nil {
		return false, err
	}
	return slices.Contains(scopes, string(proton.ScopePass)), nil
}

func (a *App) Elevate(ctx context.Context, scope proton.Scope, reason string) (func(), error) {
	if a.DryRun {
		return func() {}, nil
	}
	if err := a.Authenticate(ctx); err != nil {
		return nil, err
	}
	password, err := a.Creds.Password(reason)
	if err != nil {
		return nil, err
	}
	if err := a.API.Elevate(ctx, scope, proton.ScopeCredentials{Password: []byte(password)}); err != nil {
		return nil, err
	}
	return func() { a.API.Relock(ctx) }, nil
}

func (a *App) UnlockPass(ctx context.Context, extra string) error {
	return a.API.Elevate(ctx, proton.ScopePass, proton.ScopeCredentials{Password: []byte(extra)})
}

func (a *App) refuseAnotherAccount() error {
	return errs.Problemf("Profile %q is signed in as %s.", a.Profile, a.email).
		Hint(fmt.Sprintf("proton account logout --profile %s", a.Profile)).Exit(4)
}

func (a *App) Unlock(ctx context.Context) (*keys.Unlocked, error) {
	if err := a.Authenticate(ctx); err != nil {
		return nil, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cache != nil {
		return a.cache, nil
	}
	u, err := keys.Unlock(ctx, a.API, a.Creds.KeyPassword)
	if err != nil {
		return nil, err
	}
	a.cache = u
	return u, nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func defaultUserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return "proton-cli/" + version
}
