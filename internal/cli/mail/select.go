package mail

import (
	"context"
	"time"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	mailsvc "github.com/roman-16/proton-cli/internal/service/mail"
	"github.com/roman-16/proton-cli/internal/units"
	"github.com/spf13/cobra"
)

// The mail filters, shared by every organising verb.
//
// One flag set for trash, delete, move, label, unlabel, star, unstar, mark and
// export means learning it once. It is also why `--dry-run` can show the same
// table `list` would: the filter path already has the rows.

// filters are the ways to say "which messages" without naming them.
type filters struct {
	unread  bool
	starred bool
	from    string
	to      string
	subject string
	keyword string
	folder  string
	// whereByDefault is the folder used when none was given. It is kept apart
	// from folder so that a default never counts as something the user asked
	// for: a bulk verb refuses an empty selection, and a folder nobody named
	// would otherwise look like a narrowing and let it through.
	whereByDefault string
	days           kit.DayRange
	age            kit.Range
	all            bool
	// page is where in the result to read. A listing pairs --limit with --page; a
	// verb that acts on what a filter found takes --limit alone, because a cap on
	// a bulk change is its first page.
	page kit.Page
}

// registerNarrowing adds the flags that say which messages, and nothing else.
// `list` and every organising verb register the same set, which is what lets a
// selection be read before it is acted on.
//
// The default folder differs: a listing opens on the inbox, while a verb that
// acts on what a filter found looks everywhere unless told not to, because a
// filter is already the narrowing.
func (f *filters) registerNarrowing(c *cobra.Command, folder string) {
	f.whereByDefault = folder
	fl := c.Flags()
	fl.BoolVar(&f.unread, "unread", false, "Match unread messages")
	fl.BoolVar(&f.starred, "starred", false, "Match starred messages")
	fl.StringVar(&f.from, "from", "", "Match the sender's address")
	fl.StringVar(&f.to, "to", "", "Match a recipient's address")
	fl.StringVar(&f.subject, "subject", "", "Match text in the subject")
	fl.StringVar(&f.keyword, "keyword", "", "Match text in the subject, a name or an address, and in bodies once a mail index exists")
	f.days.Register(c)
	f.age.Register(fl, "messages")
	registerFolder(c, &f.folder, "", folder)
}

func (f *filters) register(c *cobra.Command) {
	f.registerNarrowing(c, "all")
	kit.All(c.Flags(), &f.all)
	f.page.Default = defaultLimit
	f.page.RegisterCap(c, "messages")
}

// defaultLimit is how many messages a bulk verb acts on when no cap was given.
// It is a guard rather than a technical bound: a mistyped filter takes a
// hundred and fifty messages rather than a mailbox, and says more may exist.
const defaultLimit = 150

// narrowed reports whether the user asked for a subset, which decides whether an
// empty answer means an empty folder or an unmatched filter. The folder is not
// part of it: opening a different folder is still a listing.
func (f *filters) narrowed() bool {
	return f.unread || f.starred || f.from != "" || f.to != "" || f.subject != "" ||
		f.keyword != "" || f.days.Set() || f.age.Set()
}

// set reports whether the user asked for a filtered selection at all.
func (f *filters) set() bool { return f.narrowed() || f.folder != "" || f.all }

// unbounded reports whether --all was given with nothing to narrow it, which is
// worth warning about before it happens.
func (f *filters) unbounded() bool { return f.all && !f.narrowed() && f.folder == "" }

// list converts the filters into the one request Proton takes.
//
// The folder is resolved rather than passed along, because a custom folder is a
// name here and an ID everywhere the query goes: unresolved, `--folder Receipts`
// reached Proton as a label called Receipts, which matches nothing.
func (f *filters) list(ctx context.Context, c *kit.Invocation) (mailsvc.ListOptions, error) {
	folder := f.folder
	if folder == "" {
		folder = f.whereByDefault
	}
	if folder != "" {
		box, err := c.App.Mail.ResolveMailbox(ctx, folder)
		if err != nil {
			return mailsvc.ListOptions{}, err
		}
		folder = box.ID
	}
	after, before := f.days.Days()
	opts := mailsvc.ListOptions{
		Keyword: f.keyword, From: f.from, To: f.to, Subject: f.subject,
		Folder: folder, Unread: f.unread, Starred: f.starred,
		After: after, Before: before,
		Page: f.page.Number, PageSize: f.page.Size,
	}
	// A duration is the same bound as a date, said relatively. Whichever is
	// given, the server sees a date.
	if f.age.OlderThan != "" {
		d, err := units.ParseDuration(f.age.OlderThan)
		if err != nil {
			return opts, kit.Fail("--older-than: %v", err)
		}
		opts.Before = time.Now().Add(-d)
	}
	if f.age.NewerThan != "" {
		d, err := units.ParseDuration(f.age.NewerThan)
		if err != nil {
			return opts, kit.Fail("--newer-than: %v", err)
		}
		opts.After = time.Now().Add(-d)
	}
	return opts, nil
}

// filterHint names the filters this command actually has, so the error a user
// sees lists real options rather than a generic sentence.
const filterHint = "--unread, --starred, --from, --subject or --older-than"

// registerPaging adds the two flags that read a result the server counts.
func (f *filters) registerPaging(c *cobra.Command, noun string) {
	f.page.Default = defaultPageSize
	f.page.Register(c, noun)
}

// defaultPageSize is a screenful of mail.
const defaultPageSize = 25

// total is how many messages the listing is one page of.
//
// The server's count answers that for a page of a folder. When the whole result
// was asked for it does not: everything is on screen, and --starred is applied
// here rather than by Proton, so a count taken from the server would say there
// are rows to page towards that this command has already discarded.
func (f *filters) total(counted, shown int) int {
	if f.page.Size == 0 {
		return shown
	}
	return counted
}

// selectMessages resolves what an organising verb should act on.
func selectMessages(c *kit.Invocation, f *filters) (kit.Selection[mailsvc.Message], error) {
	if f.unbounded() {
		c.Note("--all with no other filter affects every message in the account. Add --folder to narrow it.")
	}
	sel := kit.Selector[mailsvc.Message]{
		Noun:       "messages",
		Columns:    messageColumns(),
		IDOf:       func(m mailsvc.Message) string { return m.ID },
		FilterHint: filterHint,
		Scope:      "a whole folder",
		Limit:      f.page.Size,
		ByRef: func(ctx context.Context, ref string) (mailsvc.Message, error) {
			return c.App.Mail.FindMessage(ctx, ref)
		},
	}
	if f.set() {
		sel.ByFilter = func(ctx context.Context) ([]mailsvc.Message, error) {
			opts, err := f.list(ctx, c)
			if err != nil {
				return nil, err
			}
			msgs, _, cover, err := c.App.Mail.Search(ctx, opts)
			if err != nil {
				return nil, err
			}
			shortIndex(c, cover, opts)
			return msgs, nil
		}
	}
	return kit.Select(c, sel)
}

// selectConversations is the same selection for whole threads.
func selectConversations(c *kit.Invocation, f *filters) (kit.Selection[mailsvc.Conversation], error) {
	if f.unbounded() {
		c.Note("--all with no other filter affects every thread in the account. Add --folder to narrow it.")
	}
	sel := kit.Selector[mailsvc.Conversation]{
		Noun:       "conversations",
		Columns:    conversationColumns(),
		IDOf:       func(cv mailsvc.Conversation) string { return cv.ID },
		FilterHint: filterHint,
		Scope:      "a whole folder",
		Limit:      f.page.Size,
		ByRef: func(ctx context.Context, ref string) (mailsvc.Conversation, error) {
			return c.App.Mail.FindConversation(ctx, ref)
		},
	}
	if f.set() {
		sel.ByFilter = func(ctx context.Context) ([]mailsvc.Conversation, error) {
			opts, err := f.list(ctx, c)
			if err != nil {
				return nil, err
			}
			convs, _, cover, err := c.App.Mail.SearchConversations(ctx, opts)
			if err != nil {
				return nil, err
			}
			shortIndex(c, cover, opts)
			return convs, nil
		}
	}
	return kit.Select(c, sel)
}

// shortIndex says when a search read an index that is not the whole mailbox.
//
// An answer from an index is right about everything it covers and silent about
// the rest. Left unsaid, that is a wrong answer with a plausible shape: nothing
// on the screen distinguishes "there is no such message" from "the part of your
// mailbox that has it is not indexed".
//
// There are four ways it is short, and they are not the same thing to do
// something about. No index at all means Proton answered, which is subjects,
// names and addresses and not a word of what any message says. A mailbox still
// being read is missing older mail outright. A mailbox whose bodies are still
// arriving holds every message and can be searched by everything except what
// the ones at the far end say. An index Proton could not describe the changes
// to is missing nothing anybody can name, which is exactly why it has to be
// said.
func shortIndex(c *kit.Invocation, cover mailsvc.Coverage, opts mailsvc.ListOptions) {
	switch {
	case !cover.Indexed && opts.Keyword != "":
		c.Warn("There is no mail index on this machine, so Proton searched subjects, names "+
			"and addresses and no bodies. `%s index create mail` makes bodies searchable.",
			kit.Program)
	case !cover.Indexed:
	case cover.Stale:
		c.Warn("The mail index has fallen behind Proton and was searched as it stands. "+
			"`%s index update` catches it up.", kit.Program)
	case cover.Partial:
		c.Warn("Only %d of %d messages are indexed, newest first, so older mail was not searched. "+
			"`%s index create mail` continues the download.", cover.Have, cover.Total, kit.Program)
	case opts.Keyword != "" && cover.Bodies < cover.Have:
		c.Warn("Only %d of %d message bodies are indexed, newest first, so older mail was searched "+
			"by everything but its text. `%s index create mail` continues the download.",
			cover.Bodies, cover.Have, kit.Program)
	}
}
