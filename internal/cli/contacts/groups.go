package contacts

import (
	"context"
	"strconv"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func groupColumns() []ui.Column[ctsvc.Group] {
	return []ui.Column[ctsvc.Group]{
		{Header: "ID", ID: true, Cell: func(g ctsvc.Group) string { return g.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(g ctsvc.Group) string { return g.Name }},
		kit.ColorColumn(func(g ctsvc.Group) string { return g.Color }),
	}
}

func groupList(c *kit.Invocation) *kit.Lookup[ctsvc.Group] {
	return &kit.Lookup[ctsvc.Group]{
		Kind:   "group",
		Load:   func(ctx context.Context) ([]ctsvc.Group, error) { return c.App.Contacts.GroupsList(ctx) },
		ID:     func(g ctsvc.Group) string { return g.ID },
		Handle: func(g ctsvc.Group) string { return g.Name },
	}
}

func groupsCmd() *cobra.Command {
	c := &cobra.Command{Use: "groups", Short: "Contact groups"}
	c.AddCommand(groupsListCmd(), groupsGetCmd(), groupsCreateCmd(), groupsUpdateCmd(), groupsDeleteCmd(),
		membersCmd("add", "Add contacts to a group", ui.Added, "to"),
		membersCmd("remove", "Remove contacts from a group", ui.Removed, "from"),
	)
	return c
}

// groupsGetCmd shows one group and the addresses in it.
//
// Proton keeps membership on the address rather than on the group, so nothing in
// a listing of groups can say who is in one - it takes asking the addresses.
// Without this the six commands that manage a group could all be run and none of
// them could be checked.
func groupsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one group and the addresses in it",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			group, err := groupList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			members, err := c.App.Contacts.GroupMembers(c.Ctx, group.ID)
			if err != nil {
				return err
			}
			view := struct {
				ctsvc.Group
				Members []ctsvc.ContactEmail `json:"members"`
			}{group, members}
			fields := []ui.Field{
				{Label: "Name", Value: group.Name, Handle: true},
				kit.ColorField(group.Color),
				{Label: "Addresses", Value: strconv.Itoa(len(members)), Always: true},
			}
			for _, m := range members {
				fields = append(fields, ui.Field{Label: "  " + m.Email, Value: m.Name})
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: append(fields, ui.Field{Label: "ID", Value: group.ID, ID: true}),
			})
		}),
	}
}

func groupsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List contact groups",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			groups, err := groupList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[ctsvc.Group]{
				Noun: "groups", Columns: groupColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, groups)
		}),
	}
}

func groupsCreateCmd() *cobra.Command {
	var name string
	color := &kit.Color{Name: "color", Default: accent.Default}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a contact group",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if name == "" {
				return kit.Fail("A group needs a name.").Hint("--name Team")
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "groups", Name: name,
			}, func() (string, error) {
				return c.App.Contacts.GroupCreate(c.Ctx, name, color.Value())
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Group name")
	color.Register(c)
	return c
}

func groupsUpdateCmd() *cobra.Command {
	var name string
	color := &kit.Color{Name: "color", Usage: "New accent color, by name (purple) or hex (#8080FF)"}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename or recolor a contact group",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if name == "" && !color.Set() {
				return kit.Fail("Nothing to change.").Hint("pass --name or --color.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "groups", Count: 1, Name: name,
				IDs: []string{c.Args[0]},
			}, func() error {
				return c.App.Contacts.GroupUpdate(c.Ctx, c.Args[0], name, color.Value())
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New group name")
	color.Register(c)
	return c
}

func groupsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete contact groups",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "groups", groupColumns(), groupList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "groups", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(g ctsvc.Group) string { return g.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, id := range sel.IDs {
					if err := c.App.Contacts.GroupDelete(c.Ctx, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// membersCmd builds `groups add` and `groups remove`.
//
// The group is positional and the contacts follow, because the command lives in
// the groups collection: the primary object is whatever collection you are in.
// That is the same rule that puts the messages first in `mail messages label` and
// the label in a flag.
func membersCmd(use, short string, action ui.Action, preposition string) *cobra.Command {
	var addresses []string
	c := &cobra.Command{
		Use:   use + " REF CONTACT_REF...",
		Short: short,
		Long: short + ".\n\n" +
			"Proton groups addresses rather than people, so a colleague's work address\n" +
			"can be in a group while their personal one is not. Naming a contact means\n" +
			"all of their addresses; --email narrows it to the ones you name, and then\n" +
			"exactly one contact may be named.",
		Args: cobra.MinimumNArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			// Judged before the network: which contact an address belongs to is a
			// question only one contact can answer.
			if len(addresses) > 0 && len(c.Args) != 2 {
				return kit.Fail("--email applies to one contact, but %d were named.",
					len(c.Args)-1).
					Hint("name one contact, or drop --email to act on every address.")
			}
			groupID := c.Args[0]
			ids := make([]string, 0, len(c.Args)-1)
			for _, ref := range c.Args[1:] {
				id, err := c.App.Contacts.Resolve(c.Ctx, ref)
				if err != nil {
					return err
				}
				ids = append(ids, id)
			}
			ids = kit.Dedupe(ids)

			if len(addresses) > 0 {
				emailIDs, err := c.App.Contacts.ResolveContactEmails(c.Ctx, ids[0], addresses)
				if err != nil {
					return err
				}
				return kit.Mutate(c, ui.ResultSpec{
					Action: action, Kind: "addresses", Count: len(emailIDs), IDs: emailIDs,
					Detail: preposition + " the group",
				}, func() error {
					if use == "add" {
						return c.App.Contacts.GroupAddEmails(c.Ctx, groupID, emailIDs)
					}
					return c.App.Contacts.GroupRemoveEmails(c.Ctx, groupID, emailIDs)
				})
			}
			// Naming a contact means every address they hold, which is what the
			// help above promises. Proton groups addresses and nothing else, so
			// this resolves them rather than handing over contact IDs and hoping.
			emailIDs, err := c.App.Contacts.AddressesOf(c.Ctx, ids)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "addresses", Count: len(emailIDs), IDs: emailIDs,
				Detail: preposition + " the group",
			}, func() error {
				if use == "add" {
					return c.App.Contacts.GroupAddEmails(c.Ctx, groupID, emailIDs)
				}
				return c.App.Contacts.GroupRemoveEmails(c.Ctx, groupID, emailIDs)
			})
		}),
	}
	c.Flags().StringArrayVar(&addresses, "email", nil,
		"Act on this address only, rather than all of the contact's (repeatable)")
	return c
}
