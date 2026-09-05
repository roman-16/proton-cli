package mail

import (
	"context"
	"errors"
	"fmt"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

func genMailKeyRing(t *testing.T) *pgp.KeyRing {
	t.Helper()
	key, err := pgp.GenerateKey("test", "test@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return kr
}

// TestDecryptBody covers the verdict mapping and the key correctness property:
// gopenpgp returns the decrypted body alongside a signature error, so a body
// must still be recovered even when its signature cannot be verified.
func TestDecryptBody(t *testing.T) {
	kr := genMailKeyRing(t)
	other := genMailKeyRing(t)
	const plain = "secret body"

	enc, err := kr.Encrypt(pgp.NewPlainMessageFromString(plain), kr) // encrypt+sign with kr
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("GetArmored: %v", err)
	}

	tests := []struct {
		name     string
		verifier *pgp.KeyRing
		want     pgphelper.VerifyResult
	}{
		{"correct verifier verifies", kr, pgphelper.Verified},
		{"no verifier is unverified", nil, pgphelper.Unverified},
		{"unrelated verifier is unverified", other, pgphelper.Unverified},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			body, v, err := decryptBody(armored, kr, tc.verifier)
			if err != nil {
				t.Fatalf("decryptBody: %v", err)
			}
			if body != plain {
				t.Errorf("body = %q, want %q (must survive signature outcome)", body, plain)
			}
			if v != tc.want {
				t.Errorf("verdict = %q, want %q", v, tc.want)
			}
		})
	}
}

func TestResolveFolder(t *testing.T) {
	tests := []struct{ in, want string }{
		{"inbox", "0"},
		{"INBOX", "0"},
		{"trash", "3"},
		{"all", "5"},
		{"starred", "10"},
		{"Sent", "7"},
		{"scheduled", "12"},
		{"Scheduled", "12"},
		{"some-custom-label-id==", "some-custom-label-id=="}, // passthrough
	}
	for _, tc := range tests {
		if got := ResolveFolder(tc.in); got != tc.want {
			t.Errorf("ResolveFolder(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestOppositeKind(t *testing.T) {
	if OppositeKind("conversation") != "message" {
		t.Error("OppositeKind(conversation) should be message")
	}
	if OppositeKind("message") != "conversation" {
		t.Error("OppositeKind(message) should be conversation")
	}
}

func TestListQueryDefaults(t *testing.T) {
	q, err := listQuery(ListOptions{}, false)
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}
	if q.Get("LabelID") != "0" { // empty folder defaults to the inbox
		t.Errorf("LabelID = %q, want 0 (inbox)", q.Get("LabelID"))
	}
	if q.Get("Sort") != "Time" || q.Get("Desc") != "1" {
		t.Errorf("expected Sort=Time Desc=1, got Sort=%q Desc=%q", q.Get("Sort"), q.Get("Desc"))
	}
	// Which page to read is set per request, since one listing may span several.
	if q.Has("Page") || q.Has("PageSize") {
		t.Errorf("the predicates carry a page: %q/%q", q.Get("Page"), q.Get("PageSize"))
	}
}

func TestListQueryFieldMapping(t *testing.T) {
	opts := ListOptions{
		Keyword: "invoice", From: "a@x.com", To: "b@x.com",
		Subject: "hi", Folder: "inbox", PageSize: 10, Unread: true,
	}
	q, err := listQuery(opts, false)
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}
	checks := map[string]string{
		"LabelID": "0",
		"Keyword": "invoice",
		"From":    "a@x.com",
		"To":      "b@x.com",
		"Subject": "hi",
		"Unread":  "1",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	if q.Has("Recipients") {
		t.Error("a message query must not set Recipients")
	}
}

func TestListQueryRecipientsForConversations(t *testing.T) {
	q, err := listQuery(ListOptions{To: "b@x.com", PageSize: 5}, true)
	if err != nil {
		t.Fatalf("listQuery: %v", err)
	}
	if q.Get("Recipients") != "b@x.com" {
		t.Errorf("a thread query should map To\u2192Recipients, got %q", q.Get("Recipients"))
	}
	if q.Has("To") {
		t.Error("a thread query must not set To")
	}
}

func TestListQueryDates(t *testing.T) {
	q, err := listQuery(ListOptions{After: "2020-01-01", Before: "2099-12-31"}, false)
	if err != nil {
		t.Fatalf("valid dates errored: %v", err)
	}
	if q.Get("Begin") == "" || q.Get("End") == "" {
		t.Errorf("expected Begin and End set, got Begin=%q End=%q", q.Get("Begin"), q.Get("End"))
	}
	if _, err := listQuery(ListOptions{After: "nope"}, false); err == nil {
		t.Error("invalid --after should error")
	}
	if _, err := listQuery(ListOptions{Before: "nope"}, false); err == nil {
		t.Error("invalid --before should error")
	}
}

func TestToMessageMapping(t *testing.T) {
	raw := rawListMessage{ID: "id1", Subject: "Hi", Unread: 1, Time: 42, NumAttachments: 2}
	raw.Sender.Name = "Alice"
	raw.Sender.Address = "alice@x.com"
	m := toMessage(raw)
	if m.ID != "id1" || m.Subject != "Hi" || m.Unread != 1 || m.Time != 42 || m.NumAttachments != 2 {
		t.Errorf("toMessage scalar mapping wrong: %+v", m)
	}
	if m.FromName != "Alice" || m.FromAddress != "alice@x.com" {
		t.Errorf("toMessage sender mapping wrong: %+v", m)
	}
}

func TestToConversationMapping(t *testing.T) {
	raw := rawConversation{ID: "c1", Subject: "Thread", NumMessages: 3, NumUnread: 1, NumAttachments: 0, Time: 99}
	raw.Labels = []struct{ ID string }{{ID: "0"}, {ID: "5"}}
	c := toConversation(raw)
	if c.ID != "c1" || c.Subject != "Thread" || c.NumMessages != 3 || c.NumUnread != 1 || c.Time != 99 {
		t.Errorf("toConversation scalar mapping wrong: %+v", c)
	}
	if len(c.Labels) != 2 || c.Labels[0] != "0" || c.Labels[1] != "5" {
		t.Errorf("toConversation labels mapping wrong: %v", c.Labels)
	}
}

// fakeDoer captures the last request issued through the proton.Doer seam.
type fakeDoer struct{ last proton.Request }

func (f *fakeDoer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	f.last = r
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (f *fakeDoer) Decode(_ context.Context, r proton.Request, _ any) error {
	f.last = r
	return nil
}

func bodyLabelID(t *testing.T, r proton.Request) string {
	t.Helper()
	body, ok := r.Body.(map[string]any)
	if !ok {
		t.Fatalf("request body is not map[string]any: %T", r.Body)
	}
	id, _ := body["LabelID"].(string)
	return id
}

func TestTrashHitsLabelEndpoint(t *testing.T) {
	f := &fakeDoer{}
	s := New(f, testKeys(nil))
	if err := s.Trash(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if f.last.Method != "PUT" || f.last.Path != "/mail/v4/messages/label" {
		t.Errorf("Trash issued %s %s", f.last.Method, f.last.Path)
	}
	if got := bodyLabelID(t, f.last); got != labelTrash {
		t.Errorf("Trash LabelID = %q, want %q", got, labelTrash)
	}
}

func TestUnscheduleHitsCancelSendEndpoint(t *testing.T) {
	f := &fakeDoer{}
	s := New(f, testKeys(nil))
	if err := s.Unschedule(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("Unschedule: %v", err)
	}
	// The loop issues one POST per ID; the last captured request is for "b".
	if f.last.Method != "POST" || f.last.Path != "/mail/v4/messages/b/cancel_send" {
		t.Errorf("Unschedule issued %s %s, want POST /mail/v4/messages/b/cancel_send", f.last.Method, f.last.Path)
	}
}

func TestResolveScheduledFullIDPassthrough(t *testing.T) {
	full := strings.Repeat("a", 86) + "=="
	got, err := New(&fakeDoer{}, testKeys(nil)).ResolveScheduled(context.Background(), full)
	if err != nil {
		t.Fatalf("ResolveScheduled: %v", err)
	}
	if got != full {
		t.Errorf("ResolveScheduled(full ID) = %q, want passthrough %q", got, full)
	}
}

func TestResolveScheduledNotFound(t *testing.T) {
	// fakeDoer.Decode ignores the out param, so Search yields zero messages.
	_, err := New(&fakeDoer{}, testKeys(nil)).ResolveScheduled(context.Background(), "no-such-subject")
	var nf *errs.NotFound
	if !errors.As(err, &nf) {
		t.Errorf("ResolveScheduled(miss) err = %v, want *errs.NotFound", err)
	}
}

// Moving, starring and trashing are all one call with a different label, so the
// test that matters is that each verb reaches for the right one.
func TestOrganisingVerbsUseTheRightLabel(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Service) error
		want string
	}{
		{"move to a folder alias", func(s *Service) error {
			return s.Label(context.Background(), []string{"a"}, ResolveFolder("archive"))
		}, labelArchive},
		{"trash", func(s *Service) error {
			return s.Trash(context.Background(), []string{"a"})
		}, labelTrash},
		{"star", func(s *Service) error {
			return s.Label(context.Background(), []string{"a"}, StarredLabelID)
		}, labelStarred},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeDoer{}
			if err := tc.call(New(f, testKeys(nil))); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if got := bodyLabelID(t, f.last); got != tc.want {
				t.Errorf("LabelID = %q, want %q", got, tc.want)
			}
		})
	}
}

// A raw label ID passes through, so anything the account has works wherever a
// built-in name does.
func TestResolveFolderPassesUnknownThrough(t *testing.T) {
	const custom = "aBcD1234=="
	if got := ResolveFolder(custom); got != custom {
		t.Errorf("ResolveFolder(%q) = %q, want passthrough", custom, got)
	}
	if got := ResolveFolder("ARCHIVE"); got != labelArchive {
		t.Errorf("ResolveFolder is case-insensitive: got %q", got)
	}
}

// server stands in for a folder of that many rows, answering a page the way
// Proton does: never more than pageMax rows, and the total whatever the page
// holds.
type server struct {
	rows  int
	asked [][2]int
}

func (s *server) fetch(_ context.Context, page, size int) ([]int, int, error) {
	s.asked = append(s.asked, [2]int{page, size})
	size = min(size, pageMax)
	start := min(page*size, s.rows)
	out := make([]int, 0, min(size, s.rows-start))
	for i := start; i < min(start+size, s.rows); i++ {
		out = append(out, i)
	}
	return out, s.rows, nil
}

func TestWindowReadsTheReadersPageNotProtons(t *testing.T) {
	for _, c := range []struct {
		name       string
		rows       int
		page, size int
		want       []int
		requests   int
	}{
		// An ordinary page is one page Proton can cut itself, so it stays one
		// request however many rows are behind it.
		{"a page Proton serves", 4812, 0, 25, seq(0, 25), 1},
		{"a later page Proton serves", 4812, 3, 25, seq(75, 100), 1},
		{"exactly Proton's width", 4812, 1, pageMax, seq(150, 300), 1},
		// Wider than Proton serves: composed from its pages and cut down, so
		// the reader gets the number they asked for.
		{"wider than Proton serves", 4812, 0, 500, seq(0, 500), 4},
		{"a later wide page", 4812, 1, 200, seq(200, 400), 2},
		// The whole collection, which is what a size of zero means.
		{"everything", 380, 0, 0, seq(0, 380), 3},
		{"everything, exactly filling Proton's pages", 300, 0, 0, seq(0, 300), 3},
		{"everything of nothing", 0, 0, 0, nil, 1},
		// A page past the end is empty rather than short of what came before.
		{"past the end", 380, 2, 500, nil, 1},
		{"a short last page", 380, 1, 200, seq(200, 380), 2},
	} {
		srv := &server{rows: c.rows}
		got, total, err := window(context.Background(), c.page, c.size, srv.fetch)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if total != c.rows {
			t.Errorf("%s: total = %d, want %d", c.name, total, c.rows)
		}
		if !sameInts(got, c.want) {
			t.Errorf("%s: rows = %v, want %v", c.name, brief(got), brief(c.want))
		}
		if len(srv.asked) != c.requests {
			t.Errorf("%s: %d requests %v, want %d", c.name, len(srv.asked), srv.asked, c.requests)
		}
	}
}

// Half a listing presented as a whole one is a wrong answer, so a page that
// fails fails the call.
func TestWindowFailsWholeWhenAPageFails(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := window(context.Background(), 0, 0, func(_ context.Context, page, _ int) ([]int, int, error) {
		if page == 1 {
			return nil, 0, boom
		}
		return seq(0, pageMax), 400, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func seq(from, to int) []int {
	out := make([]int, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, i)
	}
	return out
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func brief(rows []int) string {
	if len(rows) == 0 {
		return "nothing"
	}
	return fmt.Sprintf("%d rows %d..%d", len(rows), rows[0], rows[len(rows)-1])
}

// testKeys hands a service the key hierarchy a test wants it to decrypt with.
// A test that decrypts nothing passes nil, which is never asked for.
func testKeys(u *keys.Unlocked) keys.Get {
	return func(context.Context) (*keys.Unlocked, error) {
		if u == nil {
			return nil, errors.New("this test has no keys")
		}
		return u, nil
	}
}
