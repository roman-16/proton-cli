package mail

import (
	"io"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Export writes messages back out as RFC 822. Selection reuses the same
// references and filters as trash and move, so "everything from this sender older
// than a year" reads the same here as it does there.

const (
	formatEML  = "eml"
	formatMbox = "mbox"
)

func exportCmd() *cobra.Command {
	var f filters
	var dest kit.Destination
	var noAttachments bool
	format := &kit.Enum{
		Name: "format", Usage: "How to lay the messages down", Default: formatEML,
		Values: []string{formatEML, formatMbox},
	}
	c := &cobra.Command{
		Use:   "export [REF...]",
		Short: "Write messages out as .eml or mbox files",
		Long: "Write messages out as standalone RFC 822 documents, readable by any mail\n" +
			"client, grep, or anything else.\n\n" +
			"eml writes one file per message; mbox concatenates everything into one stream.\n" +
			"Skipping attachments with --no-attachments is much faster for a large archive.\n\n" +
			"Exported files are not encrypted. Their DKIM and ARC headers no longer\n" +
			"verify either, since the body those headers signed was the encrypted one.\n" +
			"Proton's own web export behaves the same way.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shape, err := format.Value()
			if err != nil {
				return err
			}
			sel, err := selectMessages(c, &f)
			if err != nil {
				return wrongTable(err, "export")
			}
			// One mbox is one stream whatever it contains, so the single-file
			// check only applies to eml.
			if err := dest.Validate(shape == formatMbox || sel.Len() == 1); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "messages", Count: sel.Len(), IDs: sel.IDs,
				Detail: "to " + dest.Describe(), Preview: sel.Preview(),
			}, func() error {
				if shape == formatMbox {
					return exportMbox(c, sel.IDs, &dest, !noAttachments)
				}
				return exportEML(c, sel.IDs, &dest, !noAttachments)
			})
		}),
	}
	format.Register(c)
	dest.Register(c)
	c.Flags().BoolVar(&noAttachments, "no-attachments", false, "Skip attachments, which is much faster")
	f.register(c)
	return c
}

// exportEML writes one document per message, named after when it arrived and what
// it says, which sorts chronologically and stays readable.
func exportEML(c *kit.Invocation, ids []string, dest *kit.Destination, withAttachments bool) error {
	for _, id := range ids {
		doc, meta, err := c.App.Mail.Export(c.Ctx, id, withAttachments)
		if err != nil {
			return err
		}
		if _, err := dest.Write(c, exportName(meta), doc); err != nil {
			return err
		}
	}
	return nil
}

// exportMbox concatenates every message into one stream.
//
// One entry at a time: an archive is as large as a mailbox, so holding the whole
// thing in memory to write it would put a ceiling on what can be exported.
func exportMbox(c *kit.Invocation, ids []string, dest *kit.Destination, withAttachments bool) error {
	_, err := dest.Stream(c, "mail.mbox", func(w io.Writer) error {
		for _, id := range ids {
			doc, meta, err := c.App.Mail.Export(c.Ctx, id, withAttachments)
			if err != nil {
				return err
			}
			if _, err := w.Write(mailsvc.MboxEntry(doc, meta)); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func exportName(meta *mailsvc.ExportMeta) string {
	stamp := time.Unix(meta.Time, 0).Local().Format("2006-01-02 1504")
	return kit.SafeFilename(stamp+" "+meta.Subject) + ".eml"
}

// conversationExportCmd writes a whole thread out, oldest first.
func conversationExportCmd() *cobra.Command {
	var dest kit.Destination
	var noAttachments bool
	format := &kit.Enum{
		Name: "format", Usage: "How to lay the thread down", Default: formatMbox,
		Values: []string{formatEML, formatMbox},
	}
	c := &cobra.Command{
		Use:   "export REF",
		Short: "Write a whole thread out as .eml files or one mbox",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shape, err := format.Value()
			if err != nil {
				return err
			}
			convID, err := c.App.Mail.ResolveConversation(c.Ctx, c.Args[0])
			if err != nil {
				return wrongTable(err, "export")
			}
			ids, err := c.App.Mail.ConversationMessageIDs(c.Ctx, convID)
			if err != nil {
				return wrongTable(err, "export")
			}
			if err := dest.Validate(shape == formatMbox || len(ids) == 1); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "messages", Count: len(ids), IDs: ids,
				Detail: "to " + dest.Describe(),
			}, func() error {
				if shape == formatMbox {
					return exportMbox(c, ids, &dest, !noAttachments)
				}
				return exportEML(c, ids, &dest, !noAttachments)
			})
		}),
	}
	format.Register(c)
	dest.Register(c)
	c.Flags().BoolVar(&noAttachments, "no-attachments", false, "Skip attachments")
	return c
}
