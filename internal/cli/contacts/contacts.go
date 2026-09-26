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
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/crypto/pgp"
	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/vcard"
	"github.com/spf13/cobra"
)

// screenful is how many contacts a listing holds when nothing asked for more.
const screenful = 50

func New() *cobra.Command {
	c := &cobra.Command{
		Use:   "contacts",
		Short: "Contacts, their groups, email settings and keys",
	}
	c.AddCommand(listCmd(), getCmd(), createCmd(), updateCmd(), deleteCmd(),
		exportCmd(), importCmd(), mergeCmd(), emailsCmd(), keysCmd(), groupsCmd())
	return c
}

// columns is the contact table. ID leads, as it does in every collection, so the
// thing you paste into the next command is always in the same place.
func columns() []ui.Column[ctsvc.Contact] {
	return []ui.Column[ctsvc.Contact]{
		{Header: "ID", ID: true, Cell: func(c ctsvc.Contact) string { return c.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(c ctsvc.Contact) string { return c.Name }},
		{Header: "EMAIL", Flex: true, Handle: true, Cell: func(c ctsvc.Contact) string { return c.Email }},
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
			return kit.List(c, s, rows)
		}),
	}
	c.Flags().StringVar(&keyword, "keyword", "", "Match text in the name or the address")
	order.Register(c, "name", "email")
	page.Default = screenful
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
				{Label: "Name", Value: ct.Name, Handle: true},
				{Label: "First Name", Value: ct.FirstName},
				{Label: "Last Name", Value: ct.LastName},
			}
			each := func(label string, values []string) {
				for _, v := range values {
					fields = append(fields, ui.Field{Label: label, Value: v})
				}
			}
			each("Nickname", ct.Nicknames)
			each("Email", ct.Emails)
			each("Phone", ct.Phones)
			each("Address", ct.Addresses)
			each("Website", ct.URLs)
			each("Organization", ct.Organizations)
			each("Job Title", ct.JobTitles)
			each("Role", ct.Roles)
			fields = append(fields,
				ui.Field{Label: "Birthday", Value: ct.Birthday},
				ui.Field{Label: "Anniversary", Value: ct.Anniversary},
				ui.Field{Label: "Gender", Value: ct.Gender},
			)
			each("Language", ct.Languages)
			each("Time Zone", ct.Timezones)
			each("Note", ct.Notes)
			fields = append(fields,
				ui.Field{Label: "Photo", Value: ctsvc.DescribePhoto(ct.Photo)},
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
	nc    ctsvc.NewContact
	photo string
	clear map[ctsvc.Detail]*bool
}

// A repeatable field may say what kind it is, the way Proton's own editor offers
// one on each: --phone cell:+43… , --email work:jane@example.com. A bare value
// states no kind, which vCard distinguishes from "other".
func (d *details) register(c *cobra.Command, verb string) {
	f := c.Flags()
	f.StringVar(&d.nc.Name, "name", "", verb+" the name shown in listings")
	f.StringVar(&d.nc.FirstName, string(ctsvc.DetailFirstName), "", verb+" the given name")
	f.StringVar(&d.nc.LastName, string(ctsvc.DetailLastName), "", verb+" the family name")
	f.StringArrayVar(&d.nc.Nicknames, string(ctsvc.DetailNickname), nil, verb+" a nickname (repeatable)")
	f.StringArrayVar(&d.nc.Emails, string(ctsvc.DetailEmail), nil,
		verb+" an email address, as ADDRESS or KIND:ADDRESS (repeatable)")
	f.StringArrayVar(&d.nc.Phones, string(ctsvc.DetailPhone), nil,
		verb+" a phone number, as NUMBER or KIND:NUMBER (repeatable)")
	f.StringArrayVar(&d.nc.Addresses, string(ctsvc.DetailAddress), nil,
		verb+" a postal address, as ADDRESS or KIND:ADDRESS (repeatable)")
	f.StringArrayVar(&d.nc.URLs, string(ctsvc.DetailWebsite), nil,
		verb+" a website, as URL or KIND:URL (repeatable)")
	f.StringArrayVar(&d.nc.Organizations, string(ctsvc.DetailOrganization), nil, verb+" an organization (repeatable)")
	f.StringArrayVar(&d.nc.JobTitles, string(ctsvc.DetailJobTitle), nil, verb+" a job title (repeatable)")
	f.StringArrayVar(&d.nc.Roles, string(ctsvc.DetailRole), nil,
		verb+" a role played in an organization (repeatable)")
	f.StringVar(&d.nc.Birthday, string(ctsvc.DetailBirthday), "", verb+" the birthday (e.g. 1990-01-31)")
	f.StringVar(&d.nc.Anniversary, string(ctsvc.DetailAnniversary), "", verb+" the anniversary (e.g. 2015-06-20)")
	f.StringVar(&d.nc.Gender, string(ctsvc.DetailGender), "", verb+" the gender")
	f.StringArrayVar(&d.nc.Languages, string(ctsvc.DetailLanguage), nil,
		verb+" a preferred language, e.g. de-AT (repeatable)")
	f.StringArrayVar(&d.nc.Timezones, string(ctsvc.DetailTimezone), nil,
		verb+" a time zone, e.g. Europe/Vienna (repeatable)")
	f.StringArrayVar(&d.nc.Notes, string(ctsvc.DetailNote), nil, verb+" a note (repeatable)")
	f.StringVar(&d.photo, string(ctsvc.DetailPhoto), "",
		verb+" the photo: an image file, - for stdin, or a web address")
}

// registerClears gives every detail a --clear-x that takes it away.
func (d *details) registerClears(c *cobra.Command) {
	d.clear = map[ctsvc.Detail]*bool{}
	for _, detail := range []struct {
		detail ctsvc.Detail
		what   string
	}{
		{ctsvc.DetailFirstName, "the given name"},
		{ctsvc.DetailLastName, "the family name"},
		{ctsvc.DetailNickname, "every nickname"},
		{ctsvc.DetailEmail, "every email address"},
		{ctsvc.DetailPhone, "every phone number"},
		{ctsvc.DetailAddress, "every postal address"},
		{ctsvc.DetailWebsite, "every website"},
		{ctsvc.DetailOrganization, "every organization"},
		{ctsvc.DetailJobTitle, "every job title"},
		{ctsvc.DetailRole, "every role"},
		{ctsvc.DetailBirthday, "the birthday"},
		{ctsvc.DetailAnniversary, "the anniversary"},
		{ctsvc.DetailGender, "the gender"},
		{ctsvc.DetailLanguage, "every preferred language"},
		{ctsvc.DetailTimezone, "every time zone"},
		{ctsvc.DetailNote, "every note"},
		{ctsvc.DetailPhoto, "the photo"},
	} {
		on := new(bool)
		d.clear[detail.detail] = on
		name := "clear-" + string(detail.detail)
		c.Flags().BoolVar(on, name, false, "Remove "+detail.what)
		kit.Exclusive(c, string(detail.detail), name)
	}
}

// contact is what the flags describe, with the photo read and fitted. It is
// judged before anything is sent: a file that is not an image is wrong whoever
// is signed in.
func (d *details) contact(c *kit.Invocation) (ctsvc.NewContact, error) {
	nc := d.nc
	for detail, on := range d.clear {
		if *on {
			if nc.Clear == nil {
				nc.Clear = map[ctsvc.Detail]bool{}
			}
			nc.Clear[detail] = true
		}
	}
	if d.photo == "" {
		return nc, nil
	}
	if ctsvc.IsPhotoURL(d.photo) {
		uri, err := ctsvc.PhotoFromURL(d.photo)
		nc.Photo = uri
		return nc, err
	}
	data, err := readFileArg(c, "--photo", d.photo)
	if err != nil {
		return nc, err
	}
	name := d.photo
	if name == "-" {
		name = "Standard input"
	}
	nc.Photo, err = ctsvc.PhotoFromImage(name, data)
	return nc, err
}

func createCmd() *cobra.Command {
	var d details
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a contact",
		Long: "Create a contact.\n\n" +
			"A photo from a file is shrunk until its shorter side is at most 180 pixels and\n" +
			"stored as JPEG; JPEG, PNG, GIF and WebP are read. A web address is stored as it\n" +
			"is given.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if d.nc.Name == "" && len(d.nc.Emails) == 0 {
				return kit.Fail("A contact needs at least a name or an email address.").
					Hint("--name \"Jane Roe\"", "--email jane@example.com")
			}
			nc, err := d.contact(c)
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "contacts", Name: nc.Name,
			}, func() (string, error) {
				return c.App.Contacts.Create(c.Ctx, nc)
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
			"Only what you pass is replaced. A repeatable flag replaces the whole list\n" +
			"rather than adding to it, so pass every value you want the contact to keep.\n" +
			"--clear-note removes every note, --clear-photo the photo, and so on for each\n" +
			"detail.\n\n" +
			"A photo from a file is shrunk until its shorter side is at most 180 pixels and\n" +
			"stored as JPEG; JPEG, PNG, GIF and WebP are read. A web address is stored as it\n" +
			"is given.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			nc, err := d.contact(c)
			if err != nil {
				return err
			}
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			var rewrote rewritten
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "contacts", Count: 1,
				Name: nc.Name, IDs: []string{id},
			}, func() error {
				verdict, err := c.App.Contacts.Update(c.Ctx, id, nc)
				rewrote.card(verdict)
				return err
			}); err != nil {
				return err
			}
			rewrote.report(c)
			return nil
		}),
	}
	d.register(c, "Replace")
	d.registerClears(c)
	return c
}

// rewritten is what a write learned about the signed cards it re-signed.
//
// A card that did not verify is far more often one signed by a key this account
// has since retired than one somebody altered - the two cannot be told apart -
// and Proton's own client saves over either. So the write goes ahead, and this
// is what makes going ahead honest: the person is told that what the card held
// was not checked before their key went on it. It is filled in from inside the
// change and said after it, so a dry run, which rewrites nothing, says nothing.
type rewritten struct{ unverified int }

func (r *rewritten) card(verdict pgp.VerifyResult) {
	if verdict == pgp.Unverified || verdict == pgp.Invalid {
		r.unverified++
	}
}

func (r *rewritten) report(c *kit.Invocation) {
	switch r.unverified {
	case 0:
	case 1:
		c.Warn("The card this rewrote arrived unverified, so what it held was not checked before your key signed it.")
	default:
		c.Warn("%d of the cards this rewrote arrived unverified, so what they held was not checked before your key signed them.", r.unverified)
	}
}

func deleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete contacts",
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
			"Naming contacts writes those; naming none writes the whole address book,\n" +
			"narrowed by --keyword.\n\n" +
			"The stored card goes out whole, so properties this tool has no flag for are\n" +
			"exported too. Each address's groups go out as CATEGORIES beside it, which is\n" +
			"what `import` reads them back from.",
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
				// Membership is read from the labels, where it lives, rather than
				// from the copy in the stored card, which no client keeps current.
				groups, err := c.App.Contacts.Membership(c.Ctx)
				if err != nil {
					return err
				}
				// One stream carries every card one after another, which is what a
				// .vcf file is; separate files get one contact each.
				if dest.Stdout() {
					var doc strings.Builder
					for _, ct := range chosen {
						doc.WriteString(vcard.Document(ct.Cards, groups[ct.ID]))
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
					if _, err := dest.Write(c, name+".vcf", []byte(vcard.Document(ct.Cards, groups[ct.ID]))); err != nil {
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
	var noGroups bool
	c := &cobra.Command{
		Use:   "import PATH",
		Short: "Read contacts in from a .vcf file",
		Long: "Read contacts in from a .vcf file, or from stdin with -.\n\n" +
			"Each card goes in whole, so a property this tool has no flag for survives\n" +
			"the trip. A card with no name and no address is skipped and reported: there\n" +
			"would be nothing to file it under.\n\n" +
			"The groups a card names in CATEGORIES are applied: an address goes into a\n" +
			"group of that name, and a group you do not have is created. --no-groups\n" +
			"leaves them out, and --dry-run shows what the file would put where.\n\n" +
			"A card that carries a UID replaces the contact with that UID, which is what\n" +
			"makes a file from `export` a backup. A card without one is a new contact, so\n" +
			"importing such a file twice creates duplicates; use `merge` afterwards to\n" +
			"fold them together.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			text, err := readWholeArg(c, c.Args[0])
			if err != nil {
				return err
			}
			cards := vcard.ParseDocuments(text)
			if len(cards) == 0 {
				return kit.Fail("%s holds no contacts.", c.Args[0])
			}
			var res *ctsvc.ImportResult
			if err := kit.Attempt(c, ui.ResultSpec{
				Action: ui.Imported, Kind: "contacts", Count: len(cards),
				Detail:  "from " + c.Args[0],
				Preview: kit.Preview("contacts", offeredColumns(), offered(cards, !noGroups)),
			}, func() ([]ctsvc.SkippedContact, error) {
				res, err = c.App.Contacts.Import(c.Ctx, cards, ctsvc.ImportOptions{
					Groups: !noGroups, GroupColor: accent.Default,
				})
				if err != nil {
					return nil, err
				}
				return res.Skipped, nil
			}); err != nil || res == nil {
				return err
			}
			if res.Grouped > 0 {
				c.Note("%s", groupedNote(res))
			}
			for _, f := range res.GroupsFailed {
				c.Warn("%v", f)
			}
			return nil
		}),
	}
	c.Flags().BoolVar(&noGroups, "no-groups", false, "Leave the groups the file names out")
	return c
}

// offeredContact is one card as the file offers it, for the preview that shows
// what an import would take and where it would put it.
type offeredContact struct {
	Name      string
	Addresses []string
	Groups    []string
}

func offered(cards []string, withGroups bool) []offeredContact {
	out := make([]offeredContact, 0, len(cards))
	for _, raw := range cards {
		card, _ := vcard.EnsureIdentity(raw)
		o := offeredContact{Name: vcard.Field(card, "FN"), Addresses: vcard.Values(card, "EMAIL")}
		if withGroups {
			seen := map[string]bool{}
			for _, cat := range vcard.Categories(card) {
				if !seen[cat.Name] {
					seen[cat.Name] = true
					o.Groups = append(o.Groups, cat.Name)
				}
			}
			sort.Strings(o.Groups)
		}
		out = append(out, o)
	}
	return out
}

func offeredColumns() []ui.Column[offeredContact] {
	return []ui.Column[offeredContact]{
		{Header: "NAME", Flex: true, Cell: func(o offeredContact) string { return o.Name }},
		{Header: "ADDRESSES", Flex: true, Cell: func(o offeredContact) string { return strings.Join(o.Addresses, ", ") }},
		{Header: "GROUPS", Flex: true, Cell: func(o offeredContact) string { return strings.Join(o.Groups, ", ") }},
	}
}

// groupedNote says where an import put the addresses, naming what it created
// because a group that did not exist a moment ago is the one thing about the
// result worth a second look.
func groupedNote(res *ctsvc.ImportResult) string {
	note := "Put " + ui.Quantity(res.Grouped, "addresses") + " in " + ui.Quantity(len(res.GroupsUsed), "groups")
	if len(res.GroupsCreated) > 0 {
		quoted := make([]string, len(res.GroupsCreated))
		for i, name := range res.GroupsCreated {
			quoted[i] = strconv.Quote(name)
		}
		note += "; created " + strings.Join(quoted, ", ")
	}
	return note + "."
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
			"Contacts are duplicates when they share an email address, compared without\n" +
			"regard to case. Sharing only a name is not enough.\n\n" +
			"The oldest contact of each set is kept, so groups and pinned keys that refer\n" +
			"to it keep working. Fields from the others are added; nothing is\n" +
			"overwritten.",
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
			var rewrote rewritten
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Merged, Kind: "contacts", Count: folded,
				Detail:  duplicateDetail(groups),
				Preview: kit.Preview("duplicates", duplicateColumns(), groups),
			}, func() error {
				for _, g := range groups {
					if _, err := c.App.Contacts.Merge(c.Ctx, g); err != nil {
						return err
					}
					rewrote.card(g.Contacts[0].Signature)
				}
				return nil
			}); err != nil {
				return err
			}
			rewrote.report(c)
			return nil
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
