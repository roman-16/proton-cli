// Package app wires the Proton services, renderer and session together for
// the CLI. One App instance per invocation.
package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/idcache"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/service/calendar"
	"github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

type App struct {
	Profile profile.Name
	// Creds resolves the account email, password and two-factor code, asking the
	// user only when nothing else supplies them.
	Creds *Credentials

	API *proton.Client

	Account  *account.Service
	Mail     *mail.Service
	Drive    *drive.Service
	Calendar *calendar.Service
	Contacts *contacts.Service
	Pass     *pass.Service

	// UI renders everything the command produces.
	UI *ui.UI

	DryRun  bool
	FullIDs bool
	// Yes answers every confirmation in advance, for scripts that mean it. It
	// answers a question; it does not lift a refusal.
	Yes bool
	// Confirm is which commands stop for a yes, and which are refused outright.
	Confirm confirm.Policy
	// NoUpdateCheck suppresses the look for a new release.
	NoUpdateCheck bool

	IDCache *idcache.Cache

	// Verified is a human verification solved before this run started, as a
	// refusal from an earlier one printed it. It is what lets a caller that
	// cannot be asked a question get past a CAPTCHA at all.
	Verified string

	mu    sync.Mutex
	cache *keys.Unlocked

	stdinMu    sync.Mutex
	stdinClaim string

	sessionMu sync.Mutex
	userID    string
	email     string
}

// Options is one invocation's settled configuration, plus the few things that
// are decided per run and never persisted: a preview, an answer given in
// advance, and a verification already solved.
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

	u := ui.New(ui.Options{
		Format:   opts.Output,
		LogLevel: opts.LogLevel,
		Quiet:    opts.Quiet,
		NoColor:  opts.NoColor,
		NoInput:  opts.NoInput,
		FullIDs:  opts.FullIDs,
	})
	c := proton.New(proton.Options{
		BaseURL: apiURL, Logger: u.Log, Profile: profileName.String(),
		UserAgent: userAgent, DryRun: opts.DryRun,
	})

	var userID, email string
	if sess, err := session.Load(profileName); err == nil && sess != nil {
		c.SetTokens(sess.UID, sess.AccessToken, sess.RefreshToken)
		c.SetEncKeyBlob(sess.EncKeyBlob)
		userID, email = sess.UserID, sess.Email
	}

	a := &App{
		Profile:       profileName,
		Creds:         newCredentials(u, email),
		API:           c,
		Account:       account.New(c),
		UI:            u,
		DryRun:        opts.DryRun,
		FullIDs:       opts.FullIDs,
		Yes:           opts.Yes,
		Confirm:       opts.Confirm,
		NoUpdateCheck: opts.NoUpdateCheck,
		Verified:      verified,
		IDCache:       idcache.New(idCachePath(profileName)),
		userID:        userID,
		email:         email,
	}
	// A service that decrypts holds the keys it decrypts with, the way it holds the
	// client it fetches with. Unlock is memoised, so the hierarchy is fetched at
	// most once per invocation and only if something actually asks for it.
	a.Mail = mail.New(c, a.Unlock)
	a.Drive = drive.New(c, a.Unlock)
	a.Calendar = calendar.New(c, a.Unlock)
	a.Contacts = contacts.New(c, a.Unlock)
	a.Pass = pass.New(c, a.Unlock)
	// The client persists the session file whenever its tokens change (e.g. a
	// mid-request refresh); it stays free of the persistence format by calling
	// back into saveSession, which owns the DTO assembly. It reads them back the
	// same way, so a session refreshed by a command running alongside this one is
	// picked up rather than refreshed a second time with a token Proton has already
	// spent.
	c.SetPersistHook(func() { _ = a.saveSession() })
	// A command reaches the network only as the account it was given, so that is
	// where the requirement is enforced - not before the command body, which would
	// make every argument the command could have judged for itself cost a sign-in to
	// discover.
	c.SetSessionGuard(func() error { return a.Authenticate(context.Background()) })
	c.SetReloadHook(func() (string, string, bool) {
		stored, err := session.Load(profileName)
		if err != nil || stored == nil {
			return "", "", false
		}
		return stored.AccessToken, stored.RefreshToken, true
	})
	a.Creds.stdinOwner = a.Stdin
	a.installScopeResolver()
	a.installSecondFactorResolver()
	return a, nil
}

// Stdin hands out the process's standard input, which only one reader may have.
//
// Two things want it: a --*-stdin flag for one of the secrets, and `-` for a
// body, a key, or a file to upload. Whichever asked second would find an empty
// stream and fail somewhere further along with a puzzle, so it is told here
// instead, in terms of the two flags that collided.
func (a *App) Stdin(claim string) (io.Reader, error) {
	a.stdinMu.Lock()
	defer a.stdinMu.Unlock()
	if a.stdinClaim != "" {
		return nil, errs.Problemf("%s and %s both read standard input, which can only be read once.",
			a.stdinClaim, claim).
			Hint(elsewhere(a.stdinClaim, claim))
	}
	a.stdinClaim = claim
	return a.UI.In, nil
}

// elsewhere is the way out of a collision: whichever claim is a secret's
// --*-stdin flag has a --*-file twin that reads the same value from somewhere
// else, and that is the one thing to change. A `-` argument has no twin, so when
// neither claim is a flag the reader is left to pick which one moves.
func elsewhere(claims ...string) string {
	for _, claim := range claims {
		if flag, ok := strings.CutSuffix(claim, "-stdin"); ok && strings.HasPrefix(flag, "--") {
			return "pass it with " + flag + "-file instead"
		}
	}
	return "read one of them from a path rather than -"
}

// saveSession writes the current client state to the profile's session file,
// preserving the identity fields an earlier save established. Those come from
// /core/v4/users, which the client has no business fetching, so they are set
// once by rememberIdentity and carried forward from here on.
func (a *App) saveSession() error {
	uid, acc, refresh := a.API.Tokens()
	a.sessionMu.Lock()
	defer a.sessionMu.Unlock()
	return session.Save(a.Profile, &session.Session{
		UID:          uid,
		AccessToken:  acc,
		RefreshToken: refresh,
		UserID:       a.userID,
		Email:        a.email,
		EncKeyBlob:   a.API.EncKeyBlob(),
		AppVersion:   a.API.AppVersion(),
		BaseURL:      a.API.BaseURL(),
	})
}

// rememberIdentity records who the session belongs to, so listing profiles can
// name each account without one API call per profile.
func (a *App) rememberIdentity(userID, email string) {
	a.sessionMu.Lock()
	a.userID, a.email = userID, email
	a.sessionMu.Unlock()
}

// idCachePath mirrors the session-file convention.
func idCachePath(name profile.Name) string {
	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "idcache", name.FileName(".json"))
}

// SignedIn reports whether this profile holds a session.
// UserID is the account this profile is signed in as, as Proton names it.
//
// A Pass export records it, because an alias belongs to the account that made
// it and a reader has to be able to tell whether it is looking at its own.
func (a *App) UserID() string { return a.userID }

func (a *App) SignedIn() bool {
	uid, _, _ := a.API.Tokens()
	return uid != ""
}

// Authenticate makes sure the profile is signed in.
//
// An account reaches the CLI one way: `account login` attaches it to a profile
// and saves the session. A command acts as whichever profile it was given, so
// when that profile has no session it is said here, before anything reaches the
// network.
func (a *App) Authenticate(context.Context) error {
	if a.SignedIn() {
		return nil
	}
	if a.Profile.IsDefault() {
		return errs.Problemf("You are not signed in.").
			Hint("proton account login").Exit(2)
	}
	return errs.Problemf("Profile %q is not signed in.", a.Profile).
		Hint(fmt.Sprintf("proton account login --profile %s", a.Profile)).Exit(2)
}

// Login attaches an account to this profile and saves the session.
//
// Signing in also unlocks the key hierarchy, which seals the key password into
// the session file. Doing both here is what makes the password a one-time cost:
// a login that only stored tokens would leave the very next command asking
// again.
//
// An extra password handed over here unlocks Pass for the session, which is what
// makes Pass reachable by a run with nobody to ask. Nothing else about it is
// eager: an account whose Pass is protected and whose password nobody supplied
// signs in exactly as before, and the first Pass command asks.
//
// It is idempotent. A profile already signed in as the same account is left
// alone, so an unattended caller can run it unconditionally before its real
// work and recover by itself from a session that expired or was revoked.
//
// user is the account to attach, empty to ask for one. It is passed rather than
// resolved from the environment because this is the only place that names an
// account: everything else acts as whichever profile it was given.
func (a *App) Login(ctx context.Context, user string) error {
	if a.SignedIn() {
		if err := a.refuseRepoint(user); err != nil {
			return err
		}
		if _, err := a.Account.Get(ctx); err == nil {
			// The session works, so the only question left is whether the keys
			// open - and signing in again cannot change that answer. Reporting
			// why they did not beats a second SRP exchange that fails the same
			// way, having asked for a password to do it.
			if _, err := a.Unlock(ctx); err != nil {
				return err
			}
			if err := a.unlockPass(ctx); err != nil {
				return err
			}
			return a.saveSession()
		}
		// The saved session no longer works, so sign in again over the top of it.
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
	if err := a.saveSession(); err != nil {
		return err
	}
	if _, err := a.Unlock(ctx); err != nil {
		return err
	}
	if err := a.unlockPass(ctx); err != nil {
		return err
	}
	return a.saveSession()
}

// unlockPass spends an extra password that was handed to this sign-in, so the
// session it saves can reach Pass.
//
// The session is asked what it already holds rather than the account what it has:
// the scope is the thing that decides, and it is the same question the refusal
// would have asked later. An account with no extra password is told so, because a
// script that supplied one has a file it does not need.
func (a *App) unlockPass(ctx context.Context) error {
	if !a.Creds.ExtraPasswordOffered() {
		return nil
	}
	scopes, err := a.API.Scopes(ctx)
	if err != nil {
		return err
	}
	if slices.Contains(scopes, string(proton.ScopePass)) {
		a.UI.Note("This account has no Pass extra password, so nothing needed the one you gave.")
		return nil
	}
	extra, err := a.Creds.ExtraPassword()
	if err != nil {
		return err
	}
	return a.API.Elevate(ctx, proton.ScopePass, proton.ScopeCredentials{Password: []byte(extra)})
}

// refuseRepoint stops a profile being pointed at a second account behind its
// own back. Re-pointing is a fine thing to want; it just has to be said out
// loud, because the profile names the account everywhere else.
func (a *App) refuseRepoint(wanted string) error {
	if wanted == "" || a.email == "" || strings.EqualFold(wanted, a.email) {
		return nil
	}
	return errs.Problemf("Profile %q is signed in as %s.", a.Profile, a.email).
		Hint(fmt.Sprintf("proton account logout --profile %s", a.Profile)).Exit(4)
}

// Unlock returns the decrypted key hierarchy, memoised for the invocation.
//
// It is what every service that decrypts holds as its keys.Get, so the hierarchy
// is fetched once, on the first command that reaches a decryption, and not at all
// by a command that reaches none.
//
// The secret that opens the keys is requested lazily, and only on the path that
// actually needs it: once the session file carries the sealed key password,
// unlocking asks for nothing at all.
func (a *App) Unlock(ctx context.Context) (*keys.Unlocked, error) {
	// There are no keys to unlock for an account nobody is signed in to, and
	// asking for a password to open them would be asking the wrong question.
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

// defaultUserAgent honestly identifies the CLI in the User-Agent header, e.g.
// "proton-cli/1.2.3".
func defaultUserAgent(version string) string {
	if version == "" {
		version = "dev"
	}
	return "proton-cli/" + version
}

// RememberIdentity records who the current session belongs to. Exported for the
// account commands, which learn it from /core/v4/users after signing in.
func (a *App) RememberIdentity(userID, email string) { a.rememberIdentity(userID, email) }

// SaveSession writes the current session state to disk.
func (a *App) SaveSession() error { return a.saveSession() }
