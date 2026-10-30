package account

import (
	"context"
	"errors"
	"slices"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	var dir string
	var passphrase kit.Passphrase
	c := &cobra.Command{
		Use:   "export [APP...]",
		Short: "Write a copy of the account to a directory",
		Long: "Write a copy of the account to a directory.\n\n" +
			"Name the apps to export, or none for all of them: calendar, contacts,\n" +
			"drive, mail and pass. The directory has to be new, empty, or one this\n" +
			"command wrote before.\n\n" +
			"Run it again into the same directory to continue a run that stopped, or\n" +
			"later to bring the copy up to date. Only what is missing or changed is\n" +
			"fetched, and nothing is removed from the copy.\n\n" +
			"Everything is written decrypted. Only the Pass archive can be locked, with\n" +
			"--passphrase-file.",
		ValidArgsFunction: completeApps,
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			apps, err := named(c.Args)
			if err != nil {
				return err
			}
			if passphrase.Wanted() && !slices.Contains(apps, export.AppPass) {
				return kit.Fail("--passphrase-file locks the Pass archive, and pass is not being exported.")
			}
			if err := refusal(dir, export.Check(dir), ""); err != nil {
				return err
			}
			if err := c.App.Authenticate(c.Ctx); err != nil {
				return err
			}
			secret := ""
			if passphrase.Wanted() {
				if secret, err = c.App.Creds.Passphrase("lock the Pass archive"); err != nil {
					return err
				}
			}
			parts := partsFor(c, apps, secret)
			if c.App.DryRun {
				return preview(c, dir, parts)
			}
			return copyAccount(c, dir, parts)
		}),
	}
	c.Flags().StringVar(&dir, "dest-dir", "", "Directory to keep the copy in")
	_ = c.MarkFlagRequired("dest-dir")
	passphrase.Declare(c)
	return c
}

func copyAccount(c *kit.Invocation, dir string, parts []export.Part) error {
	into, err := export.Open(dir, c.App.UserID(), c.App.Email())
	if err := refusal(dir, err, c.App.Email()); err != nil {
		return err
	}
	defer into.Close()
	if a, err := c.App.Account.Get(c.Ctx); err == nil {
		roomFor(c, dir, spent(a, parts))
	}
	results := make([]export.Result, 0, len(parts))
	written := map[string]any{}
	var wrote []export.Part
	for i, part := range parts {
		sink := ui.Batch(ui.NewCounter(c.UI(), part.Noun()), i+1, len(parts))
		got, err := part.Run(c.Ctx, into, sink)
		results = append(results, got)
		written[string(part.App())] = got.Exported
		if got.Exported > 0 {
			wrote = append(wrote, part)
		}
		if err != nil {
			if errors.Is(err, context.Canceled) && got.Owed > 0 {
				c.Note("Exported %d of %s so far. Run it again to continue.",
					got.Exported, ui.Quantity(got.Owed, part.Noun()))
			}
			return err
		}
	}
	done := export.Total(results...)
	notCopied(c, done)
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Exported, Kind: kind(parts, wrote), Count: done.Exported,
		Detail: across(wrote) + "to " + dir, Extra: written,
	}, func() error { return nil })
}

func notCopied(c *kit.Invocation, done export.Result) {
	if done.Unreadable > 0 {
		subject := "they are"
		if done.Unreadable == 1 {
			subject = "it is"
		}
		c.Warn("%s would not open; %s in the copy as stored, encrypted.",
			ui.Quantity(done.Unreadable, "messages"), subject)
	}
	if done.Unlocked {
		c.Warn("The Pass archive is not encrypted; anything that can read it can read every password in it.")
	}
	if done.Documents > 0 {
		c.Note("%s not in the copy; open %s in Proton Docs.", copyIsOrAre(done.Documents, "Proton Docs documents"), copyThem(done.Documents))
	}
	if done.Spreadsheets > 0 {
		c.Note("%s not in the copy; open %s in Proton Sheets.", copyIsOrAre(done.Spreadsheets, "Proton Sheets spreadsheets"), copyThem(done.Spreadsheets))
	}
	var shared []string
	for _, noun := range []string{"vaults", "calendars"} {
		if n := done.Shared[noun]; n > 0 {
			shared = append(shared, ui.Quantity(n, noun))
		}
	}
	if len(shared) > 0 {
		c.Note("Shared with you and not in the copy: %s. The accounts that own them keep them.", ui.Listing(shared))
	}
}

func copyIsOrAre(n int, noun string) string {
	if n == 1 {
		return ui.Quantity(1, noun) + " is"
	}
	return ui.Quantity(n, noun) + " are"
}

func copyThem(n int) string {
	if n == 1 {
		return "it"
	}
	return "them"
}

func kind(parts, wrote []export.Part) string {
	switch {
	case len(wrote) == 1:
		return wrote[0].Noun()
	case len(wrote) == 0 && len(parts) == 1:
		return parts[0].Noun()
	}
	return "entries"
}

func across(wrote []export.Part) string {
	if len(wrote) < 2 {
		return ""
	}
	names := make([]string, 0, len(wrote))
	for _, part := range wrote {
		names = append(names, string(part.App()))
	}
	return "across " + ui.Listing(names) + " "
}

type holding struct {
	App   string
	Holds string
}

func preview(c *kit.Invocation, dir string, parts []export.Part) error {
	a, err := c.App.Account.Get(c.Ctx)
	if err != nil {
		return err
	}
	rows := make([]holding, 0, len(parts))
	for _, part := range parts {
		held, err := part.Holds(c.Ctx)
		if err != nil {
			return err
		}
		words := make([]string, 0, len(held))
		for _, h := range held {
			if h.Count == export.Uncounted {
				words = append(words, h.Noun)
				continue
			}
			words = append(words, ui.Quantity(h.Count, h.Noun))
		}
		line := ui.Listing(words)
		if size := a.SpentOn(product(part.App())); size > 0 {
			line += ", " + units.Size(size)
		}
		rows = append(rows, holding{App: string(part.App()), Holds: line})
	}
	columns := []ui.Column[holding]{
		{Header: "APP", Cell: func(h holding) string { return h.App }},
		{Header: "HOLDS", Flex: true, Cell: func(h holding) string { return h.Holds }},
	}
	if err := kit.Mutate(c, ui.ResultSpec{
		Action: ui.Exported, Kind: "apps", Count: len(parts), Detail: "to " + dir,
		Preview: kit.Preview("apps", columns, rows),
	}, func() error { return nil }); err != nil {
		return err
	}
	roomFor(c, dir, spent(a, parts))
	return nil
}

type spender interface{ SpentOn(product string) int64 }

func spent(a spender, parts []export.Part) int64 {
	var total int64
	for _, part := range parts {
		total += a.SpentOn(product(part.App()))
	}
	return total
}

func product(app export.App) string {
	switch app {
	case export.AppCalendar:
		return "Calendar"
	case export.AppContacts:
		return "Contact"
	case export.AppDrive:
		return "Drive"
	case export.AppMail:
		return "Mail"
	case export.AppPass:
		return "Pass"
	}
	return ""
}

func roomFor(c *kit.Invocation, dir string, needed int64) {
	free, err := localfile.Room(dir)
	if err != nil || needed == 0 || free >= needed {
		return
	}
	c.Warn("%s has %s free; the account holds about %s. A run that fills the disk stops, and continues once there is room.",
		dir, units.Size(free), units.Size(needed))
}

func refusal(dir string, err error, email string) error {
	var other *export.OtherAccount
	switch {
	case err == nil:
		return nil
	case errors.Is(err, export.ErrNotDirectory):
		return kit.Fail("%s exists and is not a directory.", dir)
	case errors.Is(err, export.ErrNotACopy):
		return kit.Fail("%s holds other files and is not a copy of an account.", dir).
			Hint("name a new or empty directory, or one `" + kit.Program + " account export` wrote.")
	case errors.As(err, &other):
		return kit.Fail("%s is a copy of %s, not of %s.", dir, other.Email, email).
			Hint("name another directory for this account.").Exit(4)
	case errors.Is(err, localfile.ErrBusy):
		return kit.Fail("Another run is already exporting into %s.", dir).
			Hint("wait for it to finish, or stop it").Exit(4)
	}
	return err
}

func named(args []string) ([]export.App, error) {
	if len(args) == 0 {
		return export.Apps, nil
	}
	wanted := map[export.App]bool{}
	for _, arg := range args {
		app, ok := export.Known(arg)
		if !ok {
			hint := "one of: " + appList()
			if arg == "photos" {
				hint = "photos are part of drive."
			}
			return nil, kit.Fail("%q is not an app that can be exported.", arg).Hint(hint)
		}
		wanted[app] = true
	}
	out := make([]export.App, 0, len(wanted))
	for _, app := range export.Apps {
		if wanted[app] {
			out = append(out, app)
		}
	}
	return out, nil
}

func partsFor(c *kit.Invocation, apps []export.App, passphrase string) []export.Part {
	out := make([]export.Part, 0, len(apps))
	for _, app := range apps {
		switch app {
		case export.AppCalendar:
			out = append(out, c.App.Calendar.CopyPart())
		case export.AppContacts:
			out = append(out, c.App.Contacts.CopyPart())
		case export.AppDrive:
			out = append(out, c.App.Drive.CopyPart())
		case export.AppMail:
			out = append(out, c.App.Mail.CopyPart())
		case export.AppPass:
			out = append(out, c.App.Pass.CopyPart(c.App.UserID(), passphrase))
		}
	}
	return out
}

func appList() string {
	names := export.Names(export.Apps)
	slices.Sort(names)
	return ui.Listing(names)
}

func completeApps(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return export.Names(export.Apps), cobra.ShellCompDirectiveNoFileComp
}
