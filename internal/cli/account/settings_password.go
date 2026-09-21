package account

import (
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// The password the account signs in with.
//
// It is the credential every other command in this tree is measured against:
// the current one is proved before anything changes, and a run that cannot be
// asked for it can change none of them.

func passwordCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "password",
		Short: "The password you sign in with",
	}
	c.AddCommand(passwordSetCmd())
	return c
}

func passwordSetCmd() *cobra.Command {
	var (
		chosen kit.NewPassword
		reauth kit.Reauth
	)
	c := &cobra.Command{
		Use:   "set",
		Short: "Change your password",
		Long: "Change your password.\n\n" +
			"Your current password is asked for first. The new one comes from\n" +
			"--new-password-file, which takes - for stdin, or from a prompt that asks for\n" +
			"it twice. It needs at least eight characters and cannot be the one it\n" +
			"replaces.\n\n" +
			"In two-password mode this changes the password you sign in with, and your\n" +
			"keys stay locked with your second password.\n\n" +
			"This session goes on working. Another machine signed in to this account asks\n" +
			"for the new password the next time it opens your keys.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := chosen.Supply(c); err != nil {
				return err
			}
			// What arrived on the command line is judged before anything is asked
			// of Proton, so a run with nobody to ask finds out what is wrong with
			// it whether or not it is signed in. Which of the account's passwords
			// is being set decides a prompt's wording and nothing else, so naming
			// one here does not settle a question this has yet to ask.
			if chosen.Offered() {
				secret, err := chosen.Chosen(c, app.AccountPassword)
				if err != nil {
					return err
				}
				if c.App.Creds.PasswordOffered() {
					current, err := c.App.Creds.Password("change your password")
					if err != nil {
						return err
					}
					if err := refuseUnchanged(current, secret); err != nil {
						return err
					}
				}
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			kind, name := app.AccountPassword, "your password"
			if security.TwoPasswordMode {
				kind, name = app.LoginPassword, "your login password"
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Count: 1, Name: name,
			}, func() error {
				// The current password is proved before the new one is asked for.
				// The other order has somebody invent a password and only then
				// finds out whether they were entitled to set one.
				relock, err := c.App.Elevate(c.Ctx, "change your password")
				if err != nil {
					return err
				}
				defer relock()
				current, err := c.App.Creds.Password("change your password")
				if err != nil {
					return err
				}
				secret, err := chosen.Chosen(c, kind)
				if err != nil {
					return err
				}
				if err := refuseUnchanged(current, secret); err != nil {
					return err
				}
				if security.TwoPasswordMode {
					return c.App.Account.SetLoginPassword(c.Ctx, secret, false)
				}
				u, err := c.App.Unlock(c.Ctx)
				if err != nil {
					return err
				}
				return u.Relock(c.Ctx, c.App.API, secret, true)
			})
		}),
	}
	chosen.Declare(c)
	reauth.Declare(c)
	return c
}

// refuseUnchanged catches a password being set to the one it replaces, which
// is what pointing both files at one secret amounts to.
func refuseUnchanged(current, chosen string) error {
	if current == chosen {
		return kit.Fail("The new password is the one you already have.")
	}
	return nil
}
