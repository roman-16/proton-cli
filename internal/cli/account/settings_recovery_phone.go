package account

import (
	"strings"
	"unicode"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// The number Proton texts when the account is locked out. It works the way the
// recovery address does: having one and allowing a password reset with it are
// separate, and a number counts for neither until it is verified.

// What Proton will take as a phone number: a country code and between these
// many digits, which is what the international numbering plan allows.
const (
	minPhoneDigits = 7
	maxPhoneDigits = 15
)

// recoveryPhoneView is what `recovery-phone get` answers.
type recoveryPhoneView struct {
	Number        string `json:"number"`
	Verified      bool   `json:"verified"`
	AllowRecovery bool   `json:"allow_recovery"`
}

func recoveryPhoneCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recovery-phone",
		Short: "The number Proton texts if you lose access",
	}
	c.AddCommand(recoveryPhoneGetCmd(), recoveryPhoneSetCmd(), recoveryPhoneVerifyCmd(),
		recoveryPhoneEnableCmd(), recoveryPhoneDisableCmd())
	return c
}

func recoveryPhoneGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show the recovery number and what it may do",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			phone := security.RecoveryPhone
			view := recoveryPhoneView{
				Number: phone.Number, Verified: phone.Verified, AllowRecovery: phone.AllowRecovery,
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Number", Value: orNone(view.Number), Always: true},
					{Label: "Verified", Value: yesNo(view.Verified), Always: true},
					{Label: "Allow Recovery", Value: kit.OnOffText(boolInt(view.AllowRecovery)), Always: true},
				},
			})
		}),
	}
}

func recoveryPhoneSetCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "set PHONE",
		Short: "Set the recovery number",
		Long: "Set the recovery number.\n\n" +
			"Your password is asked for. The number needs its country code, as\n" +
			"+43 660 1234567. `set none` removes the number, and with it any way to\n" +
			"reset your password by SMS.\n\n" +
			"A new number is unverified until a code Proton texts it is handed back:\n" +
			"`" + kit.Program + " account settings recovery-phone verify` sends one.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			number, err := recoveryNumber(c.Args[0])
			if err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			// Proton refuses a number that is already the one there, and a
			// removal where there is nothing to remove, in terms of its own
			// making. Both are worth saying here instead.
			if samePhone(security.RecoveryPhone.Number, number) {
				if number == "" {
					return refuseNoRecoveryPhone()
				}
				return kit.Fail("That is already your recovery phone.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Set, Count: 1, Name: "recovery phone",
				Detail: "to " + orNone(number),
				Extra:  map[string]any{"number": number},
			}, func() error {
				if err := c.App.Account.SetRecoveryPhone(c.Ctx, number); err != nil {
					return err
				}
				if number != "" {
					c.Note("Not verified yet: %s account settings recovery-phone verify", kit.Program)
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryPhoneVerifyCmd() *cobra.Command {
	var code string
	c := &cobra.Command{
		Use:   "verify",
		Short: "Confirm the recovery number with a code sent by SMS",
		Long: "Confirm the recovery number with a code sent by SMS.\n\n" +
			"With no --code, Proton texts the number a code. Run it again with --code to\n" +
			"hand the code back, which is what marks the number verified.\n\n" +
			"Until then Proton will not reset your password by SMS.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			phone := security.RecoveryPhone
			switch {
			case phone.Number == "":
				return refuseNoRecoveryPhone()
			case phone.Verified:
				return kit.Fail("Your recovery phone is already verified.")
			}
			if code == "" {
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Sent, Kind: "verification codes", Count: 1,
					Detail: "to " + phone.Number,
				}, func() error {
					if err := c.App.Account.SendPhoneCode(c.Ctx, phone.Number); err != nil {
						return err
					}
					c.Note("Run this again with --code once it arrives.")
					return nil
				})
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Verified, Kind: "recovery phones", Count: 1, Name: phone.Number,
			}, func() error {
				return c.App.Account.VerifyPhone(c.Ctx, phone.Number, code)
			})
		}),
	}
	c.Flags().StringVar(&code, "code", "", "The code Proton texted the number")
	return c
}

func recoveryPhoneEnableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "enable",
		Short: "Allow password resets by SMS",
		Long: "Allow password resets by SMS.\n\n" +
			"Your password is asked for. An account with no recovery number, and one\n" +
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
			case security.RecoveryPhone.Number == "":
				return refuseNoRecoveryPhone()
			case security.Sentinel:
				return refuseSentinel()
			case security.RecoveryPhone.AllowRecovery:
				return kit.Fail("Password resets by SMS are already allowed.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "recovery by phone",
				Detail: "- Proton can reset your password through " + security.RecoveryPhone.Number,
			}, func() error {
				return c.App.Account.AllowRecoveryByPhone(c.Ctx, true)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryPhoneDisableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Stop allowing password resets by SMS",
		Long: "Stop allowing password resets by SMS.\n\n" +
			"Your password is asked for. The number stays, and Proton goes on using it\n" +
			"for security notices.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if !security.RecoveryPhone.AllowRecovery {
				return kit.Fail("Password resets by SMS are already off.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "recovery by phone",
				Detail: "- the number is kept for security notices",
			}, func() error {
				return c.App.Account.AllowRecoveryByPhone(c.Ctx, false)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// recoveryNumber judges what was typed before anything is sent, and writes it
// the way Proton keeps one: a country code and digits, with whatever a person
// spaced it out with taken back off. The word that removes the number is the
// other thing it takes.
func recoveryNumber(arg string) (string, error) {
	if strings.EqualFold(arg, kit.None) {
		return "", nil
	}
	refusal := kit.Fail("%q is not a phone number.", arg).
		Hint("give it with its country code: "+
			kit.Program+" account settings recovery-phone set '+43 660 1234567'",
			kit.Program+" account settings recovery-phone set none")
	for i, r := range arg {
		switch {
		case unicode.IsDigit(r):
		case r == '+' && i == 0:
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '/':
		default:
			return "", refusal
		}
	}
	digits := onlyDigits(arg)
	if !strings.HasPrefix(arg, "+") || len(digits) < minPhoneDigits || len(digits) > maxPhoneDigits {
		return "", refusal
	}
	return "+" + digits, nil
}

// samePhone reports whether two numbers are one number. How a number is spaced
// is not part of it, and Proton hands one back written its own way.
func samePhone(a, b string) bool { return onlyDigits(a) == onlyDigits(b) }

func onlyDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func refuseNoRecoveryPhone() error {
	return kit.Fail("This account has no recovery phone.").
		Hint(kit.Program + " account settings recovery-phone set '+43 660 1234567'")
}
