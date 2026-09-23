package mail

import (
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func categoriesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "categories",
		Short: "The category tabs the inbox is sorted into",
		Long: "The category tabs the inbox is sorted into.\n\n" +
			"Primary is always shown, and its notifications cannot be changed. In the\n" +
			"web it also holds the mail of every hidden category. Changing the others\n" +
			"needs categories on: `" + kit.Program + " mail settings set category-view on`.",
	}
	c.AddCommand(categoriesListCmd(), categoriesShowCmd(true), categoriesShowCmd(false), categoriesUpdateCmd())
	return c
}

func categoryColumns() []ui.Column[mailsvc.Category] {
	return []ui.Column[mailsvc.Category]{
		{Header: "ID", ID: true, Cell: func(cat mailsvc.Category) string { return cat.ID }},
		{Header: "NAME", Handle: true, Cell: func(cat mailsvc.Category) string { return cat.Name }},
		{Header: "SHOWN", Cell: func(cat mailsvc.Category) string { return yesNo(cat.Shown) }},
		{Header: "NOTIFY", Cell: func(cat mailsvc.Category) string { return yesNo(cat.Notify) }},
	}
}

func categoryList(c *kit.Invocation) *kit.Lookup[mailsvc.Category] {
	return &kit.Lookup[mailsvc.Category]{
		Kind:   "category",
		Load:   c.App.Mail.Categories,
		ID:     func(cat mailsvc.Category) string { return cat.ID },
		Handle: func(cat mailsvc.Category) string { return cat.Name },
	}
}

func categoriesListCmd() *cobra.Command {
	var held kit.Held[mailsvc.Category]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the categories, and which show and notify",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := categoryList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			on, err := c.App.Mail.CategoryViewOn(c.Ctx)
			if err != nil {
				return err
			}
			if err := held.Answer(c, ui.TableSpec[mailsvc.Category]{
				Noun: "categories", Columns: categoryColumns(),
			}, rows); err != nil {
				return err
			}
			if len(rows) > 0 && !on {
				c.UI().Hint("Categories are off, so the inbox shows no tabs. Turn them on with `" +
					kit.Program + " mail settings set category-view on`.")
			}
			return nil
		}),
	}
	held.Register(c, "categories")
	return c
}

func categoriesShowCmd(shown bool) *cobra.Command {
	use, short, long, action := "enable", "Show categories as tabs in the inbox", "", ui.Enabled
	if !shown {
		use, short, action = "disable", "Hide categories from the inbox", ui.Disabled
		long = short + ".\n\n" +
			"One category besides Primary always stays shown. To stop using categories,\n" +
			"run `" + kit.Program + " mail settings set category-view off`."
	}
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Long:  long,
		Args: func(_ *cobra.Command, args []string) error {
			if shown {
				return nil
			}
			for _, ref := range args {
				if mailsvc.IsPrimary(ref) {
					return primaryAlwaysShown()
				}
			}
			return nil
		},
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lookup := categoryList(c)
			if err := categoriesChangeable(c, lookup); err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "categories", categoryColumns(), lookup)
			if err != nil {
				return err
			}
			hiding := map[string]bool{}
			for _, cat := range sel.Rows {
				if !shown && cat.Primary() {
					return primaryAlwaysShown()
				}
				if cat.Shown == shown {
					return alreadyInState(cat)
				}
				hiding[cat.ID] = !shown
			}
			if !shown {
				if err := leavesOneShown(c, lookup, hiding, sel.Rows); err != nil {
					return err
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "categories", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(cat mailsvc.Category) string { return cat.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, cat := range sel.Rows {
					if err := c.App.Mail.ShowCategory(c.Ctx, cat, shown); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func categoriesUpdateCmd() *cobra.Command {
	var notify bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change whether a category notifies",
		Long: "Change whether a category notifies.\n\n" +
			"A hidden category has no notifications to change: enable it first.",
		Args: func(_ *cobra.Command, args []string) error {
			if mailsvc.IsPrimary(args[0]) {
				return primaryNotifies()
			}
			return nil
		},
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if !c.Changed("notify") {
				return kit.Fail("Nothing to change.").Hint("pass --notify.")
			}
			lookup := categoryList(c)
			if err := categoriesChangeable(c, lookup); err != nil {
				return err
			}
			cat, err := lookup.Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if cat.Primary() {
				return primaryNotifies()
			}
			if !cat.Shown {
				return errs.Naming(cat.Name,
					kit.Fail("The %s category is hidden, so it has no notifications to change.", cat.Name).
						Hint(kit.Program+" mail settings categories enable "+cat.Name))
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "categories", Count: 1, Name: cat.Name, IDs: []string{cat.ID},
			}, func() error {
				return c.App.Mail.NotifyCategory(c.Ctx, cat, notify)
			})
		}),
	}
	c.Flags().BoolVar(&notify, "notify", true, "Tell you when mail arrives here")
	return c
}

func categoriesChangeable(c *kit.Invocation, lookup *kit.Lookup[mailsvc.Category]) error {
	rows, err := lookup.Rows(c.Ctx)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return kit.Fail("This account has no inbox categories.")
	}
	on, err := c.App.Mail.CategoryViewOn(c.Ctx)
	if err != nil {
		return err
	}
	if !on {
		return kit.Fail("Categories are off.").Hint(kit.Program + " mail settings set category-view on")
	}
	return nil
}

func leavesOneShown(c *kit.Invocation, lookup *kit.Lookup[mailsvc.Category], hiding map[string]bool,
	named []mailsvc.Category) error {
	all, err := lookup.Rows(c.Ctx)
	if err != nil {
		return err
	}
	for _, cat := range all {
		if cat.Shown && !cat.Primary() && !hiding[cat.ID] {
			return nil
		}
	}
	off := kit.Program + " mail settings set category-view off"
	if len(named) == 1 {
		return errs.Naming(named[0].Name,
			kit.Fail("The %s category is the last one shown besides primary.", named[0].Name).Hint(off))
	}
	return kit.Fail("Hiding these leaves no category shown besides primary.").Hint(off)
}

func alreadyInState(cat mailsvc.Category) error {
	if cat.Shown {
		return errs.Naming(cat.Name, kit.Fail("The %s category is already shown.", cat.Name))
	}
	return errs.Naming(cat.Name, kit.Fail("The %s category is already hidden.", cat.Name))
}

func primaryAlwaysShown() error {
	return kit.Fail("Primary is always shown: the mail of a hidden category goes there.")
}

func primaryNotifies() error {
	return kit.Fail("Primary's notifications cannot be changed.")
}
