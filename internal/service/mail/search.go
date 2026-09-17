package mail

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Where a search is answered from.
//
// Proton answers a listing: a folder, a page, and the predicates its own index
// holds. It cannot answer a question about a body, because it cannot read one,
// so a search over bodies is answered here - from the copy on this machine, over
// everything a filter can ask, and never from half of each.
//
// Which is why one index answers a whole search rather than the body half of
// one. A --from that matched an address at Proton and a display name here would
// mean two things on one command line, and a page cut by the server and filtered
// here would report a total that counts rows it had already thrown away.

// Coverage says where an answer came from and how much of the mailbox it saw.
type Coverage struct {
	// Indexed says the copy on this machine answered.
	Indexed bool
	// Have and Total are how much of the mailbox is indexed, and Partial says
	// they differ - a build that has not finished has the newest mail and not
	// the oldest, so a search over it is short at the far end.
	Partial     bool
	Have, Total int
	// Bodies is how many of them the index holds the text of. An envelope is
	// indexed in one page of a hundred and fifty and a body is a request of its
	// own, so a mailbox is searchable by its senders and dates long before it is
	// searchable by what it says.
	Bodies int
	// Stale says Proton could not describe what has happened to the mailbox, so
	// the copy answered as it stands and nothing knows what it is missing.
	Stale bool
}

// Search answers a question about messages: from the index when there is one,
// and from Proton when there is not.
func (s *Service) Search(ctx context.Context, opts ListOptions) ([]Message, int, Coverage, error) {
	in, cover, ok := s.searchable(ctx, opts)
	if !ok {
		msgs, total, err := s.List(ctx, opts)
		return msgs, total, Coverage{}, err
	}
	matched := matching(in, opts)
	sortMessages(matched)
	page, total := pageOf(matched, opts)
	out := make([]Message, 0, len(page))
	for _, m := range page {
		out = append(out, m.message())
	}
	return out, total, cover, nil
}

// SearchConversations is the same question about threads.
//
// A thread is its messages, so it is worked out from them rather than asked for
// separately: what matches is any message of it, and what is shown about it is
// what its messages say - which is the same thing Proton's own conversation
// listing is built from.
func (s *Service) SearchConversations(ctx context.Context, opts ListOptions) ([]Conversation, int, Coverage, error) {
	in, cover, ok := s.searchable(ctx, opts)
	if !ok {
		convs, total, err := s.ConversationsList(ctx, opts)
		return convs, total, Coverage{}, err
	}
	threads := threadsOf(in, matching(in, opts))
	sort.SliceStable(threads, func(i, j int) bool { return threads[i].Time > threads[j].Time })
	page, total := pageOf(threads, opts)
	return page, total, cover, nil
}

// searchable reports whether the index answers this question, and what it holds.
//
// A question the server answers as well or better stays with the server: a page
// of a folder is a page of a folder, and asking for one is not searching. What
// brings a question here is a predicate over content, which is everything a
// filter can say.
func (s *Service) searchable(ctx context.Context, opts ListOptions) ([]stored, Coverage, bool) {
	if !opts.Narrowed() || !s.Indexed() {
		return nil, Coverage{}, false
	}
	x, err := s.openIndex(ctx)
	if err != nil {
		// Recorded and not counted: nothing is missing from the answer, because
		// Proton answers the same question. What the reader loses is bodies, and
		// the command says so where it would have said the index was short.
		slog.DebugContext(ctx, "mail: the index could not be read, so Proton answered",
			"kind", string(skip.KindMessage), "reason", string(skip.Unreadable), "error", err)
		return nil, Coverage{}, false
	}
	x.syncBeforeSearch(ctx)
	in := x.records(ctx)
	status := x.Status()
	cover := Coverage{
		Indexed: true, Have: len(in), Total: max(status.Total, len(in)),
		Partial: !status.Complete, Bodies: bodiesIn(in), Stale: status.Stale,
	}
	return in, cover, true
}

// bodiesIn is how many of the messages a search just read hold their text, which
// is counted off the records rather than taken from the summary beside them: it
// is what the answer was drawn from, and the one number a caveat about the
// answer must not be wrong about.
func bodiesIn(in []stored) int {
	bodies := 0
	for _, m := range in {
		if m.settled() {
			bodies++
		}
	}
	return bodies
}

// syncBeforeSearch brings the index up to date with the account before it is
// asked anything, so an answer is never older than the command that asked for
// it.
//
// Nothing else keeps it current: there is no background process, and a run of
// `index update` is the same catch-up done deliberately. What it costs when
// nothing has happened is one request and no reading of the index at all.
//
// A directory somebody else is writing to is left alone - whatever holds it is
// keeping the index current anyway, and waiting for a build that takes hours
// would make a search take hours.
func (x *indexSession) syncBeforeSearch(ctx context.Context) {
	lock, err := x.s.index.Claim()
	if err != nil {
		slog.DebugContext(ctx, "mail: the index was busy, so the search read it as it stands",
			"kind", string(skip.KindMessage), "reason", string(skip.Unreadable), "error", err)
		return
	}
	defer lock.Release()
	if _, err := x.syncIndex(ctx); err != nil {
		// Recorded and not counted: the index is a copy, so a catch-up that
		// failed costs whatever changed since the last one and nothing that was
		// there before. The answer says how much of the mailbox it covers.
		slog.DebugContext(ctx, "mail: the index could not be brought up to date",
			"kind", string(skip.KindMessage), "reason", string(skip.Unreadable), "error", err)
	}
}

// matching is every indexed message the filters describe.
func matching(in []stored, opts ListOptions) []stored {
	terms := search.Terms(opts.Keyword)
	folder := ResolveFolder(opts.Folder)
	if opts.Folder == "" {
		folder = ""
	}
	after, before := bounds(opts)
	now := time.Now().Unix()
	out := make([]stored, 0, len(in))
	for _, m := range in {
		switch {
		case m.Expires > 0 && m.Expires <= now:
		case folder != "" && folder != labelAllMail && !hasLabel(m.Labels, folder):
		case opts.Unread && m.Unread == 0:
		case opts.Starred && !hasLabel(m.Labels, labelStarred):
		case opts.ID != "" && m.ID != opts.ID:
		case after > 0 && m.Time < after, before > 0 && m.Time > before:
		case !search.Matches(terms, keywordFields(m)...):
		case !search.Has(opts.From, m.SenderName, m.SenderAddress):
		case !search.Has(opts.To, m.recipients()...):
		case !search.Has(opts.Subject, m.Subject):
		default:
			out = append(out, m)
		}
	}
	return out
}

// keywordFields is everything --keyword looks in, which is everything a reader
// would call part of the message.
func keywordFields(m stored) []string {
	return append([]string{m.Subject, m.SenderName, m.SenderAddress, m.Body}, m.recipients()...)
}

// bounds is the window a query names, as the seconds a message's own time is
// compared against. Both named days are whole and both are included, which is
// what --after and --before mean everywhere else.
func bounds(opts ListOptions) (after, before int64) {
	if !opts.After.IsZero() {
		after = startOfDay(opts.After).Unix()
	}
	if !opts.Before.IsZero() {
		before = startOfDay(opts.Before).AddDate(0, 0, 1).Unix() - 1
	}
	return after, before
}

// sortMessages puts the newest first, which is the order Proton answers in and
// the order every listing of mail is read in. The message's own place in its
// second decides a tie, so a page boundary falls in the same place twice.
func sortMessages(msgs []stored) {
	sort.SliceStable(msgs, func(i, j int) bool {
		if msgs[i].Time != msgs[j].Time {
			return msgs[i].Time > msgs[j].Time
		}
		return msgs[i].Order > msgs[j].Order
	})
}

// pageOf cuts the page the caller asked for out of the whole result and reports
// how big the whole result was. A size of zero is all of it, as it is everywhere
// a listing takes --limit.
func pageOf[T any](rows []T, opts ListOptions) ([]T, int) {
	total := len(rows)
	if opts.PageSize <= 0 {
		return rows, total
	}
	start := opts.Page * opts.PageSize
	if start >= total {
		return nil, total
	}
	return rows[start:min(start+opts.PageSize, total)], total
}

// threadsOf gathers the threads the matched messages belong to.
//
// Every message of a thread contributes to what the row says, including the ones
// that did not match: a thread's subject is its newest message's, and its count
// is how many messages it has, not how many matched.
func threadsOf(all, matched []stored) []Conversation {
	wanted := make(map[string]bool, len(matched))
	order := make([]string, 0, len(matched))
	for _, m := range matched {
		if m.ConversationID == "" || wanted[m.ConversationID] {
			continue
		}
		wanted[m.ConversationID] = true
		order = append(order, m.ConversationID)
	}
	byThread := make(map[string][]stored, len(order))
	for _, m := range all {
		if wanted[m.ConversationID] {
			byThread[m.ConversationID] = append(byThread[m.ConversationID], m)
		}
	}
	out := make([]Conversation, 0, len(order))
	for _, id := range order {
		out = append(out, thread(id, byThread[id]))
	}
	return out
}

// thread is what a conversation row says, worked out from its messages.
func thread(id string, msgs []stored) Conversation {
	sortMessages(msgs)
	c := Conversation{ID: id, NumMessages: len(msgs)}
	seenSender := map[string]bool{}
	seenRecipient := map[string]bool{}
	seenLabel := map[string]bool{}
	for i, m := range msgs {
		if i == 0 {
			c.Subject, c.Time = m.Subject, m.Time
		}
		c.NumUnread += m.Unread
		c.NumAttachments += m.Attachments
		if !seenSender[m.SenderAddress] {
			seenSender[m.SenderAddress] = true
			c.Senders = append(c.Senders, map[string]any{"Name": m.SenderName, "Address": m.SenderAddress})
		}
		for _, p := range append(append(append([]addressee{}, m.To...), m.CC...), m.BCC...) {
			if !seenRecipient[p.Address] {
				seenRecipient[p.Address] = true
				c.Recipients = append(c.Recipients, map[string]any{"Name": p.Name, "Address": p.Address})
			}
		}
		for _, l := range m.Labels {
			if !seenLabel[l] {
				seenLabel[l] = true
				c.Labels = append(c.Labels, l)
			}
		}
	}
	return c
}
