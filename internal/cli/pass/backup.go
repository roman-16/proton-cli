package pass

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Backups, in the format Proton Pass itself writes and reads.
//
// The point of a backup is that something else can open it, so the file is
// Proton's rather than this tool's: an archive holding one JSON document, each
// item's content in the same encoding the app produces. What Proton Pass writes,
// this reads; what this writes, Proton Pass takes back.

func exportCmd() *cobra.Command {
	var dest kit.Destination
	var passphrase kit.Passphrase
	c := &cobra.Command{
		Use:   "export",
		Short: "Write the vaults you own out as a Proton Pass archive",
		Long: "Write the vaults you own out as a Proton Pass archive, or to stdout with --dest -.\n\n" +
			"The file is the one Proton Pass itself writes, so it can be read back by\n" +
			"the app as well as by this tool. Give a passphrase and the contents are\n" +
			"encrypted to it; without one the archive holds every password in the clear.\n\n" +
			"A vault somebody shared with you is theirs to back up, so it is not in the\n" +
			"file - restoring this one cannot turn their vault into a second copy under\n" +
			"your name.",
		Args: cobra.NoArgs,
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
			doc, skipped, err := c.App.Pass.Export(c.Ctx, c.App.UserID())
			if err != nil {
				return err
			}
			if skipped > 0 {
				c.Note("%s shared with you: not in this archive, and backed up by the account that owns them.",
					ui.Quantity(skipped, "vaults"))
			}
			var secret string
			if passphrase.Wanted() {
				if secret, err = c.App.Creds.Passphrase("lock the archive"); err != nil {
					return err
				}
			}
			raw, err := passsvc.Archive(doc, secret)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "items", Count: itemsIn(doc),
				Detail: "to " + dest.Describe(), AnswerFollows: dest.Stdout(),
			}, func() error {
				_, err := dest.Write(c, "proton-pass-export.zip", raw)
				return err
			})
		}),
	}
	dest.Register(c)
	passphrase.Declare(c)
	return c
}

// itemsIn is how many items a document carries, which is what the report counts:
// vaults are the shape of the file, items are what somebody has.
func itemsIn(doc *passsvc.ExportDocument) int {
	var n int
	for _, v := range doc.Vaults {
		n += len(v.Items)
	}
	return n
}

func importCmd() *cobra.Command {
	var passphrase kit.Passphrase
	c := &cobra.Command{
		Use:   "import PATH",
		Short: "Read a Proton Pass archive back in",
		Long: "Read a Proton Pass archive back in, or one on stdin with -.\n\n" +
			"A vault in the file lands in the vault of that name, and one that is not\n" +
			"there yet is made. Items are added rather than matched: nothing in a file\n" +
			"says which existing item it was, so reading the same file twice puts the\n" +
			"items in twice.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			raw, err := kit.ReadBytesArg(c, c.Args[0], "PATH")
			if err != nil {
				return err
			}
			doc, err := passsvc.Unarchive(raw, func() (string, error) {
				return c.App.Creds.Passphrase("open the archive")
			})
			if err != nil {
				return kit.Fail("%v.", capitalise(err.Error()))
			}
			plan, err := c.App.Pass.PlanImport(c.Ctx, doc)
			if err != nil {
				return err
			}
			if plan.Count() == 0 && len(plan.Skipped) == 0 {
				return kit.Fail("%s holds no items.", c.Args[0])
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "items", Count: plan.Count(),
				Detail:  "from " + c.Args[0] + newVaults(plan),
				Preview: kit.Preview("items", importColumns(), plannedRows(plan)),
			}, func() ([]passsvc.SkippedEntry, error) {
				res, err := c.App.Pass.Import(c.Ctx, plan)
				if err != nil {
					return nil, err
				}
				return res.Skipped, nil
			})
		}),
	}
	passphrase.Declare(c)
	return c
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
	Name  string `json:"name"`
	Vault string `json:"vault"`
}

func importColumns() []ui.Column[plannedRow] {
	return []ui.Column[plannedRow]{
		{Header: "NAME", Cell: func(r plannedRow) string { return r.Name }},
		{Header: "VAULT", Cell: func(r plannedRow) string { return r.Vault }},
	}
}

func plannedRows(plan *passsvc.ImportPlan) []plannedRow {
	var out []plannedRow
	for _, v := range plan.Vaults {
		for _, name := range v.Names {
			out = append(out, plannedRow{Name: name, Vault: v.Name})
		}
	}
	return out
}
