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
		vaultsTransferCmd(), vaultsUpdateCmd(), vaultsDeleteCmd(),
		vaultsVisibilityCmd("hide", "Keep vaults out of your listings", hiddenVaultsLong, ui.Hidden, true),
		vaultsVisibilityCmd("unhide", "Bring hidden vaults back into your listings", "", ui.Unhidden, false))
	return c
}

func vaultColumns() []ui.Column[passsvc.Vault] {
	return []ui.Column[passsvc.Vault]{
		{Header: "ID", ID: true, Cell: func(v passsvc.Vault) string { return v.ShareID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(v passsvc.Vault) string {
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
		{Header: "HIDDEN", Cell: func(v passsvc.Vault) string { return yesNo(v.Hidden) }},
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
	var held kit.Held[passsvc.Vault]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your vaults",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			vaults, err := vaultList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[passsvc.Vault]{
				Noun: "vaults", Columns: vaultColumns(),
			}, vaults)
		}),
	}
	held.Register(c, "vaults",
		kit.Key[passsvc.Vault]{Name: "name", Less: func(a, b passsvc.Vault) int { return kit.Fold(a.Name, b.Name) }},
		kit.Key[passsvc.Vault]{Name: "members", Less: func(a, b passsvc.Vault) int { return kit.Ints(int64(a.Members), int64(b.Members)) }},
	)
	return c
}

func vaultsCreateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a vault",
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
			"Icons and colors are named: --icon star, --color teal.\n\n" +
			"Anything you do not mention is left alone, including a description written\n" +
			"in the Pass app.",
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
			if iconValue != "" {
				patch.Icon = &iconValue
			}
			if colourValue != "" {
				patch.Color = &colourValue
			}
			if patch.Name == nil && patch.Description == nil &&
				patch.Icon == nil && patch.Color == nil {
				return kit.Fail("Nothing to change.").
					Hint("pass --name, --description, --icon or --color.")
			}
			vault, err := vaultList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			called := vault.Name
			if patch.Name != nil {
				called = name
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "vaults", Count: 1, Name: called,
				IDs: []string{vault.ShareID},
			}, func() error {
				return c.App.Pass.VaultEdit(c.Ctx, vault.ShareID, patch)
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

// hiddenVaultsLong is what hiding a vault does to the rest of the CLI.
const hiddenVaultsLong = "Keep vaults out of your listings.\n\n" +
	"A hidden vault is left out of `items list`, the trash, `aliases list`,\n" +
	"`sharing list`, the password checks and `breaches list`, and looking an item\n" +
	"up by name does not search it. Naming it still reaches it, as in\n" +
	"`items list --vault Archive`, and so does an item's ID. `vaults list` shows\n" +
	"every vault, hidden or not.\n\n" +
	"Only you stop seeing it: the other members of a shared vault are not\n" +
	"affected."

// Hiding a vault is this account's view of it, not a change to the vault: each
// member holds a share of their own, so nobody else is affected.
func vaultsVisibilityCmd(use, short, long string, action ui.Action, hidden bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Long:  long,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "vaults", vaultColumns(), vaultList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "vaults", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(v passsvc.Vault) string { return v.Name }),
				Preview: sel.Preview(),
			}, func() error {
				if hidden {
					return c.App.Pass.VaultsSetHidden(c.Ctx, sel.IDs, nil)
				}
				return c.App.Pass.VaultsSetHidden(c.Ctx, nil, sel.IDs)
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
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			v, err := vaultList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: v,
				Fields: []ui.Field{
					{Label: "Name", Value: v.Name, Handle: true},
					{Label: "Description", Value: v.Description},
					{Label: "Icon", Value: v.Icon},
					{Label: "Color", Value: v.Color},
					{Label: "Members", Value: strconv.Itoa(v.Members)},
					{Label: "Owner", Value: yesNo(v.Owner), Always: true},
					{Label: "Shared", Value: yesNo(v.Shared), Always: true},
					{Label: "Hidden", Value: yesNo(v.Hidden), Always: true},
					{Label: "ID", Value: v.ShareID, ID: true},
				},
			})
		}),
	}
}
