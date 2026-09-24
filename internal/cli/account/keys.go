package account

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// The keys an account holds, and what Proton's settings let a person do with
// one: see it, take a copy out, bring one in, make one, choose the one an
// address writes with, stop encrypting to it or trusting what it signed, remove
// it, and bring back the ones a password reset locked.

func keysCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "keys",
		Short: "The keys your data is encrypted with",
		Long: "The keys your data is encrypted with.\n\n" +
			"Account keys belong to the account. Address keys belong to one address each:\n" +
			"its primary key is what mail to it is encrypted to and what it signs with,\n" +
			"and its other keys open and verify what arrived before.",
	}
	c.AddCommand(
		keysListCmd(), keysGetCmd(), keysExportCmd(), keysImportCmd(), keysCreateCmd(),
		keysUpdateCmd(), keysDeleteCmd(), keysReactivateCmd(),
	)
	return c
}

func keyColumns() []ui.Column[keys.Held] {
	return []ui.Column[keys.Held]{
		{Header: "ID", ID: true, Cell: func(k keys.Held) string { return k.ID }},
		{Header: "KIND", Cell: func(k keys.Held) string { return k.Kind }},
		{Header: "ADDRESS", Flex: true, Cell: func(k keys.Held) string { return k.Email }},
		{Header: "FINGERPRINT", Handle: true, Cell: func(k keys.Held) string { return k.Fingerprint }},
		{Header: "ALGORITHM", Flex: true, Cell: func(k keys.Held) string { return k.Algorithm }},
		{Header: "CREATED", Cell: func(k keys.Held) string { return units.Time(k.Created) }},
		{Header: "STATUS", Cell: func(k keys.Held) string { return string(k.Status) }, Role: statusRole},
	}
}

func statusRole(k keys.Held) ui.Role {
	switch k.Status {
	case keys.StatusCompromised:
		return ui.Danger
	case keys.StatusLocked, keys.StatusUnreadable:
		return ui.Caution
	}
	return ui.Plain
}

func keyLookup(c *kit.Invocation) *kit.Lookup[keys.Held] {
	return &kit.Lookup[keys.Held]{
		Kind: "key",
		Load: func(ctx context.Context) ([]keys.Held, error) {
			u, err := c.App.Unlock(ctx)
			if err != nil {
				return nil, err
			}
			return u.Keys(ctx), nil
		},
		ID:     func(k keys.Held) string { return k.ID },
		Handle: func(k keys.Held) string { return k.Fingerprint },
	}
}

func keysListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the keys of the account and its addresses",
		Long: "List the keys of the account and its addresses.\n\n" +
			"STATUS is one of primary, active, obsolete, compromised, locked, forwarding,\n" +
			"disabled and unreadable. A locked key comes back with `" + kit.Program + " account keys\n" +
			"reactivate`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			rows := u.Keys(c.Ctx)
			return kit.List(c, ui.TableSpec[keys.Held]{
				Noun: "keys", Columns: keyColumns(), Total: len(rows), Page: ui.Unpaged,
			}, rows)
		}),
	}
}

func keysGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one key and what it is used for",
		Long: "Show one key and what it is used for.\n\n" +
			"REF is the key's ID, short ID or fingerprint.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			k, err := keyLookup(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Show(c, ui.RecordSpec{
				Object: k,
				Fields: []ui.Field{
					{Label: "Fingerprint", Value: k.Fingerprint, Handle: true},
					{Label: "Kind", Value: k.Kind},
					{Label: "Address", Value: k.Email},
					{Label: "Algorithm", Value: k.Algorithm},
					{Label: "Created", Value: units.Time(k.Created)},
					{Label: "Status", Value: string(k.Status), Always: true, Role: statusRole(k)},
					{Label: "Used For", Value: k.UsedFor, Always: true},
					{Label: "ID", Value: k.ID, ID: true},
				},
			})
		}),
	}
}

func keysExportCmd() *cobra.Command {
	var (
		dest       kit.Destination
		passphrase kit.Passphrase
		reauth     kit.Reauth
		private    bool
	)
	c := &cobra.Command{
		Use:   "export REF...",
		Short: "Write keys out to files",
		Long: "Write keys out to files, or one to stdout with --dest -.\n\n" +
			"A file is named publickey.OWNER-FINGERPRINT.asc, or privatekey.OWNER-FINGERPRINT.asc\n" +
			"with --private, where OWNER is the address or the account's username.\n\n" +
			"--private writes the private key, locked with a passphrase of at least eight\n" +
			"characters, and asks for your password even when you are signed in. With no\n" +
			"terminal to ask, pass --password-file and --passphrase-file. Only one of them\n" +
			"can be -.",
		RunE: kit.Run([]kit.Step{kit.StepExpand, passphrase.Supply}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if passphrase.Wanted() && !private {
				return kit.Fail("A public key is not locked, so --passphrase-file has nothing to lock.").
					Hint("--private exports the private key, locked with it.")
			}
			if err := dest.Validate(len(c.Args) == 1); err != nil {
				return err
			}
			// A passphrase that arrived on the command line is judged before
			// anything is asked of Proton, and so is having nobody to ask for one.
			// A typed one is asked for once the password has been proved.
			var secret string
			if private && (passphrase.Wanted() || !c.UI().CanPrompt()) {
				s, err := lockingPassphrase(c)
				if err != nil {
					return err
				}
				secret = s
			}
			sel, err := kit.SelectFrom(c, "keys", keyColumns(), keyLookup(c))
			if err != nil {
				return err
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			names := make([]string, 0, sel.Len())
			for _, k := range sel.Rows {
				if err := exportable(k, private); err != nil {
					return err
				}
				names = append(names, keyFileName(u, k, private))
			}
			to := dest.Describe()
			if len(names) == 1 {
				if to, err = dest.Where(names[0]); err != nil {
					return err
				}
			}
			detail := "to " + to
			if private {
				detail += ", locked with your passphrase"
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Exported, Kind: "keys", Count: sel.Len(), IDs: sel.IDs,
				Name:          kit.Sole(sel.Rows, func(k keys.Held) string { return k.Fingerprint }),
				Detail:        detail,
				Preview:       sel.Preview(),
				AnswerFollows: dest.Stdout(),
			}, func() error {
				if private {
					relock, err := c.App.Elevate(c.Ctx, proton.ScopeLocked, "export a private key")
					if err != nil {
						return err
					}
					defer relock()
					if secret == "" {
						if secret, err = lockingPassphrase(c); err != nil {
							return err
						}
					}
				}
				for i, k := range sel.Rows {
					armored, err := exported(k, private, secret)
					if err != nil {
						return err
					}
					if _, err := dest.Write(c, names[i], []byte(armored)); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	dest.Register(c)
	passphrase.Declare(c)
	reauth.Declare(c)
	c.Flags().BoolVar(&private, "private", false, "Export the private key, locked with a passphrase")
	return c
}

// lockingPassphrase is the passphrase an exported private key is locked with,
// held to the length a password is.
func lockingPassphrase(c *kit.Invocation) (string, error) {
	secret, err := c.App.Creds.Passphrase("lock the exported key")
	if err != nil {
		return "", err
	}
	if len([]rune(secret)) < kit.MinPassword {
		return "", kit.Fail("A passphrase needs at least eight characters.")
	}
	return secret, nil
}

// exportable refuses a key there is nothing of to write out.
func exportable(k keys.Held, private bool) error {
	switch {
	case k.Fingerprint == "":
		return kit.Fail("Key %s could not be read, so there is nothing to export.", k.ID)
	case private && k.Locked():
		return kit.Fail("Key %s is locked by a password reset, so there is no private key to export.", k.Fingerprint).
			Hint(kit.Program + " account keys reactivate")
	case private && !k.Opened():
		return kit.Fail("Key %s did not open here, so there is no private key to export.", k.Fingerprint)
	}
	return nil
}

func exported(k keys.Held, private bool, secret string) (string, error) {
	if private {
		return k.ExportPrivate(secret)
	}
	return k.ExportPublic()
}

// keyFileName is the name Proton's own settings give an exported key.
func keyFileName(u *keys.Unlocked, k keys.Held, private bool) string {
	kind, owner := "publickey", k.Email
	if private {
		kind = "privatekey"
	}
	if k.Account() {
		owner = u.Username
	}
	return kind + "." + owner + "-" + k.Fingerprint + ".asc"
}

func keysImportCmd() *cobra.Command {
	var (
		passphrase kit.Passphrase
		reauth     kit.Reauth
	)
	c := &cobra.Command{
		Use:   "import REF SRC...",
		Short: "Add keys to an address from files",
		Long: "Add keys to an address from files, or from stdin with -.\n\n" +
			"REF is the address. A key joins it as one that reads, or as its primary key\n" +
			"when it has no other. A copy of a key the address holds locked comes back as\n" +
			"that key. A key's public half is published with every name and address it\n" +
			"carries.\n\n" +
			"Every key is read and opened before anything is sent, and one that fails stops\n" +
			"the import. A locked key asks for its passphrase, and one passphrase serves the\n" +
			"whole run.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file and --passphrase-file. Only one of them, or SRC, can be -.",
		Annotations: map[string]string{kit.Addresses: "mail settings addresses"},
		RunE: kit.Run([]kit.Step{passphrase.Supply}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			ref, err := kit.Expand(c.App, c.Args[0])
			if err != nil {
				return err
			}
			offered, err := readOffered(c, c.Args[1:])
			if err != nil {
				return err
			}
			a, err := c.App.Mail.ResolveAddress(c.Ctx, ref)
			if err != nil {
				return err
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			arrivals, err := u.Arrivals(a.ID, offered)
			if err != nil {
				return err
			}
			var adding bool
			for _, arrival := range arrivals {
				adding = adding || arrival.Returning == nil
			}
			if adding {
				c.Warn("A key that was generated insecurely or has leaked weakens every message sent to %s. "+
					"Its public half is published, with every name and address it carries.", a.Email)
				if err := warnPausedForwardings(c, a.ID, a.Email); err != nil {
					return err
				}
			}
			ctx := app.WithScopeReason(c.Ctx, "add a key to "+a.Email)
			for _, arrival := range arrivals {
				spec := ui.ResultSpec{
					Action: ui.Imported, Kind: "keys", Name: arrival.Key.GetFingerprint(), Detail: "to " + a.Email,
				}
				if arrival.Returning != nil {
					spec.Action, spec.Detail = ui.Reactivated, "of "+a.Email
				}
				if err := kit.Create(c, spec, func() (string, error) {
					return u.Import(ctx, c.App.API, a.ID, arrival)
				}); err != nil {
					return err
				}
			}
			return nil
		}),
	}
	passphrase.Declare(c)
	reauth.Declare(c)
	return c
}

// readOffered reads every file named, opening the keys in them, before anything
// is asked of Proton.
func readOffered(c *kit.Invocation, paths []string) ([]keys.Offered, error) {
	ask := func() (string, error) { return c.App.Creds.Passphrase("open the key") }
	var out []keys.Offered
	for _, path := range paths {
		data, err := readKeyFile(c, path)
		if err != nil {
			return nil, err
		}
		offered, err := keys.ReadKeys(data, path, ask)
		var refused *keys.PassphraseRefused
		if len(paths) > 1 && errors.As(err, &refused) {
			return nil, refused.Hint("import each file on its own, with its own --passphrase-file")
		}
		if err != nil {
			return nil, err
		}
		out = append(out, offered...)
	}
	return out, nil
}

func readKeyFile(c *kit.Invocation, path string) ([]byte, error) {
	if path != "-" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, kit.Fail("Could not read %s: %v", path, errors.Unwrap(err))
		}
		return data, nil
	}
	r, err := c.App.Stdin("SRC -")
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, kit.Fail("could not read SRC from stdin: %v", err)
	}
	return data, nil
}

func keysCreateCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "create REF",
		Short: "Generate a key for an address",
		Long: "Generate a key for an address, as its primary key.\n\n" +
			"REF is the address. The key it replaces stays, and goes on opening and\n" +
			"verifying what arrived before. An account that creates post-quantum keys is\n" +
			"refused: generate the key in a Proton client.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		Annotations: map[string]string{kit.Addresses: "mail settings addresses"},
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			a, err := c.App.Mail.ResolveAddress(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			if err := u.Manages(); err != nil {
				return err
			}
			postQuantum, err := keys.PostQuantum(c.Ctx, c.App.API)
			if err != nil {
				return err
			}
			if postQuantum {
				return keys.UnsupportedPostQuantum("generate the key in a Proton client")
			}
			addr, err := u.Address(a.ID)
			if err != nil {
				return err
			}
			if err := warnPausedForwardings(c, a.ID, a.Email); err != nil {
				return err
			}
			key, err := u.GenerateAddressKey(c.Ctx, a.Email)
			if err != nil {
				return err
			}
			var replaced string
			for _, k := range u.Keys(c.Ctx) {
				if k.AddressID == a.ID && k.Primary && k.Version == 4 {
					replaced = k.Fingerprint
				}
			}
			if err := kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "keys", Name: key.GetFingerprint(),
				Detail: "for " + a.Email + ", as its primary key",
			}, func() (string, error) {
				ctx := app.WithScopeReason(c.Ctx, "add a key to "+a.Email)
				return u.PublishAddressKey(ctx, c.App.API, addr, key)
			}); err != nil {
				return err
			}
			if replaced != "" && !c.App.DryRun {
				c.Note("If the key it replaces has leaked, mark it: %s account keys update %s --compromised",
					kit.Program, replaced)
			}
			return nil
		}),
	}
	reauth.Declare(c)
	return c
}

func keysUpdateCmd() *cobra.Command {
	var primary, compromised, obsolete bool
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Make a key primary, or mark it obsolete or compromised",
		Long: "Make a key primary, or mark it obsolete or compromised.\n\n" +
			"--primary makes it the key its address encrypts to and signs with, and the one\n" +
			"it replaces stays. --obsolete stops encryption to the key and keeps its\n" +
			"signatures trusted. --compromised stops both, and the key goes on opening what\n" +
			"was sealed to it.\n\n" +
			"=false takes a mark off. --compromised=false leaves the key obsolete, so pass\n" +
			"--obsolete=false as well to use it again. The primary key cannot be marked, and\n" +
			"only address keys change.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			setPrimary, setCompromised, setObsolete := c.Changed("primary"), c.Changed("compromised"), c.Changed("obsolete")
			switch {
			case !setPrimary && !setCompromised && !setObsolete:
				return kit.Fail("Nothing to change.").Hint("pass --primary, --obsolete or --compromised.")
			case setPrimary && !primary:
				return kit.Fail("A key stops being primary when another key is made primary.").
					Hint(kit.Program + " account keys update REF --primary, naming the key to use instead")
			case setCompromised && compromised && setObsolete && !obsolete:
				return kit.Fail("--compromised and --obsolete=false contradict each other: nothing is encrypted to a compromised key.")
			}
			k, err := keyLookup(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			if err := u.Manages(); err != nil {
				return err
			}
			if err := changeable(k); err != nil {
				return err
			}
			if setPrimary {
				return makePrimary(c, u, k)
			}
			var marks marking
			if setCompromised {
				marks.compromised = &compromised
			}
			if setObsolete {
				marks.obsolete = &obsolete
			}
			return mark(c, u, k, marks)
		}),
	}
	c.Flags().BoolVar(&primary, "primary", false, "Make it the key its address encrypts to and signs with")
	c.Flags().BoolVar(&obsolete, "obsolete", false, "Stop encrypting to it; --obsolete=false encrypts to it again")
	c.Flags().BoolVar(&compromised, "compromised", false, "Stop encrypting to it and trusting its signatures; --compromised=false trusts them again")
	kit.Exclusive(c, "primary", "obsolete")
	kit.Exclusive(c, "primary", "compromised")
	reauth.Declare(c)
	return c
}

// changeable refuses the keys this command does not change: the account's own,
// and a forwarding's, which goes with the forwarding.
func changeable(k keys.Held) error {
	switch {
	case k.Account():
		return kit.Fail("Key %s is an account key. Only address keys are made primary, marked or deleted.", k.Fingerprint)
	case k.Forwarding():
		return kit.Fail("Key %s decrypts what a forwarding to %s carries, and goes with that forwarding.", k.Fingerprint, k.Email).
			Hint(kit.Program + " mail settings forwarding list")
	}
	return nil
}

func makePrimary(c *kit.Invocation, u *keys.Unlocked, k keys.Held) error {
	switch {
	case k.Primary:
		return kit.Fail("Key %s is already the primary key of %s.", k.Fingerprint, k.Email)
	case k.Compromised():
		return kit.Fail("Key %s is compromised, so it cannot be primary.", k.Fingerprint).
			Hint(kit.Program + " account keys update " + k.Fingerprint + " --compromised=false --obsolete=false")
	case k.Locked():
		return kit.Fail("Key %s is locked by a password reset, so it cannot be primary.", k.Fingerprint).
			Hint(kit.Program + " account keys reactivate")
	case !k.Opened():
		return kit.Fail("Key %s did not open here, so it cannot be primary.", k.Fingerprint)
	case k.AddressDisabled():
		return kit.Fail("%s is disabled, and a key of a disabled address cannot be made primary.", k.Email)
	case k.Obsolete():
		return kit.Fail("Key %s is obsolete, so it cannot be primary.", k.Fingerprint).
			Hint(kit.Program + " account keys update " + k.Fingerprint + " --obsolete=false")
	}
	if err := warnPausedForwardings(c, k.AddressID, k.Email); err != nil {
		return err
	}
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Updated, Kind: "keys", Count: 1, IDs: []string{k.ID}, Name: k.Fingerprint,
		Detail: "- now the primary key of " + k.Email,
	}, func() error {
		return u.SetPrimary(app.WithScopeReason(c.Ctx, "change a key"), c.App.API, k)
	})
}

// marking is what a change asks of a key's marks; nil leaves one as it is.
type marking struct {
	compromised, obsolete *bool
}

func mark(c *kit.Invocation, u *keys.Unlocked, k keys.Held, m marking) error {
	if err := markable(k, m); err != nil {
		return err
	}
	flags := k.Marked(m.compromised, m.obsolete)
	return kit.Mutate(c, ui.ResultSpec{
		Action: ui.Updated, Kind: "keys", Count: 1, IDs: []string{k.ID}, Name: k.Fingerprint,
		Detail: "- " + marked(k, flags),
	}, func() error {
		return u.SetFlags(app.WithScopeReason(c.Ctx, "change a key"), c.App.API, k, flags)
	})
}

// markable refuses a mark a key cannot take, or already has.
func markable(k keys.Held, m marking) error {
	lifting := m.compromised != nil && !*m.compromised
	if k.Primary {
		what := "obsolete"
		if m.compromised != nil {
			what = "compromised"
		}
		return kit.Fail("Key %s is the primary key of %s, which cannot be marked %s.", k.Fingerprint, k.Email, what).
			Hint(kit.Program+" account keys create "+k.Email, "or update another of its keys with --primary")
	}
	if m.compromised != nil {
		switch {
		case *m.compromised && k.Compromised():
			return kit.Fail("Key %s is already marked compromised.", k.Fingerprint)
		case lifting && !k.Compromised():
			return kit.Fail("Key %s is not marked compromised.", k.Fingerprint)
		}
	}
	if m.obsolete == nil {
		return nil
	}
	if *m.obsolete {
		switch {
		case k.Compromised() && !lifting:
			return kit.Fail("Key %s is compromised, which already stops encryption to it.", k.Fingerprint)
		case k.Obsolete():
			return kit.Fail("Key %s is already obsolete.", k.Fingerprint)
		case k.Locked():
			return kit.Fail("Key %s is locked by a password reset, so it cannot be marked obsolete.", k.Fingerprint)
		case !k.Opened() && !lifting:
			return kit.Fail("Key %s did not open here, so it cannot be marked obsolete.", k.Fingerprint)
		case k.AddressDisabled():
			return kit.Fail("%s is disabled, and a key of a disabled address cannot be marked obsolete.", k.Email)
		}
		return nil
	}
	switch {
	case k.Compromised() && !lifting:
		return kit.Fail("Key %s is compromised, and that mark comes off first.", k.Fingerprint).
			Hint(kit.Program + " account keys update " + k.Fingerprint + " --compromised=false --obsolete=false")
	case !k.Compromised() && !k.Obsolete():
		return kit.Fail("Key %s is not obsolete.", k.Fingerprint)
	}
	return nil
}

// marked says what a key is once it carries these flags.
func marked(k keys.Held, flags int) string {
	switch k.StatusFor(flags) {
	case keys.StatusCompromised:
		return "compromised: it still opens what was sealed to it, and nothing is encrypted to it or trusted as signed by it"
	case keys.StatusObsolete:
		return "obsolete: it still opens and verifies, and nothing new is encrypted to it"
	case keys.StatusLocked:
		return "no longer marked, and still locked by a password reset"
	}
	return "in use again"
}

func keysDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete REF...",
		Short: "Delete keys of an address",
		Long: "Delete keys of an address.\n\n" +
			"Nothing sealed to a deleted key opens again, and what it signed is no longer\n" +
			"verified. `" + kit.Program + " account keys export REF --private` keeps a copy first.\n" +
			"The primary key of an address and the account's own keys cannot be deleted.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "keys", keyColumns(), keyLookup(c))
			if err != nil {
				return err
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			if err := u.Manages(); err != nil {
				return err
			}
			for _, k := range sel.Rows {
				if err := changeable(k); err != nil {
					return err
				}
				if k.Primary {
					return kit.Fail("Key %s is the primary key of %s, which cannot be deleted.", k.Fingerprint, k.Email).
						Hint(kit.Program+" account keys create "+k.Email, "or update another of its keys with --primary")
				}
			}
			if sel.Len() == 1 {
				c.Warn("Nothing sealed to this key opens again, and what it signed is no longer verified. "+
					"`%s account keys export %s --private` keeps a copy.", kit.Program, sel.Rows[0].Fingerprint)
			} else {
				c.Warn("Nothing sealed to these keys opens again, and what they signed is no longer verified. " +
					"`" + kit.Program + " account keys export REF --private` keeps a copy.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "keys", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(k keys.Held) string { return k.Fingerprint }),
				Preview: sel.Preview(),
			}, func() error {
				ctx := app.WithScopeReason(c.Ctx, "delete a key")
				for _, k := range sel.Rows {
					if err := u.Delete(ctx, c.App.API, k); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// warnPausedForwardings says, before a change to an address's keys, which of
// its forwardings the change pauses.
func warnPausedForwardings(c *kit.Invocation, addressID, email string) error {
	paused, err := c.App.Mail.ForwardingsPausedByKeys(c.Ctx, addressID)
	if err != nil {
		return err
	}
	if paused > 0 {
		c.Warn("Changing the keys of %s pauses %s from it. `%s mail settings forwarding enable` resumes them.",
			email, ui.Quantity(paused, "end-to-end encrypted forwardings"), kit.Program)
	}
	return nil
}

// ── reactivating ──

// leftLocked is one key a reactivation did not bring back, phrased for the
// warning that names it.
type leftLocked struct {
	key keys.LockedKey
	why string
}

func (l leftLocked) String() string {
	if l.key.Email != "" {
		return fmt.Sprintf("A key of %s %s.", l.key.Email, l.why)
	}
	return fmt.Sprintf("Key %s %s.", l.key.Key.ID, l.why)
}

// leftOut names every locked key the reactivation did not bring back, and why:
// a user key the secret did not open, an address key whose user key stayed
// shut, or a key that comes back only from a copy of its own.
func leftOut(locked []keys.LockedKey, out *keys.Outcome) []leftLocked {
	reactivated := map[string]bool{}
	for _, k := range out.Reactivated {
		reactivated[k.Key.ID] = true
	}
	unsupported := map[string]bool{}
	for _, k := range out.Unsupported {
		unsupported[k.Key.ID] = true
	}
	var left []leftLocked
	for _, k := range locked {
		switch {
		case reactivated[k.Key.ID]:
		case unsupported[k.Key.ID]:
			left = append(left, leftLocked{key: k, why: "comes back only from a copy you exported: " +
				kit.Program + " account keys import " + k.Email + " SRC"})
		case k.AddressID == "":
			left = append(left, leftLocked{key: k, why: "did not open and stays locked; a key from an earlier reset opens with the secret from then"})
		default:
			left = append(left, leftLocked{key: k, why: "stays locked with the account key that holds it"})
		}
	}
	return left
}

func keysReactivateCmd() *cobra.Command {
	var (
		reauth   kit.Reauth
		recovery kit.Recovery
	)
	c := &cobra.Command{
		Use:   "reactivate",
		Short: "Bring back the keys a password reset locked",
		Long: "Bring back the keys a password reset locked.\n\n" +
			"Give one secret from before the reset - the password, the recovery phrase or\n" +
			"a recovery file - and your current password to confirm. With no flag, the\n" +
			"previous password is asked for; in two-password mode that is the second one.\n" +
			"Keys the secret does not open stay locked and are named. Drive files come back\n" +
			"with `" + kit.Program + " drive volumes restore`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if err := recovery.Supply(c); err != nil {
				return err
			}
			// A secret that arrived on the command line is judged before anything
			// is asked of Proton. A typed one is asked for once the change is going
			// ahead, and not before: a prompt for a secret this command is about to
			// refuse is a prompt nobody should have answered.
			var secret keys.Secret
			if recovery.Offered() {
				s, err := recovery.Secret(c)
				if err != nil {
					return err
				}
				secret = s
			}
			u, err := c.App.Unlock(c.Ctx)
			if err != nil {
				return err
			}
			locked := u.Locked()
			if len(locked) == 0 {
				return kit.Fail("No keys are locked.")
			}
			if len(u.LockedAccountKeys()) == 0 {
				return kit.Fail("The locked keys are address keys, which come back with the account key that holds them, or from a copy you exported.").
					Hint(kit.Program + " account keys import " + locked[0].Email + " SRC")
			}
			if recovery.UsesPhrase() && !u.HasRecoveryPhrase() {
				return kit.Fail("This account has no recovery phrase to recover with.").
					Hint("use the password from before the reset, or a recovery file")
			}
			ids := make([]string, 0, len(locked))
			for _, k := range locked {
				ids = append(ids, k.Key.ID)
			}
			ctx := app.WithScopeReason(c.Ctx, "reactivate your keys")
			return kit.Attempt(c, ui.ResultSpec{
				Action: ui.Reactivated, Kind: "keys", Count: len(locked), IDs: ids,
				Preview: kit.Preview("keys", keyColumns(), lockedKeys(c.Ctx, u, ids)),
			}, func() ([]leftLocked, error) {
				if secret == nil {
					s, err := recovery.Secret(c)
					if err != nil {
						return nil, err
					}
					secret = s
				}
				out, err := u.Reactivate(ctx, c.App.API, secret)
				if err != nil {
					return nil, err
				}
				return leftOut(locked, out), nil
			})
		}),
	}
	reauth.Declare(c)
	recovery.Declare(c)
	return c
}

// lockedKeys is the keys a reactivation would bring back, as the listing shows
// them.
func lockedKeys(ctx context.Context, u *keys.Unlocked, ids []string) []keys.Held {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	var out []keys.Held
	for _, k := range u.Keys(ctx) {
		if wanted[k.ID] {
			out = append(out, k)
		}
	}
	return out
}
