package pass

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/progress"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Backups, in the format Proton Pass itself writes and reads.
//
// The point of a backup is that something else can open it, so the file is
// Proton's rather than this tool's: an archive holding one JSON document and the
// attachments beside it, each item's content in the same encoding the app
// produces. What Proton Pass writes, this reads; what this writes, Proton Pass
// takes back.

func exportCmd() *cobra.Command {
	var dest kit.Destination
	var passphrase kit.Passphrase
	var noAttachments bool
	c := &cobra.Command{
		Use:   "export",
		Short: "Write the vaults you own out as a Proton Pass archive",
		Long: "Write the vaults you own out as a Proton Pass archive, or to stdout with --dest -.\n\n" +
			"This writes the same archive format Proton Pass writes, so the app can read\n" +
			"it back.\n\n" +
			"Give a passphrase and the items are encrypted to it. Without one, the\n" +
			"archive holds every password in the clear. Attachments are never encrypted,\n" +
			"whether a passphrase is given or not.\n\n" +
			"Attachments are included; --no-attachments leaves them out, which is much\n" +
			"faster.\n\n" +
			"Only vaults you own are included. A vault somebody shared with you is\n" +
			"theirs to back up.",
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			if err := dest.Validate(true); err != nil {
				return err
			}
			// A backup nobody locked holds every password in plain text, which is
			// worth saying once at the moment it is written rather than in a
			// manual nobody reads.
			if !passphrase.Wanted() {
				c.Warn("This archive is not encrypted; anything that can read the file can read every password in it.")
			}
			plan, err := c.App.Pass.PlanExport(c.Ctx, c.App.UserID(), !noAttachments)
			if err != nil {
				return err
			}
			if plan.SkippedVaults > 0 {
				c.Note("%s shared with you: not in this archive, and backed up by the account that owns them.",
					ui.Quantity(plan.SkippedVaults, "vaults"))
			}
			if passphrase.Wanted() && len(plan.Files) > 0 {
				c.Warn("The attachments in this archive are not encrypted; only the item data is.")
			}
			var secret string
			if passphrase.Wanted() {
				if secret, err = c.App.Creds.Passphrase("lock the archive"); err != nil {
					return err
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "items", Count: plan.Items(),
				Detail:        carried(len(plan.Files)) + "to " + dest.Describe(),
				AnswerFollows: dest.Stdout(),
			}, func() error {
				// The archive is written as it is gathered, so an account with
				// more attachments than memory still has a backup.
				_, err := dest.Stream(c, "proton-pass-export.zip", func(w io.Writer) error {
					return c.App.Pass.WriteArchive(c.Ctx, w, plan, secret, transferring(c))
				})
				return err
			})
		}),
	}
	dest.Register(c)
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

func importCmd() *cobra.Command {
	var passphrase kit.Passphrase
	c := &cobra.Command{
		Use:   "import PATH",
		Short: "Read a Proton Pass archive back in",
		Long: "Read a Proton Pass archive back in, or one on stdin with -.\n\n" +
			"A vault in the file lands in the vault of that name, which is created if it\n" +
			"does not exist.\n\n" +
			"Items are always added, never matched against what is already there, so\n" +
			"reading the same file twice creates duplicates.\n\n" +
			"The attachments in the archive are put back on their items, which needs a\n" +
			"paid Pass plan. Whatever cannot be put back is named once the items have\n" +
			"landed.",
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			path, spooled, err := archivePath(c, c.Args[0])
			if err != nil {
				return err
			}
			defer spooled()
			archive, err := passsvc.OpenArchive(path, func() (string, error) {
				return c.App.Creds.Passphrase("open the archive")
			})
			if err != nil {
				return kit.Fail("%v.", capitalise(err.Error()))
			}
			defer func() { _ = archive.Close() }()

			plan, err := c.App.Pass.PlanImport(c.Ctx, archive)
			if err != nil {
				return err
			}
			if plan.Count() == 0 && len(plan.Skipped) == 0 {
				return kit.Fail("%s holds no items.", c.Args[0])
			}
			// A preview says what will not land as well as what will, so a dry run
			// and the run itself name the same things.
			if c.App.DryRun {
				for _, skipped := range plan.Skipped {
					c.Warn("%v", skipped)
				}
				for _, skipped := range plan.SkippedFiles {
					c.Warn("%v", skipped)
				}
			}
			var files []passsvc.SkippedFile
			if err := kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "items", Count: plan.Count(),
				Detail:  "from " + c.Args[0] + newVaults(plan),
				Preview: kit.Preview("items", importColumns(plan), plannedRows(plan)),
			}, func() ([]passsvc.SkippedEntry, error) {
				res, err := c.App.Pass.Import(c.Ctx, plan, transferring(c))
				if err != nil {
					return nil, err
				}
				files = res.SkippedFiles
				return res.Skipped, nil
			}); err != nil {
				return err
			}
			// An attachment that did not go up costs the file and not the item, so
			// it is named after the result rather than counted out of it.
			for _, skipped := range files {
				c.Warn("%v", skipped)
			}
			return nil
		}),
	}
	passphrase.Declare(c)
	return c
}

// archivePath is the file to read, and how to let go of it afterwards.
//
// An archive is read in place rather than into memory, and a stream cannot be,
// so one arriving on stdin is written to a file of its own first and removed
// when the command is done with it.
func archivePath(c *kit.Invocation, path string) (string, func(), error) {
	nothingToClean := func() {}
	if path != "-" {
		return path, nothingToClean, nil
	}
	r, err := c.App.Stdin("PATH -")
	if err != nil {
		return "", nothingToClean, err
	}
	spooled, err := os.CreateTemp("", "proton-cli-import-*.zip")
	if err != nil {
		return "", nothingToClean, err
	}
	discard := func() { _ = os.Remove(spooled.Name()) }
	defer func() { _ = spooled.Close() }()
	if _, err := io.Copy(spooled, r); err != nil {
		discard()
		return "", nothingToClean, kit.Fail("could not read PATH from stdin: %v", err)
	}
	return spooled.Name(), discard, nil
}

// newVaults says how many vaults the read-back would create, since making one is
// the part a person cannot undo by deleting a few items.
func newVaults(plan *passsvc.ImportPlan) string {
	var n int
	for _, v := range plan.Vaults {
		if v.New && len(v.Items) > 0 {
			n++
		}
	}
	switch n {
	case 0:
		return ""
	case 1:
		return ", making 1 vault"
	}
	return fmt.Sprintf(", making %d vaults", n)
}

// plannedRow is one item a read-back would write, as a preview shows it.
type plannedRow struct {
	Name        string `json:"name"`
	Vault       string `json:"vault"`
	Attachments int    `json:"attachments"`
}

func importColumns(plan *passsvc.ImportPlan) []ui.Column[plannedRow] {
	cols := []ui.Column[plannedRow]{
		{Header: "NAME", Cell: func(r plannedRow) string { return r.Name }},
		{Header: "VAULT", Cell: func(r plannedRow) string { return r.Vault }},
	}
	if plan.Attachments() == 0 {
		return cols
	}
	return append(cols, ui.Column[plannedRow]{
		Header: "ATTACHMENTS", Right: true,
		Cell: func(r plannedRow) string { return fmt.Sprint(r.Attachments) },
	})
}

func plannedRows(plan *passsvc.ImportPlan) []plannedRow {
	var out []plannedRow
	for _, v := range plan.Vaults {
		for i, name := range v.Names {
			out = append(out, plannedRow{
				Name: name, Vault: v.Name, Attachments: len(v.Files[i]),
			})
		}
	}
	return out
}
