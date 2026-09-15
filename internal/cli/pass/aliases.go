package pass

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Hide-my-email aliases. An alias is a Pass item of type alias, so `items list`
// shows them too; this tree exists because creating one has its own vocabulary of
// prefixes, suffixes and mailboxes.

func aliasesCmd() *cobra.Command {
	c := &cobra.Command{Use: "aliases", Short: "Hide-my-email addresses that forward to you"}
	c.AddCommand(aliasesListCmd(), aliasesCreateCmd(), aliasContactsCmd(),
		aliasesToggleCmd("enable", "Start receiving mail sent to an alias", ui.Enabled, true),
		aliasesToggleCmd("disable", "Stop receiving mail sent to an alias", ui.Disabled, false))
	return c
}

func aliasesListCmd() *cobra.Command {
	var vault string
	var held kit.Held[passsvc.Item]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your aliases",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			vaultRef, err := kit.Expand(c.App, vault)
			if err != nil {
				return err
			}
			items, err := c.App.Pass.ItemsList(c.Ctx, vaultRef)
			if err != nil {
				return err
			}
			aliases := keepType(items, "alias")
			return held.Answer(c, ui.TableSpec[passsvc.Item]{
				Noun: "aliases",
				Columns: []ui.Column[passsvc.Item]{
					{Header: "ID", ID: true, Cell: itemRef},
					{Header: "STATUS", Cell: func(it passsvc.Item) string { return it.AliasStatus }},
					{Header: "ADDRESS", Flex: true, Handle: true, Cell: func(it passsvc.Item) string { return it.Alias }},
					{Header: "NAME", Flex: true, Handle: true, Cell: func(it passsvc.Item) string { return it.Name }},
				},
			}, aliases)
		}),
	}
	c.Flags().StringVar(&vault, "vault", "", "Show only this vault, by name or ID")
	held.Register(c, "aliases",
		kit.Key[passsvc.Item]{Name: "address", Less: func(a, b passsvc.Item) int { return kit.Fold(a.Alias, b.Alias) }},
		kit.Key[passsvc.Item]{Name: "name", Less: func(a, b passsvc.Item) int { return kit.Fold(a.Name, b.Name) }},
	)
	return c
}

func aliasesToggleCmd(use, short string, action ui.Action, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			if it.Type != "alias" {
				return kit.Fail("%s is a %s, not an alias.", it.Name, it.Type)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "aliases", Count: 1, Name: it.Name,
				Detail: "- " + it.Alias, IDs: []string{kit.JoinPair(shareID, itemID)},
			}, func() error {
				return c.App.Pass.AliasSetEnabled(c.Ctx, shareID, itemID, enabled)
			})
		}),
	}
}

func aliasesCreateCmd() *cobra.Command {
	var mailboxes []string
	var prefix, suffix, name, vault string
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an alias",
		Long: "Create an alias.\n\n" +
			"The address is a prefix you choose plus a suffix Proton offers. Mail sent to\n" +
			"it arrives in the mailboxes you name. `settings domains list` has the suffixes\n" +
			"and `settings mailboxes list` the mailboxes.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if prefix == "" {
				return kit.Fail("An alias needs a prefix.").
					Hint("--prefix shop", "proton pass settings domains list")
			}
			shareID, err := resolveVault(c, vault)
			if err != nil {
				return err
			}
			// The address is the answer, so it is worked out before the alias is
			// made: the confirmation, the machine output and a dry run then all name
			// the same address rather than the prefix it was asked for.
			plan, err := c.App.Pass.PlanAlias(c.Ctx, shareID, prefix, suffix, mailboxes)
			if err != nil {
				return err
			}
			if name == "" {
				name = prefix
			}
			spec := ui.ResultSpec{Action: ui.Created, Kind: "aliases", Name: name}
			if !c.App.DryRun {
				// Proton invents a suffix each time it is asked for one, so the
				// address is only settled by using this one. A preview can name the
				// alias it would make but not the address it would get.
				spec.Detail = "as " + plan.Address
				spec.Extra = map[string]any{"alias": plan.Address}
			}
			return kit.Create(c, spec, func() (string, error) {
				itemID, err := c.App.Pass.AliasCreate(c.Ctx, shareID, plan, name)
				if err != nil {
					return "", err
				}
				return kit.JoinPair(shareID, itemID), nil
			})
		}),
	}
	c.Flags().StringVar(&prefix, "prefix", "", "The part before the @")
	c.Flags().StringVar(&suffix, "suffix", "", "The part from the @ onwards (default: the first Proton offers)")
	c.Flags().StringArrayVar(&mailboxes, "mailbox", nil, "Where mail to the alias should arrive (repeatable)")
	c.Flags().StringVar(&name, "name", "", "Name for the alias item")
	c.Flags().StringVar(&vault, "vault", "", "Which vault to keep it in, by name or ID")
	return c
}
