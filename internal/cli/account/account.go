// Package account is the `proton account` tree: the account itself, its
// settings, and the session this machine holds.
//
// It is a first-class app, the way Proton treats it: account settings live at
// account.proton.me, not inside Mail.
package account

import (
	"fmt"
	"strings"

	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/profile"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// New builds the account tree.
func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "account",
		Short: "Your Proton account, its settings and your session",
	}
	c.AddCommand(getCmd(), loginCmd(), logoutCmd(), profilesCmd(), sessionsCmd(), settingsCmd())
	return c
}

// ── account get ──

// state is what `account get` answers: not just who the account is, but whether
// this machine can currently act as it. Those are the same question to a user,
// so they are one command rather than an `account get` plus an `account status`.
type state struct {
	*acctsvc.Account
	Profile  string   `json:"profile"`
	Session  string   `json:"session"`
	Unlocked bool     `json:"unlocked"`
	Scopes   []string `json:"scopes"`
}

func getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the account, its storage and this machine's session",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			acct, err := c.App.Account.Get(c.Ctx)
			if err != nil {
				return err
			}
			// Reaching this point means the session worked, which is the only
			// honest way to report that it is valid.
			st := state{Account: acct, Profile: c.App.Profile.String(), Session: "valid"}
			if sess, err := session.Load(c.App.Profile); err == nil {
				st.Unlocked = sess.Unlocked()
			}
			// Scopes are informative rather than essential: an account whose
			// session cannot list them is still worth reporting.
			if scopes, err := c.App.API.Scopes(c.Ctx); err == nil {
				st.Scopes = scopes
			}

			return kit.Show(c, ui.RecordSpec{
				Object: st,
				Fields: []ui.Field{
					{Label: "Email", Value: acct.Email},
					{Label: "Name", Value: acct.DisplayName},
					{Label: "Storage", Value: storage(acct.UsedSpace, acct.MaxSpace)},
					{Label: "Max Upload", Value: units.Size(acct.MaxUpload)},
					{Label: "Profile", Value: st.Profile},
					{Label: "Session", Value: st.Session, Always: true},
					{Label: "Unlocked", Value: yesNo(st.Unlocked), Always: true},
					{Label: "ID", Value: acct.ID, ID: true},
				},
			})
		}),
	}
}

// storageBar is how many cells the quota bar is drawn in. Narrow enough to sit
// inside a record's value column without becoming the widest thing on screen.
const storageBar = 20

// storage renders usage as a person reads it: how much of how much, the share
// that represents, and a bar for the share - which is the form the question
// "am I running out?" is actually asked in, and the one Proton's own clients
// answer it with.
func storage(used, max int64) string {
	if max <= 0 {
		return units.Size(used)
	}
	ratio := float64(used) / float64(max)
	filled := int(ratio * storageBar)
	if filled > storageBar {
		filled = storageBar
	}
	bar := strings.Repeat(ui.GlyphBarFilled, filled) +
		strings.Repeat(ui.GlyphBarPending, storageBar-filled)
	return fmt.Sprintf("%s  %3.0f%%  %s of %s", bar, ratio*100, units.Size(used), units.Size(max))
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ── account login / logout ──

func loginCmd() *cobra.Command {
	var (
		extra  kit.ExtraPassword
		reauth kit.Reauth
		second kit.SecondPassword
		user   string
	)
	c := &cobra.Command{
		Use:   "login",
		Short: "Sign in and save the session for this profile",
		Long: "Sign in and save the session for this profile.\n\n" +
			"Signing in also unlocks your keys, so your password is needed once per\n" +
			"machine and not again. Anything a flag has not set is asked for, as long as\n" +
			"this is a terminal.\n\n" +
			"Security key: you are asked to touch it. If the account also has an\n" +
			"authenticator app, pressing Enter at the code prompt reaches for the key\n" +
			"instead. A key needs a person present, so unattended jobs want --totp.\n\n" +
			"Two-password mode: the second password is asked for after signing in. That\n" +
			"is the password your keys are locked with. A one-password account is never\n" +
			"asked for it.\n\n" +
			"Pass extra password: not asked for here. The first `pass` command asks, and\n" +
			"the session can then reach Pass for as long as it lives. Pass it now with\n" +
			"--extra-password-file when there is nobody to ask.\n\n" +
			"Human verification: Proton may ask you to prove you are human. The page is\n" +
			"printed and can be solved on any device, so a machine with no display signs\n" +
			"in like any other. A run that cannot ask prints the page and the token to\n" +
			"repeat the command with.\n\n" +
			"Signing in again as the same account changes nothing, so an unattended job\n" +
			"can run it first to recover from an expired session.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := second.Supply(c); err != nil {
				return err
			}
			if err := extra.Supply(c); err != nil {
				return err
			}
			if err := c.App.Login(c.Ctx, user); err != nil {
				return err
			}
			acct, err := c.App.Account.Get(c.Ctx)
			if err != nil {
				return err
			}
			c.App.RememberIdentity(acct.ID, acct.Email)
			if err := c.App.SaveSession(); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.SignedIn, Count: 1, Name: acct.Email,
				Detail: fmt.Sprintf("(profile %q)", c.App.Profile),
			}, func() error { return nil })
		}),
	}
	// Naming an account belongs here. Every other command acts as whichever
	// profile it was given, and already knows the address from its session.
	c.Flags().StringVar(&user, "user", "", "Proton account email to sign in as")
	extra.Declare(c)
	reauth.Declare(c)
	second.Declare(c)
	return c
}

func logoutCmd() *cobra.Command {
	var all, revoke bool
	c := &cobra.Command{
		Use:   "logout",
		Short: "Discard the saved session for this profile",
		Long: "Discard the saved session for this profile.\n\n" +
			"The key password on disk is sealed with a key held by Proton, so deleting\n" +
			"the session file is enough to make it unreadable.\n\n" +
			"--revoke also invalidates the session at Proton, the same as signing out in\n" +
			"a Proton app. Use it if the file may have been copied.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			targets := []profile.Name{c.App.Profile}
			if all {
				profiles, err := session.Profiles()
				if err != nil {
					return err
				}
				names := make([]string, 0, len(profiles))
				for _, p := range profiles {
					names = append(names, p.Name)
				}
				if targets, err = profile.Names(names); err != nil {
					return err
				}
			}
			refs := make([]string, 0, len(targets))
			for _, t := range targets {
				refs = append(refs, t.String())
			}
			if len(targets) == 0 {
				return kit.Mutate(c, ui.ResultSpec{Action: ui.SignedOut, Kind: "profiles"},
					func() error { return nil })
			}

			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.SignedOut, Kind: "profiles", Count: len(targets),
				Name: single(refs), IDs: refs,
			}, func() error {
				// Revoking needs the session that is about to be deleted, so it
				// happens first and only for the active profile: the others'
				// tokens are not loaded.
				if revoke {
					if err := c.App.Authenticate(c.Ctx); err == nil {
						uid, _, _ := c.App.API.Tokens()
						if err := c.App.API.RevokeSession(c.Ctx, uid); err != nil {
							return err
						}
					}
				}
				for _, name := range targets {
					if err := session.Clear(name); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	kit.All(c.Flags(), &all)
	c.Flags().BoolVar(&revoke, "revoke", false, "Also invalidate the session at Proton")
	return c
}

// single returns the sole element of ss, or "" when there is more than one, so a
// confirmation can name one thing and count many.
func single(ss []string) string {
	if len(ss) == 1 {
		return ss[0]
	}
	return ""
}
