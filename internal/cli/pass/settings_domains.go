package pass

import (
	"context"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/dns"
	"github.com/roman-16/proton-cli/internal/errs"
	passsvc "github.com/roman-16/proton-cli/internal/service/pass"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
)

// The domains an alias can be made on. Proton offers a handful of its own, and a
// paid account may bring a domain it owns, which mirrors the "Domains" half of
// Pass's Aliases settings: the entries its DNS needs, and what it does with mail
// to a name no alias has claimed.

func domainsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "domains",
		Short: "The domains an alias can be made on",
		Long: "The domains an alias can be made on.\n\n" +
			"Proton's own are always there. A custom domain is one you own and needs a\n" +
			"paid Pass plan; it carries no alias until its DNS entries are in place.\n" +
			"`get` shows them and says which ones Proton can see.",
	}
	c.AddCommand(domainsListCmd(), domainsGetCmd(), domainsCreateCmd(),
		domainsUpdateCmd(), domainsDeleteCmd())
	return c
}

func domainsListCmd() *cobra.Command {
	var held kit.Held[passsvc.Domain]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the domains an alias can be made on",
		Long: "List the domains an alias can be made on.\n\n" +
			"These are the values `proton pass aliases create --suffix` accepts: the\n" +
			"part of an alias after the @. A custom domain is listed whatever state it\n" +
			"is in; STATUS is active, unverified or unconfigured, and is blank for one of\n" +
			"Proton's.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Pass.Domains(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[passsvc.Domain]{
				Noun:    "domains",
				Columns: domainColumns(),
			}, rows)
		}),
	}
	held.Register(c, "domains",
		kit.Key[passsvc.Domain]{Name: "domain", Less: func(a, b passsvc.Domain) int { return kit.Fold(a.Domain, b.Domain) }},
	)
	return c
}

func domainColumns() []ui.Column[passsvc.Domain] {
	return []ui.Column[passsvc.Domain]{
		{Header: "DOMAIN", Flex: true, Handle: true, Cell: func(d passsvc.Domain) string { return d.Domain }},
		{Header: "DEFAULT", Cell: func(d passsvc.Domain) string { return yesNo(d.Default) }},
		{Header: "CUSTOM", Cell: func(d passsvc.Domain) string { return yesNo(d.Custom) }},
		{Header: "STATUS", Cell: func(d passsvc.Domain) string { return d.Status }},
		{Header: "ALIASES", Right: true, Cell: func(d passsvc.Domain) string {
			if !d.Custom {
				return ""
			}
			return strconv.Itoa(d.Aliases)
		}},
	}
}

func domainsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show a domain, with the DNS entries a custom one needs",
		Long: "Show a domain, with the DNS entries a custom one needs.\n\n" +
			"Reads a custom domain's DNS again each time it runs, so what it reports\n" +
			"is what Proton can see now. Each check reads ok, missing or wrong, and a\n" +
			"wrong one says what was found instead. Ownership and MX are what a domain\n" +
			"needs to carry aliases; SPF, DKIM and DMARC keep its mail out of spam.\n\n" +
			"The catch-all, display name and random prefix appear once the domain is\n" +
			"verified.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Domain", Value: d.Domain, Handle: true},
				{Label: "Default", Value: yesNo(d.Default)},
				{Label: "Custom", Value: yesNo(d.Custom)},
			}
			if !d.Custom {
				fields = append(fields, ui.Field{Label: "Premium", Value: yesNo(d.Premium)})
				return kit.Show(c, ui.RecordSpec{Object: d, Fields: fields})
			}
			detail, err := c.App.Pass.DomainCheck(c.Ctx, d)
			if err != nil {
				return err
			}
			fields = append(fields,
				ui.Field{Label: "Status", Value: detail.Status, Always: true},
				ui.Field{Label: "Aliases", Value: strconv.Itoa(detail.Aliases), Always: true},
				ui.Field{Label: "Created", Value: units.Time(detail.Created)},
			)
			if s := detail.Settings; s != nil {
				fields = append(fields,
					ui.Field{Label: "Catch-all", Value: catchAllText(*s), Always: true},
					ui.Field{Label: "Display Name", Value: s.DisplayName},
					ui.Field{Label: "Random Prefix", Value: yesNo(s.RandomPrefix)},
				)
			}
			for _, check := range detail.DNS {
				fields = append(fields, ui.Field{
					Label: checkLabels[check.Name], Value: check.Text(), Always: true,
				})
			}
			return kit.Show(c, ui.RecordSpec{
				Object: detail,
				Fields: append(fields, ui.Field{Label: "ID", Value: detail.Ref(), ID: true}),
			})
		}),
	}
}

// checkLabels is how each of Proton's five checks is named on screen.
var checkLabels = map[string]string{
	"ownership": "Ownership",
	"mx":        "MX",
	"spf":       "SPF",
	"dkim":      "DKIM",
	"dmarc":     "DMARC",
}

// catchAllText is where mail to a name no alias has claimed ends up: the
// mailboxes it makes an alias into, or nowhere.
func catchAllText(s passsvc.DomainSettings) string {
	switch {
	case !s.CatchAll:
		return "(none)"
	case len(s.Mailboxes) == 0:
		return "on"
	}
	return strings.Join(s.Mailboxes, ", ")
}

func domainsCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create DOMAIN",
		Short: "Add a domain of your own for aliases",
		Long: "Add a domain of your own for aliases to be made on.\n\n" +
			"DOMAIN is a domain you own, written as example.com. Adding one needs a paid\n" +
			"Pass plan.\n\n" +
			"The domain carries no alias until its DNS entries are in place: first the\n" +
			"one that proves it is yours, then the MX entries that bring its mail here.\n" +
			"`get` shows every entry the domain needs.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			name, err := dns.Name(c.Args[0])
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "domains", Name: name,
			}, func() (string, error) {
				d, err := c.App.Pass.DomainCreate(c.Ctx, name)
				if err != nil {
					return "", err
				}
				c.Note("Add this entry at %s, then run `%s pass settings domains get %s`:\n%s",
					d.Domain.Domain, kit.Program, d.Domain.Domain, d.DNS[0].Text())
				return d.Ref(), nil
			})
		}),
	}
}

func domainsUpdateCmd() *cobra.Command {
	var catchAll []string
	var displayName string
	var makeDefault, clearDisplayName, randomPrefix bool
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change a domain: the default, catch-all or names",
		Long: "Change a domain.\n\n" +
			"--default makes new aliases take this domain when nothing names another,\n" +
			"and --default=false leaves none chosen. Every other flag is a custom\n" +
			"domain's, and waits until the domain is verified.\n\n" +
			"--catch-all names the mailboxes that take mail sent to any name at the\n" +
			"domain; an alias is made for the name as the first mail arrives. One flag\n" +
			"per mailbox, and `--catch-all none` turns it off.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if c.Changed("display-name") && clearDisplayName {
				return kit.Fail("--display-name and --clear-display-name contradict each other.")
			}
			if c.Changed("display-name") && strings.TrimSpace(displayName) == "" {
				return kit.Fail("--display-name needs a name.").Hint("--clear-display-name takes it off")
			}
			settings := c.Changed("catch-all") || c.Changed("display-name") ||
				clearDisplayName || c.Changed("random-prefix")
			if !c.Changed("default") && !settings {
				return kit.Fail("Nothing to change.").
					Hint("--default", "--catch-all me@proton.me", "--display-name \"Jane R\"", "--random-prefix")
			}
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if settings && !d.Custom {
				return kit.Fail("%s is one of Proton's domains, and has no settings of yours to change.", d.Domain)
			}
			if settings && !d.Verified() {
				return errs.Naming(d.Domain, kit.Fail("%s is not verified yet, so it has no settings to change.", d.Domain).
					Hint(kit.Program+" pass settings domains get "+d.Domain+" shows the entry it is waiting on"))
			}
			var steps []func() error
			var details []string
			if c.Changed("default") {
				step, detail, err := defaultStep(c, d, makeDefault)
				if err != nil {
					return err
				}
				steps, details = append(steps, step), append(details, detail)
			}
			if c.Changed("catch-all") {
				step, detail, err := catchAllStep(c, d, catchAll)
				if err != nil {
					return err
				}
				steps, details = append(steps, step), append(details, detail)
			}
			if c.Changed("display-name") || clearDisplayName {
				name := strings.TrimSpace(displayName)
				detail := "with no display name"
				if name != "" {
					detail = "shown as " + name
				}
				steps = append(steps, func() error { return c.App.Pass.DomainDisplayName(c.Ctx, d.ID, name) })
				details = append(details, detail)
			}
			if c.Changed("random-prefix") {
				detail := "with a random prefix on new aliases"
				if !randomPrefix {
					detail = "with no random prefix on new aliases"
				}
				steps = append(steps, func() error { return c.App.Pass.DomainRandomPrefix(c.Ctx, d.ID, randomPrefix) })
				details = append(details, detail)
			}
			var ids []string
			if d.Custom {
				ids = []string{d.Ref()}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "domains", Count: 1, Name: d.Domain,
				IDs: ids, Detail: "- " + strings.Join(details, ", "),
			}, func() error {
				for _, step := range steps {
					if err := step(); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().BoolVar(&makeDefault, "default", false, "Make new aliases take this domain; --default=false chooses none")
	c.Flags().StringArrayVar(&catchAll, "catch-all", nil,
		"Mailbox that takes mail sent to any name at the domain (repeatable), or none")
	c.Flags().StringVar(&displayName, "display-name", "", "Name recipients see on mail from the domain's aliases")
	c.Flags().BoolVar(&clearDisplayName, "clear-display-name", false, "Remove the display name")
	c.Flags().BoolVar(&randomPrefix, "random-prefix", false, "Put a random word in front of a new alias on the domain")
	return c
}

// defaultStep is the change --default asks for: this domain for new aliases,
// or none at all - which is only a change to the domain that is the default.
func defaultStep(c *kit.Invocation, d passsvc.Domain, on bool) (func() error, string, error) {
	if on {
		domain := d.Domain
		return func() error { return c.App.Pass.DomainSetDefault(c.Ctx, &domain) },
			"the default for new aliases", nil
	}
	if !d.Default {
		return nil, "", errs.Naming(d.Domain, kit.Fail("%s is not the default domain.", d.Domain).
			Hint(kit.Program+" pass settings domains list shows which is"))
	}
	return func() error { return c.App.Pass.DomainSetDefault(c.Ctx, nil) },
		"no longer the default for new aliases", nil
}

// catchAllStep is the change --catch-all asks for: the mailboxes stray mail
// makes an alias into, resolved before anything is sent, or none.
func catchAllStep(c *kit.Invocation, d passsvc.Domain, mailboxes []string) (func() error, string, error) {
	if len(mailboxes) == 1 && strings.EqualFold(strings.TrimSpace(mailboxes[0]), kit.None) {
		return func() error { return c.App.Pass.DomainCatchAll(c.Ctx, d.ID, nil) },
			"catching nothing", nil
	}
	ids := make([]int, 0, len(mailboxes))
	emails := make([]string, 0, len(mailboxes))
	for _, ref := range mailboxes {
		box, err := c.App.Pass.MailboxByEmail(c.Ctx, ref)
		if err != nil {
			return nil, "", err
		}
		ids = append(ids, box.ID)
		emails = append(emails, box.Email)
	}
	return func() error { return c.App.Pass.DomainCatchAll(c.Ctx, d.ID, ids) },
		"catching stray mail at " + strings.Join(emails, ", "), nil
}

func domainsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete REF",
		Short: "Remove a domain of your own",
		Long: "Remove a domain of your own.\n\n" +
			"Every alias made on the domain is deleted with it, and an alias address\n" +
			"cannot be brought back. Adding the domain again needs its DNS entries\n" +
			"verified from scratch.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if !d.Custom {
				return kit.Fail("%s is one of Proton's domains, and is not yours to remove.", d.Domain)
			}
			if d.Aliases > 0 {
				c.Note("This deletes the %s on %s with it, and nothing brings an alias back.",
					kit.Quantity(d.Aliases, "aliases"), d.Domain)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "domains", Count: 1,
				Name: d.Domain, IDs: []string{d.Ref()},
			}, func() error {
				return c.App.Pass.DomainDelete(c.Ctx, d.ID)
			})
		}),
	}
}

func domainList(c *kit.Invocation) *kit.Lookup[passsvc.Domain] {
	return &kit.Lookup[passsvc.Domain]{
		Kind: "domain",
		Load: func(ctx context.Context) ([]passsvc.Domain, error) {
			return c.App.Pass.Domains(ctx)
		},
		ID:     passsvc.Domain.Ref,
		Handle: func(d passsvc.Domain) string { return d.Domain },
	}
}
