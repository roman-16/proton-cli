package pass

import (
	stdctx "context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/credcheck"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/otp"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// itemTypes are the kinds of item Pass stores, spelled the way a CLI spells
// things. Proton's own names are camelCase (creditCard, sshKey); kebab-case is the
// convention everywhere else here, so these are the CLI's spelling of the same set.
var itemTypes = []string{"login", "note", "credit-card", "wifi", "ssh-key", "identity", "alias", "custom"}

func itemsCmd() *cobra.Command {
	c := &cobra.Command{Use: "items", Short: "Logins, notes, cards and the rest"}
	c.AddCommand(attachmentsCmd(), passkeysCmd(), itemsListCmd(), itemsGetCmd(), itemsCreateCmd(), itemsUpdateCmd(),
		itemsMoveCmd(), itemsRevisionsCmd(), itemsTOTPCmd(), itemsShareCmd(),
		itemsPinCmd("pin", "Keep items at the top of the list", ui.Pinned, true),
		itemsPinCmd("unpin", "Stop keeping items at the top", ui.Unpinned, false),
		itemsMonitorCmd("exclude", "Leave logins out of Pass Monitor's checks", excludeLong,
			ui.Excluded, "from Pass Monitor", true),
		itemsMonitorCmd("include", "Bring logins back into Pass Monitor's checks", "",
			ui.Included, "in Pass Monitor", false),
		itemsTrashCmd(), itemsDeleteCmd())
	return c
}

// itemsMoveCmd puts an item in another vault.
//
// An item is sealed under the key of the share it lives in, so moving it means
// sealing it again under the destination's. It keeps its history and gets a new
// ID, because an item in Pass is only unique together with its vault - so the
// new one is printed the way a creation's is.
func itemsMoveCmd() *cobra.Command {
	var into string
	c := &cobra.Command{
		Use:   "move REF",
		Short: "Put an item in another vault",
		Long: "Put an item in another vault.\n\n" +
			"The item keeps its history and everything it holds, but gets a new ID: an\n" +
			"item in Pass is identified by its vault as well as itself. The new ID is\n" +
			"printed on stdout.",
		RunE: kit.Run([]kit.Step{kit.StepExpand, func(*kit.Invocation) error {
			if into == "" {
				return kit.Fail("Which vault should it go into?").Hint("--into Work")
			}
			return nil
		}}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			vault, err := vaultList(c).Find(c.Ctx, into)
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Moved, Kind: "items", Detail: "into " + vault.Name,
			}, func() (string, error) {
				moved, err := c.App.Pass.ItemMove(c.Ctx, shareID, itemID, vault.ShareID)
				if err != nil {
					return "", err
				}
				return kit.JoinPair(vault.ShareID, moved), nil
			})
		}),
	}
	c.Flags().StringVar(&into, "into", "", "Which vault to put it in, by name or ID")
	kit.Completes(c, "into", nil, "pass vaults")
	return c
}

func itemColumns() []ui.Column[passsvc.Item] {
	return []ui.Column[passsvc.Item]{
		{Header: "ID", ID: true, Cell: itemRef},
		{Header: "TYPE", Cell: func(it passsvc.Item) string { return it.Type }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(it passsvc.Item) string { return it.Name }},
		{Header: "USERNAME", Flex: true, Cell: func(it passsvc.Item) string {
			if it.Username != "" {
				return it.Username
			}
			return it.Email
		}},
		{Header: "MODIFIED", Cell: func(it passsvc.Item) string { return units.Time(it.ModifyTime) }},
	}
}

// riskColumn says what a check found, and which logins share one password.
//
// The group number is what makes a reuse listing readable: without it two pairs
// sharing two different passwords read as one group of four.
func riskColumn() ui.Column[passsvc.Item] {
	return ui.Column[passsvc.Item]{Header: "RISK", Cell: func(it passsvc.Item) string {
		if it.ReuseGroup > 0 {
			return fmt.Sprintf("%s #%d", it.Risk, it.ReuseGroup)
		}
		return string(it.Risk)
	}}
}

// itemOrder is how a Pass listing may be ordered. Every vault's items arrive as
// one batch and are decrypted here, so the whole set is in hand.
func itemOrder() kit.Comparators[passsvc.Item] {
	return kit.Comparators[passsvc.Item]{
		"name":     func(a, b passsvc.Item) int { return kit.Fold(a.Name, b.Name) },
		"type":     func(a, b passsvc.Item) int { return kit.Fold(a.Type, b.Type) },
		"modified": func(a, b passsvc.Item) int { return kit.Ints(a.ModifyTime, b.ModifyTime) },
		"created":  func(a, b passsvc.Item) int { return kit.Ints(a.CreateTime, b.CreateTime) },
		"used":     func(a, b passsvc.Item) int { return kit.Ints(a.LastUseTime, b.LastUseTime) },
	}
}

// screenful is how many items a listing holds when nothing asked for more.
const screenful = 50

func itemsListCmd() *cobra.Command {
	var f filters
	var page kit.Page
	var order kit.Order
	var excluded bool
	risk := &kit.Enum{
		Name: "risk", Usage: "Keep only the logins failing this password check",
		Values: passsvc.Risks(),
	}
	c := &cobra.Command{
		Use:   "list",
		Short: "List items across your vaults",
		Long: "List items across your vaults.\n\n" +
			"Takes the same filters as trash and delete, so you can preview a selection\n" +
			"here before acting on it. A hidden vault is left out unless --vault names it.\n\n" +
			"--sort used orders by when a Pass app last filled an item in. Reading one\n" +
			"with this program does not count as using it.\n\n" +
			"--risk is Pass Monitor's password health, and keeps only the logins that\n" +
			"fail one check. A RISK column says what was found, numbering the logins that\n" +
			"share one password so two pairs do not read as one group of four. A login\n" +
			"excluded with `items exclude` is left out of all of them.\n\n" +
			"--risk weak is this program's own reading: a password shorter than twelve\n" +
			"characters, or shorter than sixteen and drawn from fewer than three of\n" +
			"lowercase, uppercase, digits and symbols. Pass judges strength its own way,\n" +
			"so the two can disagree.\n\n" +
			"--risk missing-2fa names the logins for sites that offer a time-based code\n" +
			"and have neither a code nor a passkey stored against them.\n\n" +
			"--risk compromised asks a public corpus of leaked passwords whether yours\n" +
			"are in it, one request per password you have stored. It is the only check\n" +
			"that reaches the network, and it sends the first six hexadecimal characters\n" +
			"of each password's SHA-1 - one bucket in sixteen million - to\n" +
			credcheck.Host + ", never the password and never the whole hash.\n\n" +
			"--excluded keeps only the logins excluded from Pass Monitor, which no check\n" +
			"looks at. `items include` brings one back.\n\n" +
			"No check prints a password; `items get` is still the only command that does.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			items, err := matchMonitorItems(c, &f, risk, excluded)
			if err != nil {
				return err
			}
			if err := kit.Sort(order, items, itemOrder()); err != nil {
				return err
			}
			columns := itemColumns()
			if risk.Set() {
				columns = append(columns, riskColumn())
			}
			rows, total := kit.Slice(page, items)
			return kit.List(c, ui.TableSpec[passsvc.Item]{
				Noun: "items", Columns: columns,
				Total: total, Page: page.Number, PageSize: page.Size,
				Filtered: f.narrowed() || risk.Set() || excluded,
			}, rows)
		}),
	}
	f.registerNarrowing(c)
	risk.Register(c)
	c.Flags().BoolVar(&excluded, "excluded", false, "Keep only the logins excluded from Pass Monitor")
	kit.Exclusive(c, "excluded", "risk")
	order.Register(c, "name", "type", "modified", "created", "used")
	page.Default = screenful
	page.Register(c, "items")
	return c
}

// matchMonitorItems is the listing through one of Pass Monitor's lists - a
// check, or the logins excluded from every check - or the ordinary listing when
// neither was asked for.
//
// Pass Monitor reads the logins and nothing else, so --type is the one filter it
// has no room for: asking for weak notes is asking for nothing.
func matchMonitorItems(c *kit.Invocation, f *filters, risk *kit.Enum, excluded bool) ([]passsvc.Item, error) {
	chosen, err := risk.Value()
	if err != nil {
		return nil, err
	}
	if chosen == "" && !excluded {
		return matchItems(c.Ctx, c, f)
	}
	asked := "--risk"
	if excluded {
		asked = "--excluded"
	}
	if kind, _ := f.itemType.Value(); kind != "" && kind != "login" {
		return nil, kit.Fail("%s looks at logins, so --type %s selects nothing.", asked, kind)
	}
	if excluded {
		items, err := matchItems(c.Ctx, c, f)
		if err != nil {
			return nil, err
		}
		out := items[:0]
		for _, it := range items {
			if it.Type == "login" && it.Excluded {
				out = append(out, it)
			}
		}
		return out, nil
	}
	vaultRef, err := kit.Expand(c.App, f.vault)
	if err != nil {
		return nil, err
	}
	return c.App.Pass.AtRisk(c.Ctx, vaultRef, passsvc.Risk(chosen),
		&http.Client{Timeout: corpusTimeout})
}

// corpusTimeout bounds one bucket fetch. The corpus is somebody else's host, and
// a listing that hangs on it is worse than one that says it could not be read.
const corpusTimeout = 30 * time.Second

func itemsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one item, decrypted",
		Long: "Show one item, decrypted.\n\n" +
			"Passwords, TOTP secrets and private keys are printed in full. This is the\n" +
			"only command that prints them; the listings do not.\n\n" +
			"Last Used is when a Pass app last filled the item in, and shows for logins,\n" +
			"cards and identities. Reading an item with this program does not count.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			fields := append(itemFields(it), historyFields(it.Item)...)
			return kit.Show(c, ui.RecordSpec{Object: it, Fields: append(fields, itemIDField(it.Item))})
		}),
	}
}

// autofilled are the kinds of item a Pass app fills in, which are the only ones
// that have a last use to show.
var autofilled = map[string]bool{"credit-card": true, "identity": true, "login": true}

// historyFields are what an item is now rather than what it holds: when it was
// made, changed and last used, and whether Pass Monitor looks at it.
func historyFields(it passsvc.Item) []ui.Field {
	used := ""
	switch {
	case !autofilled[it.Type]:
	case it.LastUseTime == 0:
		used = "never"
	default:
		used = units.Time(it.LastUseTime)
	}
	monitor := ""
	if it.Excluded {
		monitor = "excluded"
	}
	return []ui.Field{
		{Label: "Created", Value: units.Time(it.CreateTime)},
		{Label: "Modified", Value: units.Time(it.ModifyTime)},
		{Label: "Last Used", Value: used},
		{Label: "Monitor", Value: monitor},
	}
}

func itemIDField(it passsvc.Item) ui.Field {
	return ui.Field{Label: "ID", Value: itemRef(it), ID: true}
}

// itemFields is what one item holds. Every kind shares it: an empty field is
// dropped, so a note shows a note's fields and a card shows a card's without
// either needing a layout of its own.
func itemFields(it *passsvc.FullItem) []ui.Field {
	fields := []ui.Field{
		{Label: "Type", Value: it.Type},
		{Label: "Name", Value: it.Name, Handle: true},
		{Label: "Username", Value: it.Username},
		{Label: "Email", Value: it.Email},
		{Label: "Alias", Value: it.Alias, Handle: true},
		{Label: "Status", Value: it.AliasStatus},
	}
	for _, m := range it.AliasMailboxes {
		fields = append(fields, ui.Field{Label: "Forwards To", Value: m})
	}
	fields = append(fields,
		ui.Field{Label: "Display Name", Value: it.AliasDisplayName},
		ui.Field{Label: "SimpleLogin Note", Value: it.AliasNote},
		ui.Field{Label: "Activity", Value: activity(it.AliasActivity)},
		ui.Field{Label: "Password", Value: it.Password},
		ui.Field{Label: "TOTP", Value: it.TOTP},
	)
	for _, u := range it.URLs {
		fields = append(fields, ui.Field{Label: "URL", Value: u})
	}
	fields = append(fields, passkeyFields(it.Passkeys)...)
	fields = append(fields,
		ui.Field{Label: "Cardholder", Value: it.Holder},
		ui.Field{Label: "Number", Value: it.Number},
		ui.Field{Label: "Expiry", Value: it.Expiry},
		ui.Field{Label: "CVV", Value: it.CVV},
		ui.Field{Label: "PIN", Value: it.PIN},
		ui.Field{Label: "SSID", Value: it.SSID},
		ui.Field{Label: "Public Key", Value: it.PublicKey},
		ui.Field{Label: "Private Key", Value: it.PrivateKey},
	)
	// An identity's fields are declared once in the service, so the record shows
	// whatever the declaration holds rather than a second list that can drift.
	for _, f := range passsvc.IdentityFields {
		fields = append(fields, ui.Field{Label: f.Label, Value: it.Identity[f.Flag]})
	}
	fields = append(fields, ui.Field{Label: "Note", Value: it.Note})
	// A field is labelled the way --field accepts it, so what a record shows can
	// be handed straight back to a command.
	for _, f := range it.Fields {
		fields = append(fields, ui.Field{Label: f.Ref(), Value: f.Value})
	}
	return append(fields, attachmentFields(it.Attachments)...)
}

// activity is what an alias has carried lately, over the fourteen days Proton
// counts. An alias nobody has written to yet reports zeros, which is an answer;
// anything that is not an alias reports nothing.
func activity(a *passsvc.AliasActivity) string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("%d forwarded, %d replied, %d blocked (last 14 days)",
		a.Forwarded, a.Replied, a.Blocked)
}

// sharedWithEveryItem names the identity fields that already have a flag, because
// the idea is not particular to an identity.
var sharedWithEveryItem = map[string]bool{"email": true}

// checkFields refuses a field nothing can read, and a section on an item that
// has no place for one. Each refusal points at its own fix.
//
// An empty type is one not yet known, which is what an edit has until the item
// is fetched: the grammar can still be judged, the placement cannot.
func (d *fields) checkFields(itemType string) error {
	// A value no code can come out of is refused here rather than stored and found
	// useless later, on the day somebody needs the code.
	if d.nc.TOTP != "" {
		if _, err := otp.Parse(d.nc.TOTP); err != nil {
			return kit.Fail("--secret-file totp-uri: %v.", err).
				Hint("an otpauth:// URI, or the secret on its own")
		}
	}
	if err := passsvc.CheckFields(d.nc.ExtraFields); err != nil {
		return kit.Fail("%s.", capitalise(err.Error())).
			Hint("--field NAME=VALUE", "--field SECTION/NAME=VALUE")
	}
	if itemType == "" {
		return nil
	}
	if err := passsvc.CheckSections(itemType, d.nc.ExtraFields); err != nil {
		return kit.Fail("%s.", capitalise(err.Error())).
			Hint("drop the section, or --type custom")
	}
	return nil
}

// capitalise makes a service's error read as the sentence a refusal is.
func capitalise(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// identityValues is what the identity flags were given, keyed by flag. The
// shared ones are folded back in, so an identity built from --email carries it.
func (d *fields) identityValues() map[string]string {
	out := make(map[string]string, len(d.identity)+len(sharedWithEveryItem))
	for flag, v := range d.identity {
		if *v != "" {
			out[flag] = *v
		}
	}
	if d.nc.Email != "" {
		out["email"] = d.nc.Email
	}
	return out
}

// fields are everything an item can carry. create and update share them so the two
// commands cannot drift on what an item is.
type fields struct {
	nc passsvc.NewItem
	// identity holds one string per declared identity field. The set is the
	// service's to declare, so this is a map rather than thirty named members
	// that would have to be kept in step with it.
	identity map[string]*string
	// secrets are the parts of an item that argv may not carry, and generate is
	// the way to have one that never existed anywhere else.
	secrets  kit.Secrets
	generate bool
	gen      generator
	// attach are local files to put on the item, and detach names the ones to
	// take off it, which only an edit can do.
	attach []string
	detach []string
}

// readAttachments turns what --attach named into files ready to send, and
// refuses one there is nothing to read, before anything reaches Proton.
func (d *fields) readAttachments() ([]passsvc.Upload, error) { return uploads(d.attach) }

// allowed refuses the files this plan will not take: an account without file
// storage, a file over the size one may be, more than the storage left.
func (d *fields) allowed(c *kit.Invocation, ups []passsvc.Upload) error {
	if len(ups) == 0 {
		return nil
	}
	limits, err := c.App.Pass.StorageLimits(c.Ctx)
	if err != nil {
		return err
	}
	return limits.CheckUpload(ups)
}

// secretFields are the fields --secret-file names by their own flag's name. A
// name that is not one of these names a custom field instead.
var secretFields = map[string]func(nc *passsvc.NewItem, v string){
	"cvv":         func(nc *passsvc.NewItem, v string) { nc.CVV = v },
	"number":      func(nc *passsvc.NewItem, v string) { nc.Number = v },
	"password":    func(nc *passsvc.NewItem, v string) { nc.Password = v },
	"pin":         func(nc *passsvc.NewItem, v string) { nc.PIN = v },
	"private-key": func(nc *passsvc.NewItem, v string) { nc.PrivateKey = v },
	"totp-uri":    func(nc *passsvc.NewItem, v string) { nc.TOTP = v },
}

// secretFieldNames are those names, for the help that lists them.
func secretFieldNames() string {
	names := make([]string, 0, len(secretFields))
	for name := range secretFields {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// readSecrets folds what --secret-file carried into the item.
//
// A name the item type knows is that field; anything else is a custom field,
// stored hidden - or, when the value is a TOTP URI, as the two-factor field that
// is what such a value is for.
func (d *fields) readSecrets(c *kit.Invocation) error {
	values, err := d.secrets.Values()
	if err != nil {
		return err
	}
	if d.generate {
		password, err := d.gen.make()
		if err != nil {
			return err
		}
		if _, taken := values["password"]; taken {
			return kit.Fail("--generate-password and --secret-file password both set the password.").
				Hint("drop one of them")
		}
		values["password"] = password
		// The item is the answer this command prints, so the password it made goes
		// beside it rather than into stdout, which carries the new ID.
		c.Note("Password  %s", password)
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := values[name]
		if set, ok := secretFields[name]; ok {
			set(&d.nc, value)
			continue
		}
		field := name + "=" + value
		if _, err := otp.Parse(value); err == nil && strings.HasPrefix(value, "otpauth://") {
			d.nc.ExtraFields.TOTP = append(d.nc.ExtraFields.TOTP, field)
			continue
		}
		d.nc.ExtraFields.Hidden = append(d.nc.ExtraFields.Hidden, field)
	}
	return nil
}

func (d *fields) register(c *cobra.Command, verb string) {
	f := c.Flags()
	f.StringArrayVar(&d.attach, "attach", nil, "Attach a local file (repeatable)")
	f.StringVar(&d.nc.Name, "name", "", verb+" the item's name")
	f.StringVar(&d.nc.Username, "username", "", verb+" the username (login)")
	f.StringVar(&d.nc.Email, "email", "", verb+" the email address (login)")
	f.StringVar(&d.nc.URL, "url", "", verb+" the URL (login)")
	f.StringVar(&d.nc.Note, "note", "", verb+" the note")
	// A field states its own section, so the flag stays one self-contained token
	// that can be given in any order and read back exactly as it was written.
	f.StringArrayVar(&d.nc.ExtraFields.Text, "field", nil,
		verb+" a custom field, as NAME=VALUE or SECTION/NAME=VALUE (repeatable)")
	f.StringVar(&d.nc.Holder, "holder", "", verb+" the cardholder's name (credit-card)")
	f.StringVar(&d.nc.Expiry, "expiry", "", verb+" the card expiry, YYYY-MM (credit-card)")
	f.StringVar(&d.nc.SSID, "ssid", "", verb+" the network name (wifi)")
	f.StringVar(&d.nc.PublicKey, "public-key", "", verb+" the public key (ssh-key)")
	d.secrets.Declare(c)
	f.BoolVar(&d.generate, "generate-password", false, "Make the password rather than being given one")
	d.gen.register(c)
	// An identity's fields are declared once in the service; the flags are made
	// from that declaration so the two can never offer different sets.
	d.identity = make(map[string]*string, len(passsvc.IdentityFields))
	for _, id := range passsvc.IdentityFields {
		// An address is an address whichever kind of item holds one, so an
		// identity's email is the same --email a login takes rather than a second
		// flag meaning the same thing.
		if sharedWithEveryItem[id.Flag] {
			continue
		}
		v := new(string)
		d.identity[id.Flag] = v
		f.StringVar(v, id.Flag, "", verb+" the "+strings.ToLower(id.Label)+" (identity)")
	}
}

// aliasFields are what an alias carries beside the item's own, which only an
// edit takes: an alias is born from `aliases create`, since Proton and not this
// CLI decides what its address is.
type aliasFields struct {
	mailboxes                              []string
	displayName, simpleLoginNote           string
	clearDisplayName, clearSimpleLoginNote bool
}

func (a *aliasFields) register(c *cobra.Command) {
	f := c.Flags()
	f.StringArrayVar(&a.mailboxes, "mailbox", nil,
		"Replace where mail to it arrives (alias, repeatable)")
	f.StringVar(&a.displayName, "display-name", "",
		"Replace the name recipients see on mail from it (alias)")
	f.BoolVar(&a.clearDisplayName, "clear-display-name", false, "Remove the display name (alias)")
	f.StringVar(&a.simpleLoginNote, "simplelogin-note", "",
		"Replace the note SimpleLogin keeps beside the address (alias)")
	f.BoolVar(&a.clearSimpleLoginNote, "clear-simplelogin-note", false, "Remove the SimpleLogin note (alias)")
	kit.Exclusive(c, "display-name", "clear-display-name")
	kit.Exclusive(c, "simplelogin-note", "clear-simplelogin-note")
}

// check refuses an empty value where the flag beside it is the way to take one
// off, which is judged from the command line alone.
func (a *aliasFields) check(c *kit.Invocation) error {
	if c.Changed("display-name") && strings.TrimSpace(a.displayName) == "" {
		return kit.Fail("--display-name needs a name.").Hint("--clear-display-name takes it off")
	}
	if c.Changed("simplelogin-note") && strings.TrimSpace(a.simpleLoginNote) == "" {
		return kit.Fail("--simplelogin-note needs some text.").Hint("--clear-simplelogin-note takes it off")
	}
	return nil
}

func (a *aliasFields) patch() passsvc.AliasPatch {
	p := passsvc.AliasPatch{Mailboxes: a.mailboxes}
	switch {
	case a.displayName != "":
		p.DisplayName = &a.displayName
	case a.clearDisplayName:
		p.DisplayName = new(string)
	}
	switch {
	case a.simpleLoginNote != "":
		p.SimpleLoginNote = &a.simpleLoginNote
	case a.clearSimpleLoginNote:
		p.SimpleLoginNote = new(string)
	}
	return p
}

// noteChangeable refuses a SimpleLogin note on anything but an alias that
// already carries one, which is the only note of that kind Pass lets you
// change. The note is not end-to-end encrypted, so the item's own note is where
// a new one goes.
func noteChangeable(c *kit.Invocation, shareID, itemID string) error {
	it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
	if err != nil {
		return err
	}
	if it.Type != "alias" {
		return errs.Naming(it.Name, kit.Fail("%s is a %s, not an alias.", it.Name, it.Type))
	}
	if it.AliasNote == "" {
		return errs.Naming(it.Name, kit.Fail("%s has no SimpleLogin note to change.", it.Name).
			Hint("--note, which Pass encrypts end to end"))
	}
	return nil
}

func itemsCreateCmd() *cobra.Command {
	var d fields
	var vault string
	itemType := &kit.Enum{
		Name: "type", Usage: "What kind of item", Default: "login", Values: itemTypes,
	}
	security := &kit.Enum{
		Name: "security", Usage: "Wi-Fi security (wifi)",
		Values: []string{"WPA", "WPA2", "WPA3", "WEP"},
	}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create an item",
		Long: "Create an item.\n\n" +
			"A secret is read from a file, never from a flag value: --secret-file NAME=FILE,\n" +
			"and - as the file reads standard input.\n" +
			"NAME is " + secretFieldNames() + ",\n" +
			"or any name at all, which makes a hidden custom field of it.\n\n" +
			"--generate-password makes one instead, so a new login needs no file: it is\n" +
			"shaped by the same flags `pass generate` takes.\n\n" +
			"--attach puts a local file on the item, and needs a paid Pass plan.",
		// What a field says, whether this kind of item has a section to put it
		// under, and whether a file named by --attach is there to be read are all
		// answerable from the command line alone.
		RunE: kit.Run([]kit.Step{d.secrets.Supply, func(*kit.Invocation) error {
			kind, err := itemType.Value()
			if err != nil {
				return err
			}
			if _, err := d.readAttachments(); err != nil {
				return err
			}
			return d.checkFields(kind)
		}}, func(c *kit.Invocation) error {
			kind, err := itemType.Value()
			if err != nil {
				return err
			}
			wifi, err := security.Value()
			if err != nil {
				return err
			}
			if d.nc.Name == "" {
				return kit.Fail("An item needs a name.").Hint("--name GitHub")
			}
			if err := d.readSecrets(c); err != nil {
				return err
			}
			if err := d.checkFields(kind); err != nil {
				return err
			}
			d.nc.Type, d.nc.WifiSecurity = kind, wifi
			d.nc.Identity = d.identityValues()
			files, err := d.readAttachments()
			if err != nil {
				return err
			}
			if err := d.allowed(c, files); err != nil {
				return err
			}
			d.nc.Attach = reporting(c, files)
			shareID, err := resolveVault(c, vault)
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "items", Name: d.nc.Name,
				Detail: carrying(len(files), 0),
				Extra:  map[string]any{"type": kind},
			}, func() (string, error) {
				itemID, err := c.App.Pass.ItemCreate(c.Ctx, shareID, d.nc)
				if err != nil {
					return "", err
				}
				return kit.JoinPair(shareID, itemID), nil
			})
		}),
	}
	d.register(c, "Set")
	itemType.Register(c)
	security.Register(c)
	c.Flags().StringVar(&vault, "vault", "", "Which vault, by name or ID (default: your first)")
	return c
}

func itemsUpdateCmd() *cobra.Command {
	var d fields
	var a aliasFields
	security := &kit.Enum{
		Name: "security", Usage: "Wi-Fi security (wifi)",
		Values: []string{"WPA", "WPA2", "WPA3", "WEP"},
	}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change an item's fields",
		Long: "Change an item's fields.\n\n" +
			"A secret is read from a file, never from a flag value: --secret-file NAME=FILE,\n" +
			"and - as the file reads standard input.\n" +
			"NAME is " + secretFieldNames() + ",\n" +
			"or any name at all, which makes a hidden custom field of it.\n\n" +
			"--generate-password replaces the password with one it makes.\n\n" +
			"--attach puts a local file on the item, and needs a paid Pass plan.\n" +
			"--detach takes one off by name or ID; it stays in the item's history.\n\n" +
			"--simplelogin-note replaces the note an alias brought from SimpleLogin, and\n" +
			"--clear-simplelogin-note removes it. That note is not end-to-end encrypted,\n" +
			"and an alias without one is refused: --note is the encrypted one.",
		// Whether the item has a section to put a field under depends on what
		// kind it is, which is not known until it is read; what a field says, and
		// whether a file named by --attach is there to be read, are known now.
		RunE: kit.Run([]kit.Step{kit.StepExpand, d.secrets.Supply, func(c *kit.Invocation) error {
			if err := a.check(c); err != nil {
				return err
			}
			if _, err := d.readAttachments(); err != nil {
				return err
			}
			return d.checkFields("")
		}}, func(c *kit.Invocation) error {
			wifi, err := security.Value()
			if err != nil {
				return err
			}
			if err := d.readSecrets(c); err != nil {
				return err
			}
			if err := d.checkFields(""); err != nil {
				return err
			}
			files, err := d.readAttachments()
			if err != nil {
				return err
			}
			if err := d.allowed(c, files); err != nil {
				return err
			}
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			// A name that is not on the item is caught here, so a dry run refuses
			// it too and no edit lands with half of what was asked for.
			detaching, err := detached(c, shareID, itemID, d.detach)
			if err != nil {
				return err
			}
			route := a.patch()
			if route.SimpleLoginNote != nil {
				if err := noteChangeable(c, shareID, itemID); err != nil {
					return err
				}
			}
			patch := passsvc.Patch{
				Scalars: passsvc.Scalars{
					Name: d.nc.Name, Username: d.nc.Username, Password: d.nc.Password,
					Email: d.nc.Email, URL: d.nc.URL, TOTP: d.nc.TOTP, Note: d.nc.Note,
					Holder: d.nc.Holder, Number: d.nc.Number, Expiry: d.nc.Expiry,
					CVV: d.nc.CVV, PIN: d.nc.PIN, SSID: d.nc.SSID, WifiSecurity: wifi,
					PrivateKey: d.nc.PrivateKey, PublicKey: d.nc.PublicKey,
				},
				Identity:    d.identityValues(),
				ExtraFields: d.nc.ExtraFields,
				Attach:      reporting(c, files),
				Detach:      detaching,
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "items", Count: 1, Name: d.nc.Name,
				Detail: carrying(len(files), len(detaching)),
				IDs:    []string{kit.JoinPair(shareID, itemID)},
			}, func() error {
				if err := c.App.Pass.AliasEdit(c.Ctx, shareID, itemID, route); err != nil {
					return err
				}
				return c.App.Pass.ItemEdit(c.Ctx, shareID, itemID, patch)
			})
		}),
	}
	d.register(c, "Replace")
	c.Flags().StringArrayVar(&d.detach, "detach", nil,
		"Remove an attachment, by name or ID (repeatable)")
	a.register(c)
	security.Register(c)
	return c
}

// detached turns what --detach named into the IDs of the files to take off.
func detached(c *kit.Invocation, shareID, itemID string, refs []string) ([]string, error) {
	out := make([]string, 0, len(refs))
	for _, reference := range refs {
		expanded, err := kit.Expand(c.App, reference)
		if err != nil {
			return nil, err
		}
		file, err := c.App.Pass.ResolveAttachment(c.Ctx, shareID, itemID, expanded)
		if err != nil {
			return nil, err
		}
		out = append(out, file.ID)
	}
	return out, nil
}

// carrying is what a result says about the files an edit moved, and nothing at
// all when it moved none.
func carrying(added, removed int) string {
	switch {
	case added == 0 && removed == 0:
		return ""
	case removed == 0:
		return "with " + ui.Quantity(added, "attachments")
	case added == 0:
		return "with " + ui.Quantity(removed, "attachments") + " removed"
	}
	return fmt.Sprintf("with %s added and %d removed", ui.Quantity(added, "attachments"), removed)
}

// ── removing ──

func itemsTrashCmd() *cobra.Command {
	return bulkItemCmd("trash", "Move items to the trash", ui.Trashed, "to trash",
		func(c *kit.Invocation, share, item string) error {
			return c.App.Pass.ItemTrash(c.Ctx, share, item)
		})
}

func itemsDeleteCmd() *cobra.Command {
	return bulkItemCmd("delete", "Delete items permanently", ui.Deleted, "",
		func(c *kit.Invocation, share, item string) error {
			return c.App.Pass.ItemDelete(c.Ctx, share, item)
		})
}

func bulkItemCmd(use, short string, action ui.Action, detail string,
	apply func(*kit.Invocation, string, string) error) *cobra.Command {
	var f filters
	c := &cobra.Command{
		Use:   use + " [REF...]",
		Short: short,
		RunE: kit.Run([]kit.Step{
			kit.StepSelection(f.set, itemFilterHint, itemScope), kit.StepExpand,
		}, func(c *kit.Invocation) error {
			sel, err := selectItems(c, &f)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "items", Count: sel.Len(), IDs: sel.IDs,
				Detail: detail, Preview: sel.Preview(),
			}, func() error {
				for _, it := range sel.Rows {
					if err := apply(c, it.ShareID, it.ItemID); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	f.register(c)
	return c
}

// ── selection ──

type filters struct {
	vault    string
	itemType *kit.Enum
	age      kit.Range
	all      bool
	// page is the cap a bulk verb acts under, for the same reason Mail's is: a
	// filter that selected more than was meant should stop short of the vault.
	page kit.Page
}

// registerNarrowing adds the flags that say which items, and nothing else. See
// the same method on Drive's filters for why `list` shares them.
func (f *filters) registerNarrowing(c *cobra.Command) {
	f.itemType = &kit.Enum{Name: "type", Usage: "Match only this kind of item", Values: itemTypes}
	f.itemType.Register(c)
	fl := c.Flags()
	fl.StringVar(&f.vault, "vault", "", "Match only this vault, by name or ID")
	f.age.Register(fl, "items")
}

func (f *filters) register(c *cobra.Command) {
	f.registerNarrowing(c)
	kit.All(c.Flags(), &f.all)
	f.page.Default = defaultLimit
	f.page.RegisterCap(c, "items")
}

// narrowed reports whether the user asked for a subset of what is there.
func (f *filters) narrowed() bool {
	return f.vault != "" || f.itemType.Set() || f.age.Set()
}

func (f *filters) set() bool { return f.narrowed() || f.all }

const (
	itemFilterHint = "--vault, --type or --older-than"
	// itemScope is what --all covers when nothing narrows it.
	itemScope = "a whole vault"
)

// defaultLimit is how many items a bulk verb acts on when no cap was given.
const defaultLimit = 150

func selectItems(c *kit.Invocation, f *filters) (kit.Selection[passsvc.Item], error) {
	if f.all && f.vault == "" && !f.itemType.Set() && !f.age.Set() {
		c.Warn("--all with no other filter covers every vault. Add --vault to narrow it.")
	}
	sel := kit.Selector[passsvc.Item]{
		Noun:       "items",
		Columns:    itemColumns(),
		IDOf:       itemRef,
		FilterHint: itemFilterHint,
		Scope:      itemScope,
		Limit:      f.page.Size,
		ByRef: func(ctx stdctx.Context, ref string) (passsvc.Item, error) {
			shareID, itemID, err := resolveItem(c, ref)
			if err != nil {
				return passsvc.Item{}, err
			}
			it, err := c.App.Pass.ItemGet(ctx, shareID, itemID)
			if err != nil {
				return passsvc.Item{}, err
			}
			return it.Item, nil
		},
	}
	if f.set() {
		sel.ByFilter = func(ctx stdctx.Context) ([]passsvc.Item, error) {
			return matchItems(ctx, c, f)
		}
	}
	return kit.Select(c, sel)
}

func matchItems(ctx stdctx.Context, c *kit.Invocation, f *filters) ([]passsvc.Item, error) {
	kind, err := f.itemType.Value()
	if err != nil {
		return nil, err
	}
	var olderThan, newerThan int64
	if f.age.OlderThan != "" {
		d, err := units.ParseDuration(f.age.OlderThan)
		if err != nil {
			return nil, kit.Fail("--older-than: %v", err)
		}
		olderThan = time.Now().Add(-d).Unix()
	}
	if f.age.NewerThan != "" {
		d, err := units.ParseDuration(f.age.NewerThan)
		if err != nil {
			return nil, kit.Fail("--newer-than: %v", err)
		}
		newerThan = time.Now().Add(-d).Unix()
	}
	vaultRef, err := kit.Expand(c.App, f.vault)
	if err != nil {
		return nil, err
	}
	items, err := c.App.Pass.ItemsList(ctx, vaultRef)
	if err != nil {
		return nil, err
	}
	out := make([]passsvc.Item, 0, len(items))
	for _, it := range items {
		if kind != "" && it.Type != kind {
			continue
		}
		if olderThan != 0 && it.ModifyTime > olderThan {
			continue
		}
		if newerThan != 0 && it.ModifyTime < newerThan {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func keepType(items []passsvc.Item, kind string) []passsvc.Item {
	out := items[:0]
	for _, it := range items {
		if it.Type == kind {
			out = append(out, it)
		}
	}
	return out
}

// ── pinning and history ──

// Pinning puts an item at the top of the list. It carries no content, so nothing
// is encrypted: it is the vault recording that one of its items is wanted often.
func itemsPinCmd(use, short string, action ui.Action, pinned bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			type target struct{ share, item, name string }
			targets := make([]target, 0, len(c.Args))
			for _, ref := range c.Args {
				shareID, itemID, err := resolveItem(c, ref)
				if err != nil {
					return err
				}
				it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
				if err != nil {
					return err
				}
				targets = append(targets, target{shareID, itemID, it.Name})
			}
			ids := make([]string, 0, len(targets))
			for _, t := range targets {
				ids = append(ids, kit.JoinPair(t.share, t.item))
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "items", Count: len(targets), IDs: ids,
				Name: kit.Sole(targets, func(t target) string { return t.name }),
			}, func() error {
				for _, t := range targets {
					if err := c.App.Pass.ItemPin(c.Ctx, t.share, t.item, pinned); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// excludeLong is what excluding a login changes, and what it cannot be used for.
const excludeLong = "Leave logins out of Pass Monitor's checks.\n\n" +
	"An excluded login is left out of every `items list --risk` check, and\n" +
	"`items list --excluded` lists them. Only logins can be excluded: an alias's\n" +
	"address is watched or not with `breaches enable` and `breaches disable`."

// Excluding a login from Pass Monitor is an item flag, like pinning: nothing is
// encrypted and no version is written.
func itemsMonitorCmd(use, short, long string, action ui.Action, detail string, excluded bool) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Long:  long,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			type target struct{ share, item, name string }
			targets := make([]target, 0, len(c.Args))
			for _, ref := range c.Args {
				shareID, itemID, err := resolveItem(c, ref)
				if err != nil {
					return err
				}
				it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
				if err != nil {
					return err
				}
				if err := monitorable(c, it, excluded); err != nil {
					return err
				}
				targets = append(targets, target{shareID, itemID, it.Name})
			}
			ids := make([]string, 0, len(targets))
			for _, t := range targets {
				ids = append(ids, kit.JoinPair(t.share, t.item))
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "items", Count: len(targets), IDs: ids, Detail: detail,
				Name: kit.Sole(targets, func(t target) string { return t.name }),
			}, func() error {
				for _, t := range targets {
					if err := c.App.Pass.SetHealthChecked(c.Ctx, t.share, t.item, !excluded); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// monitorable refuses what Pass Monitor's switch is not offered on: anything
// but a login, and a login in a vault this account may only view. An alias has
// the same switch under another name, so its refusal says where that is.
func monitorable(c *kit.Invocation, it *passsvc.FullItem, excluding bool) error {
	switch it.Type {
	case "login":
	case "alias":
		verb, does := "disable", "stops watching its address"
		if !excluding {
			verb, does = "enable", "watches its address again"
		}
		return errs.Naming(it.Name, kit.Fail("%s is an alias, and Pass Monitor checks logins.", it.Name).
			Hint(kit.Program+" pass breaches "+verb+" "+it.Alias+" "+does))
	default:
		return errs.Naming(it.Name, kit.Fail("%s is a %s, and Pass Monitor checks logins.", it.Name, it.Type))
	}
	access, err := c.App.Pass.ShareAccess(c.Ctx, it.ShareID)
	if err != nil {
		return err
	}
	if access == "viewer" {
		return errs.Naming(it.Name, kit.Fail("You may only view %s.", it.Name))
	}
	return nil
}

// An item's revisions are what it used to be. Pass keeps every edit, so a
// password changed by mistake is recoverable by reading what it was.
//
// The word is `revisions`, as it is in Drive: the same idea gets the same word,
// whichever app it is in.
func itemsRevisionsCmd() *cobra.Command {
	c := &cobra.Command{Use: "revisions", Short: "Earlier versions of an item"}
	c.AddCommand(itemsRevisionsGetCmd(), itemsRevisionsListCmd(), itemsRevisionsRestoreCmd())
	return c
}

// revisionNumber reads the number `revisions list` shows, which is what every
// command about one earlier version takes.
func revisionNumber(c *kit.Invocation, arg string) (int, error) {
	revision, err := strconv.Atoi(arg)
	if err != nil || revision < 1 {
		return 0, kit.Fail("%q is not a revision number.", arg).
			Hint("the number in the first column of `" + kit.Program + " pass items revisions list`")
	}
	return revision, nil
}

func itemsRevisionsRestoreCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restore REF REVISION_REF",
		Short: "Put an item back to an earlier version",
		Long: "Put an item back to an earlier version.\n\n" +
			"The version is added to the history as the newest one, so nothing is lost.\n\n" +
			"The item's attachments are left as they are. To bring one back, use\n" +
			"`attachments restore`.\n\n" +
			"REVISION_REF is the number `revisions list` shows.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			revision, err := revisionNumber(c, c.Args[1])
			if err != nil {
				return err
			}
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			was, err := c.App.Pass.RevisionGet(c.Ctx, shareID, itemID, revision)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Restored, Kind: "items", Count: 1, Name: was.Name,
				Detail: "to the version from " + units.Time(was.ModifyTime),
				IDs:    []string{kit.JoinPair(shareID, itemID)},
			}, func() error {
				return c.App.Pass.RevisionRestore(c.Ctx, shareID, itemID, revision)
			})
		}),
	}
}

func itemsRevisionsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF REVISION_REF",
		Short: "Show one earlier version, decrypted",
		Long: "Show one earlier version, decrypted.\n\n" +
			"The password, TOTP secret and private key that revision held are printed in\n" +
			"full, as `items get` prints the current ones.\n\n" +
			"Attachments are not part of a version: `attachments list` shows the ones on\n" +
			"the item, and `attachments list --removed` the ones taken off it.\n\n" +
			"REVISION_REF is the number `revisions list` shows.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			revision, err := revisionNumber(c, c.Args[1])
			if err != nil {
				return err
			}
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			it, err := c.App.Pass.RevisionGet(c.Ctx, shareID, itemID, revision)
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{Object: it, Fields: append(itemFields(it), itemIDField(it.Item))})
		}),
	}
}

func itemsRevisionsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "Show what an item used to be",
		Long: "Show what an item used to be.\n\n" +
			"Pass keeps every edit, so a password changed by mistake can be recovered.\n" +
			"Newest first.\n\n" +
			"This says what changed and when. To read one revision back in full, use\n" +
			"`revisions get`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			revs, err := c.App.Pass.ItemHistory(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[passsvc.Revision]{
				Noun:  "revisions",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[passsvc.Revision]{
					{Header: "REVISION", Right: true, Cell: func(r passsvc.Revision) string {
						return strconv.Itoa(r.Revision)
					}},
					{Header: "CHANGED", Cell: func(r passsvc.Revision) string {
						return units.Time(r.ModifyTime)
					}},
					{Header: "NAME", Flex: true, Cell: func(r passsvc.Revision) string {
						if r.Item == nil {
							return "(cannot be read)"
						}
						return r.Item.Name
					}},
					{Header: "USERNAME", Flex: true, Cell: func(r passsvc.Revision) string {
						if r.Item == nil {
							return ""
						}
						if r.Item.Username != "" {
							return r.Item.Username
						}
						return r.Item.Email
					}},
				},
			}, revs)
		}),
	}
}

// ── the code behind a stored secret ──

// Pass stores the TOTP secret, not the code, so every client works the code out
// for itself. `items get` prints the secret because that is the thing being
// stored; this prints what it currently stands for, which is what a script or a
// login prompt wants.
func itemsTOTPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "totp REF",
		Short: "Print the current two-factor code for an item",
		Long: "Print the current two-factor code for an item.\n\n" +
			"How long the code has left is reported beside it, so you can tell whether\n" +
			"to wait for the next one.\n\n" +
			"For a script: --output json, then read .code.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			shareID, itemID, err := resolveItem(c, c.Args[0])
			if err != nil {
				return err
			}
			it, err := c.App.Pass.ItemGet(c.Ctx, shareID, itemID)
			if err != nil {
				return err
			}
			secret := it.TOTP
			if secret == "" {
				// A custom field can hold one too, which is where a second factor
				// for the same login usually lands.
				for _, f := range it.Fields {
					if f.Type == "totp" && f.Value != "" {
						secret = f.Value
						break
					}
				}
			}
			if secret == "" {
				return errs.Naming(it.Name, kit.Fail("%s carries no two-factor secret.", it.Name).
					Hint("`pass items update "+c.Args[0]+" --secret-file totp-uri=FILE` stores one."))
			}
			s, err := otp.Parse(secret)
			if err != nil {
				return kit.Fail("%v", err)
			}
			code, err := s.Now()
			if err != nil {
				return kit.Fail("%v", err)
			}
			return kit.Show(c, ui.RecordSpec{
				Object: code,
				Fields: []ui.Field{
					{Label: "Code", Value: code.Code},
					{Label: "Expires In", Value: ui.Quantity(code.Expires, "seconds")},
				},
			})
		}),
	}
}
