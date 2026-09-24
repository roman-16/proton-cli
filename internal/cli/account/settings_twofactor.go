package account

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// The second factor: a code from an authenticator app, and what the security
// keys beside it amount to.
//
// Turning the app on is two steps with a person in between - a secret to put
// somewhere that can compute codes, then a code proving it arrived - so it is
// offered as one command that does both and as two that can be scripted.
//
// The keys themselves are a collection with verbs of its own, next door under
// `security-keys`. What belongs here is the one thing this command answers:
// what a sign-in will ask for.

// secretGroup is how many characters of a secret are printed together, which is
// how Proton's own screen breaks it up for somebody typing it into a phone.
const secretGroup = 4

// twoFactorView is what `two-factor get` answers. The keys are named rather
// than counted, and nothing else about them is here: `security-keys list` is
// where one is addressed.
type twoFactorView struct {
	AuthenticatorApp string   `json:"authenticator_app"`
	SecurityKeys     []string `json:"security_keys"`
}

// secretView is what `two-factor generate` answers: the secret to store, and
// the same thing as an authenticator app reads it.
type secretView struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

// codesView is what `two-factor enable` answers once it is on.
type codesView struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

func twoFactorCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "two-factor",
		Short: "What you are asked for at sign-in besides your password",
	}
	c.AddCommand(twoFactorGetCmd(), twoFactorGenerateCmd(), twoFactorEnableCmd(), twoFactorDisableCmd())
	return c
}

func twoFactorGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show what this account is asked for at sign-in",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			view := twoFactorView{
				AuthenticatorApp: kit.OnOffText(boolInt(security.TwoFactor.AuthenticatorApp)),
				SecurityKeys:     securityKeyNames(security.TwoFactor.SecurityKeys),
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Authenticator App", Value: view.AuthenticatorApp, Always: true},
					{Label: "Security Keys", Value: names(view.SecurityKeys), Always: true},
				},
			})
		}),
	}
}

func twoFactorGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate",
		Short: "Make a two-factor secret for an authenticator app",
		Long: "Make a two-factor secret for an authenticator app.\n\n" +
			"Nothing about the account changes. The secret waits until\n" +
			"`" + kit.Program + " account settings two-factor enable --totp CODE` confirms it with a\n" +
			"code computed from it, and making another replaces the one waiting.\n\n" +
			"It is printed once. Anyone holding it can produce your codes.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			secret, err := c.App.Account.TOTPSecret(c.Ctx)
			if err != nil {
				return err
			}
			account, err := c.App.Account.Get(c.Ctx)
			if err != nil {
				return err
			}
			view := secretView{Secret: secret, URI: acctsvc.OTPAuthURI(account.Email, secret)}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Secret", Value: grouped(view.Secret)},
					{Label: "URI", Value: view.URI},
				},
			})
		}),
	}
}

func twoFactorEnableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "enable",
		Short: "Ask for an authenticator app code at every sign-in",
		Long: "Ask for an authenticator app code at every sign-in.\n\n" +
			"A secret is printed and you are asked for a code from it. With --totp CODE\n" +
			"the code confirms the secret from the last `two-factor generate` instead,\n" +
			"which is how this runs with nobody to ask.\n\n" +
			"Recovery codes are printed once as it turns on. Each signs you in a single\n" +
			"time if you lose the app.\n\n" +
			"Security keys are registered with `" + kit.Program +
			" account settings security-keys create`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			confirming := c.Changed("totp")
			if !confirming && !c.UI().CanPrompt() {
				return kit.Fail("A code from the authenticator app is required to turn this on.").
					Hint(kit.Program+" account settings two-factor generate",
						"then run this again with --totp CODE").Exit(2)
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if security.TwoFactor.AuthenticatorApp {
				return kit.Fail("This account already asks for a code from an authenticator app.").
					Hint("to change the secret, turn it off and on again: " +
						kit.Program + " account settings two-factor disable")
			}
			var codes []string
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Enabled, Kind: "settings", Count: 1, Name: "authenticator app",
				Detail: "- a code is asked for at every sign-in", AnswerFollows: true,
			}, func() error {
				relock, err := c.App.Elevate(c.Ctx, proton.ScopePassword, "turn on two-factor authentication")
				if err != nil {
					return err
				}
				defer relock()
				// A code that arrived as a flag was computed from the secret the
				// last run printed, so minting another here would leave it
				// standing for a secret Proton has replaced.
				if !confirming {
					if err := showSecret(c); err != nil {
						return err
					}
				}
				code, err := c.App.Creds.TOTP()
				if err != nil {
					return err
				}
				codes, err = c.App.Account.TOTPEnable(c.Ctx, code)
				return staleSecret(err)
			}); err != nil {
				return err
			}
			if len(codes) == 0 {
				return nil
			}
			c.Warn("Keep these recovery codes somewhere safe. Each signs you in once if you lose the app.")
			return kit.Show(c, ui.RecordSpec{
				Object: codesView{RecoveryCodes: codes},
				Fields: []ui.Field{{Label: "Recovery Codes", Value: strings.Join(codes, "\n")}},
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func twoFactorDisableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Stop asking for an authenticator app code",
		Long: "Stop asking for an authenticator app code.\n\n" +
			"Your password and a current code are asked for. A recovery code works in\n" +
			"place of the code.\n\n" +
			"A registered security key goes on being asked for.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if !security.TwoFactor.AuthenticatorApp {
				return kit.Fail("This account does not ask for a code from an authenticator app.")
			}
			detail := "- no code is asked for at sign-in"
			if len(security.TwoFactor.SecurityKeys) > 0 {
				detail = "- your security key is still asked for"
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "authenticator app",
				Detail: detail,
			}, func() error {
				return c.App.Account.TOTPDisable(c.Ctx)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// showSecret mints a secret and puts it where somebody can store it before
// they are asked for a code from it.
//
// It goes to the instruction stream rather than to stdout: what this command
// answers with is the recovery codes, and a secret in the middle of that would
// be captured by whatever was reading them.
func showSecret(c *kit.Invocation) error {
	secret, err := c.App.Account.TOTPSecret(c.Ctx)
	if err != nil {
		return err
	}
	account, err := c.App.Account.Get(c.Ctx)
	if err != nil {
		return err
	}
	c.UI().Break()
	c.UI().Instruct("Add this to your authenticator app:")
	c.UI().Break()
	c.UI().Instruct("  Secret:  " + grouped(secret))
	c.UI().Instruct("  URI:     " + acctsvc.OTPAuthURI(account.Email, secret))
	c.UI().Break()
	return nil
}

// staleSecret says what a refused confirmation code usually means here.
//
// The ordinary wrong code is worth the ordinary sentence, but this command has
// a second way to earn it: a code computed from a secret an intervening run
// replaced is a correct code for the wrong secret, and no number of retries
// fixes that one.
func staleSecret(err error) error {
	if !proton.WrongTwoFactorCode(err) {
		return err
	}
	return kit.Fail("Proton did not accept that code.").
		Hint("a code lasts thirty seconds - try the next one",
			"if the secret is from an earlier run, make a new one: "+
				kit.Program+" account settings two-factor generate").Exit(2)
}

// grouped breaks a secret into blocks somebody can type off a screen.
func grouped(secret string) string {
	var b strings.Builder
	for i, r := range secret {
		if i > 0 && i%secretGroup == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// securityKeyNames is the registered keys as this command reports them. What
// `two-factor get` answers is what a sign-in will ask for, and a name is the
// whole of what that takes.
func securityKeyNames(keys []acctsvc.SecurityKey) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k.Name)
	}
	return out
}

// names is a list of things as a record shows it, and the word for none of
// them.
func names(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}
