package mail

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

func makeBatch(messages ...struct {
	ID      string
	Action  int
	Message *rawListMessage
}) eventBatch {
	return eventBatch{Messages: messages}
}

func event(id string, action, unread int, flags int64, labels []string, conv string) struct {
	ID      string
	Action  int
	Message *rawListMessage
} {
	return struct {
		ID      string
		Action  int
		Message *rawListMessage
	}{
		ID: id, Action: action,
		Message: &rawListMessage{
			ID: id, ConversationID: conv, Subject: "Subject " + id, Unread: unread,
			Sender:   struct{ Name, Address string }{Name: "Fastmail", Address: "billing@fastmail.com"},
			LabelIDs: labels,
			Flags:    flags,
		},
	}
}

func inbox() WatchOptions { return WatchOptions{In: []Mailbox{{ID: labelInbox, Name: "inbox"}}} }

// An arrival is a created, unread, unimported message in a watched place. A
// saved draft, a filed copy, an imported message, or one in another folder is
// not.
func TestArrivalsFilterASetOfCreates(t *testing.T) {
	b := makeBatch(
		event("1", eventCreate, 0, 0, []string{labelDrafts}, "1"),
		event("2", eventCreate, 1, flagImported, []string{labelInbox}, "2"),
		event("3", eventCreate, 1, 0, []string{labelArchive}, "3"),
		event("4", eventCreate, 1, 0, []string{labelInbox}, "4"),
	)

	got := b.arrivals(inbox())
	if len(got) != 1 {
		t.Fatalf("arrivals = %d, want 1", len(got))
	}
	if got[0].ID != "4" {
		t.Errorf("arrivals[0].ID = %q, want the plain inbox message", got[0].ID)
	}
}

// A message already read is not an arrival, and neither is one without a
// watched label.
func TestArrivalsSkipReadAndOtherFolders(t *testing.T) {
	b := makeBatch(
		event("5", eventCreate, 0, 0, []string{labelInbox}, "5"),
		event("6", eventCreate, 1, 0, []string{labelArchive}, "6"),
	)
	if got := b.arrivals(inbox()); len(got) != 0 {
		t.Fatalf("arrivals reported %d, want 0", len(got))
	}
}

func TestArrivalsCountACategoryOnlyWhileItsMessageIsInTheInbox(t *testing.T) {
	b := makeBatch(
		event("7", eventCreate, 1, 0, []string{labelInbox, labelSocial}, "7"),
		event("8", eventCreate, 1, 0, []string{labelArchive, labelSocial}, "8"),
		event("9", eventCreate, 1, 0, []string{labelArchive, labelStarred}, "9"),
	)
	social := WatchOptions{In: []Mailbox{{ID: labelSocial}, {ID: labelStarred}}}
	got := b.arrivals(social)
	if len(got) != 2 || got[0].ID != "7" || got[1].ID != "9" {
		t.Fatalf("arrivals = %v, want the social message in the inbox and the starred one", got)
	}
}

type answers map[string]string

func (a answers) Do(context.Context, proton.Request) (*proton.Response, error) {
	return nil, fmt.Errorf("Do is not answered here")
}

func (a answers) Decode(_ context.Context, r proton.Request, out any) error {
	key := r.Method + " " + r.Path
	if q := r.Query.Encode(); q != "" {
		key += "?" + q
	}
	body, ok := a[key]
	if !ok {
		return fmt.Errorf("no answer for %s", key)
	}
	return json.Unmarshal([]byte(body), out)
}

const noCategories = `{"Labels":[{"ID":"0","Name":"Inbox","Display":1,"Notify":1}]}`

const sixCategories = `{"Labels":[` +
	`{"ID":"0","Name":"Inbox","Display":1,"Notify":1},` +
	`{"ID":"20","Name":"Social","Display":1,"Notify":0},` +
	`{"ID":"21","Name":"Promotions","Display":1,"Notify":1},` +
	`{"ID":"22","Name":"Updates","Display":0,"Notify":0},` +
	`{"ID":"24","Name":"Primary","Display":1,"Notify":1},` +
	`{"ID":"25","Name":"Newsletters","Display":1,"Notify":0},` +
	`{"ID":"26","Name":"Transactions","Display":0,"Notify":1}]}`

func watchedNames(t *testing.T, categoryView, systemFolders string) []string {
	t.Helper()
	s := New(answers{
		"GET /mail/v4/settings":      `{"MailSettings":{"MailCategoryView":` + categoryView + `}}`,
		"GET /core/v4/labels?Type=1": `{"Labels":[]}`,
		"GET /core/v4/labels?Type=3": `{"Labels":[` +
			`{"ID":"f1","Name":"Receipts","Type":3,"Notify":1},` +
			`{"ID":"f2","Name":"Quiet","Type":3,"Notify":0}]}`,
		"GET /core/v4/labels?Type=4": systemFolders,
	}, testKeys(nil))
	in, err := s.WatchedIn(context.Background(), "")
	if err != nil {
		t.Fatalf("WatchedIn: %v", err)
	}
	names := make([]string, 0, len(in))
	for _, box := range in {
		names = append(names, box.Name)
	}
	return names
}

func TestWatchedInFollowsWhatProtonNotifiesAbout(t *testing.T) {
	for _, tc := range []struct {
		name, categoryView, systemFolders string
		want                              []string
	}{
		{"categories off", "false", sixCategories, []string{"inbox", "starred", "Receipts"}},
		{"categories on", "true", sixCategories,
			[]string{"primary", "promotions", "transactions", "updates", "starred", "Receipts"}},
		{"categories on without any", "true", noCategories, []string{"inbox", "starred", "Receipts"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := watchedNames(t, tc.categoryView, tc.systemFolders); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("watched %q, want %q", got, tc.want)
			}
		})
	}
}

type stopped struct{ answers }

func (stopped) Decode(ctx context.Context, r proton.Request, _ any) error {
	return fmt.Errorf("%s %s: %w", r.Method, r.Path, ctx.Err())
}

func TestAWatchStoppedBeforeItsFirstPollStopsCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(stopped{}, testKeys(nil))
	if err := s.Watch(ctx, WatchOptions{}, func(Message) error { return nil }); err != nil {
		t.Errorf("Watch = %v, want a clean stop", err)
	}
}

// The narrowed filters reach the same substrings a listing's --from and
// --subject do.
func TestMatchesHonoursFromAndSubject(t *testing.T) {
	m := &rawListMessage{
		Subject:  "Invoice #2291 ready",
		Sender:   struct{ Name, Address string }{Name: "Fastmail Billing", Address: "billing@fastmail.com"},
		LabelIDs: []string{labelInbox},
	}

	if !m.matches(inbox()) {
		t.Errorf("no filter rejected an inbox match")
	}
	if !m.matches(WatchOptions{In: []Mailbox{{ID: labelInbox}}, Subject: "invoice"}) {
		t.Errorf("--subject invoice should have matched (case-insensitively)")
	}
	if !m.matches(WatchOptions{In: []Mailbox{{ID: labelInbox}}, From: "billing@"}) {
		t.Errorf("--from billing@ should have matched the address")
	}
	if m.matches(WatchOptions{In: []Mailbox{{ID: labelInbox}}, From: "nobody@"}) {
		t.Errorf("--from nobody@ should not have matched")
	}
	if m.matches(WatchOptions{In: []Mailbox{{ID: labelArchive}}}) {
		t.Errorf("a message in another folder matched a watch on this one")
	}
}
