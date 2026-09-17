package mail

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
)

// A mailbox to index, and a Proton that answers for it.
//
// The build is a walk anchored to the oldest message of the page before, a body
// fetched per message afterwards, and a cursor into the change feed taken before
// any of it. Each of those is a thing that can be got wrong in a way no unit of
// it would show, so what is tested here is the loop rather than its parts.
type mailbox struct {
	kr       *pgp.KeyRing
	messages []rawListMessage
	bodies   map[string]string
	events   map[string]string
	// latest is what the feed answers when asked where "from now on" is, which a
	// mailbox that has been refreshed hands out again.
	latest string

	// A build fetches bodies ten at a time, so both counters are written from
	// several goroutines at once.
	//
	// pages counts the metadata requests, so a resumed build can be shown to
	// have asked for what it did not have rather than for the whole mailbox.
	pages atomic.Int64
	// fetched counts the bodies asked for, which is the expensive half.
	fetched atomic.Int64
}

func newMailbox(t *testing.T, count int) *mailbox {
	t.Helper()
	m := newEmptyMailbox(t)
	for i := range count {
		id := fmt.Sprintf("msg-%02d", i)
		m.add(t, rawListMessage{
			ID: id, ConversationID: "thread-" + id, Subject: "Subject " + id,
			Time: int64(1000 - i), LabelIDs: []string{labelInbox, labelAllMail},
			Sender: struct{ Name, Address string }{Name: "Jane Roe", Address: "jane@example.com"},
			ToList: []map[string]any{{"Name": "Me", "Address": "me@proton.me"}},
		}, "the body of "+id)
	}
	return m
}

func newEmptyMailbox(t *testing.T) *mailbox {
	t.Helper()
	return &mailbox{
		kr: genMailKeyRing(t), bodies: map[string]string{},
		events: map[string]string{}, latest: "cursor-0",
	}
}

// forget takes a message out of the mailbox, the way deleting one elsewhere
// does: no event, and nothing left but its absence from a listing.
func (m *mailbox) forget(id string) {
	kept := m.messages[:0]
	for _, msg := range m.messages {
		if msg.ID != id {
			kept = append(kept, msg)
		}
	}
	m.messages = kept
	delete(m.bodies, id)
}

// newest puts a message at the top of the mailbox, which is where the walk
// starts and where an arrival belongs.
func (m *mailbox) newest(t *testing.T, raw rawListMessage, body string) {
	t.Helper()
	m.add(t, raw, body)
	last := len(m.messages) - 1
	m.messages = append([]rawListMessage{m.messages[last]}, m.messages[:last]...)
}

func (m *mailbox) add(t *testing.T, raw rawListMessage, body string) {
	t.Helper()
	m.messages = append(m.messages, raw)
	m.bodies[raw.ID] = armored(t, m.kr, body)
}

// armored is a body as Proton hands one over: sealed to the account's key.
func armored(t *testing.T, kr *pgp.KeyRing, body string) string {
	t.Helper()
	enc, err := kr.Encrypt(pgp.NewPlainMessageFromString(body), nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	sealed, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	return sealed
}

func (m *mailbox) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

// Decode answers the four requests an index makes, and nothing else.
func (m *mailbox) Decode(_ context.Context, r proton.Request, out any) error {
	switch {
	case r.Path == "/mail/v4/messages":
		m.pages.Add(1)
		// Proton counts what the query covers, and a page anchored partway down
		// the mailbox covers what is left from there.
		from, page := m.page(r)
		return encodeInto(out, map[string]any{
			"Total": len(m.messages) - from, "Messages": page,
		})
	case strings.HasPrefix(r.Path, "/mail/v4/messages/"):
		id := strings.TrimPrefix(r.Path, "/mail/v4/messages/")
		body, ok := m.bodies[id]
		if !ok {
			return fmt.Errorf("no such message %q", id)
		}
		m.fetched.Add(1)
		return encodeInto(out, map[string]any{"Message": map[string]any{
			"ID": id, "Body": body, "AddressID": "addr-1", "MIMEType": "text/plain",
			"ToList": []map[string]any{{"Name": "Me", "Address": "me@proton.me"}},
		}})
	case r.Path == "/core/v4/events/latest":
		return encodeInto(out, map[string]any{"EventID": m.latest})
	case strings.HasPrefix(r.Path, "/core/v5/events/"):
		return encodeInto(out, m.event(strings.TrimPrefix(r.Path, "/core/v5/events/")))
	}
	return fmt.Errorf("unexpected request %s %s", r.Method, r.Path)
}

// page is the slice of the mailbox a request asks for: everything older than
// the anchor it carries, at the size it asked for.
func (m *mailbox) page(r proton.Request) (int, []rawListMessage) {
	from := 0
	if id := r.Query.Get("EndID"); id != "" {
		for i, msg := range m.messages {
			if msg.ID == id {
				from = i + 1
				break
			}
		}
	}
	size := len(m.messages)
	if asked, err := strconv.Atoi(r.Query.Get("PageSize")); err == nil && asked > 0 {
		size = asked
	}
	return from, m.messages[min(from, len(m.messages)):min(from+size, len(m.messages))]
}

// event is what the feed answers at one cursor: whatever the test put there,
// and an empty page otherwise.
func (m *mailbox) event(cursor string) map[string]any {
	raw, ok := m.events[cursor]
	if !ok {
		return map[string]any{"EventID": cursor, "More": 0}
	}
	var batch map[string]any
	_ = json.Unmarshal([]byte(raw), &batch)
	return batch
}

func encodeInto(out any, v map[string]any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

// build brings the index to everything the mailbox holds, which is what
// `index create` does: the envelopes, and then the bodies they are owed.
func build(t *testing.T, s *Service) search.Result {
	t.Helper()
	got, err := indexing(t, s).Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	return got
}

// catchUp applies what the feed reports, which is what every command that reads
// the index does before it answers.
func catchUp(t *testing.T, s *Service) search.Result {
	t.Helper()
	got, err := indexing(t, s).Sync(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	return got
}

func indexing(t *testing.T, s *Service) *indexSession {
	t.Helper()
	x, err := s.openIndex(t.Context())
	if err != nil {
		t.Fatalf("open the index: %v", err)
	}
	return x
}

// inTheIndex is what the index holds, by message.
func inTheIndex(t *testing.T, s *Service) map[string]stored {
	t.Helper()
	out := map[string]stored{}
	for _, in := range indexing(t, s).records(t.Context()) {
		out[in.ID] = in
	}
	return out
}

func status(t *testing.T, s *Service) search.Status {
	t.Helper()
	st, err := s.index.Status(search.AppMail)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	return st
}

// indexService is a mail service whose index is a directory of its own and
// whose keys open the mailbox it is given.
func indexService(t *testing.T, m *mailbox) *Service {
	t.Helper()
	unlocked := &keys.Unlocked{
		Addresses: []keys.Address{{ID: "addr-1"}},
		AddrKRs:   map[string]keys.Rings{"addr-1": {Read: m.kr, Write: m.kr}},
	}
	s := New(m, testKeys(unlocked))
	s.SetIndex(search.New(t.TempDir(), func() string { return "user-1" },
		func(context.Context) (search.Keys, error) {
			return search.Keys{Seal: m.kr, Open: m.kr}, nil
		}))
	return s
}

// A build walks the whole mailbox, opens every body, and leaves an index a
// keyword can be answered from.
func TestABuildIndexesEveryMessageAndItsBody(t *testing.T) {
	m := newMailbox(t, 5)
	s := indexService(t, m)

	if got := build(t, s); got.Indexed != 5 {
		t.Errorf("indexed %d, want 5", got.Indexed)
	}
	st := status(t, s)
	if !st.Complete || st.Indexed != 5 || st.Total != 5 || st.Bodies != 5 {
		t.Errorf("status = %+v, want a complete index of 5 with every body", st)
	}

	msgs, total, cover, err := s.Search(t.Context(), ListOptions{Keyword: "body of msg-03", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !cover.Indexed || cover.Partial || cover.Bodies != 5 {
		t.Errorf("coverage = %+v, want a whole index answering", cover)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].ID != "msg-03" {
		t.Fatalf("searching a body found %d messages %v, want msg-03", total, msgs)
	}
	if msgs[0].Subject != "Subject msg-03" || msgs[0].FromAddress != "jane@example.com" {
		t.Errorf("row = %+v, want what a listing shows", msgs[0])
	}
}

// The envelopes come first and the bodies after, so a mailbox answers a filtered
// listing long before it answers a keyword.
//
// It is the whole reason the walk is separate: metadata is a hundred and fifty
// messages a request and a body is one, so a build that fetched as it walked
// would leave --unread waiting on hours of downloads.
func TestEveryMessageIsIndexedBeforeAnyBodyIsFetched(t *testing.T) {
	m := newMailbox(t, 4)
	s := indexService(t, m)
	x := indexing(t, s)

	if _, err := x.walkMailbox(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if fetched := m.fetched.Load(); fetched != 0 {
		t.Errorf("the walk fetched %d bodies; it is metadata only", fetched)
	}
	st := status(t, s)
	if !st.Complete || st.Indexed != 4 || st.Bodies != 0 {
		t.Fatalf("after the walk status = %+v, want 4 messages and no bodies", st)
	}

	// Everything but the text answers already.
	msgs, _, cover, err := s.Search(t.Context(), ListOptions{From: "jane@example.com", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !cover.Indexed || len(msgs) != 4 {
		t.Errorf("a filtered listing found %d of 4 from the envelopes alone", len(msgs))
	}

	if _, err := x.fetchOwedBodies(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("fetch bodies: %v", err)
	}
	if st := status(t, s); st.Bodies != 4 {
		t.Errorf("bodies = %d of 4 after the second pass", st.Bodies)
	}
	if fetched := m.fetched.Load(); fetched != 4 {
		t.Errorf("bodies fetched = %d, want one request each", fetched)
	}
}

// A reply's quoted history is left out when the thread it quotes is in the
// index, so a word in one message matches that message and not the thread.
//
// The envelopes settle this before a single body is read: which threads the
// mailbox holds and from when is what says whether a quote is covered
// elsewhere, and a walk that fetched as it went would never know in time.
func TestAQuotedReplyIsIndexedWithoutWhatItQuotes(t *testing.T) {
	m := newEmptyMailbox(t)
	m.add(t, rawListMessage{
		ID: "reply", ConversationID: "thread", Subject: "Re: agenda", Time: 2000,
		LabelIDs: []string{labelInbox, labelAllMail},
	}, "Thanks!\n\nOn Monday, Jane Roe <jane@example.com> wrote:\n\n> pineapple on the agenda\n")
	m.add(t, rawListMessage{
		ID: "original", ConversationID: "thread", Subject: "agenda", Time: 1000,
		LabelIDs: []string{labelInbox, labelAllMail},
	}, "pineapple on the agenda")
	s := indexService(t, m)
	build(t, s)

	if body := inTheIndex(t, s)["reply"].Body; strings.Contains(body, "pineapple") {
		t.Errorf("the reply was indexed with the quote it carries: %q", body)
	}
	msgs, _, _, err := s.Search(t.Context(), ListOptions{Keyword: "pineapple", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(msgs) != 1 || msgs[0].ID != "original" {
		t.Errorf("a word in one message matched %d messages %v", len(msgs), msgs)
	}
}

// A build counts towards the mailbox, not towards the page it is on.
//
// Every page after the first is anchored to where the last one ended, and an
// anchored page is answered with how many are left from there - so a build that
// took its total from the page it happened to finish on would report a mailbox
// of a handful having indexed thousands.
func TestABuildCountsTheMailboxRatherThanThePageItIsOn(t *testing.T) {
	m := newMailbox(t, indexPage+10)
	s := indexService(t, m)

	build(t, s)
	if pages := m.pages.Load(); pages < 3 {
		t.Fatalf("the walk took %d metadata requests; it has to span pages for this to mean anything", pages)
	}
	if st := status(t, s); st.Total != indexPage+10 || st.Indexed != indexPage+10 {
		t.Errorf("status = %+v, want %d of %d", st, indexPage+10, indexPage+10)
	}
}

// A build that stopped carries on from what is missing: the bodies it already
// has are not fetched again, and the ones it never got to are.
func TestAnInterruptedBuildCarriesOnWhereItStopped(t *testing.T) {
	m := newMailbox(t, 6)
	s := indexService(t, m)

	stop, cancel := context.WithCancel(t.Context())
	s.C = &cancelAfter{n: 2, cancel: cancel, to: m}

	if _, err := indexing(t, s).Build(stop, progress.Nop{}); err == nil {
		t.Fatal("a cancelled build reported success")
	}
	partial := status(t, s)
	if partial.Bodies >= partial.Indexed {
		t.Fatalf("a cancelled build left %+v, want bodies still owed", partial)
	}
	firstPass := m.fetched.Load()

	s.C = m
	build(t, s)
	if st := status(t, s); !st.Complete || st.Indexed != 6 || st.Bodies != 6 {
		t.Errorf("status = %+v, want all 6 indexed with their bodies", st)
	}
	if fetched := m.fetched.Load(); fetched != 6 {
		t.Errorf("bodies fetched = %d after %d before the interruption, want each fetched once",
			fetched, firstPass)
	}
}

// What happens to the mailbox afterwards reaches the index through the change
// feed: a message created is indexed, one moved is rewritten from the event
// without its body being fetched again, and one deleted is gone.
func TestASyncAppliesWhatTheFeedReports(t *testing.T) {
	m := newMailbox(t, 3)
	s := indexService(t, m)
	build(t, s)
	afterBuild := m.fetched.Load()

	m.newest(t, rawListMessage{
		ID: "msg-new", ConversationID: "thread-new", Subject: "Subject msg-new",
		Time: 2000, LabelIDs: []string{labelInbox, labelAllMail},
	}, "the body of msg-new")
	m.events["cursor-0"] = `{"EventID":"cursor-1","More":0,"Messages":[
		{"ID":"msg-new","Action":1,"Message":{"ID":"msg-new","ConversationID":"thread-new","Subject":"Subject msg-new","Time":2000,"LabelIDs":["0","5"]}},
		{"ID":"msg-01","Action":3,"Message":{"ID":"msg-01","ConversationID":"thread-msg-01","Subject":"Subject msg-01","Time":999,"LabelIDs":["3","5"]}},
		{"ID":"msg-02","Action":0}
	]}`

	got := catchUp(t, s)
	if got.Indexed != 2 || got.Removed != 1 {
		t.Errorf("sync = %+v, want 2 indexed and 1 removed", got)
	}
	if fetched := m.fetched.Load(); fetched != afterBuild+1 {
		t.Errorf("bodies fetched = %d, want only the new message's", fetched-afterBuild)
	}

	held := inTheIndex(t, s)
	if _, gone := held["msg-02"]; gone {
		t.Error("a deleted message is still in the index")
	}
	if moved := held["msg-01"]; !hasLabel(moved.Labels, labelTrash) {
		t.Errorf("labels = %v, want the move applied", moved.Labels)
	}
	if moved := held["msg-01"]; moved.Body == "" {
		t.Error("rewriting an envelope dropped the body that was already indexed")
	}
	if fresh := held["msg-new"]; fresh.Body == "" {
		t.Error("a message the feed reported was indexed without its body")
	}

	// And the search follows: the moved message answers for the trash rather
	// than the inbox it was built from.
	inbox, _, _, err := s.Search(t.Context(), ListOptions{Subject: "msg-01", Folder: "inbox"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(inbox) != 0 {
		t.Errorf("the inbox still answers with a message that moved: %v", inbox)
	}
}

// An index whose bodies are still arriving says so, so a keyword over it is not
// read as a keyword over everything.
func TestAnIndexStillDownloadingBodiesSaysHowMuchItCovers(t *testing.T) {
	m := newMailbox(t, 4)
	s := indexService(t, m)
	stop, cancel := context.WithCancel(t.Context())
	s.C = &cancelAfter{n: 1, cancel: cancel, to: m}
	if _, err := indexing(t, s).Build(stop, progress.Nop{}); err == nil {
		t.Fatal("a cancelled build reported success")
	}

	s.C = m
	_, _, cover, err := s.Search(t.Context(), ListOptions{Keyword: "body", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !cover.Indexed || cover.Have != 4 {
		t.Errorf("coverage = %+v, want every message of the mailbox indexed", cover)
	}
	if cover.Bodies >= cover.Have {
		t.Errorf("coverage = %+v, want fewer bodies than messages", cover)
	}
}

// A message the feed cannot account for still leaves the index, and one that
// arrived while nothing was watching still enters it.
//
// Proton says outright when its history no longer covers the gap. Nothing after
// that says what happened in it, so the mailbox is read again and compared:
// what is not there any more is what nothing came back for.
func TestARefreshedFeedIsAnsweredByReadingTheMailboxAgain(t *testing.T) {
	m := newMailbox(t, 3)
	s := indexService(t, m)
	build(t, s)

	// What happens in the gap: one message arrives, one is deleted, and the feed
	// gives up rather than describing either.
	m.newest(t, rawListMessage{
		ID: "msg-new", ConversationID: "thread-new", Subject: "Subject msg-new",
		Time: 2000, LabelIDs: []string{labelInbox, labelAllMail},
	}, "the body of msg-new")
	m.forget("msg-01")
	m.events["cursor-0"] = `{"EventID":"cursor-0","More":0,"Refresh":1}`
	m.latest = "cursor-1"

	got := catchUp(t, s)
	if !got.Refreshed {
		t.Fatalf("sync = %+v, want a refresh the run can answer", got)
	}
	if st := status(t, s); !st.Stale {
		t.Errorf("status = %+v, want an index that says it owes a reading", st)
	}

	after, err := indexing(t, s).Build(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after.Indexed != 1 || after.Removed != 1 {
		t.Errorf("the reading = %+v, want the arrival indexed and the deletion removed", after)
	}
	held := inTheIndex(t, s)
	if _, there := held["msg-new"]; !there {
		t.Error("mail that arrived during the gap is not in the index")
	}
	if _, there := held["msg-01"]; there {
		t.Error("mail deleted during the gap is still in the index")
	}
	if st := status(t, s); st.Stale || !st.Complete || st.Indexed != 3 || st.Bodies != 3 {
		t.Errorf("status = %+v, want a whole index of the 3 messages there are now", st)
	}

	// And the reading is not a rebuild: what was already indexed and unchanged is
	// not fetched again.
	if fetched := m.fetched.Load(); fetched != 4 {
		t.Errorf("bodies fetched = %d, want the 3 of the build and the 1 that arrived", fetched)
	}
}

// A message that was edited is fetched again, because what changed is the text.
//
// It is the one event that means the body in the index is wrong rather than
// merely filed somewhere else, and a draft being written is the commonest thing
// it happens to.
func TestAnEditedMessageIsIndexedAgain(t *testing.T) {
	m := newMailbox(t, 2)
	s := indexService(t, m)
	build(t, s)

	m.bodies["msg-01"] = armored(t, m.kr, "Meeting moved to Thursday.")
	m.events["cursor-0"] = `{"EventID":"cursor-1","More":0,"Messages":[
		{"ID":"msg-01","Action":2,"Message":{"ID":"msg-01","ConversationID":"thread-msg-01","Subject":"Notes","Time":999,"LabelIDs":["8","5"]}}
	]}`
	if got := catchUp(t, s); got.Indexed != 1 {
		t.Errorf("sync = %+v, want the edit indexed", got)
	}

	edited := inTheIndex(t, s)["msg-01"]
	if edited.Subject != "Notes" || !strings.Contains(edited.Body, "Thursday") {
		t.Errorf("the index holds %q / %q, want what the message says now", edited.Subject, edited.Body)
	}
	msgs, _, _, err := s.Search(t.Context(), ListOptions{Keyword: "Thursday", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("searching what the message now says found %d messages", len(msgs))
	}
}

// A message the feed reports whose body could not be fetched is in the index by
// its envelope, and the next run fetches the body.
//
// The feed is followed from a cursor that moves on once a page is applied, so
// nothing ever asks about the page again: a message the sync did not write down
// is a message no filtered listing shows until the mailbox is read whole.
func TestAMessageWhoseBodyCouldNotBeFetchedIsIndexedByItsEnvelope(t *testing.T) {
	m := newMailbox(t, 2)
	s := indexService(t, m)
	build(t, s)

	m.newest(t, rawListMessage{
		ID: "msg-new", ConversationID: "thread-new", Subject: "Subject msg-new",
		Time: 2000, LabelIDs: []string{labelInbox, labelAllMail},
	}, "the body of msg-new")
	m.events["cursor-0"] = `{"EventID":"cursor-1","More":0,"Messages":[
		{"ID":"msg-new","Action":1,"Message":{"ID":"msg-new","ConversationID":"thread-new","Subject":"Subject msg-new","Time":2000,"LabelIDs":["0","5"]}}
	]}`
	sealed := m.bodies["msg-new"]
	delete(m.bodies, "msg-new")
	catchUp(t, s)

	fresh, there := inTheIndex(t, s)["msg-new"]
	if !there {
		t.Fatal("a message whose body could not be fetched is not in the index at all")
	}
	if fresh.settled() {
		t.Error("a body that was never fetched was settled as though it had been")
	}
	msgs, _, _, err := s.Search(t.Context(), ListOptions{Subject: "msg-new", Folder: "inbox"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("a filtered listing found %d messages, want the one by its envelope", len(msgs))
	}
	if st := status(t, s); st.Indexed != 3 || st.Bodies != 2 {
		t.Errorf("status = %+v, want 3 messages of which 2 have their body", st)
	}

	m.bodies["msg-new"] = sealed
	if got := build(t, s); got.Indexed != 1 {
		t.Errorf("the next run = %+v, want the owed body fetched", got)
	}
	if fresh := inTheIndex(t, s)["msg-new"]; !strings.Contains(fresh.Body, "body of msg-new") {
		t.Errorf("the index holds %q, want the body the next run fetched", fresh.Body)
	}
}

// A page of the feed that could not be applied is asked for again, by the same
// session: a watch keeps its index open between polls, and a poll that failed
// partway must not move the cursor past what it never wrote down.
func TestAKeptOpenSessionAsksAgainForAPageItCouldNotApply(t *testing.T) {
	m := newMailbox(t, 2)
	s := indexService(t, m)
	build(t, s)
	x := indexing(t, s)

	m.newest(t, rawListMessage{
		ID: "msg-new", ConversationID: "thread-new", Subject: "Subject msg-new",
		Time: 2000, LabelIDs: []string{labelInbox, labelAllMail},
	}, "the body of msg-new")
	m.events["cursor-0"] = `{"EventID":"cursor-1","More":0,"Messages":[
		{"ID":"msg-new","Action":1,"Message":{"ID":"msg-new","ConversationID":"thread-new","Subject":"Subject msg-new","Time":2000,"LabelIDs":["0","5"]}}
	]}`

	opens := s.keys
	s.keys = testKeys(nil)
	if _, err := x.Sync(t.Context()); err == nil {
		t.Fatal("a sync whose keys would not open reported success")
	}
	if x.log.State.Cursor != "cursor-0" {
		t.Fatalf("cursor = %q after a failed page, want it left at cursor-0", x.log.State.Cursor)
	}

	s.keys = opens
	poll(t, x)
	if fresh := inTheIndex(t, s)["msg-new"]; !strings.Contains(fresh.Body, "body of msg-new") {
		t.Errorf("the index holds %q after the next poll, want the message with its body", fresh.Body)
	}
	if x.log.State.Cursor != "cursor-1" {
		t.Errorf("cursor = %q, want the page applied and moved past", x.log.State.Cursor)
	}
}

// poll is what one poll of a watch does to an open index: finishes what is
// owed, then applies what changed.
func poll(t *testing.T, x *indexSession) {
	t.Helper()
	if _, err := x.Build(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := x.Sync(t.Context()); err != nil {
		t.Fatalf("sync: %v", err)
	}
}

// A record that went bad on disk costs the index that one message until the
// mailbox is read again, which the next run does - rather than the index
// quietly holding one message fewer and calling itself complete.
func TestARecordThatWentBadIsReadFromTheMailboxAgain(t *testing.T) {
	m := newMailbox(t, 3)
	s := indexService(t, m)
	build(t, s)

	// The walk wrote three envelopes and the bodies wrote three more; the fourth
	// frame is the first body, and the message it belongs to is left with its
	// envelope alone.
	path := filepath.Join(s.index.Dir(), "mail.bin")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	at := 0
	for range 3 {
		at += 4 + int(binary.BigEndian.Uint32(data[at:at+4]))
	}
	data[at+4+8] ^= 0xff
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("spoil: %v", err)
	}

	if held := inTheIndex(t, s); len(held) != 3 {
		t.Fatalf("the index holds %d messages after one record went bad, want all 3 by their envelopes", len(held))
	}
	pages, fetched := m.pages.Load(), m.fetched.Load()
	if got := build(t, s); got.Indexed != 1 {
		t.Errorf("the next run = %+v, want the one message whose record went bad fetched again", got)
	}
	if m.pages.Load() == pages {
		t.Error("the next run did not read the mailbox again")
	}
	if m.fetched.Load() != fetched+1 {
		t.Errorf("the next run fetched %d bodies, want only the one that was lost", m.fetched.Load()-fetched)
	}
	if st := status(t, s); st.Stale || !st.Complete || st.Indexed != 3 || st.Bodies != 3 {
		t.Errorf("status = %+v, want a whole index of 3 messages with every body", st)
	}
}

// The summary beside the log says how many bodies it holds, and a run that
// stopped between writing bodies and writing the summary leaves it behind. Any
// run that touches the index afterwards - a search included - puts it right,
// so `index list` does not go on saying nothing was downloaded.
func TestASearchLeavesTheSummarySayingWhatTheLogHolds(t *testing.T) {
	m := newMailbox(t, 4)
	s := indexService(t, m)
	stop, cancel := context.WithCancel(t.Context())
	s.C = &cancelAfter{n: 2, cancel: cancel, to: m}
	if _, err := indexing(t, s).Build(stop, progress.Nop{}); err == nil {
		t.Fatal("a cancelled build reported success")
	}
	if st := status(t, s); st.Bodies != 0 {
		t.Fatalf("status = %+v, want a summary the interrupted run never got to write", st)
	}

	s.C = m
	if _, _, _, err := s.Search(t.Context(), ListOptions{Subject: "msg", Folder: "all"}); err != nil {
		t.Fatalf("search: %v", err)
	}
	if st := status(t, s); st.Bodies != 2 {
		t.Errorf("status = %+v after a search, want the 2 bodies the log holds", st)
	}
}

// A body that would not open is settled rather than retried: the message is in
// the index by everything else, and the count says how many are in that state.
func TestABodyThatWillNotOpenIsIndexedByEverythingElse(t *testing.T) {
	m := newMailbox(t, 2)
	m.bodies["msg-00"] = "not a message anybody can open"
	s := indexService(t, m)

	if got := build(t, s); got.Unreadable != 1 {
		t.Errorf("build = %+v, want one body that would not open", got)
	}
	st := status(t, s)
	if st.Unreadable != 1 || st.Bodies != 2 {
		t.Errorf("status = %+v, want 2 settled bodies of which 1 unreadable", st)
	}

	// An index that owes nothing asks for nothing: neither the mailbox it has
	// already read nor a body it has already established will not open.
	fetched, pages := m.fetched.Load(), m.pages.Load()
	build(t, s)
	if again, walked := m.fetched.Load(), m.pages.Load(); again != fetched || walked != pages {
		t.Errorf("a second build made %d requests again, want it to ask for nothing",
			(again-fetched)+(walked-pages))
	}
	msgs, _, _, err := s.Search(t.Context(), ListOptions{Subject: "msg-00", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("a message whose body would not open is not searchable by its subject")
	}
}

// Without an index, the question goes to Proton and the answer says so - which
// is what the command needs to know before it tells somebody their mail holds
// no such thing.
func TestWithoutAnIndexProtonAnswers(t *testing.T) {
	m := newMailbox(t, 2)
	s := indexService(t, m)
	if m.pages.Load() != 0 {
		t.Fatal("a service with no index read the mailbox before it was asked anything")
	}

	_, _, cover, err := s.Search(t.Context(), ListOptions{Keyword: "body of msg-00", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if cover.Indexed {
		t.Error("an index answered when none was built")
	}
}

// cancelAfter is a Proton that stops the run partway through fetching bodies,
// which is where a build of any size is when somebody gives up on it.
type cancelAfter struct {
	n      int
	cancel context.CancelFunc
	to     proton.Doer

	mu   sync.Mutex
	seen int
}

func (c *cancelAfter) Do(ctx context.Context, r proton.Request) (*proton.Response, error) {
	return c.to.Do(ctx, r)
}

func (c *cancelAfter) Decode(ctx context.Context, r proton.Request, out any) error {
	if strings.HasPrefix(r.Path, "/mail/v4/messages/") {
		c.mu.Lock()
		c.seen++
		stop := c.seen > c.n
		c.mu.Unlock()
		if stop {
			c.cancel()
			return context.Canceled
		}
	}
	return c.to.Decode(ctx, r, out)
}
