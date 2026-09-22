package account

import (
	"github.com/roman-16/proton-cli/internal/account/fork"
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
	c.AddCommand(sessionsCreateCmd(), sessionsListCmd(), sessionsRevokeCmd())
	return c
}

func sessionsCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create CODE",
		Short: "Sign another device in from this one",
		Long: "Sign another device in from this one.\n\n" +
			"CODE is what the other device is showing: run `" + kit.Program +
			" account login --qr`\nthere, or read the code off a Proton app's sign-in " +
			"screen. That device is\nsigned in as this account, with no password typed " +
			"on it, and stays signed in\nafter this one signs out.\n\n" +
			"A code works once. Only ever pass one you are looking at yourself.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			code, err := fork.Parse(c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.SignedIn, Count: 1, Name: c.App.Email(),
				Detail: "on the device that showed the code",
			}, func() error { return c.App.SignInDevice(c.Ctx, code) })
		}),
	}
}

func sessionsListCmd() *cobra.Command {
	var held kit.Held[proton.Session]
	c := &cobra.Command{
		Use:   "list",
		Short: "List every signed-in session",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			sessions, err := c.App.API.Sessions(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[proton.Session]{
				Noun: "sessions",
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
			}, sessions)
		}),
	}
	held.Register(c, "sessions",
		kit.Key[proton.Session]{Name: "created", Less: func(a, b proton.Session) int { return kit.Ints(a.CreateTime, b.CreateTime) }},
		kit.Key[proton.Session]{Name: "client", Less: func(a, b proton.Session) int { return kit.Fold(a.ClientID, b.ClientID) }},
	)
	return c
}

func sessionsRevokeCmd() *cobra.Command {
	var (
		others bool
		reauth kit.Reauth
	)
	c := &cobra.Command{
		Use:   "revoke [REF...]",
		Short: "Invalidate sessions at Proton",
		Long: "Invalidate sessions at Proton.\n\n" +
			"A revoked session can no longer decrypt the key password sealed into its\n" +
			"saved file, so revoking makes a leaked session file worthless.\n\n" +
			"Your password is asked for again. Pass it with --password-file when there\n" +
			"is nobody to ask.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
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
	reauth.Declare(c)
	return c
}
