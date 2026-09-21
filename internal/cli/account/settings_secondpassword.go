package account

import (
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Two-password mode: the account signs in with one secret and opens its keys
// with another.
//
// One field decides which mode the account ends in: the request that hands
// Proton the re-locked keys either carries a verifier for the same secret,
// which makes it the password that signs in as well, or carries none, which
// leaves the two apart.
//
// Entering the mode takes one thing more. Proton refuses keys locked with a
// second password until it has been told there is one, and what tells it is
// the login password written on its own - so `enable` writes the password it
// just proved, unchanged, and then moves the keys.
//
// None of the three asks for a new login password. Proton's own switch does,
// and it does not have to: the password that signs in is proved on the way in
// and can simply stay. Anybody who wants a new one has `password set`.

// twoPasswordView is what `second-password get` answers.
type twoPasswordView struct {
	Enabled bool `json:"enabled"`
}

func secondPasswordCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "second-password",
		Short: "The second password your keys can be locked with",
		Long: "The second password your keys can be locked with.\n\n" +
			"In two-password mode one password signs you in and another opens your keys.",
	}
	c.AddCommand(secondPasswordGetCmd(), secondPasswordEnableCmd(), secondPasswordSetCmd(),
		secondPasswordDisableCmd())
	return c
}

func secondPasswordEnableCmd() *cobra.Command {
	var (
		chosen kit.NewPassword
		reauth kit.Reauth
	)
	c := &cobra.Command{
		Use:   "enable",
		Short: "Lock your keys with a second password",
		Long: "Lock your keys with a second password.\n\n" +
			"The password you sign in with is asked for first and stays as it is. The\n" +
			"second password comes from --new-password-file, which takes - for stdin, or\n" +
			"from a prompt that asks for it twice. It needs at least eight characters and\n" +
			"cannot be the one you sign in with.\n\n" +
			"Signing in then takes both: `" + kit.Program + " account login --second-password-file`,\n" +
			"or a prompt for the second one after your password.\n\n" +
			"An account already in two-password mode is refused.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := chosen.Supply(c); err != nil {
				return err
			}
			if chosen.Offered() {
				secret, err := chosen.Chosen(c, app.SecondPassword)
				if err != nil {
					return err
				}
				if c.App.Creds.PasswordOffered() {
					login, err := c.App.Creds.Password("add a second password")
					if err != nil {
						return err
					}
					if err := refuseLoginPassword(login, secret); err != nil {
						return err
					}
				}
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if security.TwoPasswordMode {
				return kit.Fail("This account already uses two-password mode.").
					Hint(kit.Program + " account settings second-password set")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "two-password mode",
				Detail: "- your keys open with the second password from now on",
			}, func() error {
				relock, err := c.App.Elevate(c.Ctx, "add a second password")
				if err != nil {
					return err
				}
				defer relock()
				login, err := c.App.Creds.Password("add a second password")
				if err != nil {
					return err
				}
				secret, err := chosen.Chosen(c, app.SecondPassword)
				if err != nil {
					return err
				}
				if err := refuseLoginPassword(login, secret); err != nil {
					return err
				}
				u, err := c.App.Unlock(c.Ctx)
				if err != nil {
					return err
				}
				// The password that signs in, written again as it is: that is
				// what puts the account in the mode, and the keys that follow
				// carry no verifier, which is what keeps the two apart.
				if err := c.App.Account.SetLoginPassword(c.Ctx, login, true); err != nil {
					return err
				}
				return u.Relock(c.Ctx, c.App.API, secret, false)
			})
		}),
	}
	chosen.Declare(c)
	reauth.Declare(c)
	return c
}

func secondPasswordGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show whether this account uses two-password mode",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			view := twoPasswordView{Enabled: security.TwoPasswordMode}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Status", Value: kit.OnOffText(boolInt(view.Enabled)), Always: true},
				},
			})
		}),
	}
}

func secondPasswordSetCmd() *cobra.Command {
	var (
		chosen kit.NewPassword
		reauth kit.Reauth
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Change your second password",
		Long: "Change your second password.\n\n" +
			"The password you sign in with is asked for first. The new second password\n" +
			"comes from --new-password-file, which takes - for stdin, or from a prompt\n" +
			"that asks for it twice. It needs at least eight characters and cannot be the\n" +
			"one you sign in with.\n\n" +
			"Your keys are locked with it from then on. This session goes on working;\n" +
			"another machine signed in to this account asks for it the next time it opens\n" +
			"your keys.\n\n" +
			"An account that does not use two-password mode is refused.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := chosen.Supply(c); err != nil {
				return err
			}
			if chosen.Offered() {
				secret, err := chosen.Chosen(c, app.SecondPassword)
				if err != nil {
					return err
				}
				if c.App.Creds.PasswordOffered() {
					login, err := c.App.Creds.Password("change your second password")
					if err != nil {
						return err
					}
					if err := refuseLoginPassword(login, secret); err != nil {
						return err
					}
				}
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if !security.TwoPasswordMode {
				return refuseOnePassword()
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Count: 1, Name: "your second password",
			}, func() error {
				relock, err := c.App.Elevate(c.Ctx, "change your second password")
				if err != nil {
					return err
				}
				defer relock()
				login, err := c.App.Creds.Password("change your second password")
				if err != nil {
					return err
				}
				secret, err := chosen.Chosen(c, app.SecondPassword)
				if err != nil {
					return err
				}
				if err := refuseLoginPassword(login, secret); err != nil {
					return err
				}
				u, err := c.App.Unlock(c.Ctx)
				if err != nil {
					return err
				}
				// The keys move and the sign-in does not: in two-password mode
				// they are separate secrets, and this is the one that opens keys.
				return u.Relock(c.Ctx, c.App.API, secret, false)
			})
		}),
	}
	chosen.Declare(c)
	reauth.Declare(c)
	return c
}

func secondPasswordDisableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Leave two-password mode",
		Long: "Leave two-password mode.\n\n" +
			"Your password is asked for, and your keys are locked with it. The second\n" +
			"password stops working, and nothing else about the account changes.\n\n" +
			"This session goes on working. Another machine signed in to this account asks\n" +
			"for your password the next time it opens your keys.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if !security.TwoPasswordMode {
				return refuseOnePassword()
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "two-password mode",
				Detail: "- your password opens your keys too",
			}, func() error {
				relock, err := c.App.Elevate(c.Ctx, "turn off two-password mode")
				if err != nil {
					return err
				}
				defer relock()
				password, err := c.App.Creds.Password("turn off two-password mode")
				if err != nil {
					return err
				}
				u, err := c.App.Unlock(c.Ctx)
				if err != nil {
					return err
				}
				// One secret from here on: the keys are locked with the password
				// that signs in, and its verifier is written again with them.
				return u.Relock(c.Ctx, c.App.API, password, true)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// refuseOnePassword answers a command that only means something for an account
// keeping two secrets.
func refuseOnePassword() error {
	return kit.Fail("This account does not use two-password mode.").
		Hint(kit.Program + " account settings password set")
}

// refuseLoginPassword catches the second password being set to the one that
// signs in, which leaves an account in two-password mode with one password to
// remember and nothing on screen saying so.
func refuseLoginPassword(login, chosen string) error {
	if login == chosen {
		return kit.Fail("That is the password you sign in with.").
			Hint("to keep one password for both, leave two-password mode: " +
				kit.Program + " account settings second-password disable")
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
