package mail

import (
	"context"
	"slices"
	"strings"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/inflect"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Folders and labels are two trees, not one.
//
// Proton's settings page is called "Folders and labels" and shows two separate
// lists, because they are separate things: a message lives in one folder and
// carries any number of labels. Keeping them apart is what stops `move` from
// quietly accepting a label and doing something else.
//
// It also makes the pairing obvious: `move --to` takes what
// `settings folders list` shows, and `label --label` takes what
// `settings labels list` shows.

func foldersCmd() *cobra.Command {
	return mailboxTree("folders", "Folders, which a message lives in", true)
}

func labelsCmd() *cobra.Command {
	return mailboxTree("labels", "Labels, which a message carries", false)
}

func mailboxTree(noun, short string, folder bool) *cobra.Command {
	c := &cobra.Command{Use: noun, Short: short}
	c.AddCommand(
		mailboxListCmd(noun, folder),
		mailboxCreateCmd(noun, folder),
		mailboxUpdateCmd(noun, folder),
		mailboxReorderCmd(noun, folder),
		mailboxDeleteCmd(noun, folder),
	)
	return c
}

// mailboxColumns renders a label or a folder. A folder can nest, so its path is
// the part a user needs to tell two same-named subfolders apart.
func mailboxColumns(folder bool) []ui.Column[mailsvc.Label] {
	cols := []ui.Column[mailsvc.Label]{
		{Header: "ID", ID: true, Cell: func(l mailsvc.Label) string { return l.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(l mailsvc.Label) string { return l.Name }},
		kit.ColorColumn(func(l mailsvc.Label) string { return l.Color }),
	}
	if folder {
		cols = append(cols,
			ui.Column[mailsvc.Label]{
				Header: "PATH", Flex: true,
				Cell: func(l mailsvc.Label) string { return l.Path },
			},
			// Only a folder has it, because only a folder is somewhere mail
			// lands. It is here rather than left implicit because it is what
			// `messages watch` watches by default: a default nobody can look up
			// is a rule they have to be told.
			ui.Column[mailsvc.Label]{
				Header: "NOTIFY",
				Cell: func(l mailsvc.Label) string {
					if l.Notifies() {
						return "yes"
					}
					return "no"
				},
			},
		)
	}
	return cols
}

// mailboxes looks up one of the two trees by ID, name or - for a folder - path.
func mailboxes(c *kit.Invocation, noun string, folder bool) *kit.Lookup[mailsvc.Label] {
	return &kit.Lookup[mailsvc.Label]{
		Kind: inflect.Singular(noun),
		Load: func(ctx context.Context) ([]mailsvc.Label, error) {
			labels, folders, err := c.App.Mail.LabelsList(ctx)
			if err != nil {
				return nil, err
			}
			if folder {
				return folders, nil
			}
			return labels, nil
		},
		ID:     func(l mailsvc.Label) string { return l.ID },
		Handle: func(l mailsvc.Label) string { return l.Name },
	}
}

func mailboxListCmd(noun string, folder bool) *cobra.Command {
	var held kit.Held[mailsvc.Label]
	c := &cobra.Command{
		Use:   "list",
		Short: "List your " + noun,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := mailboxes(c, noun, folder).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.Label]{
				Noun: noun, Columns: mailboxColumns(folder),
			}, rows)
		}),
	}
	held.Register(c, noun)
	return c
}

func mailboxCreateCmd(noun string, folder bool) *cobra.Command {
	var name, parent string
	var notify bool
	color := &kit.Color{Name: "color", Default: accent.Default}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a " + inflect.Singular(noun),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if name == "" {
				return kit.Fail("A %s needs a name.", inflect.Singular(noun)).Hint("--name Work")
			}
			spec := mailsvc.LabelSpec{Name: name, Color: color.Value()}
			if folder {
				spec.Notify = &notify
				inside, err := parentFolder(c, parent)
				if err != nil {
					return err
				}
				spec.Parent = inside
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: noun, Name: name,
			}, func() (string, error) {
				return c.App.Mail.LabelCreate(c.Ctx, spec, folder)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new "+inflect.Singular(noun))
	color.Register(c)
	if folder {
		c.Flags().StringVar(&parent, "parent", "", "Put it inside this folder")
		c.Flags().BoolVar(&notify, "notify", true, "Tell you when mail arrives here")
	}
	return c
}

// parentFolder resolves --parent to what Proton stores against a folder: the
// ID of the one that holds it, or nothing at all for the top level.
func parentFolder(c *kit.Invocation, ref string) (*string, error) {
	if ref == "" {
		return nil, nil
	}
	top := ""
	if strings.EqualFold(strings.TrimSpace(ref), kit.None) {
		return &top, nil
	}
	inside, err := mailboxes(c, "folders", true).Find(c.Ctx, ref)
	if err != nil {
		return nil, err
	}
	return &inside.ID, nil
}

func mailboxUpdateCmd(noun string, folder bool) *cobra.Command {
	var name, parent string
	var notify bool
	color := &kit.Color{Name: "color", Usage: "New accent color, by name (purple) or hex (#8080FF)"}
	c := &cobra.Command{
		Use:   "update REF",
		Short: updateShort(folder),
		Long:  updateLong(folder),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if name == "" && !color.Set() && parent == "" && !c.Changed("notify") {
				return kit.Fail("Nothing to change.").Hint(hintFor(folder))
			}
			spec := mailsvc.LabelSpec{Name: name, Color: color.Value()}
			if c.Changed("notify") {
				spec.Notify = &notify
			}
			if folder {
				inside, err := parentFolder(c, parent)
				if err != nil {
					return err
				}
				spec.Parent = inside
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: noun, Count: 1, Name: name,
				IDs: []string{c.Args[0]},
			}, func() error {
				return c.App.Mail.LabelUpdate(c.Ctx, c.Args[0], spec)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name")
	color.Register(c)
	if folder {
		c.Flags().StringVar(&parent, "parent", "", "Move it inside this folder, or none for the top level")
		c.Flags().BoolVar(&notify, "notify", true, "Tell you when mail arrives here")
	}
	return c
}

func updateShort(folder bool) string {
	if folder {
		return "Rename a folder, recolor it, or move it"
	}
	return "Rename or recolor a label"
}

func updateLong(folder bool) string {
	if !folder {
		return ""
	}
	return updateShort(true) + ".\n\n" +
		"--parent takes the folder to put it inside, or none to bring it back to\n" +
		"the top level. --notify says whether mail landing here is worth telling\n" +
		"you about."
}

func hintFor(folder bool) string {
	if folder {
		return "pass --name, --color, --parent or --notify."
	}
	return "pass --name or --color."
}

// Order is what a sidebar shows, and Proton keeps one sequence per list: every
// label is one list, and the folders inside each folder are another. So a
// folder is placed among the folders it sits beside and nowhere else.
func mailboxReorderCmd(noun string, folder bool) *cobra.Command {
	var alphabetical bool
	c := &cobra.Command{
		Use:   "reorder [REF...]",
		Short: "Set the order your " + noun + " are kept in",
		Long:  reorderLong(noun, folder),
		RunE: kit.Run([]kit.Step{
			func(c *kit.Invocation) error { return mailboxReorderTakes(noun, alphabetical, len(c.Args)) },
			kit.StepExpand,
		}, func(c *kit.Invocation) error {
			list := mailboxes(c, noun, folder)
			all, err := list.Rows(c.Ctx)
			if err != nil {
				return err
			}
			if alphabetical {
				return reorderAlphabetically(c, noun, folder, all)
			}
			sel, err := kit.SelectFrom(c, noun, mailboxColumns(folder), list)
			if err != nil {
				return err
			}
			return reorderNamed(c, noun, folder, all, sel)
		}),
	}
	c.Flags().BoolVar(&alphabetical, "alphabetical", false,
		"Sort them all alphabetically instead of naming any")
	return c
}

func reorderLong(noun string, folder bool) string {
	long := "Set the order your " + noun + " are kept in.\n\n" +
		"Name the " + noun + " that should come first, in order; the rest keep the\n" +
		"order they are in. --alphabetical sorts them all instead."
	if folder {
		long += "\n\nA folder is ordered among the folders it sits beside, so name folders\n" +
			"from one place at a time. --alphabetical covers every level."
	}
	return long
}

// Both halves are judgements about the command line, so neither costs a request.
func mailboxReorderTakes(noun string, alphabetical bool, named int) error {
	switch {
	case alphabetical && named > 0:
		return kit.Fail("Name the %s to put first, or pass --alphabetical - not both.", noun)
	case !alphabetical && named == 0:
		return kit.Fail("Nothing to reorder.").
			Hint("name the " + noun + " that should come first, or pass --alphabetical.")
	}
	return nil
}

// Sorting by name and laying the tree out again puts every list in alphabetical
// order, which for folders means each level of them.
func reorderAlphabetically(c *kit.Invocation, noun string, folder bool, all []mailsvc.Label) error {
	sorted := slices.Clone(all)
	slices.SortStableFunc(sorted, func(a, b mailsvc.Label) int { return kit.Fold(a.Name, b.Name) })
	sorted = mailsvc.Tree(sorted)
	if sameMailboxes(all, sorted) {
		return kit.Fail("The %s are already in alphabetical order.", noun)
	}
	lists := mailboxLists(sorted)
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Reordered, Kind: noun, Count: len(sorted), IDs: mailboxIDs(sorted),
		Preview: kit.Preview(noun, mailboxColumns(folder), sorted),
	}, func() error {
		for _, list := range lists {
			if err := c.App.Mail.LabelsReorder(c.Ctx, mailboxIDs(list.rows), list.parent, folder); err != nil {
				return err
			}
		}
		return nil
	})
}

func reorderNamed(c *kit.Invocation, noun string, folder bool, all []mailsvc.Label, sel kit.Selection[mailsvc.Label]) error {
	parent := sel.Rows[0].Parent
	if folder {
		for _, row := range sel.Rows[1:] {
			if row.Parent != parent {
				return errs.Naming(row.Name, errs.Problemf(
					"A reorder sets the order of folders that sit in the same folder.").
					Hint("`"+kit.Program+" mail settings folders list` shows where each one sits"))
			}
		}
	}
	siblings := mailboxSiblings(all, parent)
	order := reorderedMailboxes(siblings, sel.Rows)
	if sameMailboxes(siblings, order) {
		return kit.Fail("The %s are already in that order.", noun)
	}
	spec := ui.ResultSpec{
		Action: ui.Reordered, Kind: noun, Count: len(order), IDs: mailboxIDs(order),
		Preview: kit.Preview(noun, mailboxColumns(folder), order),
	}
	if parent != "" {
		spec.Detail = `inside "` + mailboxName(all, parent) + `"`
	}
	return kit.Mutate(c, spec, func() error {
		return c.App.Mail.LabelsReorder(c.Ctx, mailboxIDs(order), parent, folder)
	})
}

// mailboxList is one sequence Proton keeps on its own: every label, or the
// folders inside one folder.
type mailboxList struct {
	parent string
	rows   []mailsvc.Label
}

// mailboxLists cuts a laid-out tree into the sequences that are written
// separately, each in the order it is already in.
func mailboxLists(rows []mailsvc.Label) []mailboxList {
	var out []mailboxList
	at := map[string]int{}
	for _, row := range rows {
		i, ok := at[row.Parent]
		if !ok {
			i = len(out)
			at[row.Parent] = i
			out = append(out, mailboxList{parent: row.Parent})
		}
		out[i].rows = append(out[i].rows, row)
	}
	return out
}

func mailboxSiblings(all []mailsvc.Label, parent string) []mailsvc.Label {
	var out []mailsvc.Label
	for _, row := range all {
		if row.Parent == parent {
			out = append(out, row)
		}
	}
	return out
}

// reorderedMailboxes puts the named ones first, in the order they were named,
// and leaves the rest where they are.
func reorderedMailboxes(siblings, named []mailsvc.Label) []mailsvc.Label {
	first := make(map[string]bool, len(named))
	for _, row := range named {
		first[row.ID] = true
	}
	order := make([]mailsvc.Label, 0, len(siblings))
	order = append(order, named...)
	for _, row := range siblings {
		if !first[row.ID] {
			order = append(order, row)
		}
	}
	return order
}

func sameMailboxes(a, b []mailsvc.Label) bool {
	return slices.EqualFunc(a, b, func(x, y mailsvc.Label) bool { return x.ID == y.ID })
}

func mailboxIDs(rows []mailsvc.Label) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	return ids
}

func mailboxName(all []mailsvc.Label, id string) string {
	for _, row := range all {
		if row.ID == id {
			return row.Name
		}
	}
	return id
}

func mailboxDeleteCmd(noun string, folder bool) *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete " + noun,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, noun, mailboxColumns(folder), mailboxes(c, noun, folder))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: noun, Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(l mailsvc.Label) string { return l.Name }),
				Preview: sel.Preview(),
			}, func() error { return c.App.Mail.LabelDelete(c.Ctx, sel.IDs) })
		}),
	}
}
