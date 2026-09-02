package pass

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Pass has a trash, so the CLI gives it the same three verbs Drive's has -
// including the `list` that makes restoring possible without knowing in advance
// what is in there.

// trashedState is the State value Proton gives an item in the trash.
const trashedState = 2

func trashCmd() *cobra.Command {
	c := &cobra.Command{Use: "trash", Short: "Items you have removed but not yet deleted"}
	c.AddCommand(trashListCmd(), trashRestoreCmd(), trashEmptyCmd())
	return c
}

// trashed lists the items in the trash, across every vault.
func trashed(c *kit.Invocation) ([]passsvc.Item, error) {
	items, err := c.App.Pass.ItemsList(c.Ctx, "")
	if err != nil {
		return nil, err
	}
	out := make([]passsvc.Item, 0)
	for _, it := range items {
		if it.State == trashedState {
			out = append(out, it)
		}
	}
	return out, nil
}

func trashListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List what is in the trash",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := trashed(c)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Item]{
				Noun: "items", Columns: itemColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, items, func(it passsvc.Item) []string { return []string{it.ShareID, it.ItemID} })
		}),
	}
}

func trashRestoreCmd() *cobra.Command {
	var all bool
	c := &cobra.Command{
		Use:   "restore [REF...]",
		Short: "Put items back where they came from",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			targets, err := trashTargets(c, all)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Restored, Kind: "items", Count: len(targets),
				IDs: refsOf(targets), Preview: previewOf(targets),
			}, func() error {
				for _, it := range targets {
					if err := c.App.Pass.ItemRestore(c.Ctx, it.ShareID, it.ItemID); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	kit.All(c.Flags(), &all)
	return c
}

func trashEmptyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "empty",
		Short: "Delete everything in the trash, permanently",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := trashed(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Emptied, Kind: "items", Count: len(items),
				Detail: "from the trash", IDs: refsOf(items), Preview: previewOf(items),
			}, func() error {
				for _, it := range items {
					if err := c.App.Pass.ItemDelete(c.Ctx, it.ShareID, it.ItemID); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// trashTargets resolves what to restore: the items named, or everything in the
// trash. A bare invocation is refused rather than read as "everything".
func trashTargets(c *kit.Invocation, all bool) ([]passsvc.Item, error) {
	if all {
		if len(c.Args) > 0 {
			return nil, kit.Fail("--all restores everything, so it takes no REF.")
		}
		return trashed(c)
	}
	if len(c.Args) == 0 {
		return nil, kit.Fail("Nothing selected.").
			Hint("pass a REF, or --all to restore everything in the trash.",
				"proton pass trash list")
	}
	out := make([]passsvc.Item, 0, len(c.Args))
	for _, ref := range c.Args {
		shareID, itemID, err := resolveItem(c, ref)
		if err != nil {
			return nil, err
		}
		it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
		if err != nil {
			return nil, err
		}
		out = append(out, it.Item)
	}
	return out, nil
}

func refsOf(items []passsvc.Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, itemRef(it))
	}
	return out
}

func previewOf(items []passsvc.Item) func(*ui.UI) error {
	return kit.Preview("items", itemColumns(), items)
}
