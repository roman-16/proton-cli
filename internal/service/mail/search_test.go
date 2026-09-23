package mail

import (
	"context"
	"slices"
	"testing"
	"time"
)

// indexed is a message as the index holds it, for the tests below.
func indexed(id, subject, body string, at int64, labels ...string) stored {
	return stored{
		ID: id, ConversationID: "thread-" + id, Subject: subject, Body: body, Opened: true,
		SenderName: "Jane Roe", SenderAddress: "jane@example.com",
		To:     []addressee{{Name: "Me", Address: "me@proton.me"}},
		Time:   at,
		Labels: labels,
	}
}

func ids(msgs []stored) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

// A keyword reaches the body, which is the whole reason the index exists: this
// is the question Proton answers "no such message" to.
func TestAKeywordSearchesTheBody(t *testing.T) {
	in := []stored{
		indexed("a", "The north trail is open again", "your parking permit is enclosed", 100, labelInbox),
		indexed("b", "Weekly notes", "nothing of the sort", 200, labelInbox),
	}
	got := matching(in, ListOptions{Keyword: "parking permit", Folder: "all"})
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("matched %v, want the message whose body says it", ids(got))
	}
}

// Words are matched one at a time and a quoted run as a phrase, which is the
// difference between "both of these appear" and "this is what it says".
func TestWordsMatchApartAndAPhraseMatchesTogether(t *testing.T) {
	in := []stored{
		indexed("a", "The north trail is open again", "your parking permit is enclosed", 100, labelInbox),
		indexed("b", "Parking", "nothing to do with permits", 200, labelInbox),
	}
	if got := matching(in, ListOptions{Keyword: "parking permit"}); len(got) != 2 {
		t.Errorf("two words matched %v, want both messages that hold each somewhere", ids(got))
	}
	if got := matching(in, ListOptions{Keyword: `"parking permit"`}); len(got) != 1 || got[0].ID != "a" {
		t.Errorf("a phrase matched %v, want only the message that says it", ids(got))
	}
}

// A message is in a folder or it is not, and a folder is the one filter that
// answers with a whole mailbox when it is All Mail.
func TestAFolderNarrowsToWhatCarriesItsLabel(t *testing.T) {
	in := []stored{
		indexed("a", "One", "", 100, labelInbox),
		indexed("b", "Two", "", 200, labelArchive),
	}
	if got := matching(in, ListOptions{Folder: labelArchive, Keyword: "t"}); len(got) != 1 || got[0].ID != "b" {
		t.Errorf("archive matched %v, want b", ids(got))
	}
	if got := matching(in, ListOptions{Folder: labelAllMail, Subject: "o"}); len(got) != 2 {
		t.Errorf("all mail matched %v, want both", ids(got))
	}
}

// --from matches the display name as well as the address, which is what an
// index can answer and the server cannot.
func TestFromMatchesTheNameAsWellAsTheAddress(t *testing.T) {
	in := []stored{indexed("a", "One", "", 100, labelInbox)}
	if got := matching(in, ListOptions{From: "Jane Roe"}); len(got) != 1 {
		t.Error("a display name should match --from")
	}
	if got := matching(in, ListOptions{From: "jane@example.com"}); len(got) != 1 {
		t.Error("an address should match --from")
	}
	if got := matching(in, ListOptions{From: "somebody@example.com"}); len(got) != 0 {
		t.Errorf("matched %v, want nothing", ids(got))
	}
}

// A message that has expired is gone from the account, so it is gone from an
// answer - the index is a copy, and a copy that answers with what no longer
// exists is worse than no copy.
func TestAnExpiredMessageIsNotAnAnswer(t *testing.T) {
	gone := indexed("a", "One", "", 100, labelInbox)
	gone.Expires = time.Now().Add(-time.Hour).Unix()
	later := indexed("b", "One", "", 200, labelInbox)
	later.Expires = time.Now().Add(time.Hour).Unix()

	got := matching([]stored{gone, later}, ListOptions{Subject: "One"})
	if len(got) != 1 || got[0].ID != "b" {
		t.Errorf("matched %v, want the message that still exists", ids(got))
	}
}

// Both named days are included whole, which is what --after and --before mean
// everywhere else in the CLI.
func TestTheDayBoundsIncludeBothDays(t *testing.T) {
	day := func(s string) time.Time {
		at, err := time.ParseInLocation("2006-01-02", s, time.Local)
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return at
	}
	at := func(s string) int64 {
		when, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
		if err != nil {
			t.Fatalf("parse %s: %v", s, err)
		}
		return when.Unix()
	}
	in := []stored{
		indexed("early", "One", "", at("2026-04-14 23:59"), labelInbox),
		indexed("first", "One", "", at("2026-04-15 00:01"), labelInbox),
		indexed("last", "One", "", at("2026-04-17 23:58"), labelInbox),
		indexed("late", "One", "", at("2026-04-18 00:02"), labelInbox),
	}
	got := matching(in, ListOptions{Subject: "One", After: day("2026-04-15"), Before: day("2026-04-17")})
	if len(got) != 2 || got[0].ID != "first" || got[1].ID != "last" {
		t.Errorf("matched %v, want both named days included whole", ids(got))
	}
}

// The newest is first, and a page is cut out of the whole result - so the total
// counts everything that matched rather than what fitted on the page.
func TestAPageIsCutFromTheNewestFirst(t *testing.T) {
	in := []stored{
		indexed("old", "One", "", 100, labelInbox),
		indexed("new", "One", "", 300, labelInbox),
		indexed("mid", "One", "", 200, labelInbox),
	}
	matched := matching(in, ListOptions{Subject: "One"})
	sortMessages(matched)
	page, total := pageOf(matched, ListOptions{PageSize: 2})
	if total != 3 {
		t.Errorf("total = %d, want every match counted", total)
	}
	if len(page) != 2 || page[0].ID != "new" || page[1].ID != "mid" {
		t.Errorf("page = %v, want the two newest", ids(page))
	}
	second, _ := pageOf(matched, ListOptions{Page: 1, PageSize: 2})
	if len(second) != 1 || second[0].ID != "old" {
		t.Errorf("second page = %v, want the oldest", ids(second))
	}
	past, _ := pageOf(matched, ListOptions{Page: 9, PageSize: 2})
	if len(past) != 0 {
		t.Errorf("a page past the end = %v, want nothing", ids(past))
	}
}

// A thread is found by any of its messages, and what the row says comes from
// all of them: a search for a word in the reply still says how long the thread
// is and who is in it.
func TestAThreadIsFoundByAnyOfItsMessages(t *testing.T) {
	first := indexed("a", "Quarterly numbers", "here are the numbers", 100, labelInbox)
	first.ConversationID = "thread"
	reply := indexed("b", "Re: Quarterly numbers", "the parking permit is sorted", 200, labelInbox)
	reply.ConversationID = "thread"
	reply.SenderName, reply.SenderAddress = "Me", "me@proton.me"
	elsewhere := indexed("c", "Unrelated", "", 300, labelInbox)

	in := []stored{first, reply, elsewhere}
	threads := threadsOf(in, matching(in, ListOptions{Keyword: "parking permit"}))
	if len(threads) != 1 {
		t.Fatalf("threads = %d, want 1", len(threads))
	}
	got := threads[0]
	switch {
	case got.ID != "thread":
		t.Errorf("id = %q, want the thread the reply is in", got.ID)
	case got.NumMessages != 2:
		t.Errorf("messages = %d, want both of the thread's", got.NumMessages)
	case got.Subject != "Re: Quarterly numbers":
		t.Errorf("subject = %q, want the newest message's", got.Subject)
	case len(got.Senders) != 2:
		t.Errorf("senders = %d, want everyone who wrote in it", len(got.Senders))
	}
}

// A star is a label, so a starred question is asked of the starred label and
// narrowed here - and Proton's own listing has no starred predicate to send.
//
// The page is cut after the narrowing, which is the whole point: a page cut
// first would show three rows of a folder's fifty and count the fifty.
func TestStarredIsAskedAsTheLabelItIs(t *testing.T) {
	inbox := []Message{
		{ID: "a", Labels: []string{labelInbox, labelStarred}},
		{ID: "b", Labels: []string{labelArchive, labelStarred}},
		{ID: "c", Labels: []string{labelInbox, labelStarred}},
	}
	var asked ListOptions
	list := func(_ context.Context, opts ListOptions) ([]Message, int, error) {
		asked = opts
		return inbox, len(inbox), nil
	}
	labels := func(m Message) []string { return m.Labels }

	rows, total, err := starredOnly(t.Context(), ListOptions{Starred: true, Folder: "inbox", PageSize: 25},
		list, labels)
	if err != nil {
		t.Fatalf("starredOnly: %v", err)
	}
	switch {
	case asked.Folder != "starred":
		t.Errorf("asked Proton for %q, want the starred label", asked.Folder)
	case asked.Starred:
		t.Error("the starred flag was sent to Proton, which has nothing to do with it")
	case asked.PageSize != 0:
		t.Errorf("asked for a page of %d; the whole result is what the narrowing needs", asked.PageSize)
	}
	if total != 2 || len(rows) != 2 {
		t.Fatalf("got %d of %d, want the two starred messages in the inbox", len(rows), total)
	}

	// Without a folder beside it, everything starred answers.
	rows, total, err = starredOnly(t.Context(), ListOptions{Starred: true, Folder: "all"}, list, labels)
	if err != nil {
		t.Fatalf("starredOnly: %v", err)
	}
	if total != 3 || len(rows) != 3 {
		t.Errorf("got %d of %d across the account, want all three", len(rows), total)
	}
}

func TestTheIndexNarrowsByReadStateAttachmentsAndAddress(t *testing.T) {
	read := indexed("read", "One", "", 100, labelInbox)
	unread := indexed("unread", "One", "", 200, labelInbox)
	unread.Unread = 1
	attached := indexed("attached", "One", "", 300, labelInbox)
	attached.Attachments = 2
	work := indexed("work", "One", "", 400, labelInbox)
	work.AddressID = "work-id"
	in := []stored{read, unread, attached, work}
	for _, tc := range []struct {
		name string
		opts ListOptions
		want []string
	}{
		{"read", ListOptions{Read: true}, []string{"read", "attached", "work"}},
		{"unread", ListOptions{Unread: true}, []string{"unread"}},
		{"with attachments", ListOptions{HasAttachments: true}, []string{"attached"}},
		{"on one address", ListOptions{AddressID: "work-id"}, []string{"work"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ids(matching(in, tc.opts)); !slices.Equal(got, tc.want) {
				t.Errorf("matched %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAThreadIsReadOnlyWhenNothingInItIsUnread(t *testing.T) {
	first := indexed("a", "Plans", "", 100, labelInbox)
	first.ConversationID = "mixed"
	reply := indexed("b", "Re: Plans", "", 200, labelInbox)
	reply.ConversationID, reply.Unread = "mixed", 1
	settled := indexed("c", "Done", "", 300, labelInbox)
	settled.ConversationID = "settled"
	in := []stored{first, reply, settled}

	if got := matchingThreads(in, ListOptions{Read: true}); len(got) != 1 || got[0].ID != "settled" {
		t.Errorf("read threads = %v, want only the one with nothing unread", threadIDs(got))
	}
	if got := matchingThreads(in, ListOptions{Unread: true}); len(got) != 1 || got[0].ID != "mixed" {
		t.Errorf("unread threads = %v, want the one with an unread message", threadIDs(got))
	}
}

func TestTheIndexOrdersBySizeEitherWay(t *testing.T) {
	small := indexed("small", "One", "", 300, labelInbox)
	small.Size = 10
	large := indexed("large", "One", "", 100, labelInbox)
	large.Size = 900
	mid := indexed("mid", "One", "", 200, labelInbox)
	mid.Size = 500
	msgs := []stored{small, large, mid}

	orderMessages(msgs, ListOptions{Sort: SortBySize})
	if got := ids(msgs); !slices.Equal(got, []string{"large", "mid", "small"}) {
		t.Errorf("by size = %v, want largest first", got)
	}
	orderMessages(msgs, ListOptions{Sort: SortBySize, Reverse: true})
	if got := ids(msgs); !slices.Equal(got, []string{"small", "mid", "large"}) {
		t.Errorf("by size reversed = %v, want smallest first", got)
	}
	orderMessages(msgs, ListOptions{})
	if got := ids(msgs); !slices.Equal(got, []string{"small", "mid", "large"}) {
		t.Errorf("by time = %v, want newest first", got)
	}
}

func TestAThreadIsAsLargeAsItsMessages(t *testing.T) {
	first := indexed("a", "Plans", "", 100, labelInbox)
	first.ConversationID, first.Size = "long", 400
	reply := indexed("b", "Re: Plans", "", 200, labelInbox)
	reply.ConversationID, reply.Size = "long", 600
	single := indexed("c", "Plans", "", 300, labelInbox)
	single.ConversationID, single.Size = "short", 700

	threads := matchingThreads([]stored{first, reply, single}, ListOptions{Subject: "Plans"})
	orderThreads(threads, ListOptions{Sort: SortBySize})
	if len(threads) != 2 || threads[0].ID != "long" || threads[0].Size != 1000 {
		t.Errorf("threads by size = %+v, want the thread of 1000 bytes first", threads)
	}
}

func threadIDs(threads []Conversation) []string {
	out := make([]string, 0, len(threads))
	for _, c := range threads {
		out = append(out, c.ID)
	}
	return out
}
