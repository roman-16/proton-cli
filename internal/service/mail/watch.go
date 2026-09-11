package mail

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Watching the mailbox is asking Proton what has changed since last time.
//
// Proton keeps one feed of everything that happens to an account, addressed by a
// cursor: /core/v4/events/latest hands out the cursor for "now", and
// /core/v5/events/{cursor} answers with what has happened since and the cursor to
// ask with next. That is how every Proton client learns about new mail - the web
// clients, and Bridge through go-proton-api - and there is nothing to subscribe
// to, so this polls at the same 30 seconds the web clients use.
//
// A watch starts at the latest cursor rather than a remembered one. It reports
// what happens while it is watching, which is what a notification is; replaying
// a night's mail into somebody's notification daemon at breakfast is not.

const (
	// watchPeriod is INTERVAL_EVENT_TIMER, the interval Proton's own web clients
	// poll this feed at and the floor their override flag is clamped to.
	watchPeriod = 30 * time.Second
	// maxDrain caps how many pages one poll will follow when Proton says there
	// is more, so a long backlog cannot hold the loop indefinitely. It is
	// go-proton-api's maxCollectedEvents.
	maxDrain = 50
)

// watchBackoff multiplies the period after a failed poll, and is the Fibonacci
// series the web clients back off along. The last step is where it stays until a
// poll succeeds and resets it - or, here, where the watch gives up: a watcher
// that has been failing for the whole series is not watching anything, and
// saying so is better than looking alive.
var watchBackoff = [...]int{1, 1, 2, 3, 5, 8, 13}

// The two of Proton's event actions an arrival is told by: a message is posted
// as a creation, and a thread returns from snooze as a change of flags.
const (
	eventCreate = 1
	eventFlags  = 3
)

// WatchOptions says which arrivals are worth reporting.
type WatchOptions struct {
	// In is where to watch. WatchedIn works out the default.
	In []Mailbox
	// From and Subject narrow by substring, the way the same flags narrow a
	// listing.
	From, Subject string
}

// WatchedIn resolves where a watch looks.
//
// Named, it is that one place. Unnamed, it is the inbox together with every
// folder whose notifications are on - the same set Proton's clients notify from,
// and the reason `mail settings folders list` prints a NOTIFY column: what this
// returns is a thing the reader can look up rather than a rule they have to be
// told.
func (s *Service) WatchedIn(ctx context.Context, folder string) ([]Mailbox, error) {
	if folder != "" {
		box, err := s.ResolveMailbox(ctx, folder)
		if err != nil {
			return nil, err
		}
		return []Mailbox{box}, nil
	}
	_, folders, err := s.LabelsList(ctx)
	if err != nil {
		return nil, err
	}
	in := []Mailbox{{ID: labelInbox, Name: "inbox", Folder: true, System: true}}
	for _, f := range folders {
		if f.Notifies() {
			in = append(in, Mailbox{ID: f.ID, Name: f.Name, Folder: true})
		}
	}
	return in, nil
}

// Watch reports messages as they land, until the context ends.
//
// A context that ends is how a watch stops, so it is not a failure: Ctrl+C and a
// service manager's SIGTERM both arrive that way, and both mean the reader is
// done rather than that something went wrong.
func (s *Service) Watch(ctx context.Context, opts WatchOptions, emit func(Message) error) error {
	cursor, err := s.LatestEventID(ctx)
	if err != nil {
		return err
	}
	retry := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(watchPeriod * time.Duration(watchBackoff[retry])):
		}
		next, err := s.poll(ctx, cursor, opts, emit)
		switch {
		case err == nil:
			cursor, retry = next, 0
		case ctx.Err() != nil:
			return nil
		// A session Proton no longer honours is not going to start working, and
		// the client has already tried to refresh it by the time this is seen.
		case errors.Is(err, proton.ErrUnauthorized), retry == len(watchBackoff)-1:
			return err
		default:
			retry++
		}
	}
}

// LatestEventID is the cursor for "from now on".
func (s *Service) LatestEventID(ctx context.Context) (string, error) {
	var r struct{ EventID string }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/events/latest",
	}, &r); err != nil {
		return "", err
	}
	return r.EventID, nil
}

// poll reads everything waiting at the cursor and returns the cursor to ask with
// next.
//
// Proton answers one page at a time and says whether more is waiting, so a poll
// keeps asking until it has caught up. Refresh means the account changed too much
// for the feed to describe - the answer to which is to take a fresh cursor and
// carry on watching, not to report a mailbox's worth of mail as though it had
// just arrived.
func (s *Service) poll(ctx context.Context, cursor string, opts WatchOptions, emit func(Message) error) (string, error) {
	for page := 0; page < maxDrain; page++ {
		var batch eventBatch
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/core/v5/events/" + cursor,
		}, &batch); err != nil {
			return cursor, err
		}
		if batch.Refresh != 0 {
			return s.LatestEventID(ctx)
		}
		cursor = batch.EventID
		for _, m := range batch.arrivals(opts) {
			if err := emit(m); err != nil {
				return cursor, err
			}
		}
		if batch.More == 0 {
			break
		}
	}
	return cursor, nil
}

// eventBatch is one page of the feed, reduced to what an arrival needs.
type eventBatch struct {
	EventID string
	More    int
	Refresh int

	Messages []struct {
		ID      string
		Action  int
		Message *rawListMessage
	}
	Conversations []struct {
		ID           string
		Action       int
		Conversation *struct {
			ID                     string
			DisplaySnoozedReminder bool
			LabelIDsRemoved        []string
		}
	}
}

// arrivals are the messages in this page worth telling somebody about.
//
// Two things put a message here, which is what the web clients notify on as
// well. One is post: a message created unread in a watched place. The other is a
// thread coming back from snooze, which is mail landing in the inbox again at a
// moment the reader chose - the conversation says it is due, and the message to
// report is its newest.
func (b eventBatch) arrivals(opts WatchOptions) []Message {
	var out []Message
	for _, e := range b.Messages {
		if e.Action == eventCreate && e.Message.arrived(opts) {
			out = append(out, toMessage(*e.Message))
		}
	}
	for _, e := range b.Conversations {
		c := e.Conversation
		if e.Action != eventFlags || c == nil || !c.DisplaySnoozedReminder {
			continue
		}
		if !hasLabel(c.LabelIDsRemoved, labelSnoozed) {
			continue
		}
		if m := b.newestIn(c.ID, opts); m != nil {
			out = append(out, *m)
		}
	}
	return out
}

// newestIn finds the latest message of a conversation in this page, which is the
// one a thread is worth naming by.
func (b eventBatch) newestIn(conversation string, opts WatchOptions) *Message {
	var newest *rawListMessage
	for _, e := range b.Messages {
		m := e.Message
		if m == nil || m.ConversationID != conversation || !m.matches(opts) {
			continue
		}
		if newest == nil || m.Time > newest.Time {
			newest = m
		}
	}
	if newest == nil {
		return nil
	}
	msg := toMessage(*newest)
	return &msg
}

// arrived reports whether a created message is one that just came in, rather
// than one the account produced or absorbed: a draft saved, a copy filed in
// Sent, or a mailbox imported. The web clients leave an import out of their
// notifications too, because importing a mailbox is not fifty thousand new
// messages.
func (m *rawListMessage) arrived(opts WatchOptions) bool {
	if m == nil || m.Unread != 1 || m.Flags&flagImported != 0 {
		return false
	}
	return m.matches(opts)
}

func (m *rawListMessage) matches(opts WatchOptions) bool {
	if m == nil {
		return false
	}
	watched := false
	for _, box := range opts.In {
		if hasLabel(m.LabelIDs, box.ID) {
			watched = true
			break
		}
	}
	if !watched {
		return false
	}
	if opts.From != "" && !has(m.Sender.Address, opts.From) && !has(m.Sender.Name, opts.From) {
		return false
	}
	return opts.Subject == "" || has(m.Subject, opts.Subject)
}

func has(haystack, needle string) bool {
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}
