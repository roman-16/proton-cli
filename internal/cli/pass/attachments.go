package pass

import (
	"io"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/progress"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Files hang off the item that holds them, so the tree is `items attachments` -
// the same shape, and the same words, as a message's attachments in Mail.
//
// Putting a file on an item and taking one off are edits of the item, so they
// are `items create --attach` and `items update --attach/--detach`. What is here
// is everything else: reading them, renaming one, and bringing one back.

func attachmentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Files attached to an item"}
	c.AddCommand(attachmentsDownloadCmd(), attachmentsListCmd(),
		attachmentsRestoreCmd(), attachmentsUpdateCmd())
	return c
}

func attachmentColumns(removed bool) []ui.Column[passsvc.Attachment] {
	cols := []ui.Column[passsvc.Attachment]{
		{Header: "ID", ID: true, Cell: func(a passsvc.Attachment) string { return a.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(a passsvc.Attachment) string { return a.Name }},
		{Header: "SIZE", Right: true, Cell: func(a passsvc.Attachment) string { return units.Size(a.Size) }},
		{Header: "TYPE", Flex: true, Cell: func(a passsvc.Attachment) string { return a.MIMEType }},
	}
	if !removed {
		return cols
	}
	// Which version it went at is what tells two removals of the same name apart,
	// and it is the only thing a removed file has that a present one does not.
	return append(cols, ui.Column[passsvc.Attachment]{
		Header: "REMOVED AT", Right: true,
		Cell: func(a passsvc.Attachment) string { return strconv.Itoa(a.RemovedAt()) },
	})
}

func attachmentsListCmd() *cobra.Command {
	var removed bool
	c := &cobra.Command{
		Use:   "list REF",
		Short: "List an item's attachments",
		Long: "List an item's attachments.\n\n" +
			"--removed lists the files taken off the item instead, which are the ones\n" +
			"`attachments restore` can bring back.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			files, err := attachments(c, shareID, itemID, removed)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Attachment]{
				Noun: "attachments", Columns: attachmentColumns(removed),
				Total: ui.Unknown, Page: ui.Unpaged, Filtered: removed,
			}, files)
		}),
	}
	c.Flags().BoolVar(&removed, "removed", false, "List what was taken off the item rather than what is on it")
	return c
}

// attachments reads one of the two sets a listing shows: what the item carries,
// or what it used to.
func attachments(c *kit.Invocation, shareID, itemID string, removed bool) ([]passsvc.Attachment, error) {
	if removed {
		return c.App.Pass.RemovedAttachments(c.Ctx, shareID, itemID)
	}
	return c.App.Pass.Attachments(c.Ctx, shareID, itemID)
}

func attachmentsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	c := &cobra.Command{
		Use:   "download REF [ATTACHMENT_REF]",
		Short: "Download and decrypt attachments",
		Long: "Download and decrypt attachments.\n\n" +
			"Naming an attachment downloads that one; naming none downloads them all.\n" +
			"An attachment is named by its own name or by its ID.\n\n" +
			"Existing files are never overwritten silently: a collision becomes\n" +
			"\"file (2).pdf\" unless --force says otherwise.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			one := len(c.Args) == 2
			if err := dest.Validate(one); err != nil {
				return err
			}
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			if one {
				file, err := c.App.Pass.ResolveAttachment(c.Ctx, shareID, itemID, c.Args[1])
				if err != nil {
					return err
				}
				return downloadAttachment(c, shareID, itemID, *file, &dest)
			}
			files, err := c.App.Pass.Attachments(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				return kit.Mutate(c, ui.ResultSpec{Action: ui.Downloaded, Kind: "attachments", Count: 0},
					func() error { return nil })
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Downloaded, Kind: "attachments", Count: len(files),
				Detail: "to " + dest.Describe(),
			}, func() error {
				for i, file := range files {
					// Each file draws its own bar, so a download of a dozen says
					// where it is rather than drawing a dozen identical lines.
					sink := ui.Batch(ui.NewProgress(c.UI()), i+1, len(files))
					if _, err := writeAttachment(c, shareID, itemID, file, &dest, sink); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	dest.Register(c)
	return c
}

// downloadAttachment writes the one file a command was pointed at, and reports
// it by name and where it landed.
func downloadAttachment(c *kit.Invocation, shareID, itemID string, file passsvc.Attachment, dest *kit.Destination) error {
	if dest.Stdout() {
		// The payload's own consumer has the terminal, so the bar stays off.
		return c.App.Pass.AttachmentDownload(c.Ctx, shareID, itemID, file, c.UI().Out, nil)
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Count: 1, Name: file.Name,
		Detail: "to " + dest.Describe(), IDs: []string{file.ID},
	}, func() error {
		_, err := writeAttachment(c, shareID, itemID, file, dest, ui.NewProgress(c.UI()))
		return err
	})
}

// writeAttachment puts one file on disk, beside its destination and moved into
// place at the end, so a transfer that stops part way leaves nothing behind.
func writeAttachment(c *kit.Invocation, shareID, itemID string, file passsvc.Attachment, dest *kit.Destination, sink progress.Sink) (string, error) {
	return dest.Stream(c, file.Name, func(w io.Writer) error {
		return c.App.Pass.AttachmentDownload(c.Ctx, shareID, itemID, file, w, sink)
	})
}

func attachmentsUpdateCmd() *cobra.Command {
	var name string
	c := &cobra.Command{
		Use:   "update REF ATTACHMENT_REF",
		Short: "Rename an attachment",
		Long: "Rename an attachment.\n\n" +
			"Renaming is `update --name`; there is no `rename` verb.\n\n" +
			"The item is not touched, so this adds nothing to its history.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			file, err := c.App.Pass.ResolveAttachment(c.Ctx, shareID, itemID, c.Args[1])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Count: 1, Name: file.Name,
				Detail: "to " + name, IDs: []string{file.ID},
			}, func() error {
				return c.App.Pass.AttachmentRename(c.Ctx, shareID, itemID, *file, name)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name for the file")
	_ = c.MarkFlagRequired("name")
	return c
}

func attachmentsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore REF ATTACHMENT_REF...",
		Short: "Put a removed attachment back on an item",
		Long: "Put a removed attachment back on an item.\n\n" +
			"ATTACHMENT_REF is the name or ID `attachments list --removed` shows. A name\n" +
			"that was removed more than once is refused, with the IDs to choose from.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			files := make([]passsvc.Attachment, 0, len(c.Args)-1)
			for _, reference := range c.Args[1:] {
				file, err := c.App.Pass.ResolveRemovedAttachment(c.Ctx, shareID, itemID, reference)
				if err != nil {
					return err
				}
				files = append(files, *file)
			}
			ids := make([]string, 0, len(files))
			for _, file := range files {
				ids = append(ids, file.ID)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Restored, Kind: "attachments", Count: len(files), IDs: ids,
				Name: kit.Sole(files, func(a passsvc.Attachment) string { return a.Name }),
			}, func() error {
				for _, file := range files {
					if err := c.App.Pass.AttachmentRestore(c.Ctx, shareID, itemID, file); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// attachmentFields are the files an item carries, as a record shows them.
func attachmentFields(files []passsvc.Attachment) []ui.Field {
	out := make([]ui.Field, 0, len(files))
	for _, a := range files {
		out = append(out, ui.Field{
			Label: "Attachment",
			Value: a.Name + " (" + units.Size(a.Size) + ")",
		})
	}
	return out
}

// uploads reads what --attach named, refusing a file there is nothing to send
// before anything reaches Proton.
func uploads(paths []string) ([]passsvc.Upload, error) {
	out := make([]passsvc.Upload, 0, len(paths))
	for _, path := range paths {
		up, err := passsvc.ReadUpload(path)
		if err != nil {
			return nil, err
		}
		out = append(out, up)
	}
	return out, nil
}

// reporting gives each file its own bar, numbered within the run, so a create
// that attaches several says which one is going up.
func reporting(c *kit.Invocation, ups []passsvc.Upload) []passsvc.Upload {
	for i := range ups {
		ups[i].Progress = ui.Batch(ui.NewProgress(c.UI()), i+1, len(ups))
	}
	return ups
}
