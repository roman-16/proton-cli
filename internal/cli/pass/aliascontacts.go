package pass

import (
	"context"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Writing as an alias, rather than only receiving as one.
//
// A reply to mail an alias forwarded would leave from your real address and give
// the alias away. A contact is Proton's answer: a second address standing for
// one correspondent, which passes what you send there on to them as the alias.

func aliasContactsCmd() *cobra.Command {
	c := &cobra.Command{Use: "contacts", Short: "Addresses an alias can write to"}
	c.AddCommand(
		aliasContactsListCmd(), aliasContactsCreateCmd(), aliasContactsDeleteCmd(),
		aliasContactBlockCmd("block", "Stop a contact's mail reaching you", ui.Blocked, true),
		aliasContactBlockCmd("allow", "Let a contact's mail reach you again", ui.Allowed, false),
	)
	return c
}

func aliasContactColumns() []ui.Column[passsvc.AliasContact] {
	return []ui.Column[passsvc.AliasContact]{
		{Header: "ID", ID: true, Cell: func(a passsvc.AliasContact) string { return strconv.Itoa(a.ID) }},
		{Header: "EMAIL", Flex: true, Cell: func(a passsvc.AliasContact) string { return a.Email }},
		{Header: "WRITE TO", Flex: true, Cell: func(a passsvc.AliasContact) string { return a.ReverseAlias }},
		{Header: "BLOCKED", Cell: func(a passsvc.AliasContact) string { return yesNo(a.Blocked) }},
		{Header: "FORWARDED", Right: true, Cell: func(a passsvc.AliasContact) string {
			return strconv.Itoa(a.Forwarded)
		}},
	}
}

func aliasContactsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List the addresses an alias can write to",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := aliasOf(c, c.Args[0])
			if err != nil {
				return err
			}
			rows, err := c.App.Pass.AliasContacts(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.AliasContact]{
				Noun: "contacts", Columns: aliasContactColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, rows, func(a passsvc.AliasContact) []string { return []string{strconv.Itoa(a.ID)} })
		}),
	}
}

func aliasContactsCreateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "create REF EMAIL",
		Short: "Make an address that writes to somebody as the alias",
		Long: "Make an address that writes to somebody as the alias.\n\n" +
			"Proton answers with a second address standing for that one person. Mail you\n" +
			"send there reaches them as though the alias had written it, so your real\n" +
			"address is never shown.",
		Args: cobra.ExactArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := aliasOf(c, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "contacts", Name: c.Args[1],
			}, func() (string, error) {
				contact, err := c.App.Pass.AliasContactCreate(c.Ctx, shareID, itemID, c.Args[1], name)
				if err != nil {
					return "", err
				}
				// The address it minted is the whole point, so it is what the
				// person is told, not the number Proton files it under.
				c.Note("Write to %s to reach them as the alias.", contact.ReverseAlias)
				return strconv.Itoa(contact.ID), nil
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "A name for them")
	return c
}

func aliasContactsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF CONTACT_REF...",
		Short: "Remove an address an alias can write to",
		Args:  cobra.MinimumNArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return actOnAliasContacts(c, ui.Deleted, func(shareID, itemID string, id int) error {
				return c.App.Pass.AliasContactDelete(c.Ctx, shareID, itemID, id)
			})
		}),
	}
}

func aliasContactBlockCmd(use, short string, action ui.Action, blocked bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF CONTACT_REF...",
		Short: short,
		Args:  cobra.MinimumNArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return actOnAliasContacts(c, action, func(shareID, itemID string, id int) error {
				return c.App.Pass.AliasContactSetBlocked(c.Ctx, shareID, itemID, id, blocked)
			})
		}),
	}
}

// actOnAliasContacts is every verb that acts on named contacts of one alias.
// They differ only in what they do to each, so finding them is shared.
func actOnAliasContacts(c *kit.Invocation, action ui.Action, apply func(shareID, itemID string, id int) error) error {
	shareID, itemID, err := aliasOf(c, c.Args[0])
	if err != nil {
		return err
	}
	contacts := &kit.Lookup[passsvc.AliasContact]{
		Kind: "contact",
		Load: func(ctx context.Context) ([]passsvc.AliasContact, error) {
			return c.App.Pass.AliasContacts(ctx, shareID, itemID)
		},
		ID:     func(a passsvc.AliasContact) string { return strconv.Itoa(a.ID) },
		Handle: func(a passsvc.AliasContact) string { return a.Email },
	}
	// The first argument named the alias, so the rest are the contacts.
	sel, err := kit.Select(c, kit.Selector[passsvc.AliasContact]{
		Noun: "contacts", Columns: aliasContactColumns(), Refs: c.Args[1:],
		IDOf: contacts.ID, ByRef: contacts.Find,
	})
	if err != nil {
		return err
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: action, Kind: "contacts", Count: sel.Len(), IDs: sel.IDs,
		Name:    kit.Sole(sel.Rows, func(a passsvc.AliasContact) string { return a.Email }),
		Preview: sel.Preview(),
	}, func() error {
		for _, row := range sel.Rows {
			if err := apply(shareID, itemID, row.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// aliasOf resolves the reference to an alias, refusing anything that is not one:
// a contact only means something for an address that forwards.
func aliasOf(c *kit.Invocation, ref string) (string, string, error) {
	shareID, itemID, err := resolveItem(c, ref)
	if err != nil {
		return "", "", err
	}
	it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
	if err != nil {
		return "", "", err
	}
	if it.Type != "alias" {
		return "", "", kit.Fail("%s is a %s, not an alias.", it.Name, it.Type).
			Hint("only an alias has contacts; `proton pass aliases list` shows yours.")
	}
	return shareID, itemID, nil
}
