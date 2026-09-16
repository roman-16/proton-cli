package account

import (
	"github.com/roman-16/proton-cli/internal/account/session"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// A profile is a named session slot on this machine. Two similar-sounding lists
// live under `account`, and the distinction matters: profiles are local, sessions
// are Proton's. Naming both explicitly is clearer than making one of them the
// bare `account list`.

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
		// No authentication: this reads the filesystem, and being able to see
		// which accounts are configured without contacting Proton is the point.
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

// Removing a profile removes everything this machine kept for it, which is the
// session, the references it has been shown, and the index of its contents. A
// profile is the whole of what one account is here, so leaving any of it behind
// would leave a copy of an account nothing can name.
func profilesDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Remove a profile and everything it keeps on this machine",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			// A profile names a file, so the names are judged before anything is
			// removed rather than at the point of removal.
			names, err := profile.Names(c.Args)
			if err != nil {
				return kit.Fail("%v.", err)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "profiles", Count: len(names),
				Name: single(c.Args), IDs: c.Args,
			}, func() error {
				for _, name := range names {
					if err := app.Forget(name); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
