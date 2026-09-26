package contacts

import (
	"io"
	"os"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	ctsvc "github.com/roman-16/proton-cli/internal/service/contacts"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

func keysCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "keys",
		Short: "Public keys you trust for a contact",
		Long: "Public keys you trust for a contact.\n\n" +
			"Trusting a key means mail to that address is encrypted to that key, rather\n" +
			"than to whatever the server hands back. Whether it is encrypted at all is one\n" +
			"of the address's settings: see `contacts emails`.",
	}
	c.AddCommand(keysListCmd(), keysTrustCmd(), keysUntrustCmd())
	return c
}

// trustedKey is one trusted key as reported, with the fingerprint standing in
// for the key material: an armoured block is not something to print into a table.
type trustedKey struct {
	Email       string `json:"email"`
	Fingerprint string `json:"fingerprint"`
	Verified    bool   `json:"signature_verified"`
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List the keys you trust for a contact",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			ct, err := c.App.Contacts.Get(c.Ctx, id)
			if err != nil {
				return err
			}
			var rows []trustedKey
			// A key that would not decode is counted where it is skipped, so the
			// listing says it is short; what did decode is shown.
			for _, es := range ctsvc.ContactEmailSettings(c.Ctx, ct) {
				for _, armored := range es.Keys {
					rows = append(rows, trustedKey{
						Email:       es.Address,
						Fingerprint: pgphelper.Fingerprint(armored),
						Verified:    es.SignatureVerified,
					})
				}
			}
			return kit.List(c, ui.TableSpec[trustedKey]{
				Noun:  "keys",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[trustedKey]{
					{Header: "EMAIL", Flex: true, Cell: func(k trustedKey) string { return k.Email }},
					{Header: "FINGERPRINT", Cell: func(k trustedKey) string { return k.Fingerprint }},
					{Header: "VERIFIED", Cell: func(k trustedKey) string { return yesNo(k.Verified) }},
				},
			}, rows)
		}),
	}
}

func keysTrustCmd() *cobra.Command {
	var keyPath string
	c := &cobra.Command{
		Use:   "trust REF",
		Short: "Trust a public key so mail to a contact is encrypted to it",
		Long: "Trust a public key so mail to a contact is encrypted to it.\n\n" +
			"A key is trusted for one address. Name that address as REF when the contact\n" +
			"holds more than one; naming the contact is enough when they hold one.\n\n" +
			"Trusting turns encryption to the address on. To keep a key for verifying\n" +
			"signatures only, trust it and then run `contacts emails update REF --encrypt off`.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if keyPath == "" {
				return kit.Fail("A key is required.").
					Hint("--key jane-pubkey.asc, or --key - to read an armoured key from stdin.")
			}
			armored, err := readKey(c, keyPath)
			if err != nil {
				return err
			}
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			_, target, err := pickEmail(c, id, c.Args[0])
			if err != nil {
				return err
			}
			var rewrote rewritten
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Trusted, Kind: "keys", Count: 1,
				Detail: "for " + target,
			}, func() error {
				verdict, err := c.App.Contacts.TrustKey(c.Ctx, id, target, armored)
				rewrote.card(verdict)
				return err
			}); err != nil {
				return err
			}
			rewrote.report(c)
			return nil
		}),
	}
	c.Flags().StringVar(&keyPath, "key", "", "Armoured public key file (- for stdin)")
	return c
}

func keysUntrustCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "untrust REF",
		Short: "Stop trusting a contact's keys",
		Long: "Stop trusting a contact's keys.\n\n" +
			"Keys are trusted for one address. Name that address as REF when the contact\n" +
			"holds more than one; naming the contact is enough when they hold one.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			_, target, err := pickEmail(c, id, c.Args[0])
			if err != nil {
				return err
			}
			var rewrote rewritten
			if err := kit.Mutate(c, ui.ResultSpec{
				Action: ui.Untrusted, Kind: "keys", Count: 1,
				Detail: "for " + target,
			}, func() error {
				verdict, err := c.App.Contacts.UntrustKey(c.Ctx, id, target)
				rewrote.card(verdict)
				return err
			}); err != nil {
				return err
			}
			rewrote.report(c)
			return nil
		}),
	}
}

// pickEmail decides which of a contact's addresses a command targets, and hands
// back the contact it read to decide.
//
// A key and a setting belong to an address, and a reference already names one:
// `trust jane` and `trust jane@example.com` both find the contact, and only the
// second says which of her addresses. So the reference answers when it can, the
// sole address answers when there is one, and anything else is a refusal that
// lists the choice - guessing would silently encrypt to the wrong address.
func pickEmail(c *kit.Invocation, id, ref string) (*ctsvc.Contact, string, error) {
	ct, err := c.App.Contacts.Get(c.Ctx, id)
	if err != nil {
		return nil, "", err
	}
	addresses := ct.EmailAddresses()
	for _, e := range addresses {
		if strings.EqualFold(e, strings.TrimSpace(ref)) {
			return ct, e, nil
		}
	}
	switch len(addresses) {
	case 0:
		return nil, "", kit.Fail("That contact has no email address.")
	case 1:
		return ct, addresses[0], nil
	}
	lines := []string{"name one of them instead of the contact:"}
	for _, e := range addresses {
		lines = append(lines, "  "+e)
	}
	return nil, "", kit.Fail("That contact has %d email addresses.", len(addresses)).
		Hint(lines...).Exit(4)
}

func readKey(c *kit.Invocation, path string) (string, error) {
	data, err := readFileArg(c, "--key", path)
	return string(data), err
}

// readFileArg reads the file a flag names, or standard input when it names -.
func readFileArg(c *kit.Invocation, flag, path string) ([]byte, error) {
	if path == "-" {
		r, err := c.App.Stdin(flag + " -")
		if err != nil {
			return nil, err
		}
		return io.ReadAll(r)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, kit.Fail("could not read %s: %v", path, err)
	}
	return data, nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
