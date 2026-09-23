// Package app wires the Proton services, renderer and session together for
// the CLI. One App instance per invocation.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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

	// Index is the encrypted copy of the account this machine keeps, which is
	// what answers a question Proton cannot read the answer to.
	Index *search.Store

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
	// Settings is where this run's preferences came from, which a report says.
	Settings config.Source

	IDCache *idcache.Cache

	// Verified is a human verification solved before this run started, as a
	// refusal from an earlier one printed it. It is what lets a caller that
	// cannot be asked a question get past a CAPTCHA at all.
	Verified string

	// run is this invocation's diagnostic file, or nil when nothing is being
	// recorded, and started is when it opened.
	run     *runlog.Run
	started time.Time
	version string

	zone  zoneCache
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
	// Services log through the package-level logger, so this is what makes a
	// debug line in a service reach the same two places a client's does. Without
	// it they went to slog's own default handler, which discards anything below
	// info and knows nothing about --log-level.
	slog.SetDefault(u.Log)
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
	}
	// Adopted before anything runs, so a listing prints in the same zone a write
	// would be anchored to. Only a machine that cannot name its own zone leaves
	// this empty, and that one asks the account the first time a command needs a
	// name for it.
	a.zone.name = opts.Zone
	adoptZone(opts.Zone)
	// A service that decrypts holds the keys it decrypts with, the way it holds the
	// client it fetches with. Unlock is memoised, so the hierarchy is fetched at
	// most once per invocation and only if something actually asks for it.
	// The index is the account's own content at rest on this machine, so it is
	// sealed to the account's keys and belongs to the profile that holds them.
	a.Index = search.New(indexDir(profileName), a.UserID, a.indexKeys)
	a.Account = account.New(c, a.Unlock)
	a.Mail = mail.New(c, a.Unlock)
	a.Mail.SetIndex(a.Index)
	a.Drive = drive.New(c, a.Unlock)
	a.Drive.SetIndex(a.Index)
	a.Calendar = calendar.New(c, a.Unlock)
	a.Calendar.SetIndex(a.Index)
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
// Everything that wants it asks with the same word: `-`, as the value of a flag
// or as an argument. Whichever asked second would find an empty stream and fail
// somewhere further along with a puzzle, so it is told here instead, in terms of
// the two flags that collided.
//
// Standard input that nothing is piped into is a terminal, and reading one
// waits for typing. A run that sat there silently would look like a run that
// had hung, so it says whose input it is waiting for and which key ends it.
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

// elsewhere is the way out of a collision: whichever claim reads a secret takes
// a path as readily as it takes `-`, and that is the one thing to change. A `-`
// argument has no path to move to, so when neither claim is a secret's flag the
// reader is left to pick which one moves.
func elsewhere(claims ...string) string {
	for _, claim := range claims {
		flag, _, ok := strings.Cut(claim, " ")
		if ok && strings.HasSuffix(flag, "-file") {
			return "pass it with " + flag + " FILE instead"
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

// Seen is a profile's record of the references it has been shown.
//
// It is reachable without an App because a shell completion has none: answering
// what a listing showed needs no client, no session and no settings but which
// profile is being typed about, and building the rest of an invocation to find
// that out would log a run nobody made.
func Seen(name profile.Name) *idcache.Cache { return idcache.New(idCachePath(name)) }

// idCachePath mirrors the session-file convention.
func idCachePath(name profile.Name) string {
	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "idcache", name.FileName(".json"))
}

// Forget removes everything this machine keeps for a profile: the session, the
// references it has been shown, and the index of the account's contents.
//
// They are one thing rather than three - what this computer holds about one
// account - so a profile that has been removed leaves nothing that outlives it.
func Forget(name profile.Name) error {
	if err := session.Clear(name); err != nil {
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

// indexDir is where a profile's indexes live, beside everything else that is
// this machine's rather than the account's.
func indexDir(name profile.Name) string {
	dir, err := config.Dir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "index", name.FileName(""))
}

// indexKeys is what seals the index: the account's primary user key to write
// with, and every user key to read with, so an index made before a key rotation
// still opens after one.
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

// SignedIn reports whether this profile holds a session.
// UserID is the account this profile is signed in as, as Proton names it.
//
// A Pass export records it, because an alias belongs to the account that made
// it and a reader has to be able to tell whether it is looking at its own.
func (a *App) UserID() string { return a.userID }

// Email is the address this profile is signed in as, empty when it holds no
// session. It is what a preview names, since a preview signs nobody in to find
// out.
func (a *App) Email() string { return a.email }

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
	}
	if resumed, err := a.resume(ctx); resumed || err != nil {
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
	if err := a.saveSession(); err != nil {
		return err
	}
	return a.settle(ctx)
}

// resume finishes a sign-in that has nothing left to do, and reports whether it
// was one.
//
// A profile whose saved session still works needs neither a password nor a code:
// the only question left is whether the keys open, and proving the account again
// cannot change that answer. Reporting why they did not beats a second exchange
// that fails the same way, having asked for a secret to do it.
func (a *App) resume(ctx context.Context) (bool, error) {
	if !a.SignedIn() {
		return false, nil
	}
	if _, err := a.Account.Get(ctx); err != nil {
		// The saved session no longer works, so sign in again over the top of it.
		return false, nil
	}
	return true, a.settle(ctx)
}

// settle is the end of every sign-in: the keys open, an extra password that was
// handed over is spent, and the session file carries what both produced.
func (a *App) settle(ctx context.Context) error {
	if _, err := a.Unlock(ctx); err != nil {
		return err
	}
	if err := a.unlockPass(ctx); err != nil {
		return err
	}
	return a.saveSession()
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

// LoginQR attaches an account to this profile through a code approved on a
// device that is already signed in.
//
// Nothing about the account is asked for here: no password, no second factor, no
// second password. What the approving device seals into the fork is the
// passphrase the keys are already locked with, so the session this saves is
// unlocked from the start, the same as one a password opened.
//
// show is handed the code to put in front of a person, and is called once. What
// it draws is the CLI's business rather than this function's; what happens next
// is waiting.
//
// It is idempotent for the same reason Login is: a profile whose session still
// works is left alone rather than being made to mint a code nobody needs.
func (a *App) LoginQR(ctx context.Context, show func(code string) error) error {
	if resumed, err := a.resume(ctx); resumed || err != nil {
		return err
	}
	if err := a.API.AnonymousSession(ctx); err != nil {
		return err
	}
	opened, err := a.API.NewFork(ctx)
	if err != nil {
		return err
	}
	// The client ID is how the new session names itself, and this build has one
	// name wherever it says who it is.
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

	// Whose account it is, before anything about it is written down: a profile
	// names one account everywhere else, and a code approved by a second one would
	// otherwise repoint it silently.
	acct, err := a.Account.Get(ctx)
	if err != nil {
		return err
	}
	if err := a.refuseRepoint(acct.Email); err != nil {
		// The fork is spent either way, and leaving it standing would put a session
		// on the account that nothing on this machine holds the tokens to.
		if rerr := a.API.RevokeSession(ctx, approved.UID); rerr != nil {
			slog.DebugContext(ctx, "the refused fork could not be revoked", "error", rerr.Error())
		}
		return err
	}
	a.rememberIdentity(acct.ID, acct.Email)
	if err := a.saveSession(); err != nil {
		return err
	}
	// A code that carried no key signs the machine in and unlocks nothing, which
	// is a session to save as it is: the first command that decrypts asks for the
	// password, exactly as one signed in before this build would.
	if keyPass != "" {
		if err := keys.Seal(ctx, a.API, keyPass); err != nil {
			return err
		}
	}
	return a.settle(ctx)
}

// AccessInto signs the profile named by target in to the account this one holds
// emergency access to, from the session that access yielded.
//
// The accessed session goes to its own profile through a client of its own, so
// the acting profile - the emergency contact's - is left exactly as it was. The
// key password is the delegated token, sealed the way every session's is, so the
// accessed account opens on this machine without its owner's password.
func (a *App) AccessInto(ctx context.Context, target profile.Name, sess account.AccessSession) (string, error) {
	c := proton.New(proton.Options{
		BaseURL: a.API.BaseURL(), AppVersion: a.API.AppVersion(),
		Profile: target.String(), Logger: a.UI.Log,
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
	if err := session.Save(target, &session.Session{
		UID: uid, AccessToken: acc, RefreshToken: ref,
		UserID: acct.ID, Email: acct.Email, EncKeyBlob: blob,
		AppVersion: c.AppVersion(), BaseURL: c.BaseURL(),
	}); err != nil {
		return "", err
	}
	return acct.Email, nil
}

// awaitFork waits for somebody to approve the fork, and gives up when nobody
// does.
//
// Every ask that finds it unapproved is the ordinary case and says nothing; a
// count of them is what the log carries, because "it timed out" and "it was
// never asked" are different failures and look identical afterwards.
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

// SignInDevice signs another device in as this account, by the code it is
// showing.
//
// The passphrase that opens this account's keys is sealed under a key that
// device made and Proton never sees, which is what lets it read anything at all
// once it is signed in.
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

// Elevate proves the account password before the work starts, and hands back
// what drops the elevation again.
//
// Almost nothing calls this: an endpoint that wants an elevated session says so
// when it is asked, and the client answers that by itself, which is why no
// command knows which endpoints are guarded. What this is for is the handful
// that go on to ask for a *second* secret - a new password, a code from an
// authenticator app - where the order is the whole point. Proving the current
// password first means a person is never asked to invent one and then told they
// could not have, and a code is never typed before a prompt that outlives it.
//
// reason completes "Your password is required to <reason>".
//
// A dry run proves nothing: it changes nothing, so there is nothing to
// authorise, and a preview that asked for a password would be the one path that
// costs more than the change it is previewing.
func (a *App) Elevate(ctx context.Context, reason string) (func(), error) {
	if a.DryRun {
		return func() {}, nil
	}
	if err := a.Authenticate(ctx); err != nil {
		return nil, err
	}
	user, err := a.Creds.User()
	if err != nil {
		return nil, err
	}
	password, err := a.Creds.Password(reason)
	if err != nil {
		return nil, err
	}
	if err := a.API.Elevate(ctx, proton.ScopePassword, proton.ScopeCredentials{
		Username: user, Password: []byte(password),
	}); err != nil {
		return nil, err
	}
	return func() { a.API.Relock(ctx) }, nil
}

// UnlockPass proves an extra password to this session, so what follows reaches
// Pass and so the saved session does too.
func (a *App) UnlockPass(ctx context.Context, extra string) error {
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
