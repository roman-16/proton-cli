package mail

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"

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

func TestOppositeKind(t *testing.T) {
	if OppositeKind("conversation") != "message" {
		t.Error("OppositeKind(conversation) should be message")
	}
	if OppositeKind("message") != "conversation" {
		t.Error("OppositeKind(message) should be conversation")
	}
}

func TestListQueryDefaults(t *testing.T) {
	q := listQuery(ListOptions{}, false)
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
		Subject: "hi", Folder: labelInbox, PageSize: 10, Unread: true,
	}
	q := listQuery(opts, false)
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

func TestListQuerySelectsByAddressAttachmentsAndReadState(t *testing.T) {
	q := listQuery(ListOptions{Read: true, HasAttachments: true, AddressID: "addr-1"}, false)
	for k, want := range map[string]string{"Unread": "0", "Attachments": "1", "AddressID": "addr-1"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
	for _, k := range []string{"Unread", "Attachments", "AddressID"} {
		if q := listQuery(ListOptions{}, true); q.Has(k) {
			t.Errorf("a query that asked for nothing carries %s=%q", k, q.Get(k))
		}
	}
}

func TestListQueryRecipientsForConversations(t *testing.T) {
	q := listQuery(ListOptions{To: "b@x.com", PageSize: 5}, true)
	if q.Get("Recipients") != "b@x.com" {
		t.Errorf("a thread query should map To\u2192Recipients, got %q", q.Get("Recipients"))
	}
	if q.Has("To") {
		t.Error("a thread query must not set To")
	}
}

// Both named days are included whole, so the window opens at the midnight the
// first day begins with and closes at the last second of the last.
func TestListQueryDates(t *testing.T) {
	after := time.Date(2026, 3, 4, 0, 0, 0, 0, time.Local)
	before := time.Date(2026, 3, 6, 17, 42, 0, 0, time.Local)
	q := listQuery(ListOptions{After: after, Before: before}, false)

	wantBegin := after.Unix()
	wantEnd := time.Date(2026, 3, 7, 0, 0, 0, 0, time.Local).Unix() - 1
	if q.Get("Begin") != strconv.FormatInt(wantBegin, 10) {
		t.Errorf("Begin = %q, want %d", q.Get("Begin"), wantBegin)
	}
	if q.Get("End") != strconv.FormatInt(wantEnd, 10) {
		t.Errorf("End = %q, want %d", q.Get("End"), wantEnd)
	}

	if q := listQuery(ListOptions{}, false); q.Has("Begin") || q.Has("End") {
		t.Errorf("a query with no days bounds the range: Begin=%q End=%q", q.Get("Begin"), q.Get("End"))
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
			box, err := s.ResolveMailbox(context.Background(), "archive")
			if err != nil {
				return err
			}
			return s.Label(context.Background(), []string{"a"}, box.ID)
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
