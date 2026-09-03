package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/cli/kit"
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
		Kind: ui.Singular(noun),
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
	return &cobra.Command{
		Use:   "list",
		Short: "List your " + noun,
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := mailboxes(c, noun, folder).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Label]{
				Noun: noun, Columns: mailboxColumns(folder),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, rows)
		}),
	}
}

func mailboxCreateCmd(noun string, folder bool) *cobra.Command {
	var name, parent string
	var notify bool
	color := &kit.Color{Name: "color", Default: kit.DefaultAccentColor}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a " + ui.Singular(noun),
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if name == "" {
				return kit.Fail("A %s needs a name.", ui.Singular(noun)).Hint("--name Work")
			}
			spec := mailsvc.LabelSpec{Name: name, Color: color.Value(), Parent: parent}
			if folder {
				spec.Notify = &notify
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: noun, Name: name,
			}, func() (string, error) {
				return c.App.Mail.LabelCreate(c.Ctx, spec, folder)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new "+ui.Singular(noun))
	color.Register(c)
	if folder {
		c.Flags().StringVar(&parent, "parent", "", "Put it inside this folder, by ID")
		c.Flags().BoolVar(&notify, "notify", true, "Tell you when mail arrives here")
	}
	return c
}

func mailboxUpdateCmd(noun string, folder bool) *cobra.Command {
	var name, parent string
	var notify bool
	color := &kit.Color{Name: "color", Usage: "New accent color, as a hex value"}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Rename or recolor a " + ui.Singular(noun),
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			spec := mailsvc.LabelSpec{Name: name, Color: color.Value(), Parent: parent}
			if c.Changed("notify") {
				spec.Notify = &notify
			}
			if name == "" && !color.Set() && parent == "" && spec.Notify == nil {
				return kit.Fail("Nothing to change.").Hint(hintFor(folder))
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
		c.Flags().StringVar(&parent, "parent", "", "Move it inside this folder, by ID")
		c.Flags().BoolVar(&notify, "notify", true, "Tell you when mail arrives here")
	}
	return c
}

func hintFor(folder bool) string {
	if folder {
		return "pass --name, --color or --notify."
	}
	return "pass --name or --color."
}

func mailboxDeleteCmd(noun string, folder bool) *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete " + noun,
		Args:  cobra.MinimumNArgs(1),
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
