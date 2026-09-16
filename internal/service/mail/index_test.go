package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
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
// fetched per message, and a cursor into the change feed taken before any of it.
// Each of those is a thing that can be got wrong in a way no unit of it would
// show, so what is tested here is the loop rather than its parts.
type mailbox struct {
	kr       *pgp.KeyRing
	messages []rawListMessage
	bodies   map[string]string
	events   map[string]string
	// pages counts the metadata requests, so a resumed build can be shown to
	// have asked for what it did not have rather than for the whole mailbox.
	pages int
	// fetched counts the bodies asked for, which is the expensive half.
	fetched int
}

func newMailbox(t *testing.T, count int) *mailbox {
	t.Helper()
	m := &mailbox{kr: genMailKeyRing(t), bodies: map[string]string{}, events: map[string]string{}}
	for i := range count {
		id := fmt.Sprintf("msg-%02d", i)
		m.add(t, rawListMessage{
			ID: id, ConversationID: "thread-" + id, Subject: "Subject " + id,
			Time: int64(1000 - i), LabelIDs: []string{labelInbox, labelAllMail},
			Sender: struct{ Name, Address string }{Name: "Jane Roe", Address: "jane@example.com"},
		}, "the body of "+id)
	}
	return m
}

func (m *mailbox) add(t *testing.T, raw rawListMessage, body string) {
	t.Helper()
	enc, err := m.kr.Encrypt(pgp.NewPlainMessageFromString(body), nil)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("armor: %v", err)
	}
	m.messages = append(m.messages, raw)
	m.bodies[raw.ID] = armored
}

func (m *mailbox) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

// Decode answers the four requests an index makes, and nothing else.
func (m *mailbox) Decode(_ context.Context, r proton.Request, out any) error {
	switch {
	case r.Path == "/mail/v4/messages":
		m.pages++
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
		m.fetched++
		return encodeInto(out, map[string]any{"Message": map[string]any{
			"ID": id, "Body": body, "AddressID": "addr-1", "MIMEType": "text/plain",
			"ToList": []map[string]any{{"Name": "Me", "Address": "me@proton.me"}},
		}})
	case r.Path == "/core/v4/events/latest":
		return encodeInto(out, map[string]any{"EventID": "cursor-0"})
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

	got, err := s.buildIndex(t.Context(), progress.Nop{})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if got.Indexed != 5 {
		t.Errorf("indexed %d, want 5", got.Indexed)
	}
	status, err := s.index.Status(search.AppMail)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Complete || status.Indexed != 5 || status.Total != 5 {
		t.Errorf("status = %+v, want a complete index of 5", status)
	}

	msgs, total, cover, err := s.Search(t.Context(), ListOptions{Keyword: "body of msg-03", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !cover.Indexed || cover.Partial {
		t.Errorf("coverage = %+v, want a whole index answering", cover)
	}
	if total != 1 || len(msgs) != 1 || msgs[0].ID != "msg-03" {
		t.Fatalf("searching a body found %d messages %v, want msg-03", total, msgs)
	}
	if msgs[0].Subject != "Subject msg-03" || msgs[0].FromAddress != "jane@example.com" {
		t.Errorf("row = %+v, want what a listing shows", msgs[0])
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

	if _, err := s.buildIndex(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	if m.pages < 3 {
		t.Fatalf("the walk took %d metadata requests; it has to span pages for this to mean anything", m.pages)
	}
	status, err := s.index.Status(search.AppMail)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Total != indexPage+10 || status.Indexed != indexPage+10 {
		t.Errorf("status = %+v, want %d of %d", status, indexPage+10, indexPage+10)
	}
}

// A build that stopped carries on from its mark: the metadata it already has is
// walked again, and the bodies it already has are not fetched again.
func TestAnInterruptedBuildCarriesOnWhereItStopped(t *testing.T) {
	m := newMailbox(t, 6)
	s := indexService(t, m)

	stop, cancel := context.WithCancel(t.Context())
	s.C = &cancelAfter{n: 2, cancel: cancel, to: m}

	if _, err := s.buildIndex(stop, progress.Nop{}); err == nil {
		t.Fatal("a cancelled build reported success")
	}
	partial, err := s.index.Status(search.AppMail)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if partial.Complete {
		t.Fatal("a cancelled build marked the index complete")
	}
	firstPass := m.fetched

	s.C = m
	if _, err := s.buildIndex(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	status, err := s.index.Status(search.AppMail)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !status.Complete || status.Indexed != 6 {
		t.Errorf("status = %+v, want all 6 indexed", status)
	}
	if m.fetched != 6 {
		t.Errorf("bodies fetched = %d after %d before the interruption, want each fetched once",
			m.fetched, firstPass)
	}
}

// What happens to the mailbox afterwards reaches the index through the change
// feed: a message created is indexed, one moved is rewritten from the event
// without its body being fetched again, and one deleted is gone.
func TestASyncAppliesWhatTheFeedReports(t *testing.T) {
	m := newMailbox(t, 3)
	s := indexService(t, m)
	if _, err := s.buildIndex(t.Context(), progress.Nop{}); err != nil {
		t.Fatalf("build: %v", err)
	}
	afterBuild := m.fetched

	m.add(t, rawListMessage{
		ID: "msg-new", ConversationID: "thread-new", Subject: "Subject msg-new",
		Time: 2000, LabelIDs: []string{labelInbox, labelAllMail},
	}, "the body of msg-new")
	m.events["cursor-0"] = `{"EventID":"cursor-1","More":0,"Messages":[
		{"ID":"msg-new","Action":1,"Message":{"ID":"msg-new","ConversationID":"thread-new","Subject":"Subject msg-new","Time":2000,"LabelIDs":["0","5"]}},
		{"ID":"msg-01","Action":3,"Message":{"ID":"msg-01","ConversationID":"thread-msg-01","Subject":"Subject msg-01","Time":999,"LabelIDs":["3","5"]}},
		{"ID":"msg-02","Action":0}
	]}`

	got, err := s.syncIndex(t.Context())
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got.Indexed != 2 || got.Removed != 1 {
		t.Errorf("sync = %+v, want 2 indexed and 1 removed", got)
	}
	if m.fetched != afterBuild+1 {
		t.Errorf("bodies fetched = %d, want only the new message's", m.fetched-afterBuild)
	}

	in, err := s.indexRecords(t.Context())
	if err != nil {
		t.Fatalf("records: %v", err)
	}
	held := map[string]stored{}
	for _, rec := range in {
		held[rec.ID] = rec
	}
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

// An index that does not hold the whole mailbox yet says so, so a search over
// it is not read as a search over everything.
func TestAPartialIndexSaysHowMuchItCovers(t *testing.T) {
	m := newMailbox(t, 4)
	s := indexService(t, m)
	stop, cancel := context.WithCancel(t.Context())
	s.C = &cancelAfter{n: 1, cancel: cancel, to: m}
	if _, err := s.buildIndex(stop, progress.Nop{}); err == nil {
		t.Fatal("a cancelled build reported success")
	}

	s.C = m
	_, _, cover, err := s.Search(t.Context(), ListOptions{Keyword: "body", Folder: "all"})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !cover.Indexed || !cover.Partial {
		t.Errorf("coverage = %+v, want an index that says it is short", cover)
	}
	if cover.Total != 4 || cover.Have >= cover.Total {
		t.Errorf("coverage = %+v, want fewer than the mailbox's 4", cover)
	}
}

// Without an index, the question goes to Proton and the answer says so - which
// is what the command needs to know before it tells somebody their mail holds
// no such thing.
func TestWithoutAnIndexProtonAnswers(t *testing.T) {
	m := newMailbox(t, 2)
	s := indexService(t, m)

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
