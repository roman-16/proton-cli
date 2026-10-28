package meet

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	meetsvc "github.com/roman-16/proton-cli/internal/service/meet"
	"github.com/roman-16/proton-cli/internal/ui"
)

func roomsCmd() *cobra.Command {
	c := &cobra.Command{Use: "rooms", Short: "Rooms that stay open, your personal room first"}
	c.AddCommand(roomsCreateCmd(), roomsDeleteCmd(), roomsGetCmd(), roomsListCmd(), roomsRotateCmd(), roomsUpdateCmd())
	return c
}

const refParagraph = "REF is the ID its link shows after id-, such as E87W2DAJ60, the whole link, its\n" +
	"title, or personal for your personal room."

func roomsListCmd() *cobra.Command {
	var held kit.Held[row]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your rooms",
		Long: "List your rooms, your personal room first.\n\n" +
			"The links are not shown: anyone holding one can join. `rooms get` shows a link.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			all, err := rooms.list(c)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[row]{
				Noun: "rooms",
				Columns: []ui.Column[row]{
					{Header: "ID", ID: true, Cell: func(r row) string { return r.ID }},
					{Header: "TITLE", Flex: true, Handle: true, Cell: func(r row) string { return r.Title }},
					{Header: "TYPE", Cell: func(r row) string { return r.Type.String() }},
				},
			}, rowsOf(all))
		}),
	}
	held.Register(c, "rooms",
		kit.Key[row]{Name: "title", Less: func(a, b row) int {
			if ap, bp := a.Type == meetsvc.Personal, b.Type == meetsvc.Personal; ap != bp {
				if ap {
					return -1
				}
				return 1
			}
			return kit.Fold(a.Title, b.Title)
		}},
	)
	return c
}

func roomsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one room, link and all",
		Long:  "Show one room, link and all.\n\n" + refParagraph,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			m, err := rooms.resolve(c)
			if err != nil {
				return err
			}
			return show(c, m)
		}),
	}
}

func roomsCreateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a room and print its link",
		Long:  "Create a room and print its link. A room has no time: its link works until you\ndelete it.",
		RunE: kit.Run([]kit.Step{title(&name)}, func(c *kit.Invocation) error {
			if strings.TrimSpace(name) == "" {
				return kit.Fail("A room needs a title.").Hint("--title 'Team room'")
			}
			var made *meetsvc.Meeting
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "rooms", Count: 1, Name: name, AnswerFollows: true,
			}, func() error {
				var err error
				made, err = c.App.Meet.Create(c.Ctx, meetsvc.NewMeeting{Title: name, Type: meetsvc.Room})
				return err
			}); err != nil {
				return err
			}
			if made == nil {
				return nil
			}
			return show(c, made)
		}),
	}
	c.Flags().StringVar(&name, "title", "", "Set the title")
	return c
}

func roomsUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a room",
		Long:  "Rename a room. Its link stays as it is.\n\n" + refParagraph,
		RunE: kit.Run([]kit.Step{title(&name), kit.StepExpand}, func(c *kit.Invocation) error {
			if !c.Changed("title") {
				return kit.Fail("Nothing to change.").Hint("--title 'Team room'")
			}
			m, err := rooms.resolve(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "rooms", Count: 1, Name: name, IDs: []string{m.Name},
			}, func() error {
				return c.App.Meet.Rename(c.Ctx, m, name)
			})
		}),
	}
	c.Flags().StringVar(&name, "title", "", "Replace the title")
	return c
}

func roomsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF",
		Short: "Delete a room",
		Long:  "Delete a room. Its link stops working for everyone who has it.\n\n" + refParagraph,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			m, err := rooms.resolve(c)
			if err != nil {
				return err
			}
			if m.Type == meetsvc.Personal {
				return kit.Fail("Your personal room cannot be deleted.").
					Hint(kit.Program + " meet rooms rotate " + personal)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "rooms", Count: 1, Name: m.Title, IDs: []string{m.Name},
			}, func() error {
				return c.App.Meet.Delete(c.Ctx, m)
			})
		}),
	}
}

func roomsRotateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rotate REF",
		Short: "Give your personal room a new link",
		Long: "Give your personal room a new link, and print it. The old link stops working\n" +
			"for everyone who has it. It can change once every 24 hours.\n\n" +
			"REF is personal, the one room whose link can change.",
		RunE: kit.Run([]kit.Step{func(c *kit.Invocation) error {
			if c.Args[0] != personal {
				return kit.Fail("Only your personal room gets a new link.").
					Hint(kit.Program + " meet rooms rotate " + personal)
			}
			return nil
		}}, func(c *kit.Invocation) error {
			m, err := c.App.Meet.Personal(c.Ctx)
			if err != nil {
				return err
			}
			var rotated *meetsvc.Meeting
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Rotated, Count: 1, Name: "the link of your personal room", IDs: []string{m.Name},
				AnswerFollows: true,
			}, func() error {
				var err error
				rotated, err = c.App.Meet.RotatePersonal(c.Ctx, m.Title)
				return err
			}); err != nil {
				return err
			}
			if rotated == nil {
				return nil
			}
			return show(c, rotated)
		}),
	}
}
