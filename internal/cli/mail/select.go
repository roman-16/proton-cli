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
	after          string
	before         string
	age            kit.Range
	all            bool
	// page is where in the result to read. A listing spells it --page and
	// --page-size; a verb that acts on what a filter found spells the width
	// --limit, because a cap on a bulk change is its first page.
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
	fl.StringVar(&f.keyword, "keyword", "", "Match text anywhere, including display names and bodies")
	fl.StringVar(&f.after, "after", "", "Match messages after this date (YYYY-MM-DD)")
	fl.StringVar(&f.before, "before", "", "Match messages before this date (YYYY-MM-DD)")
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
		f.keyword != "" || f.after != "" || f.before != "" || f.age.Set()
}

// set reports whether the user asked for a filtered selection at all.
func (f *filters) set() bool { return f.narrowed() || f.folder != "" || f.all }

// unbounded reports whether --all was given with nothing to narrow it, which is
// worth warning about before it happens.
func (f *filters) unbounded() bool { return f.all && !f.narrowed() && f.folder == "" }

// list converts the filters into the one request Proton takes.
func (f *filters) list() (mailsvc.ListOptions, error) {
	folder := f.folder
	if folder == "" {
		folder = f.whereByDefault
	}
	opts := mailsvc.ListOptions{
		Keyword: f.keyword, From: f.from, To: f.to, Subject: f.subject,
		Folder: folder, Unread: f.unread,
		After: f.after, Before: f.before,
		Page: f.page.Number, PageSize: f.page.Size,
	}
	// A duration is the same bound as a date, said relatively. Whichever is
	// given, the server sees a date.
	if f.age.OlderThan != "" {
		d, err := units.ParseDuration(f.age.OlderThan)
		if err != nil {
			return opts, kit.Fail("--older-than: %v", err)
		}
		opts.Before = time.Now().Add(-d).Format("2006-01-02")
	}
	if f.age.NewerThan != "" {
		d, err := units.ParseDuration(f.age.NewerThan)
		if err != nil {
			return opts, kit.Fail("--newer-than: %v", err)
		}
		opts.After = time.Now().Add(-d).Format("2006-01-02")
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
			opts, err := f.list()
			if err != nil {
				return nil, err
			}
			msgs, _, err := c.App.Mail.List(ctx, opts)
			if err != nil {
				return nil, err
			}
			return applyLocalFilters(msgs, f), nil
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
			opts, err := f.list()
			if err != nil {
				return nil, err
			}
			convs, _, err := c.App.Mail.ConversationsList(ctx, opts)
			if err != nil {
				return nil, err
			}
			return keepStarred(convs, f.starred), nil
		}
	}
	return kit.Select(c, sel)
}

// applyLocalFilters narrows what the server could not. Proton's search has no
// starred predicate, so that one is applied here rather than being silently
// ignored - which is what a flag the server drops amounts to.
func applyLocalFilters(msgs []mailsvc.Message, f *filters) []mailsvc.Message {
	if f == nil || !f.starred {
		return msgs
	}
	kept := make([]mailsvc.Message, 0, len(msgs))
	for _, m := range msgs {
		if m.Starred() {
			kept = append(kept, m)
		}
	}
	return kept
}

// keepStarred narrows what the server could not: Proton's query has no starred
// predicate, so a flag it would silently drop is applied here instead.
func keepStarred(convs []mailsvc.Conversation, starred bool) []mailsvc.Conversation {
	if !starred {
		return convs
	}
	kept := make([]mailsvc.Conversation, 0, len(convs))
	for _, cv := range convs {
		if cv.Starred() {
			kept = append(kept, cv)
		}
	}
	return kept
}
