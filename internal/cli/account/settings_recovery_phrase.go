package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// The twelve words that open the account when nothing else will.
//
// They stand for a second copy of the account's keys, made from the keys as
// they are the moment the phrase is set - which is why a phrase set before the
// keys were replaced still opens what it was made for and cannot open the
// account, and why setting a new one replaces the old.

// phraseView is what `recovery-phrase get` answers.
type phraseView struct {
	Status  string `json:"status"`
	Changed int64  `json:"changed,omitempty"`
}

// wordsView is what `recovery-phrase set` answers.
type wordsView struct {
	RecoveryPhrase string `json:"recovery_phrase"`
}

func recoveryPhraseCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "recovery-phrase",
		Short: "The twelve words that open your account without your password",
	}
	c.AddCommand(recoveryPhraseGetCmd(), recoveryPhraseSetCmd(), recoveryPhraseDisableCmd())
	return c
}

func recoveryPhraseGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Show whether a recovery phrase is set",
		Long: "Show whether a recovery phrase is set.\n\n" +
			"The status is one of: on, outdated, not set, off. An outdated phrase was\n" +
			"set before your keys were replaced: it still opens the data it was made\n" +
			"for, and it will not get you back into the account.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			view := phraseView{
				Status:  security.RecoveryPhrase.Status,
				Changed: security.RecoveryPhrase.Changed,
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Status", Value: view.Status, Always: true},
					{Label: "Changed", Value: changed(view.Changed)},
				},
			})
		}),
	}
}

func recoveryPhraseSetCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "set",
		Short: "Make a recovery phrase and print it",
		Long: "Make a recovery phrase and print it.\n\n" +
			"Your password is asked for. The twelve words are printed once and kept\n" +
			"nowhere: write them down. Anyone holding them can open the account.\n\n" +
			"A phrase that is already set is replaced and stops working.\n\n" +
			"Recover with `" + kit.Program + " account keys reactivate --recovery-phrase`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			detail := ""
			if security.RecoveryPhrase.Set() {
				detail = "- the previous one no longer works"
			}
			var phrase string
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Set, Count: 1, Name: "a new recovery phrase",
				Detail: detail, AnswerFollows: true,
			}, func() error {
				u, err := c.App.Unlock(c.Ctx)
				if err != nil {
					return err
				}
				phrase, err = u.NewRecoveryPhrase(c.Ctx, c.App.API)
				return err
			}); err != nil {
				return err
			}
			if phrase == "" {
				return nil
			}
			c.Warn("Write these words down and keep them safe. They open your account and your data.")
			return kit.Show(c, ui.RecordSpec{
				Object: wordsView{RecoveryPhrase: phrase},
				Fields: []ui.Field{{Label: "Recovery Phrase", Value: phrase}},
			})
		}),
	}
	reauth.Declare(c)
	return c
}

func recoveryPhraseDisableCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "disable",
		Short: "Remove the recovery phrase",
		Long: "Remove the recovery phrase.\n\n" +
			"Your password is asked for. The words you wrote down stop working, and\n" +
			"nothing else recovers the account through them.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			security, err := c.App.Account.Security(c.Ctx)
			if err != nil {
				return err
			}
			if security.RecoveryPhrase.Status == acctsvc.PhraseOff {
				return kit.Fail("This account has no recovery phrase.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Disabled, Kind: "settings", Count: 1, Name: "recovery phrase",
			}, func() error {
				return c.App.Account.DisableRecoveryPhrase(c.Ctx)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// changed renders when a phrase was set, and nothing for an account that has
// none to have set.
func changed(unix int64) string {
	if unix == 0 {
		return ""
	}
	return units.Time(unix)
}
