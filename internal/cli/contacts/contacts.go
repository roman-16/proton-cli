// Package contacts is the `proton contacts` tree.
//
// The app hosts its primary collection's verbs directly - `contacts list`, not
// `contacts contacts list` - under a rule that applies to exactly one app: an
// app whose name is already the plural of its primary collection needs no second
// level to say so. Groups and pinned keys are secondary collections and do get
// their own level.
package contacts

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/vcard"
	"github.com/spf13/cobra"
)

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "contacts",
		Short: "Contacts, their groups and their pinned keys",
	}
	c.AddCommand(listCmd(), getCmd(), createCmd(), updateCmd(), deleteCmd(),
		exportCmd(), importCmd(), mergeCmd(), keysCmd(), groupsCmd())
	return c
}

// columns is the contact table. ID leads, as it does in every collection, so the
// thing you paste into the next command is always in the same place.
func columns() []ui.Column[ctsvc.Contact] {
	return []ui.Column[ctsvc.Contact]{
		{Header: "ID", ID: true, Cell: func(c ctsvc.Contact) string { return c.ID }},
		{Header: "NAME", Flex: true, Cell: func(c ctsvc.Contact) string { return c.Name }},
		{Header: "EMAIL", Flex: true, Cell: func(c ctsvc.Contact) string { return c.Email }},
		{Header: "PHONE", Cell: func(c ctsvc.Contact) string { return c.Phone }},
	}
}

func spec() ui.TableSpec[ctsvc.Contact] {
	return ui.TableSpec[ctsvc.Contact]{
		Noun: "contacts", Columns: columns(),
		Total: ui.Unknown, Page: ui.Unpaged,
	}
}

// contactOrder is how a contact list may be ordered. Proton hands the whole
// address book over as one encrypted export, so the ordering is this process's
// to do and the whole set is there to do it with.
func contactOrder() kit.Comparators[ctsvc.Contact] {
	return kit.Comparators[ctsvc.Contact]{
		"name":  func(a, b ctsvc.Contact) int { return kit.Fold(a.Name, b.Name) },
		"email": func(a, b ctsvc.Contact) int { return kit.Fold(a.Email, b.Email) },
	}
}

func listCmd() *cobra.Command {
	var page kit.Page
	var order kit.Order
	var keyword string
	c := &cobra.Command{
		Use:   "list",
		Short: "List contacts",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			all, err := c.App.Contacts.List(c.Ctx)
			if err != nil {
				return err
			}
			all = matchContacts(all, keyword)
			if err := kit.Sort(order, all, contactOrder()); err != nil {
				return err
			}
			rows, total := kit.Slice(page, all)
			s := spec()
			s.Total, s.Page, s.PageSize, s.Filtered = total, page.Number, page.Size, keyword != ""
			return kit.List(c, s, rows, func(ct ctsvc.Contact) []string { return []string{ct.ID} })
		}),
	}
	c.Flags().StringVar(&keyword, "keyword", "", "Match text in the name or the address")
	order.Register(c, "name", "email")
	page.Register(c, "contacts")
	return c
}

// matchContacts narrows an address book by free text, over the fields a listing
// shows. The whole book is already decrypted here, so this is the search Proton
// has no endpoint for.
func matchContacts(all []ctsvc.Contact, keyword string) []ctsvc.Contact {
	if keyword == "" {
		return all
	}
	needle := strings.ToLower(keyword)
	kept := make([]ctsvc.Contact, 0, len(all))
	for _, ct := range all {
		if strings.Contains(strings.ToLower(ct.Name), needle) ||
			strings.Contains(strings.ToLower(ct.Email), needle) {
			kept = append(kept, ct)
		}
	}
	return kept
}

func getCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one contact in full",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			ct, err := c.App.Contacts.Get(c.Ctx, id)
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Name", Value: ct.Name},
				{Label: "First Name", Value: ct.FirstName},
				{Label: "Last Name", Value: ct.LastName},
				{Label: "Nickname", Value: ct.Nickname},
			}
			for _, group := range []struct {
				label  string
				values []string
			}{
				{"Email", ct.Emails},
				{"Phone", ct.Phones},
				{"Address", ct.Addresses},
				{"Website", ct.URLs},
			} {
				for _, v := range group.values {
					fields = append(fields, ui.Field{Label: group.label, Value: v})
				}
			}
			fields = append(fields,
				ui.Field{Label: "Organization", Value: ct.Org},
				ui.Field{Label: "Job Title", Value: ct.Title},
				ui.Field{Label: "Role", Value: ct.Role},
				ui.Field{Label: "Birthday", Value: ct.Birthday},
				ui.Field{Label: "Anniversary", Value: ct.Anniversary},
				ui.Field{Label: "Gender", Value: ct.Gender},
				ui.Field{Label: "Language", Value: ct.Language},
				ui.Field{Label: "Time Zone", Value: ct.Timezone},
				ui.Field{Label: "Note", Value: ct.Note},
				kit.SignatureField(string(ct.Signature)),
				ui.Field{Label: "ID", Value: ct.ID, ID: true},
			)
			return kit.Show(c, ui.RecordSpec{Object: ct, Fields: fields})
		}),
	}
}

// details are the fields a contact carries. create and update share them so the
// two commands can never drift apart on what a contact is.
type details struct {
	nc ctsvc.NewContact
}

// A repeatable field may say what kind it is, the way Proton's own editor offers
// one on each: --phone cell:+43… , --email work:jane@example.com. A bare value
// states no kind, which vCard distinguishes from "other".
func (d *details) register(c *cobra.Command, verb string) {
	f := c.Flags()
	f.StringVar(&d.nc.Name, "name", "", verb+" the name shown in listings")
	f.StringVar(&d.nc.FirstName, "first-name", "", verb+" the given name")
	f.StringVar(&d.nc.LastName, "last-name", "", verb+" the family name")
	f.StringVar(&d.nc.Nickname, "nickname", "", verb+" the nickname")
	f.StringArrayVar(&d.nc.Emails, "email", nil,
		verb+" an email address, as ADDRESS or KIND:ADDRESS (repeatable)")
	f.StringArrayVar(&d.nc.Phones, "phone", nil,
		verb+" a phone number, as NUMBER or KIND:NUMBER (repeatable)")
	f.StringArrayVar(&d.nc.Addresses, "address", nil,
		verb+" a postal address, as ADDRESS or KIND:ADDRESS (repeatable)")
	f.StringArrayVar(&d.nc.URLs, "website", nil,
		verb+" a website, as URL or KIND:URL (repeatable)")
	f.StringVar(&d.nc.Org, "organization", "", verb+" the organization")
	f.StringVar(&d.nc.Title, "job-title", "", verb+" the job title")
	f.StringVar(&d.nc.Role, "role", "", verb+" the role played in the organization")
	f.StringVar(&d.nc.Birthday, "birthday", "", verb+" the birthday (e.g. 1990-01-31)")
	f.StringVar(&d.nc.Anniversary, "anniversary", "", verb+" the anniversary (e.g. 2015-06-20)")
	f.StringVar(&d.nc.Gender, "gender", "", verb+" the gender")
	f.StringVar(&d.nc.Language, "language", "", verb+" the preferred language (e.g. de-AT)")
	f.StringVar(&d.nc.Timezone, "timezone", "", verb+" the time zone (e.g. Europe/Vienna)")
	f.StringVar(&d.nc.Note, "note", "", verb+" the note")
}

func createCmd() *cobra.Command {
	var d details
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a contact",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if d.nc.Name == "" && len(d.nc.Emails) == 0 {
				return kit.Fail("A contact needs at least a name or an email address.").
					Hint("--name \"Jane Roe\"", "--email jane@example.com")
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "contacts", Name: d.nc.Name,
			}, func() (string, error) {
				return c.App.Contacts.Create(c.Ctx, d.nc)
			})
		}),
	}
	d.register(c, "Set")
	return c
}

func updateCmd() *cobra.Command {
	var d details
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change a contact's details",
		Long: "Change a contact's details.\n\n" +
			"Only what you pass is replaced. --email and --phone replace the whole list\n" +
			"rather than adding to it, so pass every address you want the contact to keep.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "contacts", Count: 1,
				Name: d.nc.Name, IDs: []string{id},
			}, func() error {
				return c.App.Contacts.Update(c.Ctx, id, d.nc)
			})
		}),
	}
	d.register(c, "Replace")
	return c
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete contacts",
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.Select(c, kit.Selector[ctsvc.Contact]{
				Noun:    "contacts",
				Columns: columns(),
				IDOf:    func(ct ctsvc.Contact) string { return ct.ID },
				ByRef: func(ctx context.Context, ref string) (ctsvc.Contact, error) {
					id, err := c.App.Contacts.Resolve(ctx, ref)
					if err != nil {
						return ctsvc.Contact{}, err
					}
					ct, err := c.App.Contacts.Get(ctx, id)
					if err != nil {
						return ctsvc.Contact{}, err
					}
					return *ct, nil
				},
			})
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "contacts", Count: sel.Len(), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error {
				return c.App.Contacts.Delete(c.Ctx, sel.IDs)
			})
		}),
	}
}

// ── export and import ──

// Export writes contacts as vCards, which is what every other address book
// reads and what Proton's own Contacts widget offers.
//
// A contact is stored as several cards, each a complete vCard carrying a slice
// of the properties; a file has to be one card with all of them, so they are
// merged. That merge is why this is not simply `get --dest`.
func exportCmd() *cobra.Command {
	var dest kit.Destination
	var keyword string
	c := &cobra.Command{
		Use:   "export [REF...]",
		Short: "Write contacts out as vCards",
		Long: "Write contacts out as .vcf files, or as one stream with --dest -.\n\n" +
			"Named contacts are written; with none named, the whole address book is,\n" +
			"narrowed by --keyword. Properties this tool has no flag for travel too,\n" +
			"since the stored card goes out whole.",
		Args: cobra.ArbitraryArgs,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			all, err := c.App.Contacts.List(c.Ctx)
			if err != nil {
				return err
			}
			chosen, err := chooseContacts(c, all, keyword)
			if err != nil {
				return err
			}
			if err := dest.Validate(len(chosen) == 1 || dest.Stdout()); err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "contacts", Count: len(chosen),
				Detail: "to " + dest.Describe(), AnswerFollows: dest.Stdout(),
				Preview: kit.Preview("contacts", columns(), chosen),
			}, func() error {
				// One stream carries every card one after another, which is what a
				// .vcf file is; separate files get one contact each.
				if dest.Stdout() {
					var doc strings.Builder
					for _, ct := range chosen {
						doc.WriteString(vcard.Document(ct.Cards))
						doc.WriteString("\r\n")
					}
					_, err := dest.Write(c, "", []byte(doc.String()))
					return err
				}
				for _, ct := range chosen {
					name := ct.Name
					if name == "" {
						name = ct.ID
					}
					if _, err := dest.Write(c, name+".vcf", []byte(vcard.Document(ct.Cards))); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().StringVar(&keyword, "keyword", "", "Match text in the name or the address")
	dest.Register(c)
	return c
}

// chooseContacts resolves what to export: the references named, or everything
// the keyword matched.
func chooseContacts(c *kit.Invocation, all []ctsvc.Contact, keyword string) ([]ctsvc.Contact, error) {
	if len(c.Args) == 0 {
		return matchContacts(all, keyword), nil
	}
	byID := make(map[string]ctsvc.Contact, len(all))
	for _, ct := range all {
		byID[ct.ID] = ct
	}
	out := make([]ctsvc.Contact, 0, len(c.Args))
	for _, ref := range c.Args {
		id, err := c.App.Contacts.Resolve(c.Ctx, ref)
		if err != nil {
			return nil, err
		}
		ct, ok := byID[id]
		if !ok {
			return nil, kit.Fail("%q resolved to a contact the address book does not hold.", ref)
		}
		out = append(out, ct)
	}
	return out, nil
}

// Import reads vCards in. It is export's inverse and the other half of what
// Proton's own Contacts offers.
func importCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "import PATH",
		Short: "Read contacts in from a .vcf file",
		Long: "Read contacts in from a .vcf file, or from stdin with -.\n\n" +
			"Each card goes in whole, so a property this tool has no flag for survives\n" +
			"the trip. A card with no name and no address is skipped and named, since\n" +
			"there would be nothing to file it under.\n\n" +
			"Nothing is merged: importing the same file twice makes duplicates, because\n" +
			"nothing here can tell a re-import from a file somebody edited.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			text, err := readWholeArg(c, c.Args[0])
			if err != nil {
				return err
			}
			cards := vcard.ParseDocuments(text)
			if len(cards) == 0 {
				return kit.Fail("%s holds no contacts.", c.Args[0])
			}
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "contacts", Count: len(cards),
				Detail: "from " + c.Args[0],
			}, func() ([]ctsvc.SkippedContact, error) {
				res, err := c.App.Contacts.Import(c.Ctx, cards)
				if err != nil {
					return nil, err
				}
				return res.Skipped, nil
			})
		}),
	}
}

// readWholeArg reads a path, or standard input when it is "-".
func readWholeArg(c *kit.Invocation, path string) (string, error) {
	if path == "-" {
		return kit.ReadTextArg(c, "-", "PATH")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", kit.Fail("could not read %s: %v", path, err)
	}
	return string(b), nil
}

// ── merge ──

// Merge folds duplicate contacts together, which is what Proton's own Contacts
// offers and what an address book imported from two places needs.
func mergeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "merge",
		Short: "Fold duplicate contacts into one",
		Long: "Fold duplicate contacts into one.\n\n" +
			"A shared address decides, compared case-insensitively; two entries merely\n" +
			"sharing a name are not duplicates.\n\n" +
			"The oldest of each set is kept, so a group or a pinned key referring to it\n" +
			"still does. Everything the others had is added, and nothing is overwritten.",
		Args: cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			all, err := c.App.Contacts.List(c.Ctx)
			if err != nil {
				return err
			}
			groups := ctsvc.Duplicates(all)
			folded := 0
			for _, g := range groups {
				folded += len(g.Contacts) - 1
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Merged, Kind: "contacts", Count: folded,
				Detail:  duplicateDetail(groups),
				Preview: kit.Preview("duplicates", duplicateColumns(), groups),
			}, func() error {
				for _, g := range groups {
					if _, err := c.App.Contacts.Merge(c.Ctx, g); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

func duplicateDetail(groups []ctsvc.Duplicate) string {
	if len(groups) == 0 {
		return ""
	}
	return "into " + ui.Quantity(len(groups), "contacts")
}

// duplicateColumns previews what a merge would fold, by the address that says
// they are the same person.
func duplicateColumns() []ui.Column[ctsvc.Duplicate] {
	return []ui.Column[ctsvc.Duplicate]{
		{Header: "EMAIL", Flex: true, Cell: func(d ctsvc.Duplicate) string { return d.Email }},
		{Header: "KEEPING", Flex: true, Cell: func(d ctsvc.Duplicate) string {
			return d.Contacts[0].Name
		}},
		{Header: "FOLDING IN", Right: true, Cell: func(d ctsvc.Duplicate) string {
			return strconv.Itoa(len(d.Contacts) - 1)
		}},
	}
}
