package pass

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
)

// The password Pass itself can be put behind.
//
// It is the one credential this CLI writes, and it opens nothing: a verifier is
// stored or deleted, and the password never leaves the machine. Which is why the
// flags every other Pass command answers a locked session with are declared on
// these two - here the extra password is the subject rather than the toll.

// minExtraPassword is the length Proton's own clients hold a new one to.
const minExtraPassword = 8

func extraPasswordCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "extra-password",
		Short: "The extra password Pass can be protected with",
	}
	c.AddCommand(extraPasswordGetCmd(), extraPasswordEnableCmd(), extraPasswordDisableCmd())
	return c
}

func extraPasswordGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show whether Pass has an extra password",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			extra, err := c.App.Pass.ExtraPassword(c.Ctx)
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: extra,
				Fields: []ui.Field{
					{Label: "Status", Value: kit.OnOffText(boolInt(extra.Enabled)), Always: true},
				},
			})
		}),
	}
}

func extraPasswordEnableCmd() *cobra.Command {
	var extra kit.ExtraPassword
	c := &cobra.Command{
		Use:   "enable",
		Short: "Protect Pass with an extra password",
		Long: "Protect Pass with an extra password.\n\n" +
			"The password comes from --extra-password-file, from --extra-password-stdin, or\n" +
			"from a prompt that asks for it twice. It needs at least eight characters.\n\n" +
			"Keep it safe: without it nothing opens Pass, on any device. Your other devices\n" +
			"ask for it the next time they open Pass, and this session goes on working.\n\n" +
			"A Pass that already has one is refused. Turn it off and on again to change it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := extra.Supply(c); err != nil {
				return err
			}
			// A password that arrived on the command line is judged before
			// anything is asked of Proton, so a run with nobody to ask finds out
			// what is wrong with it whether or not it is signed in. A typed one is
			// asked for once the change is going ahead, and not before: a prompt
			// for a password this command is about to refuse is a prompt nobody
			// should have answered.
			if c.App.Creds.ExtraPasswordOffered() {
				if _, err := chosenExtraPassword(c); err != nil {
					return err
				}
			}
			has, err := c.App.Pass.ExtraPassword(c.Ctx)
			if err != nil {
				return err
			}
			if has.Enabled {
				return kit.Fail("Pass already has an extra password.").
					Hint("to change it, turn it off and on again: " +
						kit.Program + " pass settings extra-password disable")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "extra password",
				Detail: "- Pass now asks for it on every device",
			}, func() error {
				password, err := chosenExtraPassword(c)
				if err != nil {
					return err
				}
				if err := c.App.Pass.ExtraPasswordSet(c.Ctx, password); err != nil {
					return err
				}
				keepReachingPass(c, password)
				return nil
			})
		}),
	}
	extra.Declare(c)
	return c
}

func extraPasswordDisableCmd() *cobra.Command {
	var extra kit.ExtraPassword
	c := &cobra.Command{
		Use:   "disable",
		Short: "Remove the extra password from Pass",
		Long: "Remove the extra password from Pass.\n\n" +
			"The password is asked for first, or comes from --extra-password-file or\n" +
			"--extra-password-stdin.\n\n" +
			"Pass then opens with your account password alone, on every device. This\n" +
			"session goes on working.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := extra.Supply(c); err != nil {
				return err
			}
			if c.App.Creds.ExtraPasswordOffered() {
				if _, err := c.App.Creds.ExtraPassword(); err != nil {
					return err
				}
			}
			has, err := c.App.Pass.ExtraPassword(c.Ctx)
			if err != nil {
				return err
			}
			if !has.Enabled {
				return kit.Fail("Pass has no extra password.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "extra password",
				Detail: "- Pass opens with your account password alone",
			}, func() error {
				password, err := c.App.Creds.ExtraPassword()
				if err != nil {
					return err
				}
				// What authorises the removal is the secret being removed, so it
				// is proved before it is asked for back.
				if err := c.App.API.ProveExtraPassword(c.Ctx, []byte(password)); err != nil {
					return err
				}
				return c.App.Pass.ExtraPasswordRemove(c.Ctx)
			})
		}),
	}
	extra.Declare(c)
	return c
}

// chosenExtraPassword is the password Pass is to be protected with, held to the
// length Pass holds it to before it is sent anywhere.
func chosenExtraPassword(c *kit.Invocation) (string, error) {
	password, err := c.App.Creds.ChooseExtraPassword()
	if err != nil {
		return "", err
	}
	if len([]rune(password)) < minExtraPassword {
		return "", kit.Fail("An extra password needs at least eight characters.")
	}
	return password, nil
}

// keepReachingPass proves the password just set, for a session Proton has locked
// out of Pass by the setting of it.
//
// A failure here is not the command's failure: the extra password is set either
// way, and what is lost is this session's reach into Pass until it is given the
// password again. Saying so is worth more than an exit code that reads as though
// nothing had happened.
func keepReachingPass(c *kit.Invocation, password string) {
	if err := provePassScope(c, password); err != nil {
		slog.WarnContext(c.Ctx, "The extra password is set. This session could not prove it, "+
			"so the next pass command asks for it.", "error", err.Error())
	}
}

// provePassScope gives the session what Pass now wants of it, unless Proton left
// it holding that already - which is Proton's to decide, so it is asked.
func provePassScope(c *kit.Invocation, password string) error {
	reaches, err := c.App.ReachesPass(c.Ctx)
	if err != nil || reaches {
		return err
	}
	return c.App.UnlockPass(c.Ctx, password)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
