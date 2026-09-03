package pass

import (
	"strconv"

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
	c.AddCommand(aliasesListCmd(), aliasesCreateCmd(), aliasesOptionsCmd(), aliasContactsCmd(),
		aliasesToggleCmd("enable", "Start receiving mail sent to an alias", ui.Enabled, true),
		aliasesToggleCmd("disable", "Stop receiving mail sent to an alias", ui.Disabled, false))
	return c
}

func aliasesListCmd() *cobra.Command {
	var vault string
	c := &cobra.Command{
		Use:   "list",
		Short: "List your aliases",
		Args:  cobra.NoArgs,
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
			return kit.List(c, ui.TableSpec[passsvc.Item]{
				Noun:  "aliases",
				Total: ui.Unknown, Page: ui.Unpaged,
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
	return c
}

// aliasesToggleCmd builds enable and disable, which differ only in which way the
// switch goes. A disabled alias keeps its address and stops receiving, so it is
// the answer to an address that has started attracting spam - `items delete`
// burns the address instead, and cannot be taken back.
func aliasesToggleCmd(use, short string, action ui.Action, enabled bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF",
		Short: short,
		Args:  cobra.ExactArgs(1),
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
			"it arrives in the mailboxes you name. Run `aliases options` to see the\n" +
			"suffixes and mailboxes available.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if prefix == "" {
				return kit.Fail("An alias needs a prefix.").
					Hint("--prefix shop", "proton pass aliases options")
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

// option is one choice `aliases options` offers, in either category.
type option struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	ID    string `json:"id,omitempty"`
}

func aliasesOptionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "options",
		Short: "List the suffixes and mailboxes an alias can use",
		Long: "List the suffixes and mailboxes an alias can use.\n\n" +
			"A suffix is the domain an alias is made on, and is what --suffix takes.\n\n" +
			"Proton adds a random word in front of the suffix, and only settles on it\n" +
			"when the alias is created.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			shareID, err := resolveVault(c, "")
			if err != nil {
				return err
			}
			suffixes, mailboxes, err := c.App.Pass.AliasOptions(c.Ctx, shareID)
			if err != nil {
				return err
			}
			rows := make([]option, 0, len(suffixes)+len(mailboxes))
			// The domain, not the whole suffix. Proton mints the word before it
			// afresh on every request, so the full form is out of date by the
			// time it is read, and passing it back is refused.
			seen := make(map[string]bool, len(suffixes))
			for _, s := range suffixes {
				if seen[s.Domain] {
					continue
				}
				seen[s.Domain] = true
				rows = append(rows, option{Kind: "suffix", Value: s.Domain})
			}
			for _, m := range mailboxes {
				rows = append(rows, option{Kind: "mailbox", Value: m.Email, ID: strconv.Itoa(m.ID)})
			}
			return kit.List(c, ui.TableSpec[option]{
				Noun:  "options",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[option]{
					{Header: "KIND", Cell: func(o option) string { return o.Kind }},
					{Header: "VALUE", Flex: true, Cell: func(o option) string { return o.Value }},
					{Header: "ID", Cell: func(o option) string { return o.ID }},
				},
			}, rows)
		}),
	}
}
