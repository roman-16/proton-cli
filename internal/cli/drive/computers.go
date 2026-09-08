package drive

import (
	stdctx "context"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	drivesvc "github.com/roman-16/proton-cli/internal/service/drive"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func computersCmd() *cobra.Command {
	c := &cobra.Command{Use: "computers", Short: "Computers syncing files to Drive"}
	c.AddCommand(computersListCmd(), computersUpdateCmd(), computersDeleteCmd())
	return c
}

func computerColumns() []ui.Column[drivesvc.Computer] {
	return []ui.Column[drivesvc.Computer]{
		{Header: "ID", ID: true, Cell: func(c drivesvc.Computer) string { return c.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(c drivesvc.Computer) string { return c.Name }},
		{Header: "SYSTEM", Cell: func(c drivesvc.Computer) string { return c.System }},
		{Header: "SYNCED", Cell: func(c drivesvc.Computer) string { return units.Time(c.LastSync) }},
	}
}

func computerList(c *kit.Invocation) *kit.Lookup[drivesvc.Computer] {
	return &kit.Lookup[drivesvc.Computer]{
		Kind:   "computer",
		Load:   func(ctx stdctx.Context) ([]drivesvc.Computer, error) { return c.App.Drive.Computers(ctx) },
		ID:     func(c drivesvc.Computer) string { return c.ID },
		Handle: func(c drivesvc.Computer) string { return c.Name },
	}
}

func computersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the computers syncing to Drive",
		Long: "List the computers syncing to Drive.\n\n" +
			"A computer appears here once the Proton Drive desktop app is signed in on\n" +
			"it; nothing else puts one on the list. SYNCED is when it last synced, and a\n" +
			"dash means it never has.\n\n" +
			"What a computer syncs is a tree of its own. Pass `--computer REF` to any\n" +
			"`items` command to work inside it.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			computers, err := computerList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[drivesvc.Computer]{
				Noun: "computers", Columns: computerColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, computers)
		}),
	}
}

func computersUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a computer",
		Long: "Rename a computer.\n\n" +
			"The name is the computer's everywhere: the desktop app and the web client\n" +
			"show what you set here. The files it syncs are untouched.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			computer, err := computerList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "computers", Count: 1, Name: computer.Name,
				Detail: `to "` + name + `"`, IDs: []string{computer.ID},
			}, func() error {
				return c.App.Drive.RenameComputer(c.Ctx, computer, name)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name for the computer")
	_ = c.MarkFlagRequired("name")
	return c
}

func computersDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Remove computers from Drive",
		Long: "Remove computers from Drive.\n\n" +
			"The computer stops syncing and leaves the list. Set it up again by signing\n" +
			"the desktop app in on it once more.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "computers", computerColumns(), computerList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "computers", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(c drivesvc.Computer) string { return c.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, computer := range sel.Rows {
					if err := c.App.Drive.DeleteComputer(c.Ctx, computer); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
