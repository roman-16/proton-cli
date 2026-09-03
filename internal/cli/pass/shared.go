package pass

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// The two sides of sharing one item, which Pass itself keeps in two places: what
// other people have given you, and what you have given away.
//
// A vault is not in either. One somebody shared with you is a vault like any
// other in `vaults list`, with the members it has - an item is not, because it
// arrives without a vault around it and belongs to no listing of yours.

func sharedCmd() *cobra.Command {
	c := &cobra.Command{Use: "shared", Short: "Items other people have shared with you"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the items other people have shared with you",
		Long: "List the items other people have shared with you.\n\n" +
			"These are in no vault of yours, so `items list` does not show them. Address\n" +
			"them by the ID shown here, or by name.\n\n" +
			"An item whose content cannot be decrypted is still listed, so you can still\n" +
			"act on it.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.SharedWithMe(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Item]{
				Noun: "items", Total: len(rows), Page: ui.Unpaged,
				Columns: []ui.Column[passsvc.Item]{
					{Header: "ID", ID: true, Cell: itemRef},
					{Header: "TYPE", Cell: func(it passsvc.Item) string { return it.Type }},
					{Header: "NAME", Flex: true, Handle: true, Cell: func(it passsvc.Item) string {
						if it.Name == "" {
							return "(could not be decrypted)"
						}
						return it.Name
					}},
					{Header: "ACCESS", Cell: func(it passsvc.Item) string { return it.Access }},
				},
			}, rows)
		}),
	})
	return c
}

func sharingCmd() *cobra.Command {
	c := &cobra.Command{Use: "sharing", Short: "Items you have shared with other people"}
	c.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List the items you have shared",
		Long: "List the items you have shared with somebody on their own.\n\n" +
			"To check a single item instead, run `items share get REF`. Shared vaults\n" +
			"are in `vaults list` with the number of people in each, and secure links\n" +
			"are in `links list`.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.SharedByMe(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Item]{
				Noun: "items", Total: len(rows), Page: ui.Unpaged,
				Columns: []ui.Column[passsvc.Item]{
					{Header: "ID", ID: true, Cell: itemRef},
					{Header: "TYPE", Cell: func(it passsvc.Item) string { return it.Type }},
					{Header: "NAME", Flex: true, Handle: true, Cell: func(it passsvc.Item) string { return it.Name }},
					{Header: "SHARES", Right: true, Cell: func(it passsvc.Item) string {
						return strconv.Itoa(it.Shares)
					}},
				},
			}, rows)
		}),
	})
	return c
}
