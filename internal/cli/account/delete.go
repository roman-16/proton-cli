package account

import (
	"strings"
	"unicode/utf8"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

const shortestFeedback = 10

func deleteCmd() *cobra.Command {
	var (
		message string
		reauth  kit.Reauth
	)
	reason := &kit.Enum{
		Name: "reason", Usage: "The main reason you are leaving", Values: acctsvc.DeletionReasons,
	}
	c := &cobra.Command{
		Use:   "delete",
		Short: "Delete the account and everything in it",
		Long: "Delete the account and everything in it: every address, message, contact,\n" +
			"event, file and password. It cannot be reactivated.\n\n" +
			"Proton may refuse before anything is asked. Otherwise you confirm, then give\n" +
			"your password. A member of a family or duo plan leaves the plan first.\n\n" +
			"--message says in at least ten characters why you are leaving, and is left\n" +
			"out with --reason merge. It reaches the people who improve Proton, not\n" +
			"support.\n\n" +
			"Afterwards this machine keeps nothing of the account: the profile's session,\n" +
			"index and short-ID cache are removed.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			why, err := reason.Value()
			if err != nil {
				return err
			}
			if why != acctsvc.ReasonMerge && utf8.RuneCountInString(strings.TrimSpace(message)) < shortestFeedback {
				return kit.Fail("--message is required: say in at least ten characters why you are leaving.")
			}
			if err := reauth.Supply(c); err != nil {
				return err
			}
			deletion, err := c.App.Account.CanDelete(c.Ctx)
			if err != nil {
				return err
			}
			if deletion.Visionary {
				c.Warn("Your Visionary plan cannot be bought again.")
			}
			if why == acctsvc.ReasonMerge {
				c.Warn("Add %s to the other account straight away, or the address is lost for good.",
					deletion.Email)
			}
			detail := "with all its mail, contacts, events, files and passwords"
			if deletion.FamilyMember {
				detail += ", after leaving the plan it shares"
			}
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "accounts", Count: 1, Name: deletion.Email, Detail: detail,
			}, func() error {
				relock, err := c.App.Elevate(c.Ctx, proton.ScopePassword, "delete your account")
				if err != nil {
					return err
				}
				if err := c.App.Account.Delete(c.Ctx, deletion, why, message); err != nil {
					relock()
					return err
				}
				return app.Forget(c.App.Profile)
			}); err != nil {
				return err
			}
			if !c.App.DryRun {
				c.Note("The session, index and short-ID cache of profile %q were removed from this machine.",
					c.App.Profile.String())
			}
			return nil
		}),
	}
	reason.Register(c)
	_ = c.MarkFlagRequired("reason")
	c.Flags().StringVar(&message, "message", "", "Why you are leaving, in at least ten characters")
	reauth.Declare(c)
	return c
}
