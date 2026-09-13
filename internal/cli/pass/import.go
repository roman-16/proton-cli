package pass

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/passfile"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
)

// Reading a file of items in, whichever password manager wrote it.

func importCmd() *cobra.Command {
	var passphrase kit.Passphrase
	var vault string
	manager := kit.Enum{
		Name:    "manager",
		Usage:   "The password manager that wrote PATH",
		Values:  passfile.Formats(),
		Default: passfile.Proton,
	}
	c := &cobra.Command{
		Use:   "import PATH",
		Short: "Read items in from a password manager's export",
		Long: "Read items in from a password manager's export, or one on stdin with -.\n\n" +
			"--manager names the program that wrote the file, and defaults to Proton\n" +
			"Pass: its archive, the document inside one, or its CSV. Each other value\n" +
			"reads what that program exports - 1Password .1pux, .1pif or .zip,\n" +
			"Bitwarden .json or .zip, Dashlane .csv or .zip, KeePass .xml, Kaspersky\n" +
			".txt, and CSV or JSON for the rest.\n\n" +
			"A vault, folder or group in the file lands in the vault of that name, which\n" +
			"is created if it does not exist. Items in none land in your first vault.\n" +
			"--vault puts everything into the one you name.\n\n" +
			"Items are always added, never matched against what is already there, so\n" +
			"reading the same file twice creates duplicates.\n\n" +
			"Attachments in the file are put back on their items, which needs a paid\n" +
			"Pass plan. Whatever cannot be put back is named once the items have landed.",
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			wrote, err := manager.Value()
			if err != nil {
				return err
			}
			path, spooled, err := importPath(c, c.Args[0])
			if err != nil {
				return err
			}
			defer spooled()
			doc, err := passfile.Open(path, wrote, func() (string, error) {
				return c.App.Creds.Passphrase("open the export")
			})
			if err != nil {
				return err
			}
			defer func() { _ = doc.Close() }()
			for _, warning := range doc.Warnings {
				c.Warn("%s", warning)
			}

			plan, err := c.App.Pass.PlanImport(c.Ctx, doc, c.App.UserID(), vault)
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
			var files []passfile.SkippedFile
			if err := kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "items", Count: plan.Count(),
				Detail:  "from " + c.Args[0] + landing(plan, vault),
				Preview: kit.Preview("items", importColumns(plan), plannedRows(plan)),
			}, func() ([]passfile.Skip, error) {
				res, err := c.App.Pass.Import(c.Ctx, plan, transferring(c))
				if err != nil {
					return nil, err
				}
				files = res.Files
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
	manager.Register(c)
	passphrase.Declare(c)
	c.Flags().StringVar(&vault, "vault", "", "Put everything into this vault")
	return c
}

// importPath is the file to read, and how to let go of it afterwards.
//
// A file is read in place rather than into memory, and a stream cannot be, so
// one arriving on stdin is written to a file of its own first and removed when
// the command is done with it.
func importPath(c *kit.Invocation, path string) (string, func(), error) {
	nothingToClean := func() {}
	if path != "-" {
		return path, nothingToClean, nil
	}
	r, err := c.App.Stdin("PATH -")
	if err != nil {
		return "", nothingToClean, err
	}
	spooled, err := os.CreateTemp("", "proton-cli-import-*")
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

// landing says where the items go, since making a vault is the part a person
// cannot undo by deleting a few items.
func landing(plan *passsvc.ImportPlan, vault string) string {
	if vault != "" {
		return " into " + vault
	}
	switch n := plan.NewVaults(); n {
	case 0:
		return ""
	case 1:
		return ", making 1 vault"
	default:
		return fmt.Sprintf(", making %d vaults", n)
	}
}

// plannedRow is one item an import would write, as a preview shows it.
type plannedRow struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Vault       string `json:"vault"`
	Attachments int    `json:"attachments"`
}

func importColumns(plan *passsvc.ImportPlan) []ui.Column[plannedRow] {
	cols := []ui.Column[plannedRow]{
		{Header: "NAME", Cell: func(r plannedRow) string { return r.Name }},
		{Header: "TYPE", Cell: func(r plannedRow) string { return r.Type }},
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
		for _, item := range v.Items {
			out = append(out, plannedRow{
				Name: item.Name, Type: item.Kind, Vault: v.Name,
				Attachments: len(item.Files),
			})
		}
	}
	return out
}
