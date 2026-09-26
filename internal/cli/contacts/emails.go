package contacts

import (
	"strconv"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/vcard"
	"github.com/spf13/cobra"
)

const (
	formatAutomatic = "automatic"
	formatPlainText = "plain-text"
	settingOn       = "on"
	settingOff      = "off"
	settingDefault  = "default"
)

func emailsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "emails",
		Short: "How mail to a contact's addresses is sent",
		Long: "How mail to a contact's addresses is sent.\n\n" +
			"Each address has its own format, encryption, signing and scheme. Where an\n" +
			"address says default, it follows the account's `mail settings` sign and\n" +
			"pgp-scheme.",
	}
	c.AddCommand(emailsListCmd(), emailsUpdateCmd())
	return c
}

// emailRow is one address's settings, in the words `emails update` takes.
type emailRow struct {
	Email   string `json:"email"`
	Format  string `json:"format"`
	Encrypt string `json:"encrypt,omitempty"`
	Sign    string `json:"sign"`
	Scheme  string `json:"scheme"`
	Keys    int    `json:"keys"`
}

func emailRowOf(es ctsvc.EmailSettings) emailRow {
	row := emailRow{Email: es.Address, Format: formatAutomatic, Sign: settingDefault, Scheme: settingDefault, Keys: len(es.Keys)}
	if es.PlainText {
		row.Format = formatPlainText
	}
	switch {
	case es.Encrypt != nil:
		row.Encrypt = onOff(*es.Encrypt)
	case len(es.Keys) > 0:
		row.Encrypt = settingOn
	}
	if es.Sign != nil {
		row.Sign = onOff(*es.Sign)
	}
	if es.Scheme != "" {
		row.Scheme = es.Scheme
	}
	return row
}

func onOff(b bool) string {
	if b {
		return settingOn
	}
	return settingOff
}

func emailsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List a contact's addresses and how mail to each is sent",
		Long: "List a contact's addresses and how mail to each is sent.\n\n" +
			"ENCRYPT is blank for an address with nothing pinned that states no choice: mail\n" +
			"to it is encrypted when its provider publishes a key.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			ct, err := c.App.Contacts.Get(c.Ctx, id)
			if err != nil {
				return err
			}
			var rows []emailRow
			for _, es := range ctsvc.ContactEmailSettings(c.Ctx, ct) {
				rows = append(rows, emailRowOf(es))
			}
			return kit.List(c, ui.TableSpec[emailRow]{
				Noun:  "emails",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[emailRow]{
					{Header: "EMAIL", Flex: true, Handle: true, Cell: func(r emailRow) string { return r.Email }},
					{Header: "FORMAT", Cell: func(r emailRow) string { return r.Format }},
					{Header: "ENCRYPT", Cell: func(r emailRow) string { return r.Encrypt }},
					{Header: "SIGN", Cell: func(r emailRow) string { return r.Sign }},
					{Header: "SCHEME", Cell: func(r emailRow) string { return r.Scheme }},
					{Header: "KEYS", Right: true, Cell: func(r emailRow) string { return strconv.Itoa(r.Keys) }},
				},
			}, rows)
		}),
	}
}

// emailChange is what `emails update` was asked to change; "" is a setting it
// was not given.
type emailChange struct {
	format, encrypt, sign, scheme string
}

func emailsUpdateCmd() *cobra.Command {
	format := &kit.Enum{Name: "email-format", Usage: "What mail to the address is sent as",
		Values: []string{formatAutomatic, formatPlainText}}
	encrypt := &kit.Enum{Name: "encrypt", Usage: "Whether mail to the address is encrypted", Values: kit.OnOff}
	sign := &kit.Enum{Name: "sign", Usage: "Whether mail to the address is signed",
		Values: []string{settingOn, settingOff, settingDefault}}
	scheme := &kit.Enum{Name: "scheme", Usage: "How signed or encrypted mail to the address is packaged",
		Values: []string{vcard.SchemeMIME, vcard.SchemeInline, settingDefault}}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change how mail to an address is sent",
		Long: "Change how mail to one of a contact's addresses is sent.\n\n" +
			"Name the address as REF when the contact holds more than one. A Proton address\n" +
			"takes --email-format alone: mail to it is always encrypted and signed. Encrypted\n" +
			"mail is always signed, and the scheme decides the format of signed mail: plain\n" +
			"text under pgp-inline, as written under pgp-mime. --encrypt needs a pinned key,\n" +
			"or one the address's provider publishes.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			var ch emailChange
			for _, e := range []struct {
				enum *kit.Enum
				into *string
			}{{format, &ch.format}, {encrypt, &ch.encrypt}, {sign, &ch.sign}, {scheme, &ch.scheme}} {
				v, err := e.enum.Value()
				if err != nil {
					return err
				}
				*e.into = v
			}
			if ch == (emailChange{}) {
				return kit.Fail("Nothing to change.").Hint("pass --email-format, --encrypt, --sign or --scheme")
			}
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			ct, target, err := pickEmail(c, id, c.Args[0])
			if err != nil {
				return err
			}
			current := ctsvc.EmailSettings{Address: target}
			if es := ctsvc.SettingsOf(c.Ctx, ct, target); es != nil {
				current = *es
			}
			dest, err := c.App.Mail.Destination(c.Ctx, target)
			if err != nil {
				return err
			}
			defaults, err := c.App.Mail.SendDefaults(c.Ctx)
			if err != nil {
				return err
			}
			prefs, err := decideEmailPreferences(target, dest, defaults, current, ch)
			if err != nil {
				return err
			}
			var rewrote rewritten
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "emails", Count: 1, Name: target,
			}, func() error {
				verdict, err := c.App.Contacts.SetEmailPreferences(c.Ctx, id, target, prefs)
				rewrote.card(verdict)
				return err
			}); err != nil {
				return err
			}
			rewrote.report(c)
			return nil
		}),
	}
	format.Register(c)
	encrypt.Register(c)
	sign.Register(c)
	scheme.Register(c)
	return c
}

// decideEmailPreferences lays a change over what an address holds and returns
// what to store, refusing a combination no send would honour. What it stores
// is what Proton's own editor would: a Proton address keeps its format alone,
// encrypted mail is recorded as signed, and signed mail's format is the one its
// scheme fixes.
func decideEmailPreferences(address string, dest mailsvc.Destination, defaults mailsvc.SendDefaults, current ctsvc.EmailSettings, ch emailChange) (ctsvc.EmailPreferences, error) {
	p := current.EmailPreferences
	if ch.format != "" {
		p.PlainText = ch.format == formatPlainText
	}
	if dest.Proton {
		if ch.encrypt != "" || ch.sign != "" || ch.scheme != "" {
			return p, kit.Fail("%s is a Proton address: mail to it is always encrypted and signed.", address).
				Hint("--email-format is the one setting a Proton address takes")
		}
		return ctsvc.EmailPreferences{PlainText: p.PlainText}, nil
	}
	resolve := func(p ctsvc.EmailPreferences) mailsvc.Preferences {
		return mailsvc.Resolve(dest, mailsvc.ContactSettings{
			Keys: current.Keys, Encrypt: p.Encrypt, Sign: p.Sign, Scheme: p.Scheme, PlainText: p.PlainText,
		}, defaults)
	}
	encrypted := resolve(current.EmailPreferences).Encrypt
	if ch.encrypt != "" {
		if len(current.Keys) == 0 && !dest.ProviderKey {
			return p, kit.Fail("There is no key to encrypt mail to %s with.", address).
				Hint("proton contacts keys pin " + address + " --key FILE pins one")
		}
		on := ch.encrypt == settingOn
		p.Encrypt = &on
		if encrypted && !on && ch.sign == "" {
			p.Sign = nil
		}
	}
	switch ch.sign {
	case settingOn, settingOff:
		on := ch.sign == settingOn
		p.Sign = &on
	case settingDefault:
		p.Sign = nil
	}
	switch ch.scheme {
	case settingDefault:
		p.Scheme = ""
	case vcard.SchemeMIME, vcard.SchemeInline:
		p.Scheme = ch.scheme
	}
	after := resolve(p)
	if after.Encrypt {
		if ch.sign != "" && ch.sign != settingOn {
			return p, kit.Fail("Mail to %s is encrypted, and encrypted mail is always signed.", address).
				Hint("--encrypt off stops encrypting it")
		}
		on := true
		p.Sign = &on
	}
	if after.Sign {
		if ch.format != "" && p.PlainText != after.Inline {
			if after.Inline {
				return p, kit.Fail("Mail to %s is signed with PGP/Inline, which sends plain text only.", address).
					Hint("--scheme pgp-mime sends it in the format it was written in")
			}
			return p, kit.Fail("Mail to %s is signed with PGP/MIME, which sends it in the format it was written in.", address).
				Hint("--scheme pgp-inline sends it as plain text")
		}
		p.PlainText = after.Inline
	}
	return p, nil
}
