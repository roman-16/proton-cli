package mail

import (
	"context"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// A custom domain is the account's own name on the internet, and the DNS
// entries that let Proton carry its mail. This mirrors Proton's "Domain names"
// settings page, catch-all address and all.

func domainsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "domains",
		Short: "Custom domains, and where stray mail goes",
		Long: "Custom domains, and where stray mail goes.\n\n" +
			"A custom domain sends and receives under your own name. Adding one needs a\n" +
			"paid Mail plan, and how many you may have depends on it.\n\n" +
			"A domain carries no mail until its DNS entries are in place. `get` shows\n" +
			"them and says which ones Proton can see.",
	}
	c.AddCommand(
		domainsListCmd(), domainsGetCmd(), domainsCreateCmd(),
		domainsUpdateCmd(), domainsDeleteCmd(),
	)
	return c
}

func domainsListCmd() *cobra.Command {
	var held kit.Held[mailsvc.Domain]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the custom domains on the account",
		Long: "List the custom domains on the account.\n\n" +
			"STATUS is active, unverified or warning. DNS is ok, or the checks that are\n" +
			"not passing. Both come from the last check Proton made; `get` checks again.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := c.App.Mail.DomainsList(c.Ctx)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.Domain]{
				Noun:    "domains",
				Columns: domainColumns(),
			}, rows)
		}),
	}
	held.Register(c, "domains",
		kit.Key[mailsvc.Domain]{Name: "domain", Less: func(a, b mailsvc.Domain) int { return kit.Fold(a.Domain, b.Domain) }},
	)
	return c
}

func domainsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show a domain's DNS entries and their status",
		Long: "Show a domain's DNS entries and their status.\n\n" +
			"Reads the domain's DNS again each time it runs, so what it reports is what\n" +
			"Proton can see now.\n\n" +
			"Each check reads ok, missing, wrong, duplicate, wrong-priority, backup,\n" +
			"error, warning, delegated or relaxed. Every entry has to stay in place:\n" +
			"removing the verification entry gives the domain up.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			fresh, err := c.App.Mail.DomainRefresh(c.Ctx, d)
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Domain", Value: fresh.Domain, Handle: true},
				{Label: "Status", Value: fresh.Status, Always: true},
				{Label: "Addresses", Value: strconv.Itoa(fresh.Addresses), Always: true},
				{Label: "Catch-all", Value: catchAllText(fresh), Always: true},
				{Label: "Checked", Value: units.Time(fresh.Checked)},
			}
			for _, g := range fresh.DNS {
				fields = append(fields, ui.Field{
					Label: groupLabels[g.Name], Value: groupText(g), Always: true,
				})
			}
			return kit.Show(c, ui.RecordSpec{
				Object: fresh,
				Fields: append(fields, ui.Field{Label: "ID", Value: fresh.ID, ID: true}),
			})
		}),
	}
}

func domainsCreateCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "create DOMAIN",
		Short: "Add a custom domain to the account",
		Long: "Add a custom domain to the account.\n\n" +
			"DOMAIN is a domain you own, written as example.com. Adding one needs a paid\n" +
			"Mail plan.\n\n" +
			"The domain carries no mail until its verification entry is in your DNS, and\n" +
			"no address can be added on it until Proton has seen that entry. `get` shows\n" +
			"every entry the domain needs.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			name, err := domainName(c.Args[0])
			if err != nil {
				return err
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "domains", Name: name,
			}, func() (string, error) {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "add a domain")
				d, err := c.App.Mail.DomainCreate(ctx, name)
				if err != nil {
					return "", err
				}
				c.Note("Add this entry at %s, then run `%s mail settings domains get %s`:\n%s",
					d.Domain, kit.Program, d.Domain, groupText(d.DNS[0]))
				return d.ID, nil
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// domainsUpdateCmd writes the one field of a domain that is the account's to
// choose: which address takes mail sent to a name the domain has not got.
func domainsUpdateCmd() *cobra.Command {
	var catchAll string
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Set the address that catches stray mail",
		Long: "Set the address that catches stray mail.\n\n" +
			"Mail sent to a name that does not exist at the domain arrives at the\n" +
			"catch-all address instead of being refused.\n\n" +
			"--catch-all takes one of your addresses on that domain. One address catches\n" +
			"at a time, so naming another moves it, and `--catch-all none` turns it off.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			if !c.Changed("catch-all") {
				return kit.Fail("Nothing to change.").
					Hint("--catch-all work@example.com, or --catch-all none to turn it off")
			}
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			var addressID *string
			detail := "to catch nothing"
			if !strings.EqualFold(strings.TrimSpace(catchAll), kit.None) {
				a, err := c.App.Mail.ResolveAddress(c.Ctx, catchAll)
				if err != nil {
					return err
				}
				if err := onDomain(c, d, *a); err != nil {
					return err
				}
				addressID, detail = &a.ID, "to catch stray mail at "+a.Email
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "domains", Count: 1,
				Name: d.Domain, IDs: []string{d.ID}, Detail: detail,
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "set a catch-all address")
				return c.App.Mail.DomainCatchAll(ctx, d.ID, addressID)
			})
		}),
	}
	c.Flags().StringVar(&catchAll, "catch-all", "",
		"Address that takes mail sent to a name the domain has not got, or none")
	reauth.Declare(c)
	return c
}

func domainsDeleteCmd() *cobra.Command {
	var reauth kit.Reauth
	c := &cobra.Command{
		Use:   "delete REF",
		Short: "Remove a custom domain",
		Long: "Remove a custom domain.\n\n" +
			"Every address on the domain stops sending and receiving, and the mail they\n" +
			"hold stays. Adding the domain again needs its DNS entries verified from\n" +
			"scratch.\n\n" +
			"Asks for your password even when you are signed in. With no terminal to ask,\n" +
			"pass --password-file, which takes - for stdin.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if err := reauth.Supply(c); err != nil {
				return err
			}
			d, err := domainList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			if d.Addresses > 0 {
				c.Note("This disables %s on %s. The mail already there stays.",
					kit.Quantity(d.Addresses, "addresses"), d.Domain)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Deleted, Kind: "domains", Count: 1,
				Name: d.Domain, IDs: []string{d.ID},
			}, func() error {
				// Nothing here arranges the elevation: the client does it when the
				// server asks, and drops the scope again afterwards. All this owes
				// the user is a reason for the prompt.
				ctx := app.WithScopeReason(c.Ctx, "delete a domain")
				return c.App.Mail.DomainDelete(ctx, d.ID)
			})
		}),
	}
	reauth.Declare(c)
	return c
}

// groupLabels is how each of Proton's five checks is named on screen.
var groupLabels = map[string]string{
	"verification": "Verification",
	"mx":           "MX",
	"spf":          "SPF",
	"dkim":         "DKIM",
	"dmarc":        "DMARC",
}

// groupText is one check's verdict with the entries it judges written under it,
// aligned with one another.
//
// A record field keeps a value whole however wide it is, which is what an entry
// needs: a DKIM value is long, exact, and copied by hand.
func groupText(g mailsvc.RecordGroup) string {
	rows := make([][]string, 0, len(g.Records))
	var widths []int
	for _, r := range g.Records {
		cells := recordCells(r)
		for len(widths) < len(cells) {
			widths = append(widths, 0)
		}
		for i, cell := range cells {
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
		rows = append(rows, cells)
	}
	lines := []string{g.Status}
	for _, cells := range rows {
		var line strings.Builder
		for i, cell := range cells {
			line.WriteString(cell)
			line.WriteString(strings.Repeat(" ", widths[i]-len(cell)+2))
		}
		lines = append(lines, strings.TrimRight(line.String(), " "))
	}
	return strings.Join(lines, "\n")
}

// recordCells is one entry as a zone file writes it: what to add, where, at what
// priority where a priority is part of it, and the value itself last.
func recordCells(r mailsvc.Record) []string {
	if r.Priority > 0 {
		return []string{r.Type, r.Host, strconv.Itoa(r.Priority), r.Value}
	}
	return []string{r.Type, r.Host, r.Value}
}

func domainColumns() []ui.Column[mailsvc.Domain] {
	return []ui.Column[mailsvc.Domain]{
		{Header: "ID", ID: true, Cell: func(d mailsvc.Domain) string { return d.ID }},
		{Header: "DOMAIN", Flex: true, Handle: true, Cell: func(d mailsvc.Domain) string { return d.Domain }},
		{Header: "STATUS", Cell: func(d mailsvc.Domain) string { return d.Status }},
		{Header: "DNS", Cell: dnsSummary},
		{Header: "ADDRESSES", Right: true, Cell: func(d mailsvc.Domain) string {
			return strconv.Itoa(d.Addresses)
		}},
		{Header: "CATCH-ALL", Flex: true, Cell: func(d mailsvc.Domain) string { return d.CatchAll }},
	}
}

// dnsSummary is the whole of what a listing can say about five checks in one
// cell: that they pass, or which of them do not.
func dnsSummary(d mailsvc.Domain) string {
	failing := d.Failing()
	if len(failing) == 0 {
		return mailsvc.Ok
	}
	return strings.Join(failing, ", ")
}

func catchAllText(d mailsvc.Domain) string {
	if d.CatchAll == "" {
		return "(none)"
	}
	return d.CatchAll
}

func domainList(c *kit.Invocation) *kit.Lookup[mailsvc.Domain] {
	return &kit.Lookup[mailsvc.Domain]{
		Kind: "domain",
		Load: func(ctx context.Context) ([]mailsvc.Domain, error) {
			return c.App.Mail.DomainsList(ctx)
		},
		ID:     func(d mailsvc.Domain) string { return d.ID },
		Handle: func(d mailsvc.Domain) string { return d.Domain },
	}
}

// onDomain admits an address the named domain can catch mail at, and names the
// ones that would have worked.
//
// The refusal says "that domain" rather than naming it: a custom domain is the
// account's own name on the internet and has no shape internal/redact can find
// again, and the command line the reader just typed already names it. The
// addresses do carry it, and an address is a shape redact knows.
func onDomain(c *kit.Invocation, d mailsvc.Domain, a mailsvc.Address) error {
	if a.DomainID == d.ID {
		return nil
	}
	addrs, err := c.App.Mail.AddressesList(c.Ctx)
	if err != nil {
		return err
	}
	var on []string
	for _, other := range addrs {
		if other.DomainID == d.ID {
			on = append(on, other.Email)
		}
	}
	problem := errs.Problemf("%s is not an address on that domain.", a.Email)
	if len(on) == 0 {
		return problem.Hint("that domain has no addresses yet")
	}
	return problem.Hint(strings.Join(on, ", "))
}

// domainName reads DOMAIN as Proton files it, which is a bare name with no
// scheme, no address around it and no path after it. It is judged from the
// command line, so a malformed one costs nothing.
func domainName(arg string) (string, error) {
	name := strings.TrimSpace(arg)
	bad := name == "" || !strings.Contains(name, ".") ||
		strings.ContainsAny(name, "@ \t/:") ||
		strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".")
	if bad {
		return "", kit.Fail("%q is not a domain name.", arg).
			Hint("write it on its own, as example.com")
	}
	return strings.ToLower(name), nil
}
