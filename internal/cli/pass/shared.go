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
			"These are in no vault of yours, so they are not in `items list`: they are\n" +
			"addressed by the ID this shows, or by their name like anything else.\n\n" +
			"An item whose content will not open is still listed, because knowing it is\n" +
			"there is what lets you act on it.",
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
					{Header: "NAME", Flex: true, Cell: func(it passsvc.Item) string {
						if it.Name == "" {
							return "(could not be decrypted)"
						}
						return it.Name
					}},
					{Header: "ACCESS", Cell: func(it passsvc.Item) string { return it.Access }},
				},
			}, rows, func(it passsvc.Item) []string { return []string{it.ShareID, it.ItemID} })
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
			"`items share get REF` answers the question for one item; this answers the\n" +
			"one you actually have, which is what have I left open. A vault you share\n" +
			"is in `vaults list`, with the number of people in it, and a link you made\n" +
			"is in `links list`.",
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
					{Header: "NAME", Flex: true, Cell: func(it passsvc.Item) string { return it.Name }},
					{Header: "SHARES", Right: true, Cell: func(it passsvc.Item) string {
						return strconv.Itoa(it.Shares)
					}},
				},
			}, rows, func(it passsvc.Item) []string { return []string{it.ShareID, it.ItemID} })
		}),
	})
	return c
}
