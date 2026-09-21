package account

import (
	"net/mail"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// The address Proton writes to when the account is locked out.
//
// Having one and allowing a password reset with it are two things: Proton
// sends security notices to the address either way, and only the second is
// what `enable` and `disable` are about.

// recoveryEmailView is what `recovery-email get` answers.
type recoveryEmailView struct {
	Address       string `json:"address"`
	Verified      bool   `json:"verified"`
	AllowRecovery bool   `json:"allow_recovery"`
}

func recoveryEmailCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recovery-email",
		Short: "The address Proton writes to if you lose access",
	}
	c.AddCommand(recoveryEmailGetCmd(), recoveryEmailSetCmd(), recoveryEmailVerifyCmd(),
		recoveryEmailEnableCmd(), recoveryEmailDisableCmd())
	return c
}

func recoveryEmailGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the recovery address and what it may do",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			email := security.RecoveryEmail
			view := recoveryEmailView{
				Address: email.Address, Verified: email.Verified, AllowRecovery: email.AllowRecovery,
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Address", Value: orNone(view.Address), Always: true},
					{Label: "Verified", Value: yesNo(view.Verified), Always: true},
					{Label: "Allow Recovery", Value: kit.OnOffText(boolInt(view.AllowRecovery)), Always: true},
				},
			})
		}),
	}
}

func recoveryEmailSetCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "set EMAIL",
		Short: "Set the recovery address",
		Long: "Set the recovery address.\n\n" +
			"Your password is asked for. `set none` removes the address, and with it any\n" +
			"way to reset your password by email.\n\n" +
			"A new address is unverified until you open the link Proton mails to it:\n" +
			"`" + kit.Program + " account settings recovery-email verify` sends one.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			address, err := recoveryAddress(c.Args[0])
			if err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			// Proton refuses an address that is already the one there, and a
			// removal where there is nothing to remove, in terms of its own
			// making. Both are worth saying here instead.
			if security.RecoveryEmail.Address == address {
				if address == "" {
					return refuseNoRecoveryEmail()
				}
				return kit.Fail("That is already your recovery email.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Set, Count: 1, Name: "recovery email",
				Detail: "to " + orNone(address),
				Extra:  map[string]any{"address": address},
			}, func() error {
				if err := c.App.Account.SetRecoveryEmail(c.Ctx, address); err != nil {
					return err
				}
				if address != "" {
					c.Note("Not verified yet: %s account settings recovery-email verify", kit.Program)
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryEmailVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Send a verification email to the recovery address",
		Long: "Send a verification email to the recovery address.\n\n" +
			"Open the link in it to finish. Until then Proton will not reset your\n" +
			"password by email.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			email := security.RecoveryEmail
			switch {
			case email.Address == "":
				return refuseNoRecoveryEmail()
			case email.Verified:
				return kit.Fail("Your recovery email is already verified.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Sent, Kind: "verification emails", Count: 1,
				Detail: "to " + email.Address,
			}, func() error {
				if err := c.App.Account.SendEmailVerification(c.Ctx); err != nil {
					return err
				}
				c.Note("Open the link in it to finish.")
				return nil
			})
		}),
	}
}

func recoveryEmailEnableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "enable",
		Short: "Allow password resets by email",
		Long: "Allow password resets by email.\n\n" +
			"Your password is asked for. An account with no recovery address, and one\n" +
			"with Proton Sentinel turned on, is refused.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			switch {
			case security.RecoveryEmail.Address == "":
				return refuseNoRecoveryEmail()
			case security.Sentinel:
				return refuseSentinel()
			case security.RecoveryEmail.AllowRecovery:
				return kit.Fail("Password resets by email are already allowed.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "recovery by email",
				Detail: "- Proton can reset your password through " + security.RecoveryEmail.Address,
			}, func() error {
				return c.App.Account.AllowRecoveryByEmail(c.Ctx, true)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryEmailDisableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Stop allowing password resets by email",
		Long: "Stop allowing password resets by email.\n\n" +
			"Your password is asked for. The address stays, and Proton goes on sending\n" +
			"security notices to it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if !security.RecoveryEmail.AllowRecovery {
				return kit.Fail("Password resets by email are already off.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "recovery by email",
				Detail: "- the address is kept for security notices",
			}, func() error {
				return c.App.Account.AllowRecoveryByEmail(c.Ctx, false)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// recoveryAddress judges what was typed before anything is sent: an address
// Proton would refuse, and the word that takes the one there is away.
func recoveryAddress(arg string) (string, error) {
	if strings.EqualFold(arg, kit.None) {
		return "", nil
	}
	parsed, err := mail.ParseAddress(arg)
	if err != nil || parsed.Address != arg {
		return "", kit.Fail("%q is not an email address.", arg).
			Hint("proton account settings recovery-email set jane.roe@example.com",
				"proton account settings recovery-email set none")
	}
	return arg, nil
}

func refuseNoRecoveryEmail() error {
	return kit.Fail("This account has no recovery email.").
		Hint(kit.Program + " account settings recovery-email set jane.roe@example.com")
}

// refuseSentinel answers the one account whose recovery Proton decides for
// itself.
func refuseSentinel() error {
	return kit.Fail("Proton Sentinel is on, so Proton chooses which recovery methods this account may use.")
}

// orNone is a value a record shows, and the word for not having one.
func orNone(value string) string {
	if value == "" {
		return kit.None
	}
	return value
}
