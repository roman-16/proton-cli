package mail

import (
	"testing"
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
