package pass

import (
	"context"
	"strconv"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func vaultsCmd() *cobra.Command {
	c := &cobra.Command{Use: "vaults", Short: "The vaults your items live in"}
	c.AddCommand(vaultsListCmd(), vaultsGetCmd(), vaultsShareCmd(), vaultsCreateCmd(),
		vaultsTransferCmd(), vaultsUpdateCmd(), vaultsDeleteCmd())
	return c
}

func vaultColumns() []ui.Column[passsvc.Vault] {
	return []ui.Column[passsvc.Vault]{
		{Header: "ID", ID: true, Cell: func(v passsvc.Vault) string { return v.ShareID }},
		{Header: "NAME", Flex: true, Cell: func(v passsvc.Vault) string {
			if v.Name == "" {
				return "(could not be decrypted)"
			}
			return v.Name
		}},
		{Header: "MEMBERS", Right: true, Cell: func(v passsvc.Vault) string {
			return strconv.Itoa(v.Members)
		}},
		{Header: "OWNER", Cell: func(v passsvc.Vault) string { return yesNo(v.Owner) }},
		{Header: "SHARED", Cell: func(v passsvc.Vault) string { return yesNo(v.Shared) }},
	}
}

func vaultList(c *kit.Invocation) *kit.Lookup[passsvc.Vault] {
	return &kit.Lookup[passsvc.Vault]{
		Kind:   "vault",
		Load:   func(ctx context.Context) ([]passsvc.Vault, error) { return c.App.Pass.VaultsList(ctx) },
		ID:     func(v passsvc.Vault) string { return v.ShareID },
		Handle: func(v passsvc.Vault) string { return v.Name },
	}
}

func vaultsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your vaults",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			vaults, err := vaultList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Vault]{
				Noun: "vaults", Columns: vaultColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, vaults, func(v passsvc.Vault) []string { return []string{v.ShareID} })
		}),
	}
}

func vaultsCreateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a vault",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if name == "" {
				return kit.Fail("A vault needs a name.").Hint("--name Work")
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "vaults", Name: name,
			}, func() (string, error) {
				return c.App.Pass.VaultCreate(c.Ctx, name)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new vault")
	return c
}

func vaultsUpdateCmd() *cobra.Command {
	var name, description string
	icon := &kit.Enum{
		Name: "icon", Usage: "Which of Pass's icons represents it",
		Values: passsvc.VaultIcons(),
	}
	colour := &kit.Enum{
		Name: "color", Usage: "Which of Pass's vault colors it takes",
		Values: passsvc.VaultColors(),
	}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename a vault, or change how it looks",
		Long: "Rename a vault, or change how it looks.\n\n" +
			"Icons and colors are numbers, because Pass shows them as an unnamed grid:\n" +
			"--icon 7, --color 3.\n\n" +
			"Anything you do not mention is left alone, including a description written\n" +
			"in the Pass app.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			iconValue, err := icon.Value()
			if err != nil {
				return err
			}
			colourValue, err := colour.Value()
			if err != nil {
				return err
			}
			var patch passsvc.VaultPatch
			if c.Changed("name") {
				patch.Name = &name
			}
			if c.Changed("description") {
				patch.Description = &description
			}
			if n, err := strconv.Atoi(iconValue); err == nil {
				patch.Icon = &n
			}
			if n, err := strconv.Atoi(colourValue); err == nil {
				patch.Color = &n
			}
			if patch.Name == nil && patch.Description == nil &&
				patch.Icon == nil && patch.Color == nil {
				return kit.Fail("Nothing to change.").
					Hint("pass --name, --description, --icon or --color.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "vaults", Count: 1, Name: name,
				IDs: []string{c.Args[0]},
			}, func() error {
				return c.App.Pass.VaultEdit(c.Ctx, c.Args[0], patch)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name")
	c.Flags().StringVar(&description, "description", "", "What the vault is for")
	icon.Register(c)
	colour.Register(c)
	return c
}

func vaultsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete vaults, and everything in them",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "vaults", vaultColumns(), vaultList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "vaults", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(v passsvc.Vault) string { return v.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Pass.VaultDelete(c.Ctx, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// A vault has more to it than a listing has room for: what it is for, and which
// of Pass's icons and colours it took.
func vaultsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one vault in full",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			v, err := vaultList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: v,
				Fields: []ui.Field{
					{Label: "Name", Value: v.Name},
					{Label: "Description", Value: v.Description},
					{Label: "Icon", Value: displayNumber(v.Icon)},
					{Label: "Color", Value: displayNumber(v.Color)},
					{Label: "Members", Value: strconv.Itoa(v.Members)},
					{Label: "Owner", Value: yesNo(v.Owner), Always: true},
					{Label: "Shared", Value: yesNo(v.Shared), Always: true},
					{Label: "ID", Value: v.ShareID, ID: true},
				},
			})
		}),
	}
}

// displayNumber renders a vault's icon or colour. Zero is a vault that never
// chose, which is a fact rather than the number nought.
func displayNumber(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}
