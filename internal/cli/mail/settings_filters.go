package mail

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/proton"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Server-side Sieve filters, the same ones the web client creates.

func filtersCmd() *cobra.Command {
	c := &cobra.Command{Use: "filters", Short: "Server-side Sieve filters"}
	c.AddCommand(
		filtersListCmd(), filtersGetCmd(), filtersCreateCmd(), filtersUpdateCmd(),
		filterVerbCmd("delete", "Delete filters", ui.Deleted,
			func(c *kit.Invocation, id string) error { return c.App.Mail.FilterDelete(c.Ctx, id) }),
		filterVerbCmd("enable", "Enable filters", ui.Enabled,
			func(c *kit.Invocation, id string) error { return c.App.Mail.FilterEnable(c.Ctx, id) }),
		filterVerbCmd("disable", "Disable filters", ui.Disabled,
			func(c *kit.Invocation, id string) error { return c.App.Mail.FilterDisable(c.Ctx, id) }),
		filtersApplyCmd(), filtersReorderCmd(),
	)
	return c
}

// Apply runs filters over mail that is already here.
//
// A filter ordinarily acts once, as mail arrives, so a rule written today does
// nothing about what came yesterday. This is the catching-up.
func filtersApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply [REF...]",
		Short: "Run filters over mail that is already in the mailbox",
		Long: "Run filters over mail that is already in the mailbox.\n\n" +
			"A filter normally runs once, as mail arrives, so a rule written today does\n" +
			"nothing about yesterday's mail.\n\n" +
			"With no filter named, every enabled filter runs.",
		Args: cobra.ArbitraryArgs,
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			var ids []string
			name := "every enabled filter"
			if len(c.Args) > 0 {
				sel, err := kit.SelectFrom(c, "filters", filterColumns(), filterList(c))
				if err != nil {
					return err
				}
				ids = sel.IDs
				name = kit.Sole(sel.Rows, func(f mailsvc.Filter) string { return f.Name })
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Applied, Kind: "filters", Count: max(len(ids), 1),
				Name: name, Detail: "to the mail already here",
			}, func() error {
				return c.App.Mail.FilterApply(c.Ctx, ids)
			})
		}),
	}
}

// Order decides the outcome, so it is set as a whole rather than nudged.
func filtersReorderCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reorder REF...",
		Short: "Set the order filters run in",
		Long: "Set the order filters run in.\n\n" +
			"The first rule to file a message wins, so the order decides where mail\n" +
			"lands. Name every filter, in the order you want them. This replaces the\n" +
			"whole order; a partial one is refused.",
		Args: cobra.MinimumNArgs(2),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			all, err := filterList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			sel, err := kit.SelectFrom(c, "filters", filterColumns(), filterList(c))
			if err != nil {
				return err
			}
			// Naming some of them would leave the rest in an order nobody chose,
			// so the command line has to account for all of them.
			if len(sel.IDs) != len(all) {
				return kit.Fail("You have %d filters but named %d.", len(all), len(sel.IDs)).
					Hint("name every filter, in the order you want them.",
						"`proton mail settings filters list` shows them.")
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Reordered, Kind: "filters", Count: len(sel.IDs), IDs: sel.IDs,
				Preview: sel.Preview(),
			}, func() error {
				return c.App.Mail.FilterReorder(c.Ctx, sel.IDs)
			})
		}),
	}
}

func filterColumns() []ui.Column[mailsvc.Filter] {
	return []ui.Column[mailsvc.Filter]{
		{Header: "ID", ID: true, Cell: func(f mailsvc.Filter) string { return f.ID }},
		{Header: "NAME", Flex: true, Cell: func(f mailsvc.Filter) string { return f.Name }},
		{Header: "ENABLED", Cell: func(f mailsvc.Filter) string { return yesNo(f.Status == 1) }},
		{Header: "VERSION", Right: true, Cell: func(f mailsvc.Filter) string {
			return strconv.Itoa(f.Version)
		}},
	}
}

func filterList(c *kit.Invocation) *kit.Lookup[mailsvc.Filter] {
	return &kit.Lookup[mailsvc.Filter]{
		Kind:   "filter",
		Load:   func(ctx context.Context) ([]mailsvc.Filter, error) { return c.App.Mail.FiltersList(ctx) },
		ID:     func(f mailsvc.Filter) string { return f.ID },
		Handle: func(f mailsvc.Filter) string { return f.Name },
	}
}

func filtersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your filters",
		Args:  cobra.NoArgs,
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			rows, err := filterList(c).Rows(c.Ctx)
			if err != nil {
				return err
			}
			return kit.List(c, ui.TableSpec[mailsvc.Filter]{
				Noun: "filters", Columns: filterColumns(),
				Total: ui.Unknown, Page: ui.Unpaged,
			}, rows, func(f mailsvc.Filter) []string { return []string{f.ID} })
		}),
	}
}

func filtersCreateCmd() *cobra.Command {
	var name, sieve, moveTo string
	var conditions, labels []string
	var disabled, markRead, star bool
	match := &kit.Enum{
		Name: "match", Usage: "Whether every condition must hold, or any one of them",
		Values: []string{"all", "any"}, Default: "all",
	}
	c := &cobra.Command{
		Use:   "create",
		Short: "Create a filter",
		Long: "Create a filter.\n\n" +
			"Describe it with --if and the actions below, and Proton writes the Sieve.\n" +
			"A condition reads FIELD [not] COMPARATOR VALUE:\n\n" +
			"  field       subject, sender, recipient, attachments\n" +
			"  comparator  contains, is, starts, ends, matches\n\n" +
			"`is` wants the whole value; `matches` takes * and ? as wildcards. An\n" +
			"attachments condition takes no value - it asks whether there is one.\n\n" +
			"--sieve takes a script you wrote yourself instead.",
		Args: cobra.NoArgs,
		RunE: kit.Run([]kit.Step{func(*kit.Invocation) error {
			return checkRule(sieve, conditions, moveTo, labels, markRead, star)
		}}, func(c *kit.Invocation) error {
			script, err := kit.ReadTextArg(c, sieve, "--sieve")
			if err != nil {
				return err
			}
			if name == "" {
				return kit.Fail("A filter needs a name.").
					Hint("--name \"Archive invoices\"")
			}
			statement, err := match.Value()
			if err != nil {
				return err
			}
			create := func() (string, error) {
				if script != "" {
					return c.App.Mail.FilterCreate(c.Ctx, name, script)
				}
				parsed, err := parseConditions(conditions)
				if err != nil {
					return "", err
				}
				return c.App.Mail.FilterCreateRule(c.Ctx, name, mailsvc.FilterRule{
					Conditions: parsed, MatchAny: statement == "any",
					MoveTo: moveTo, Labels: labels, MarkRead: markRead, Star: star,
				})
			}
			return kit.Create(c, ui.ResultSpec{
				Action: ui.Created, Kind: "filters", Name: name,
			}, func() (string, error) {
				id, err := create()
				if proton.AtFilterLimit(err) {
					return "", kit.Fail("Proton will not have another filter running.").
						Hint("it was created, but left turned off.",
							"`proton mail settings filters list` shows it.",
							"turn another one off to make room for it.")
				}
				if err != nil || !disabled {
					return id, err
				}
				return id, c.App.Mail.FilterDisable(c.Ctx, id)
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "Name for the new filter")
	c.Flags().StringArrayVar(&conditions, "if", nil,
		"A condition matching mail must meet, as FIELD [not] COMPARATOR VALUE (repeatable)")
	match.Register(c)
	c.Flags().StringVar(&moveTo, "move-to", "",
		"Move matching mail into this folder (archive, inbox, spam, trash, or one of yours)")
	c.Flags().StringArrayVar(&labels, "label", nil, "Apply this label to matching mail (repeatable)")
	c.Flags().BoolVar(&markRead, "mark-read", false, "Mark matching mail as read")
	c.Flags().BoolVar(&star, "star", false, "Star matching mail")
	c.Flags().StringVar(&sieve, "sieve", "", "Sieve script (- reads stdin)")
	c.Flags().BoolVar(&disabled, "disabled", false, "Create it without turning it on")
	c.MarkFlagsMutuallyExclusive("if", "sieve")
	c.MarkFlagsMutuallyExclusive("sieve", "move-to")
	c.MarkFlagsMutuallyExclusive("sieve", "label")
	return c
}

// checkRule judges a described filter before anything signs in. Everything here
// is answerable from the command line alone, and a filter that is wrong is worth
// hearing about before it is asked for.
func checkRule(sieve string, conditions []string, moveTo string, labels []string, markRead, star bool) error {
	if sieve != "" {
		return nil
	}
	if len(conditions) == 0 {
		return kit.Fail("A filter needs something to match.").
			Hint(`--if "subject contains invoice"`,
				"or --sieve, for a script you have written yourself.")
	}
	if _, err := parseConditions(conditions); err != nil {
		return err
	}
	// A filter that matches and then does nothing is one Proton will accept and
	// nobody will ever notice not working.
	if moveTo == "" && len(labels) == 0 && !markRead && !star {
		return kit.Fail("That filter matches mail but does nothing with it.").
			Hint("--move-to Archive", "--label Receipts", "--mark-read", "--star")
	}
	return nil
}

// parseConditions reads the conditions as they were typed.
//
// The grammar is FIELD [not] COMPARATOR VALUE, with the value being the rest of
// the line so it may contain spaces. `not` is a word rather than a punctuation
// mark on the comparator, because a shell would eat the punctuation.
func parseConditions(conditions []string) ([]mailsvc.FilterCondition, error) {
	out := make([]mailsvc.FilterCondition, 0, len(conditions))
	for _, raw := range conditions {
		parsed, err := parseCondition(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed)
	}
	return out, nil
}

func parseCondition(raw string) (mailsvc.FilterCondition, error) {
	var zero mailsvc.FilterCondition
	field, rest := nextWord(raw)
	word, value := nextWord(rest)
	negate := word == "not"
	if negate {
		word, value = nextWord(value)
	}
	cond := mailsvc.FilterCondition{
		Field: field, Comparator: word, Value: strings.TrimSpace(value), Negate: negate,
	}
	if !slices.Contains(mailsvc.FilterFields, cond.Field) {
		return zero, kit.Fail("There is nothing called %q to match on in %q.", cond.Field, raw).
			Hint("a condition starts with " + strings.Join(mailsvc.FilterFields, ", ") + ".")
	}
	if !slices.Contains(mailsvc.FilterComparators, cond.Comparator) {
		return zero, kit.Fail("There is no way to match called %q in %q.", cond.Comparator, raw).
			Hint("the ways to match are "+strings.Join(mailsvc.FilterComparators, ", ")+".",
				`put "not" before it to invert it: --if "subject not contains invoice".`)
	}
	// An attachment is there or it is not; there is nothing to compare it to.
	if cond.Field == "attachments" {
		if cond.Value != "" {
			return zero, kit.Fail("An attachments condition takes no value, but %q has one.", raw).
				Hint(`--if "attachments contains" asks whether there is an attachment.`)
		}
		return cond, nil
	}
	if cond.Value == "" {
		return zero, kit.Fail("The condition %q says what to match on but not what to match.", raw).
			Hint(`--if "` + cond.Field + " " + cond.Comparator + ` invoice"`)
	}
	return cond, nil
}

// nextWord peels the first word off, and returns the rest with its leading
// spaces gone: what follows the comparator is a value, and may hold spaces.
func nextWord(s string) (string, string) {
	s = strings.TrimLeft(s, " \t")
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimLeft(s[i+1:], " \t")
}

func filtersUpdateCmd() *cobra.Command {
	var name, sieve, moveTo string
	var conditions, labels []string
	var markRead, star bool
	match := &kit.Enum{
		Name: "match", Usage: "Whether every condition must hold, or any one of them",
		Values: []string{"all", "any"}, Default: "all",
	}
	c := &cobra.Command{
		Use:   "update REF",
		Short: "Change what a filter is called, matches, or does",
		Long: "Change what a filter is called, matches, or does.\n\n" +
			"--if and the actions beside it replace the whole rule rather than adding to\n" +
			"it. The filter keeps its place in the order, and stays enabled or disabled\n" +
			"as it was.",
		Args: cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand, func(*kit.Invocation) error {
			if len(conditions) == 0 && sieve == "" {
				return nil
			}
			return checkRule(sieve, conditions, moveTo, labels, markRead, star)
		}}, func(c *kit.Invocation) error {
			script, err := kit.ReadTextArg(c, sieve, "--sieve")
			if err != nil {
				return err
			}
			if name == "" && script == "" && len(conditions) == 0 {
				return kit.Fail("Nothing to change.").
					Hint("--name renames it.",
						`--if "subject contains invoice" rewrites what it matches.`,
						"--sieve replaces it with a script of your own.")
			}
			statement, err := match.Value()
			if err != nil {
				return err
			}
			found, err := filterList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "filters", Count: 1,
				Name: cmp.Or(name, found.Name), IDs: []string{found.ID},
			}, func() error {
				if len(conditions) == 0 {
					return c.App.Mail.FilterUpdate(c.Ctx, found.ID, name, script)
				}
				parsed, err := parseConditions(conditions)
				if err != nil {
					return err
				}
				return c.App.Mail.FilterUpdateRule(c.Ctx, found.ID, name, mailsvc.FilterRule{
					Conditions: parsed, MatchAny: statement == "any",
					MoveTo: moveTo, Labels: labels, MarkRead: markRead, Star: star,
				})
			})
		}),
	}
	c.Flags().StringVar(&name, "name", "", "New name")
	c.Flags().StringArrayVar(&conditions, "if", nil,
		"A condition matching mail must meet, as FIELD [not] COMPARATOR VALUE (repeatable)")
	match.Register(c)
	c.Flags().StringVar(&moveTo, "move-to", "",
		"Move matching mail into this folder (archive, inbox, spam, trash, or one of yours)")
	c.Flags().StringArrayVar(&labels, "label", nil, "Apply this label to matching mail (repeatable)")
	c.Flags().BoolVar(&markRead, "mark-read", false, "Mark matching mail as read")
	c.Flags().BoolVar(&star, "star", false, "Star matching mail")
	c.Flags().StringVar(&sieve, "sieve", "", "New Sieve script (- reads stdin)")
	c.MarkFlagsMutuallyExclusive("if", "sieve")
	c.MarkFlagsMutuallyExclusive("sieve", "move-to")
	c.MarkFlagsMutuallyExclusive("sieve", "label")
	return c
}

// filtersGetCmd shows one filter: what it matches and does, and the script
// Proton runs. Without it there is no way to see a rule before replacing it.
func filtersGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show what a filter matches and does",
		Args:  cobra.ExactArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			found, err := filterList(c).Find(c.Ctx, c.Args[0])
			if err != nil {
				return err
			}
			detail, err := c.App.Mail.FilterGet(c.Ctx, found.ID)
			if err != nil {
				return err
			}
			fields := []ui.Field{
				{Label: "Name", Value: detail.Name},
				{Label: "Enabled", Value: yesNo(detail.Status == 1), Always: true},
			}
			fields = append(fields, ruleFields(detail)...)
			return kit.Show(c, ui.RecordSpec{
				Object: detail,
				Fields: append(fields, ui.Field{Label: "ID", Value: detail.ID, ID: true}),
			})
		}),
	}
}

// ruleFields says what a filter does, in the words that would write it again.
// A script saying more than those words can is shown as itself instead.
func ruleFields(detail *mailsvc.FilterDetail) []ui.Field {
	if detail.Rule == nil {
		return []ui.Field{{Label: "Script", Value: detail.Sieve, Always: true}}
	}
	rule := detail.Rule
	held := "all of"
	if rule.MatchAny {
		held = "any of"
	}
	fields := []ui.Field{{Label: "Matches", Value: held, Always: true}}
	for _, cond := range rule.Conditions {
		fields = append(fields, ui.Field{Label: "  if", Value: conditionWords(cond)})
	}
	if len(rule.Labels) > 0 {
		fields = append(fields, ui.Field{Label: "Files into", Value: strings.Join(rule.Labels, ", ")})
	}
	if rule.MarkRead {
		fields = append(fields, ui.Field{Label: "Marks read", Value: "yes"})
	}
	if rule.Star {
		fields = append(fields, ui.Field{Label: "Stars", Value: "yes"})
	}
	return fields
}

// conditionWords writes a condition the way --if takes one.
func conditionWords(cond mailsvc.FilterCondition) string {
	words := []string{cond.Field}
	if cond.Negate {
		words = append(words, "not")
	}
	words = append(words, cond.Comparator)
	if cond.Value != "" {
		words = append(words, cond.Value)
	}
	return strings.Join(words, " ")
}

// filterVerbCmd builds every verb that acts on named filters. They differ only
// in what they do to each one, so they share how the filters are found - which
// is what lets `delete Newsletters` work as well as `delete FILTER_ID`.
func filterVerbCmd(use, short string, action ui.Action, apply func(*kit.Invocation, string) error) *cobra.Command {
	return &cobra.Command{
		Use:   use + " REF...",
		Short: short,
		Args:  cobra.MinimumNArgs(1),
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			sel, err := kit.SelectFrom(c, "filters", filterColumns(), filterList(c))
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: action, Kind: "filters", Count: sel.Len(), IDs: sel.IDs,
				Name:    kit.Sole(sel.Rows, func(f mailsvc.Filter) string { return f.Name }),
				Preview: sel.Preview(),
			}, func() error {
				for _, id := range sel.IDs {
					if err := apply(c, id); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
