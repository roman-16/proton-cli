package account

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func profilesCmd() *cobra.Command {
	c := &cobra.Command{Use: "profiles", Short: "Accounts signed in on this machine"}
	c.AddCommand(profilesListCmd(), profilesDeleteCmd())
	return c
}

func profilesListCmd() *cobra.Command {
	var held kit.Held[session.Profile]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the profiles with a saved session",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			profiles, err := session.Profiles(c.Ctx)
			if err != nil {
				return err
			}
			active := c.App.Profile.String()
			return held.Answer(c, ui.TableSpec[session.Profile]{
				Noun: "profiles",
				Columns: []ui.Column[session.Profile]{
					{Header: "PROFILE", Cell: func(p session.Profile) string { return p.Name }},
					{Header: "EMAIL", Flex: true, Cell: func(p session.Profile) string { return p.Email }},
					{Header: "UNLOCKED", Cell: func(p session.Profile) string { return yesNo(p.Unlocked) }},
					{Header: "SAVED", Cell: func(p session.Profile) string { return units.Time(p.PersistedAt) }},
					{
						Header: "ACTIVE",
						Role:   func(session.Profile) ui.Role { return ui.Success },
						Cell: func(p session.Profile) string {
							if p.Name == active {
								return ui.GlyphSuccess
							}
							return ""
						},
					},
				},
			}, profiles)
		}),
	}
	held.Register(c, "profiles",
		kit.Key[session.Profile]{Name: "name", Less: func(a, b session.Profile) int { return kit.Fold(a.Name, b.Name) }},
		kit.Key[session.Profile]{Name: "saved", Less: func(a, b session.Profile) int { return kit.Ints(a.PersistedAt, b.PersistedAt) }},
	)
	return c
}

func profilesDeleteCmd() *cobra.Command {
	var local bool
	c := &cobra.Command{
		Use:   "delete REF...",
		Short: "Remove a profile and everything it keeps on this machine",
		Long: "Remove a profile and everything it keeps on this machine, ending its\n" +
			"session at Proton first.\n\n" +
			"When Proton cannot be reached, that profile and the ones after it stay as\n" +
			"they are. Run the command again, or pass --local to remove them from this\n" +
			"machine only, leaving their sessions valid at Proton.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			names, err := profile.Names(c.Args)
			if err != nil {
				return kit.Fail("%v.", err)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "profiles", Count: len(names),
				Name: single(c.Args), IDs: c.Args,
			}, func() error {
				for _, name := range names {
					if err := c.App.Forget(c.Ctx, name, local); err != nil {
						return revokeFailed(err, kit.Program+" account profiles delete "+strings.Join(c.Args, " "))
					}
				}
				return nil
			})
		}),
	}
	c.Flags().BoolVar(&local, "local", false, "Remove the profiles from this machine only, leaving their sessions valid at Proton")
	return c
}
