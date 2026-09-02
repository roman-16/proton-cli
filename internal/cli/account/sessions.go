package account

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Sessions are the ones Proton holds for the account, across every device. This
// mirrors the "Sessions" section of Proton's account settings, which lists them
// and offers Revoke and Revoke all other sessions.

func sessionsCmd() *cobra.Command {
	c := &cobra.Command{Use: "sessions", Short: "Sessions Proton holds for this account"}
	c.AddCommand(sessionsListCmd(), sessionsRevokeCmd())
	return c
}

func sessionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every signed-in session",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			sessions, err := c.App.API.Sessions(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[proton.Session]{
				Noun:  "sessions",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[proton.Session]{
					{Header: "ID", ID: true, Cell: func(s proton.Session) string { return s.UID }},
					{Header: "CLIENT", Flex: true, Cell: func(s proton.Session) string { return s.ClientID }},
					{Header: "CREATED", Cell: func(s proton.Session) string { return units.Time(s.CreateTime) }},
					{
						Header: "CURRENT",
						Role:   func(proton.Session) ui.Role { return ui.Success },
						Cell: func(s proton.Session) string {
							if s.Current {
								return ui.GlyphSuccess
							}
							return ""
						},
					},
				},
			}, sessions, func(s proton.Session) []string { return []string{s.UID} })
		}),
	}
}

func sessionsRevokeCmd() *cobra.Command {
	var others bool
	c := &cobra.Command{
		Use:   "revoke [REF...]",
		Short: "Invalidate sessions at Proton",
		Long: "Invalidate sessions at Proton.\n\n" +
			"A revoked session can no longer decrypt the key password sealed into its\n" +
			"saved file, so revoking makes a leaked session file worthless.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if others {
				if len(c.Args) > 0 {
					return kit.Fail("--others revokes every other session, so it takes no REF.")
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: ui.Revoked, Kind: "sessions", Count: 1,
					Detail: "other than this one",
				}, func() error { return c.App.API.RevokeOtherSessions(c.Ctx) })
			}
			if len(c.Args) == 0 {
				return kit.Fail("Nothing selected.").
					Hint("pass a session REF, or --others to revoke every session but this one.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Revoked, Kind: "sessions", Count: len(c.Args), IDs: c.Args,
			}, func() error {
				for _, uid := range c.Args {
					if err := c.App.API.RevokeSession(c.Ctx, uid); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().BoolVar(&others, "others", false, "Revoke every session except this one")
	return c
}
