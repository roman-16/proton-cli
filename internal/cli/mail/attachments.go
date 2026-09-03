package mail

import (
	"path/filepath"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Attachments hang off whatever holds them, so the tree is `messages attachments`
// and `conversations attachments` - the same idea spelled the same way in both
// places.

func attachmentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "attachments", Short: "Files attached to a message"}
	c.AddCommand(attachmentsListCmd(), attachmentsDownloadCmd())
	return c
}

func attachmentTableSpec(includeInline bool) ui.TableSpec[mailsvc.Attachment] {
	cols := []ui.Column[mailsvc.Attachment]{
		{Header: "ID", ID: true, Cell: func(a mailsvc.Attachment) string { return a.ID }},
		{Header: "NAME", Flex: true, Cell: func(a mailsvc.Attachment) string { return a.Name }},
		{Header: "SIZE", Right: true, Cell: func(a mailsvc.Attachment) string { return units.Size(a.Size) }},
		{Header: "TYPE", Flex: true, Cell: func(a mailsvc.Attachment) string { return a.MIMEType }},
	}
	if includeInline {
		cols = append(cols, ui.Column[mailsvc.Attachment]{
			Header: "DISPOSITION", Cell: func(a mailsvc.Attachment) string {
				if a.Disposition == "" {
					return "attachment"
				}
				return a.Disposition
			},
		})
	}
	return ui.TableSpec[mailsvc.Attachment]{
		Noun: "attachments", Columns: cols,
		Total: ui.Unknown, Page: ui.Unpaged,
	}
}

func attachmentsListCmd() *cobra.Command {
	var includeInline bool
	c := &cobra.Command{
		Use:   "list REF",
		Short: "List a message's attachments",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Mail.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "attachments list")
			}
			atts, err := c.App.Mail.AttachmentsList(c.Ctx, id, includeInline)
			if err != nil {
				return wrongTable(err, "attachments list")
			}
			return kit.List(c, attachmentTableSpec(includeInline), atts)
		}),
	}
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments, such as signature graphics")
	return c
}

func attachmentsDownloadCmd() *cobra.Command {
	var dest kit.Destination
	var includeInline bool
	c := &cobra.Command{
		Use:   "download REF [ATTACHMENT_REF]",
		Short: "Download and decrypt attachments",
		Long: "Download and decrypt attachments.\n\n" +
			"Naming an attachment downloads that one; naming none downloads them all.\n" +
			"Existing files are never overwritten silently: a collision becomes\n" +
			"\"file (2).pdf\" unless --force says otherwise.",
		Args: cobra.RangeArgs(1, 2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			one := len(c.Args) == 2
			if err := dest.Validate(one); err != nil {
				return err
			}
			msgID, err := c.App.Mail.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "attachments download")
			}
			if one {
				return downloadOne(c, msgID, c.Args[1], &dest)
			}
			return downloadAll(c, msgID, &dest, includeInline)
		}),
	}
	dest.Register(c)
	c.Flags().BoolVar(&includeInline, "include-inline", false, "Include inline attachments when downloading them all")
	return c
}

func downloadOne(c *kit.Invocation, msgID, attID string, dest *kit.Destination) error {
	data, name, err := c.App.Mail.AttachmentDownload(c.Ctx, msgID, attID)
	if err != nil {
		return err
	}
	written, err := dest.Write(c, name, data)
	if err != nil {
		return err
	}
	if written == "" {
		return nil // streamed to stdout
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Count: 1, Name: filepath.Base(written),
		Detail: "to " + written,
	}, func() error { return nil })
}

func downloadAll(c *kit.Invocation, msgID string, dest *kit.Destination, includeInline bool) error {
	atts, err := c.App.Mail.AttachmentsList(c.Ctx, msgID, includeInline)
	if err != nil {
		return wrongTable(err, "attachments download")
	}
	if len(atts) == 0 {
		return kit.Mutate(c, ui.ResultSpec{Action: ui.Downloaded, Kind: "attachments", Count: 0},
			func() error { return nil })
	}
	names := make([]string, 0, len(atts))
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Downloaded, Kind: "attachments", Count: len(atts),
		Detail: "to " + dest.Describe(),
	}, func() error {
		for _, at := range atts {
			data, _, err := c.App.Mail.AttachmentDownload(c.Ctx, msgID, at.ID)
			if err != nil {
				return kit.Fail("could not download %s: %v", at.Name, err)
			}
			written, err := dest.Write(c, at.Name, data)
			if err != nil {
				return err
			}
			names = append(names, written)
		}
		return nil
	})
}
