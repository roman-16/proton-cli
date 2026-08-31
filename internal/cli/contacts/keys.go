package contacts

import (
	"io"
	"os"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Pinned keys are a collection, so they get the ordinary verbs - which is also
// what gives `list` somewhere obvious to live.

func keysCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "keys",
		Short: "Public keys pinned to a contact",
		Long: "Public keys pinned to a contact.\n\n" +
			"Pinning a key means mail to that address is encrypted to the key you trust,\n" +
			"rather than to whatever the server hands back.",
	}
	c.AddCommand(keysListCmd(), keysPinCmd(), keysUnpinCmd())
	return c
}

// pinnedKey is one pinned key as reported, with the fingerprint standing in for
// the key material: an armoured block is not something to print into a table.
type pinnedKey struct {
	Email       string `json:"email"`
	Fingerprint string `json:"fingerprint"`
	Encrypt     bool   `json:"encrypt"`
	Scheme      string `json:"scheme,omitempty"`
	Verified    bool   `json:"signature_verified"`
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list REF",
		Short: "List the keys pinned to a contact",
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
			var rows []pinnedKey
			for _, email := range ct.EmailAddresses() {
				crypto, err := c.App.Contacts.PinnedKeysFor(c.Ctx, email)
				if err != nil {
					return err
				}
				if crypto == nil {
					continue
				}
				for _, armored := range crypto.ArmoredKeys {
					rows = append(rows, pinnedKey{
						Email:       email,
						Fingerprint: pgphelper.Fingerprint(armored),
						Encrypt:     crypto.Encrypt == nil || *crypto.Encrypt,
						Scheme:      crypto.Scheme,
						Verified:    crypto.SignatureVerified,
					})
				}
			}
			return kit.List(c, ui.TableSpec[pinnedKey]{
				Noun:  "keys",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[pinnedKey]{
					{Header: "EMAIL", Flex: true, Cell: func(k pinnedKey) string { return k.Email }},
					{Header: "FINGERPRINT", Cell: func(k pinnedKey) string { return k.Fingerprint }},
					{Header: "ENCRYPT", Cell: func(k pinnedKey) string { return yesNo(k.Encrypt) }},
					{Header: "SCHEME", Cell: func(k pinnedKey) string { return k.Scheme }},
					{Header: "VERIFIED", Cell: func(k pinnedKey) string { return yesNo(k.Verified) }},
				},
			}, rows, nil)
		}),
	}
}

func keysPinCmd() *cobra.Command {
	var keyPath, email string
	var noEncrypt bool
	scheme := &kit.Enum{
		Name: "scheme", Usage: "PGP scheme for recipients outside Proton",
		Values: []string{"pgp-mime", "pgp-inline"},
	}
	c := &cobra.Command{
		Use:   "pin REF",
		Short: "Pin a public key so mail to a contact is encrypted to it",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if keyPath == "" {
				return kit.Fail("A key is required.").
					Hint("--key jane-pubkey.asc, or --key - to read an armoured key from stdin.")
			}
			pgpScheme, err := scheme.Value()
			if err != nil {
				return err
			}
			armored, err := readKey(c, keyPath)
			if err != nil {
				return err
			}
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			target, err := pickEmail(c, id, email)
			if err != nil {
				return err
			}
			var encrypt *bool
			if noEncrypt {
				off := false
				encrypt = &off
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Pinned.WithConsent(), Kind: "keys", Count: 1,
				Detail: "for " + target,
			}, func() error {
				return c.App.Contacts.PinKey(c.Ctx, id, target, armored, encrypt, nil, pgpScheme)
			})
		}),
	}
	c.Flags().StringVar(&keyPath, "key", "", "Armoured public key file (- for stdin)")
	c.Flags().StringVar(&email, "email", "", "Which of the contact's addresses the key applies to")
	c.Flags().BoolVar(&noEncrypt, "no-encrypt", false, "Store the key for verification only, leaving encryption off")
	scheme.Register(c)
	return c
}

func keysUnpinCmd() *cobra.Command {
	var email string
	c := &cobra.Command{
		Use:   "unpin REF",
		Short: "Remove the keys pinned to a contact",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			id, err := c.App.Contacts.Resolve(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			target, err := pickEmail(c, id, email)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unpinned.WithConsent(), Kind: "keys", Count: 1,
				Detail: "for " + target,
			}, func() error {
				return c.App.Contacts.UnpinKey(c.Ctx, id, target)
			})
		}),
	}
	c.Flags().StringVar(&email, "email", "", "Which of the contact's addresses to unpin")
	return c
}

// pickEmail decides which of a contact's addresses a key operation targets: the
// one asked for, the only one there is, or an error listing the choice. Guessing
// would silently encrypt to the wrong address.
func pickEmail(c *kit.Invocation, id, flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	ct, err := c.App.Contacts.Get(c.Ctx, id)
	if err != nil {
		return "", err
	}
	addresses := ct.EmailAddresses()
	switch len(addresses) {
	case 0:
		return "", kit.Fail("That contact has no email address.").
			Hint("--email jane@example.com")
	case 1:
		return addresses[0], nil
	}
	lines := []string{"choose one with --email:"}
	for _, e := range addresses {
		lines = append(lines, "  "+e)
	}
	return "", kit.Fail("That contact has %d email addresses.", len(addresses)).
		Hint(lines...).Exit(4)
}

func readKey(c *kit.Invocation, path string) (string, error) {
	if path == "-" {
		r, err := c.App.Stdin("--key -")
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(r)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
