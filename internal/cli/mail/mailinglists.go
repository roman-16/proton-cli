package mail

import (
	"context"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/errs"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// Who is filling the mailbox.
//
// A mailbox answers "what arrived"; this answers "who keeps sending", which is
// the question behind an inbox nobody can keep up with. Proton's own client gives
// it a view rather than a filter for the same reason, and the four things it
// offers there are the four verbs here: leave the list, file its mail, forget the
// entry, or look at one in full.

func mailingListsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "mailing-lists",
		Short: "The senders that write to you as a list",
	}
	c.AddCommand(
		mailingListsListCmd(), mailingListsGetCmd(), mailingListsUnsubscribeCmd(),
		mailingListsUpdateCmd(), mailingListsRemoveCmd(),
	)
	return c
}

// rule is the standing decision on a list, in the words the verbs that set it
// use. An empty cell is a list nothing has been decided about.
func rule(l mailsvc.MailingList, folders map[string]string) string {
	var parts []string
	if l.MoveToFolder != "" {
		if name, ok := folders[l.MoveToFolder]; ok {
			parts = append(parts, name)
		} else {
			parts = append(parts, l.MoveToFolder)
		}
	}
	if l.MarkAsRead {
		parts = append(parts, "read")
	}
	return strings.Join(parts, " + ")
}

func mailingListColumns(folders map[string]string) []ui.Column[mailsvc.MailingList] {
	return []ui.Column[mailsvc.MailingList]{
		{Header: "ID", ID: true, Cell: func(l mailsvc.MailingList) string { return l.ID }},
		{Header: "NAME", Flex: true, Handle: true, Cell: func(l mailsvc.MailingList) string { return l.Name }},
		{Header: "SENDER", Flex: true, Cell: func(l mailsvc.MailingList) string { return l.SenderAddress }},
		{Header: "UNREAD", Right: true, Cell: func(l mailsvc.MailingList) string {
			return strconv.Itoa(l.Unread)
		}},
		{Header: "30 DAYS", Right: true, Cell: func(l mailsvc.MailingList) string {
			return strconv.Itoa(l.Last30Days)
		}},
		{Header: "LAST", Cell: func(l mailsvc.MailingList) string {
			if l.LastReceived == 0 {
				return ""
			}
			return units.Time(l.LastReceived)
		}},
		{Header: "RULE", Cell: func(l mailsvc.MailingList) string { return rule(l, folders) }},
	}
}

// mailboxNames is the ID-to-name map the RULE column reads, fetched only when
// something is filed: an account that has decided nothing about any list pays no
// request for a column of blanks.
func mailboxNames(c *kit.Invocation, lists []mailsvc.MailingList) (map[string]string, error) {
	for _, l := range lists {
		if l.MoveToFolder != "" {
			return c.App.Mail.MailboxNames(c.Ctx)
		}
	}
	return nil, nil
}

func mailingListsListCmd() *cobra.Command {
	var held kit.Held[mailsvc.MailingList]
	var left bool
	c := &cobra.Command{
		Use:   "list",
		Short: "List the senders that write to you as a list",
		Long: "List the senders that write to you as a list.\n\n" +
			"Shows the ones still writing. --unsubscribed shows the ones you have left.\n\n" +
			"RULE is what happens to a list's mail as it arrives, set by `update`.",
		RunE: kit.Run(nil, func(c *kit.Invocation) error {
			scope := mailsvc.MailingListsActive
			if left {
				scope = mailsvc.MailingListsLeft
			}
			lists, err := c.App.Mail.MailingLists(c.Ctx, scope)
			if err != nil {
				return err
			}
			folders, err := mailboxNames(c, lists)
			if err != nil {
				return err
			}
			return held.Answer(c, ui.TableSpec[mailsvc.MailingList]{
				Noun: "mailing lists", Columns: mailingListColumns(folders),
			}, lists)
		}),
	}
	c.Flags().BoolVar(&left, "unsubscribed", false, "List the ones you have left instead")
	held.Register(c, "mailing lists",
		// Newest first, which is the order Proton answers in and the order a
		// mailbox is read in. Every key here reverses with --desc.
		kit.Key[mailsvc.MailingList]{Name: "received", Less: func(a, b mailsvc.MailingList) int {
			return kit.Ints(b.LastReceived, a.LastReceived)
		}},
		kit.Key[mailsvc.MailingList]{Name: "name", Less: func(a, b mailsvc.MailingList) int {
			return kit.Fold(a.Name, b.Name)
		}},
		kit.Key[mailsvc.MailingList]{Name: "unread", Less: func(a, b mailsvc.MailingList) int {
			return kit.Ints(int64(b.Unread), int64(a.Unread))
		}},
		kit.Key[mailsvc.MailingList]{Name: "frequency", Less: func(a, b mailsvc.MailingList) int {
			return kit.Ints(int64(b.Last30Days), int64(a.Last30Days))
		}},
		kit.Key[mailsvc.MailingList]{Name: "read", Less: func(a, b mailsvc.MailingList) int {
			return kit.Ints(b.LastRead, a.LastRead)
		}},
	)
	return c
}

// mailingListView is the shape `get` reports.
type mailingListView struct {
	Name          string `json:"name"`
	SenderAddress string `json:"sender_address"`
	Received      string `json:"received"`
	Unread        int    `json:"unread"`
	First         string `json:"first_received,omitempty"`
	Last          string `json:"last_received,omitempty"`
	Trackers      int    `json:"trackers,omitempty"`
	Rule          string `json:"rule,omitempty"`
	Unsubscribe   string `json:"unsubscribe"`
	Left          string `json:"unsubscribed,omitempty"`
	ID            string `json:"id"`
}

func mailingListsGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get REF",
		Short: "Show one mailing list in full",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			l, err := findMailingList(c, c.Args[0])
			if err != nil {
				return err
			}
			folders, err := mailboxNames(c, []mailsvc.MailingList{l})
			if err != nil {
				return err
			}
			view := mailingListView{
				Name: l.Name, SenderAddress: l.SenderAddress,
				Received:    receivedSummary(l),
				Unread:      l.Unread,
				Trackers:    l.Trackers,
				Rule:        ruleSentence(l, folders),
				Unsubscribe: unsubscribeSummary(l),
				ID:          l.ID,
			}
			if l.FirstReceived > 0 {
				view.First = units.Time(l.FirstReceived)
			}
			if l.LastReceived > 0 {
				view.Last = units.Time(l.LastReceived)
			}
			if l.Unsubscribed {
				view.Left = "yes"
				if l.UnsubscribedTime > 0 {
					view.Left = units.Time(l.UnsubscribedTime)
				}
			}
			return kit.Show(c, ui.RecordSpec{
				Object: view,
				Fields: []ui.Field{
					{Label: "Name", Value: view.Name},
					{Label: "Sender", Value: view.SenderAddress},
					{Label: "Received", Value: view.Received},
					{Label: "Unread", Value: strconv.Itoa(view.Unread), Always: true},
					{Label: "First", Value: view.First},
					{Label: "Last", Value: view.Last},
					{Label: "Trackers", Value: strconv.Itoa(view.Trackers)},
					{Label: "Rule", Value: view.Rule},
					{Label: "Unsubscribe", Value: view.Unsubscribe, Always: true},
					{Label: "Unsubscribed", Value: view.Left},
					{Label: "ID", Value: view.ID},
				},
			})
		}),
	}
}

func receivedSummary(l mailsvc.MailingList) string {
	return kit.Quantity(l.Received, "total") + ", " +
		strconv.Itoa(l.Last30Days) + " in the last 30 days, " +
		strconv.Itoa(l.Last90Days) + " in the last 90"
}

// ruleSentence says what is being done with the list's mail, and names the
// filter that does it - which is what turns it off again.
func ruleSentence(l mailsvc.MailingList, folders map[string]string) string {
	r := rule(l, folders)
	if r == "" {
		return ""
	}
	var what []string
	if name := folders[l.MoveToFolder]; l.MoveToFolder != "" {
		if name == "" {
			name = l.MoveToFolder
		}
		what = append(what, "moves to "+name)
	}
	if l.MarkAsRead {
		what = append(what, "marks read")
	}
	s := strings.Join(what, ", ")
	if l.FilterID != "" {
		s += " (filter " + l.FilterID + ")"
	}
	return s
}

func unsubscribeSummary(l mailsvc.MailingList) string {
	switch l.Way {
	case mailsvc.UnsubscribeOneClick:
		return "one-click"
	case mailsvc.UnsubscribeEmail:
		return "by email"
	case mailsvc.UnsubscribeLink:
		return "by link"
	}
	return "not offered"
}

// findMailingList resolves a reference against every list, left ones included: a
// list you have already unsubscribed from is still a thing to look at and to
// forget.
func findMailingList(c *kit.Invocation, ref string) (mailsvc.MailingList, error) {
	lookup := mailingListLookup(c)
	return lookup.Find(c.Ctx, ref)
}

func mailingListLookup(c *kit.Invocation) *kit.Lookup[mailsvc.MailingList] {
	return &kit.Lookup[mailsvc.MailingList]{
		Kind: "mailing list",
		Load: func(ctx context.Context) ([]mailsvc.MailingList, error) {
			return c.App.Mail.MailingLists(ctx, mailsvc.MailingListsAll)
		},
		ID:     func(l mailsvc.MailingList) string { return l.ID },
		Handle: func(l mailsvc.MailingList) string { return l.Name },
	}
}

// resolveMailingLists turns the references into rows, resolving all of them
// against one listing.
func resolveMailingLists(c *kit.Invocation) ([]mailsvc.MailingList, error) {
	lookup := mailingListLookup(c)
	out := make([]mailsvc.MailingList, 0, len(c.Args))
	seen := map[string]bool{}
	for _, ref := range c.Args {
		l, err := lookup.Find(c.Ctx, ref)
		if err != nil {
			return nil, err
		}
		if seen[l.ID] {
			continue
		}
		seen[l.ID] = true
		out = append(out, l)
	}
	return out, nil
}

func mailingListIDs(lists []mailsvc.MailingList) []string {
	ids := make([]string, 0, len(lists))
	for _, l := range lists {
		ids = append(ids, l.ID)
	}
	return ids
}

func mailingListsUnsubscribeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unsubscribe REF...",
		Short: "Ask a mailing list to stop writing to you",
		Long: "Ask a mailing list to stop writing to you.\n\n" +
			"A list offers one of three ways, and the answer says which was used.\n" +
			"Proton submits a one-click form on your behalf; an unsubscribe address is\n" +
			"a message sent from the address the list writes to; a link is a page,\n" +
			"which is opened in your browser and printed either way.\n\n" +
			"A list that offers none of them can only be kept out by blocking the\n" +
			"sender.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lists, err := resolveMailingLists(c)
			if err != nil {
				return err
			}
			targets := make([]mailsvc.UnsubscribeTarget, 0, len(lists))
			for _, l := range lists {
				if l.Way == "" {
					return noWayToLeave(l)
				}
				targets = append(targets, l)
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Unsubscribed, Kind: "mailing lists", Count: len(lists),
				IDs:    mailingListIDs(lists),
				Name:   kit.Sole(lists, func(l mailsvc.MailingList) string { return l.Name }),
				Detail: unsubscribeDetail(targets),
			}, func() error {
				for _, t := range targets {
					if err := askToStop(c, t); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// unsubscribeDetail says which way is about to be used, for the one list where
// there is one way to name. It is the difference between a request Proton makes
// and a message leaving the account, which is worth reading while a --dry-run
// still has a chance to be one.
func unsubscribeDetail(targets []mailsvc.UnsubscribeTarget) string {
	if len(targets) != 1 {
		return ""
	}
	switch targets[0].Offer().Way {
	case mailsvc.UnsubscribeEmail:
		return "- by email, as the list asked"
	case mailsvc.UnsubscribeLink:
		return "- by opening its unsubscribe page"
	}
	return ""
}

// noWayToLeave is the refusal for a list that offers none of the three, which
// leaves keeping the sender out as the only thing that works.
func noWayToLeave(t mailsvc.UnsubscribeTarget) error {
	return errs.Naming(t.Named(), kit.Fail("%s offers no way to unsubscribe.", t.Named()).
		Hint(kit.Program+" mail settings senders block "+t.Sender()).Exit(3))
}

// askToStop carries out whichever way the list offers. Two of the three need
// this machine, which is why the choice is made here rather than in the service.
func askToStop(c *kit.Invocation, t mailsvc.UnsubscribeTarget) error {
	switch offer := t.Offer(); offer.Way {
	case mailsvc.UnsubscribeEmail:
		return c.App.Mail.UnsubscribeByEmail(c.Ctx, t)
	case mailsvc.UnsubscribeLink:
		// The address is printed whether or not a browser opened, because whether
		// this machine can show a page is not a question worth asking: a run that
		// claimed to have opened one it had not would leave nothing on screen to
		// act on.
		if kit.ShowInBrowser(offer.Link) {
			c.UI().Instruct("It should have opened in your browser, and works on any device too:")
		} else {
			c.UI().Instruct("Open it on any device to finish:")
		}
		c.UI().Instruct("  " + offer.Link)
		return c.App.Mail.MarkLeft(c.Ctx, t)
	default:
		return c.App.Mail.UnsubscribeOneClick(c.Ctx, t)
	}
}

func mailingListsUpdateCmd() *cobra.Command {
	var into string
	var markRead bool
	c := &cobra.Command{
		Use:   "update REF...",
		Short: "Set what happens to a mailing list's mail",
		Long: "Set what happens to a mailing list's mail.\n\n" +
			"It covers the mail already here and everything that arrives afterwards.\n\n" +
			"Proton keeps the standing half as a filter of its own, so this is turned\n" +
			"off again with `proton mail settings filters disable`, naming the filter\n" +
			"`get` shows.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			if into == "" && !c.Changed("mark-read") {
				return kit.Fail("Nothing to change.").
					Hint("pass --into to file its mail, --mark-read to have it arrive read, or both.").Exit(3)
			}
			var dest mailsvc.Mailbox
			if into != "" {
				var err error
				if dest, err = c.App.Mail.ResolveFolderTarget(c.Ctx, into); err != nil {
					return err
				}
			}
			lists, err := resolveMailingLists(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Updated, Kind: "mailing lists", Count: len(lists),
				IDs:    mailingListIDs(lists),
				Name:   kit.Sole(lists, func(l mailsvc.MailingList) string { return l.Name }),
				Detail: ruleDetail(dest.Name, markRead, c.Changed("mark-read")),
			}, func() error {
				for _, l := range lists {
					read := l.MarkAsRead
					if c.Changed("mark-read") {
						read = markRead
					}
					if err := c.App.Mail.MailingListRule(c.Ctx, l, dest.ID, read); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
	c.Flags().StringVar(&into, "into", "", "Folder its mail goes to, by name or ID")
	c.Flags().BoolVar(&markRead, "mark-read", false, "Have its mail arrive read")
	completeMoveTargets(c)
	return c
}

func ruleDetail(folder string, markRead, readSet bool) string {
	var parts []string
	if folder != "" {
		parts = append(parts, "its mail goes to "+folder)
	}
	if readSet {
		if markRead {
			parts = append(parts, "it arrives read")
		} else {
			parts = append(parts, "it arrives unread")
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "- " + strings.Join(parts, " and ")
}

func mailingListsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove REF...",
		Short: "Drop a mailing list from the listing",
		Long: "Drop a mailing list from the listing.\n\n" +
			"It unsubscribes from nothing, and the list comes back the next time it\n" +
			"writes to you.",
		RunE: kit.Run([]kit.Step{kit.StepExpand}, func(c *kit.Invocation) error {
			lists, err := resolveMailingLists(c)
			if err != nil {
				return err
			}
			return kit.Mutate(c, ui.ResultSpec{
				Action: ui.Removed, Kind: "mailing lists", Count: len(lists),
				IDs:    mailingListIDs(lists),
				Name:   kit.Sole(lists, func(l mailsvc.MailingList) string { return l.Name }),
				Detail: "- it comes back if they write again",
			}, func() error {
				for _, l := range lists {
					if err := c.App.Mail.MailingListForget(c.Ctx, l); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}
