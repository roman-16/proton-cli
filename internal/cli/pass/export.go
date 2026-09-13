package pass

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Writing an account out.
//
// The zip is Proton's own, so the app reads back what this writes. The other two
// shapes are the document inside that zip on its own, and the spreadsheet the
// app also offers.

// exportNames are the files each format is written to when no path was named.
var exportNames = map[string]string{
	"csv":  "proton-pass-export.csv",
	"json": "proton-pass-export.json",
	"zip":  "proton-pass-export.zip",
}

func exportCmd() *cobra.Command {
	var dest kit.Destination
	var passphrase kit.Passphrase
	var noAttachments bool
	format := kit.Enum{
		Name:    "format",
		Usage:   "How to lay the items down",
		Values:  []string{"zip", "csv", "json"},
		Default: "zip",
	}
	c := &cobra.Command{
		Use:   "export",
		Short: "Write the vaults you own out to a file",
		Long: "Write the vaults you own out to a file, or to stdout with --dest -.\n\n" +
			"--format zip writes the archive Proton Pass reads back, with the\n" +
			"attachments in it. --format json writes the document that archive holds,\n" +
			"without them. --format csv writes a spreadsheet, which leaves out custom\n" +
			"fields, attachments, passkeys and the keys of SSH items.\n\n" +
			"Give a passphrase and the items are encrypted to it. Without one, the file\n" +
			"holds every password in the clear. A CSV cannot be encrypted, and\n" +
			"attachments never are.\n\n" +
			"An archive includes the attachments; --no-attachments leaves them out,\n" +
			"which is much faster.\n\n" +
			"Only vaults you own are included. A vault somebody shared with you is\n" +
			"theirs to back up.",
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			layout, err := format.Value()
			if err != nil {
				return err
			}
			if layout == "csv" && passphrase.Wanted() {
				return kit.Fail("A CSV cannot be encrypted.").
					Hint("--format zip or --format json to encrypt what you write.")
			}
			// A backup nobody locked holds every password in plain text, which is
			// worth saying once at the moment it is written rather than in a
			// manual nobody reads.
			if !passphrase.Wanted() {
				c.Warn("This file is not encrypted; anything that can read it can read every password in it.")
			}
			if layout == "csv" {
				c.Warn("A CSV leaves out custom fields, attachments, passkeys and the keys of SSH items.")
			}

			withFiles := !noAttachments && layout == "zip"
			plan, err := c.App.Pass.PlanExport(c.Ctx, c.App.UserID(), withFiles)
			if err != nil {
				return err
			}
			if plan.SkippedVaults > 0 {
				c.Note("%s shared with you: not in this file, and backed up by the account that owns them.",
					ui.Quantity(plan.SkippedVaults, "vaults"))
			}
			if passphrase.Wanted() && len(plan.Files) > 0 {
				c.Warn("The attachments in this archive are not encrypted; only the item data is.")
			}
			var secret string
			if passphrase.Wanted() {
				if secret, err = c.App.Creds.Passphrase("lock the file"); err != nil {
					return err
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "items", Count: plan.Items(),
				Detail:        carried(len(plan.Files)) + "to " + dest.Describe(),
				AnswerFollows: dest.Stdout(),
			}, func() error {
				// The file is written as it is gathered, so an account with more
				// attachments than memory still has a backup.
				_, err := dest.Stream(c, exportNames[layout], func(w io.Writer) error {
					if layout == "zip" {
						return c.App.Pass.WriteArchive(c.Ctx, w, plan, secret, transferring(c))
					}
					return c.App.Pass.WriteDocument(w, plan, layout, secret)
				})
				return err
			})
		}),
	}
	dest.Register(c)
	format.Register(c)
	passphrase.Declare(c)
	c.Flags().BoolVar(&noAttachments, "no-attachments", false, "Leave attachments out, which is much faster")
	return c
}

// carried is what a backup says about the attachments it moved, and nothing at
// all when it moved none.
func carried(files int) string {
	if files == 0 {
		return ""
	}
	return "and " + ui.Quantity(files, "attachments") + " "
}

// transferring numbers the files a backup moves, so a run of them says where it
// is rather than drawing one identical bar after another.
func transferring(c *kit.Invocation) func(int, int) progress.Sink {
	return func(index, total int) progress.Sink {
		return ui.Batch(ui.NewProgress(c.UI()), index, total)
	}
}
