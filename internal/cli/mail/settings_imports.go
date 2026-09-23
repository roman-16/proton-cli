package mail

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// Mail brought over from another mailbox, which Proton calls Easy Switch.
//
// Proton does the fetching: it is given the other mailbox's address, server and
// password, and connects outward from its own machines. So an import is not
// something this command waits for - it is started here and runs for as long as
// it takes, and everything else in this file is reading it, stopping it, picking
// it up again, or taking it back.

// defaultIMAPPort is where IMAP over TLS answers, and what is used for a server
// somebody named without one.
const defaultIMAPPort = 993

func importsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "imports",
		Short: "Mail brought in from another provider",
		Long: "Mail brought in from another provider.\n\n" +
			"An import carries on after the command returns, for as long as it takes.\n" +
			"`list` and `get` say how far it has got.\n\n" +
			"Every import on the account is listed, including one of a Google or\n" +
			"Microsoft account. Starting one of those needs a browser, so start it in\n" +
			"Proton's own settings and follow it here.",
	}
	c.AddCommand(importsListCmd(), importsGetCmd(), importsCreateCmd(),
		importsCancelCmd(), importsResumeCmd(), importsUndoCmd(), importsDeleteCmd())
	return c
}

func importColumns() []ui.Column[mailsvc.Import] {
	return []ui.Column[mailsvc.Import]{
		{Header: "ID", ID: true, Cell: func(i mailsvc.Import) string { return i.ID }},
		{Header: "ACCOUNT", Flex: true, Handle: true, Cell: func(i mailsvc.Import) string { return i.Account }},
		{Header: "VIA", Cell: func(i mailsvc.Import) string { return i.Provider }},
		{Header: "STATE", Cell: func(i mailsvc.Import) string { return i.State }},
		{Header: "MESSAGES", Right: true, Cell: func(i mailsvc.Import) string { return i.Progress() }},
		{Header: "SIZE", Right: true, Cell: func(i mailsvc.Import) string { return importSize(i) }},
		{Header: "WHEN", Cell: func(i mailsvc.Import) string { return units.Time(importWhen(i)) }},
	}
}

// importSize is how much arrived, which Proton totals only once an import is
// over: while one runs there is nothing true to put in the column.
func importSize(i mailsvc.Import) string {
	if i.Size == 0 {
		return ""
	}
	return units.Size(i.Size)
}

func importWhen(i mailsvc.Import) int64 {
	if i.Ended > 0 {
		return i.Ended
	}
	return i.Started
}

func importsListCmd() *cobra.Command {
	var held kit.Held[mailsvc.Import]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the imports on the account",
		Long: "List the imports on the account, newest first.\n\n" +
			"MESSAGES counts what has arrived, against how many there are to fetch.\n" +
			"SIZE is filled in once an import is over. VIA is how the other mailbox\n" +
			"is reached: imap, google or outlook.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Mail.Imports(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.Import]{
				Noun: "imports", Columns: importColumns(),
			}, rows)
		}),
	}
	held.Register(c, "imports",
		kit.Key[mailsvc.Import]{Name: "date", Less: func(a, b mailsvc.Import) int {
			return kit.Ints(importWhen(b), importWhen(a))
		}},
		kit.Key[mailsvc.Import]{Name: "account", Less: func(a, b mailsvc.Import) int {
			return kit.Fold(a.Account, b.Account)
		}},
		kit.Key[mailsvc.Import]{Name: "state", Less: func(a, b mailsvc.Import) int {
			return kit.Fold(a.State, b.State)
		}},
		kit.Key[mailsvc.Import]{Name: "size", Less: func(a, b mailsvc.Import) int {
			return kit.Ints(b.Size, a.Size)
		}},
	)
	return c
}

func importsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one import, folder by folder",
		Long: "Show one import, folder by folder.\n\n" +
			"FOLDERS is each folder of the other mailbox, where its mail lands here,\n" +
			"and how much of it has.\n\n" +
			"An import that stopped says why. A delayed one is waiting on the other\n" +
			"provider and picks itself up; a paused one needs `resume`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := importLookup(c, anyImport).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: found,
				Fields: []ui.Field{
					{Label: "Account", Value: found.Account, Handle: true},
					{Label: "Via", Value: found.Provider},
					{Label: "Server", Value: found.Server},
					{Label: "State", Value: found.State},
					{Label: "Problem", Value: found.Problem},
					{Label: "Messages", Value: found.Progress()},
					{Label: "Size", Value: importSize(found)},
					{Label: "Started", Value: units.Time(found.Started)},
					{Label: "Ended", Value: units.Time(found.Ended)},
					{Label: "Folders", Value: importFolders(found)},
					{Label: "ID", Value: found.ID, ID: true},
				},
			})
		}),
	}
}

// importFolders is the mapping as a person reads it: one folder of the other
// mailbox per line, where it lands, and how far through it Proton is.
func importFolders(i mailsvc.Import) string {
	lines := make([]string, 0, len(i.Folders))
	for _, f := range i.Folders {
		line := f.Source + " → " + f.Destination
		switch {
		case f.Total > 0 && f.Processed < f.Total:
			line += fmt.Sprintf(" %d of %d", f.Processed, f.Total)
		case f.Total > 0:
			line += fmt.Sprintf(" %d", f.Total)
		case f.Size > 0:
			line += " " + units.Size(f.Size)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func importsCreateCmd() *cobra.Command {
	password := kit.IMAPPassword()
	var days kit.DayRange
	var server, into, label string
	var port int
	var skip []string
	var selfSigned bool
	c := &cobra.Command{
		Use:   "create LOGIN",
		Short: "Bring another mailbox in over IMAP",
		Long: "Bring another mailbox in over IMAP.\n\n" +
			"LOGIN signs in to the mailbox to import from, and is usually its address;\n" +
			"--imap-password-file is what opens it. That password is sent to Proton,\n" +
			"which connects to the mailbox and keeps the password until the import is\n" +
			"over. Most providers want an app password here rather than the one you\n" +
			"sign in with.\n\n" +
			"--server and --port are looked up from the address, and have to be given\n" +
			"for a provider that is not known, or for a login that is not an address.\n\n" +
			"Every folder is imported unless --skip leaves it out, which leaves out\n" +
			"what is inside it too. A folder that matches one of yours lands in it and\n" +
			"the rest keep their own names; Gmail's folders arrive as labels. Mail\n" +
			"outside --after and --before is left behind, by the day it arrived.\n\n" +
			"Everything that arrives carries one label, named after the other mailbox\n" +
			"unless --label says otherwise.\n\n" +
			"The import carries on after this returns. `get` says how far it has got,\n" +
			"and `undo` takes back everything it brought in.",
		RunE: kit.Run([]kit.Step{password.Supply}, func(c *kit.Invocation) error {
			account := strings.TrimSpace(c.Args[0])
			secret, err := password.Value()
			if err != nil {
				return err
			}
			address, err := importInto(c, into)
			if err != nil {
				return err
			}
			if server, port, err = importServer(c, account, server, port); err != nil {
				return err
			}
			if label == "" {
				label = mailsvc.ImportLabel(account, time.Now())
			}
			after, before := days.Days()
			spec := mailsvc.ImportSpec{
				Account: account, Password: secret, Server: server, Port: port,
				AllowSelfSigned: selfSigned, Into: *address, Label: label, Skip: skip,
			}
			if !after.IsZero() {
				spec.After = after.Unix()
			}
			if !before.IsZero() {
				spec.Before = before.AddDate(0, 0, 1).Unix()
			}

			// The server is in the account of the change rather than only in the
			// record below it, because a dry run prints the account and no record,
			// and where it would connect is what a dry run is for.
			var started mailsvc.Import
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Created, Kind: "imports", Count: 1, Name: account,
				Detail:        "at " + spec.Server + ":" + strconv.Itoa(spec.Port) + " into " + address.Email,
				AnswerFollows: true,
			}, func() error {
				started, err = c.App.Mail.ImportCreate(c.Ctx, spec)
				return err
			}); err != nil {
				return err
			}
			if c.App.DryRun {
				return nil
			}
			return kit.Show(c, ui.RecordSpec{
				Object: started,
				Fields: []ui.Field{
					{Label: "Account", Value: started.Account, Handle: true},
					{Label: "Server", Value: started.Server},
					{Label: "To", Value: address.Email},
					{Label: "Label", Value: label},
					{Label: "Folders", Value: strconv.Itoa(len(started.Folders))},
					{Label: "Size", Value: units.Size(started.Size)},
					{Label: "State", Value: started.State},
					{Label: "ID", Value: started.ID, ID: true},
				},
			})
		}),
	}
	password.Declare(c)
	days.Register(c)
	c.Flags().StringVar(&server, "server", "", "IMAP server of the other mailbox")
	c.Flags().IntVar(&port, "port", 0, "Port the IMAP server answers on")
	c.Flags().BoolVar(&selfSigned, "allow-self-signed", false,
		"Accept a certificate Proton cannot verify")
	c.Flags().StringVar(&into, "to", "", "Your address the imported mail belongs to")
	c.Flags().StringVar(&label, "label", "", "Label to put on everything that arrives")
	c.Flags().StringArrayVar(&skip, "skip", nil,
		"Folder of the other mailbox to leave out, as a name or a glob (repeatable)")
	return c
}

// importInto is the address of this account the imported mail belongs to: the
// one named, or the first one in full working order, which is the one Proton's
// own client offers first.
func importInto(c *kit.Invocation, ref string) (*mailsvc.Address, error) {
	if ref != "" {
		return c.App.Mail.ResolveAddress(c.Ctx, ref)
	}
	addrs, err := c.App.Mail.AddressesList(c.Ctx)
	if err != nil {
		return nil, err
	}
	for _, a := range addrs {
		if a.CanSend() {
			return &a, nil
		}
	}
	return nil, kit.Fail("No address on this account is in a state mail can be imported into.").
		Hint(kit.Program + " mail settings addresses list")
}

// importServer is where Proton connects, which it knows for the providers it
// has met. A provider that wants a sign-in in a browser still opens to an app
// password, so it is said rather than refused.
func importServer(c *kit.Invocation, account, server string, port int) (string, int, error) {
	if server != "" {
		if port == 0 {
			port = defaultIMAPPort
		}
		return server, port, nil
	}
	target, err := c.App.Mail.ImportServer(c.Ctx, account)
	if err != nil {
		return "", 0, err
	}
	if target.Host == "" {
		return "", 0, kit.Fail("Proton does not know which server holds %s.", account).
			Hint("--server imap.example.com --port 993")
	}
	if target.OAuthOnly {
		c.Warn("%s signs in through a browser, so it wants an app password over IMAP rather than the one you type to sign in.",
			target.Host)
	}
	if port == 0 {
		port = target.Port
	}
	if port == 0 {
		port = defaultIMAPPort
	}
	return target.Host, port, nil
}

func importsCancelCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel REF...",
		Short: "Stop an import that is running",
		Long: "Stop an import that is running.\n\n" +
			"What it has already brought over stays where it landed, and the import\n" +
			"stays on the list as a record of what it did. `undo` removes the mail.\n\n" +
			"An import reads as cancelling until it has stopped.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return actOnImports(c, ui.Cancelled, running, func(i mailsvc.Import) error {
				return c.App.Mail.ImportCancel(c.Ctx, i.ID)
			})
		}),
	}
}

func importsResumeCmd() *cobra.Command {
	password := kit.IMAPPassword()
	c := &cobra.Command{
		Use:   "resume REF",
		Short: "Set a stopped import going again",
		Long: "Set a stopped import going again, from where it got to.\n\n" +
			"One that stopped because the other mailbox refused the connection needs\n" +
			"the password again, as --imap-password-file. One that stopped because\n" +
			"this account is nearly full needs room made first.\n\n" +
			"A delayed import needs nothing: it picks itself up.",
		RunE: kit.Run([]kit.Step{password.Supply, kit.StepExpand}, func(c *kit.Invocation) error {
			var secret string
			if password.Wanted() {
				var err error
				if secret, err = password.Value(); err != nil {
					return err
				}
			}
			return actOnImports(c, ui.Resumed, running, func(i mailsvc.Import) error {
				return c.App.Mail.ImportResume(c.Ctx, i.ID, secret)
			})
		}),
	}
	password.Declare(c)
	return c
}

func importsUndoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "undo REF...",
		Short: "Take back everything an import brought in",
		Long: "Take back everything an import brought in.\n\n" +
			"The messages it imported go, and so do the folders and labels it made\n" +
			"for them. Nothing brings them back: the mail is still in the mailbox it\n" +
			"came from, and fetching it again is a new import.\n\n" +
			"Only a finished import can be taken back, and only for as long as it is\n" +
			"offered.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return actOnImports(c, ui.Undone, undoable, func(i mailsvc.Import) error {
				return c.App.Mail.ImportUndo(c.Ctx, i.ID)
			})
		}),
	}
}

func importsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Forget the record of a finished import",
		Long: "Forget the record of a finished import.\n\n" +
			"The mail it brought over stays where it is. What goes is the row: how\n" +
			"much arrived, when, and the offer to undo it.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			return actOnImports(c, ui.Deleted, finished, func(i mailsvc.Import) error {
				return c.App.Mail.ImportForget(c.Ctx, i.ID)
			})
		}),
	}
}

// actOnImports resolves what a verb was pointed at, refuses what that verb
// cannot act on, and does it.
//
// Each verb owns one half of the collection: what is running can be stopped and
// picked up, and what is over can be undone and forgotten. So the half is what a
// reference is resolved against, and a reference naming the other half is
// answered with the verb that fits it rather than with "no such import".
func actOnImports(c *kit.Invocation, action ui.Action, want importHalf, apply func(mailsvc.Import) error) error {
	lookup := importLookup(c, want)
	sel, err := kit.SelectFrom(c, "imports", importColumns(), lookup)
	if err != nil {
		return importElsewhere(c, err, want)
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: action, Kind: "imports", Count: sel.Len(), IDs: sel.IDs,
		Name:    kit.Sole(sel.Rows, func(i mailsvc.Import) string { return i.Account }),
		Preview: sel.Preview(),
	}, func() error {
		for _, row := range sel.Rows {
			if err := apply(row); err != nil {
				return errs.Naming(row.Account, err)
			}
		}
		return nil
	})
}

// importHalf is the imports one verb can act on.
type importHalf struct {
	// name is what the half is called in a refusal.
	name string
	// holds reports whether an import is in it.
	holds func(mailsvc.Import) bool
}

var (
	anyImport = importHalf{name: "import", holds: func(mailsvc.Import) bool { return true }}
	running   = importHalf{
		name:  "import that is still running",
		holds: func(i mailsvc.Import) bool { return !i.Finished },
	}
	finished = importHalf{
		name:  "import that is over",
		holds: func(i mailsvc.Import) bool { return i.Finished },
	}
	undoable = importHalf{
		name:  "import that can still be taken back",
		holds: func(i mailsvc.Import) bool { return i.Finished && i.Undoable },
	}
)

// importVerbFor is what acts on an import in the state it is in, which is what
// a refusal offers instead of the verb that was typed.
func importVerbFor(i mailsvc.Import) string {
	switch {
	case !i.Finished:
		return "cancel"
	case i.Undoable:
		return "undo"
	}
	return "delete"
}

func importLookup(c *kit.Invocation, want importHalf) *kit.Lookup[mailsvc.Import] {
	return &kit.Lookup[mailsvc.Import]{
		Kind: want.name,
		Load: func(ctx context.Context) ([]mailsvc.Import, error) {
			all, err := c.App.Mail.Imports(ctx)
			if err != nil {
				return nil, err
			}
			out := make([]mailsvc.Import, 0, len(all))
			for _, i := range all {
				if want.holds(i) {
					out = append(out, i)
				}
			}
			return out, nil
		},
		ID:     func(i mailsvc.Import) string { return i.ID },
		Handle: func(i mailsvc.Import) string { return i.Account },
	}
}

// importElsewhere turns "no such import" into the command that would have
// worked, for a reference that names an import this verb cannot act on.
func importElsewhere(c *kit.Invocation, err error, want importHalf) error {
	var missing *errs.NotFound
	if !errors.As(err, &missing) {
		return err
	}
	all, listErr := c.App.Mail.Imports(c.Ctx)
	if listErr != nil {
		return err
	}
	verb := c.Cmd.Name()
	for _, i := range all {
		if i.ID != missing.Ref && !strings.EqualFold(i.Account, missing.Ref) {
			continue
		}
		refusal := kit.Fail("That import is %s, and %s acts on an %s.", i.State, verb, want.name).Exit(3)
		if fits := importVerbFor(i); fits != verb {
			refusal = refusal.Hint(kit.Program + " mail settings imports " + fits + " " + missing.Ref)
		}
		return refusal
	}
	return err
}
