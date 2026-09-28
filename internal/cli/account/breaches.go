package account

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/breach"
	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	acctsvc "github.com/roman-16/proton-cli/internal/service/account"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

func breachesCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "breaches",
		Short: "Breaches your addresses were found in",
		Long: "Breaches Dark Web Monitoring found your addresses in.\n\n" +
			"A breach is new until you read it in a Proton app, open until you resolve it,\n" +
			"and resolved until you reopen it. The detail of each breach needs a plan that\n" +
			"includes Dark Web Monitoring.\n\n" +
			"The addresses Pass Monitor watches, aliases and addresses you added included,\n" +
			"are under `" + kit.Program + " pass breaches`.",
	}
	c.AddCommand(breachesListCmd(), breachesGetCmd(),
		breachStateCmd("resolve", "Mark breaches as resolved", ui.Resolved, breach.StateResolved),
		breachStateCmd("reopen", "Mark resolved breaches as open again", ui.Reopened, breach.StateOpen),
		switchCmd("enable", "Turn Dark Web Monitoring on", true, darkWebMonitoring),
		switchCmd("disable", "Turn Dark Web Monitoring off", false, darkWebMonitoring))
	return c
}

func breachColumns() []ui.Column[breach.Breach] {
	return []ui.Column[breach.Breach]{
		{Header: "ID", ID: true, Cell: func(b breach.Breach) string { return b.ID }},
		{Header: "NAME", Handle: true, Cell: func(b breach.Breach) string { return b.Name }},
		{Header: "EMAIL", Cell: func(b breach.Breach) string { return b.Email }},
		{Header: "FOUND", Cell: func(b breach.Breach) string { return units.Time(b.Found) }},
		{Header: "SEVERITY", Cell: func(b breach.Breach) string { return b.Severity }},
		{Header: "EXPOSED", Flex: true, Cell: func(b breach.Breach) string { return strings.Join(b.Exposed, ", ") }},
		{Header: "STATE", Cell: func(b breach.Breach) string { return b.State }},
	}
}

func openFirst(a, b breach.Breach) int {
	if a.Resolved() != b.Resolved() {
		if a.Resolved() {
			return 1
		}
		return -1
	}
	return cmp.Compare(b.Found, a.Found)
}

type breachLookup struct {
	*kit.Lookup[breach.Breach]
	report breach.Report
}

func breachList(c *kit.Invocation) *breachLookup {
	l := &breachLookup{}
	l.Lookup = &kit.Lookup[breach.Breach]{
		Kind: "breach",
		Load: func(ctx context.Context) ([]breach.Breach, error) {
			report, err := c.App.Account.Breaches(ctx)
			if err != nil {
				return nil, err
			}
			l.report = report
			slices.SortStableFunc(report.Breaches, openFirst)
			return report.Breaches, nil
		},
		ID:     func(b breach.Breach) string { return b.ID },
		Handle: func(b breach.Breach) string { return b.Name },
	}
	return l
}

func withheld(c *kit.Invocation, report breach.Report) {
	if report.Withheld == 0 {
		return
	}
	c.Warn("Proton found %s and is withholding the detail of %d on this plan.",
		ui.Quantity(len(report.Breaches)+report.Withheld, "breaches"), report.Withheld)
}

func breachesListCmd() *cobra.Command {
	var held kit.Held[breach.Breach]
	c := &cobra.Command{
		Use:   "list",
		Short: "List the breaches Proton found your addresses in",
		Long: "List the breaches Proton found your addresses in, open ones first.\n\n" +
			"Without a plan that includes Dark Web Monitoring, Proton names a few of them\n" +
			"and withholds the rest, and the answer says how many.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			lookup := breachList(c)
			var (
				rows     []breach.Breach
				security *acctsvc.Security
			)
			if err := fetch.Together(c.Ctx,
				func(ctx context.Context) error {
					var err error
					rows, err = lookup.Rows(ctx)
					return err
				},
				func(ctx context.Context) error {
					var err error
					security, err = c.App.Account.Security(ctx)
					return err
				},
			); err != nil {
				return err
			}
			if err := held.Answer(c, ui.TableSpec[breach.Breach]{
				Noun: "breaches", Columns: breachColumns(),
			}, rows); err != nil {
				return err
			}
			withheld(c, lookup.report)
			if d := security.DarkWebMonitoring; d.Eligible && !d.On {
				c.UI().Hint("Dark Web Monitoring is off, so Proton is not looking for new breaches. " +
					"Turn it on with `" + kit.Program + " account breaches enable`.")
			}
			return nil
		}),
	}
	held.Register(c, "breaches")
	return c
}

func breachesGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one breach, and what it exposed",
		Long: "Show one breach, and what it exposed.\n\n" +
			"Names what leaked, the last few characters of the password if one leaked in\n" +
			"the clear, and what Proton recommends doing about it.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lookup := breachList(c)
			b, err := lookup.Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			withheld(c, lookup.report)
			var size string
			if b.Size > 0 {
				size = strconv.Itoa(b.Size)
			}
			var found, happened string
			if b.Found != 0 {
				found = units.Time(b.Found)
			}
			if b.Published != 0 {
				happened = units.Time(b.Published)
			}
			return kit.Show(c, ui.RecordSpec{
				Object: b,
				Fields: []ui.Field{
					{Label: "Name", Value: b.Name, Handle: true},
					{Label: "Email", Value: b.Email},
					{Label: "Found", Value: found},
					{Label: "Happened", Value: happened},
					{Label: "Severity", Value: b.Severity},
					{Label: "Records", Value: size},
					{Label: "Source", Value: b.Source},
					{Label: "Exposed", Value: strings.Join(b.Exposed, ", ")},
					{Label: "Password ends", Value: b.PasswordTail},
					{Label: "To do", Value: strings.Join(b.Actions, "\n")},
					{Label: "State", Value: b.State},
					{Label: "ID", Value: b.ID, ID: true},
				},
			})
		}),
	}
}

func breachStateCmd(use, short string, action ui.Action, state string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lookup := breachList(c)
			sel, err := kit.SelectFrom(c, "breaches", breachColumns(), lookup.Lookup)
			if err != nil {
				return err
			}
			if !lookup.report.Eligible {
				return kit.Fail("Your plan does not include Dark Web Monitoring.")
			}
			for _, b := range sel.Rows {
				switch {
				case state == breach.StateResolved && b.Resolved():
					return errs.Naming(b.Name, kit.Fail("%s is already resolved.", b.Name))
				case state == breach.StateOpen && !b.Resolved():
					return errs.Naming(b.Name, kit.Fail("%s is not resolved.", b.Name))
				}
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "breaches", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(b breach.Breach) string { return b.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, b := range sel.Rows {
					if err := c.App.Account.SetBreachState(c.Ctx, b.ID, state); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
