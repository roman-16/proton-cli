package mail

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/mailtext"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/search"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Indexing the mailbox is reading all of it once and then following the same
// change feed a watch follows.
//
// It is read twice over, because the mailbox answers two different questions at
// two different prices. What every message is - who it is from, when it arrived,
// which folder it is in - comes back a hundred and fifty at a time, so the whole
// of it is minutes. What a message says is a request each, so the same mailbox
// is hours. Walking the envelopes first means a filtered listing is answered
// from here within the minute, the count a build is working towards is exact
// rather than a page's worth of guess, and every thread is known before a single
// body is read - which is what makes it safe to leave a quoted reply out.
//
// Bodies then follow newest first, which is the order Proton's own client
// indexes in: the half of a mailbox somebody is going to search for is the
// recent half, so a download that is interrupted at any point has covered the
// part that matters most, and a search over it says how far it reaches.

const (
	// indexPage is how many messages of metadata one request brings back, which
	// is the most Proton will answer with.
	indexPage = pageMax
	// bodiesAtOnce is how many bodies are fetched in parallel. It is the width
	// the CLI already uses for the other place it fetches many small things at
	// once, and it is what keeps a build from spending its whole time waiting
	// for one round trip after another.
	bodiesAtOnce = 10
)

// stored is one message as the index holds it: everything a listing shows,
// everything a filter is judged against, and the body as text.
//
// It is the decrypted message, so it lives only inside a sealed record. The
// body is text rather than the HTML it may have arrived as, because what a
// search matches is what a reader would have read.
type stored struct {
	ID             string      `json:"id"`
	ConversationID string      `json:"conversation,omitempty"`
	Subject        string      `json:"subject,omitempty"`
	SenderName     string      `json:"sender_name,omitempty"`
	SenderAddress  string      `json:"sender_address,omitempty"`
	To             []addressee `json:"to"`
	CC             []addressee `json:"cc"`
	BCC            []addressee `json:"bcc"`
	Time           int64       `json:"time"`
	Order          int64       `json:"order,omitempty"`
	Size           int64       `json:"size,omitempty"`
	Unread         int         `json:"unread,omitempty"`
	Flags          int64       `json:"flags,omitempty"`
	Labels         []string    `json:"labels"`
	Attachments    int         `json:"attachments,omitempty"`
	Expires        int64       `json:"expires,omitempty"`
	Body           string      `json:"body,omitempty"`
	// Opened says the body was fetched and read. A message whose body would not
	// open is indexed without one, so it is still found by everything else it
	// says about itself.
	Opened bool `json:"opened,omitempty"`
	// Unreadable says the body was fetched and would not open, which is a
	// different state from not having been fetched at all: one is settled, and
	// the other is what the next run carries on with.
	Unreadable bool `json:"unreadable,omitempty"`
}

type addressee struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
}

// settled reports that the index is not waiting on this message's body: it has
// the text, or it has established that the text will not open.
func (s stored) settled() bool { return s.Opened || s.Unreadable }

// message is the row a listing shows, rebuilt from what was indexed.
func (s stored) message() Message {
	v := verdicts(s.Flags)
	return Message{
		ID: s.ID, ConversationID: s.ConversationID, Subject: s.Subject,
		FromName: s.SenderName, FromAddress: s.SenderAddress,
		Time: s.Time, Unread: s.Unread, NumAttachments: s.Attachments, Labels: s.Labels,
		Size:        s.Size,
		DMARCFailed: v.dmarcFailed, MarkedLegitimate: v.markedLegitimate,
		Phishing: v.phishing, Suspicious: v.suspicious,
	}
}

// recipients is every address and name the message was addressed to, which is
// what --to is matched against.
func (s stored) recipients() []string {
	out := make([]string, 0, 2*(len(s.To)+len(s.CC)+len(s.BCC)))
	for _, list := range [][]addressee{s.To, s.CC, s.BCC} {
		for _, p := range list {
			out = append(out, p.Name, p.Address)
		}
	}
	return out
}

// envelopeIs reports whether the index already says exactly this about a
// message, which is what keeps a walk of a mailbox that has not changed from
// rewriting every record in it.
func (s stored) envelopeIs(other stored) bool {
	return s.ID == other.ID && s.ConversationID == other.ConversationID &&
		s.Subject == other.Subject && s.SenderName == other.SenderName &&
		s.SenderAddress == other.SenderAddress && s.Time == other.Time &&
		s.Order == other.Order && s.Unread == other.Unread && s.Flags == other.Flags &&
		s.Attachments == other.Attachments && s.Expires == other.Expires &&
		sameLabels(s.Labels, other.Labels) &&
		slices.Equal(s.To, other.To) && slices.Equal(s.CC, other.CC) &&
		slices.Equal(s.BCC, other.BCC)
}

// sameLabels compares two sets of labels as sets: which folders a message is in
// is what the index holds, and Proton is free to list them in whatever order.
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// SetIndex hands the service the profile's index directory, which is the one
// thing about it decided outside: where this machine keeps its files.
func (s *Service) SetIndex(store *search.Store) { s.index = store }

// indexPart is the mail app as the index commands see it.
type indexPart struct{ s *Service }

// IndexPart is what `search` drives to keep the mail index current.
func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

func (indexPart) App() search.App { return search.AppMail }
func (indexPart) Noun() string    { return "messages" }

func (p indexPart) Open(ctx context.Context) (search.Session, error) { return p.s.openIndex(ctx) }

// Count is how much a run would do: the messages All Mail holds that the index
// has never seen, and the bodies it is still owed. It is the first page of the
// walk a build would do, so a preview costs one request rather than doing the
// work to find out.
func (p indexPart) Count(ctx context.Context) (int, error) {
	total, err := p.s.mailboxCount(ctx)
	if err != nil {
		return 0, err
	}
	status, err := p.s.index.Status(search.AppMail)
	if err != nil {
		if errors.Is(err, search.ErrNotIndexed) {
			return total, nil
		}
		return 0, err
	}
	return max(total-status.Indexed, 0) + max(status.Indexed-status.Bodies, 0), nil
}

// indexSession is the mail index, open: the log decrypted once, and what this
// run has worked out about what is in it.
type indexSession struct {
	s   *Service
	log *search.Log

	// surveyed says the records have been read through, which is what the three
	// fields below are made of. It is put off until something needs them: a
	// catch-up that finds an empty feed - which is what a search's is, nearly
	// every time - needs none of them, and reading them is the whole file.
	surveyed bool
	// oldest is the time of the earliest message each thread has in the index,
	// which is what says whether a reply's quoted history is already covered.
	oldest map[string]int64
	// bodies is how many messages are not waiting on their text, unreadable how
	// many of those went in without it, and owed the ones still waiting, by ID,
	// with when each arrived, which is the order the text is fetched in.
	bodies     int
	unreadable int
	owed       map[string]int64
}

func (s *Service) openIndex(ctx context.Context) (*indexSession, error) {
	log, err := s.index.Load(ctx, search.AppMail)
	if err != nil {
		return nil, err
	}
	return &indexSession{s: s, log: log, oldest: map[string]int64{}, owed: map[string]int64{}}, nil
}

func (x *indexSession) Status() search.Status {
	if x.surveyed {
		x.counts()
	}
	return x.log.Status()
}

// Build brings the index to every message the account holds, and then to the
// bodies of the ones it is owed.
//
// The walk runs when the index has never covered the mailbox, and when Proton
// has said it cannot describe what has happened since - which is the only way
// to find out what changed while nothing was watching.
func (x *indexSession) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	var done search.Result
	if !x.log.State.Complete || x.log.State.Stale {
		walked, err := x.walkMailbox(ctx, sink)
		done = search.Total(done, walked)
		if err != nil {
			return done, err
		}
	}
	fetched, err := x.fetchOwedBodies(ctx, sink)
	return search.Total(done, fetched), err
}

// Sync applies everything that has happened to the mailbox since the index was
// last brought up to date.
func (x *indexSession) Sync(ctx context.Context) (search.Result, error) { return x.syncIndex(ctx) }

// survey reads what is in the log, which is where the run's picture of it
// starts: which threads it holds and from when, and which messages it owes a
// body to.
func (x *indexSession) survey(ctx context.Context) {
	if x.surveyed {
		return
	}
	x.surveyed = true
	for _, rec := range x.log.Records() {
		var in stored
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindMessage, rec.ID, skip.Malformed, err)
			continue
		}
		x.reckon(stored{}, false, in)
	}
	x.counts()
}

// reckon takes one record into the run's picture of the index: a thread may
// start earlier than anything else it holds, and a message may have gained the
// body it was owed or be owed one for the first time.
func (x *indexSession) reckon(before stored, had bool, now stored) {
	if now.ConversationID != "" {
		if at, ok := x.oldest[now.ConversationID]; !ok || now.Time < at {
			x.oldest[now.ConversationID] = now.Time
		}
	}
	settled, unreadable := had && before.settled(), had && before.Unreadable
	switch {
	case now.settled() && !settled:
		x.bodies++
	case !now.settled() && settled:
		x.bodies--
	}
	if now.settled() {
		delete(x.owed, now.ID)
	} else {
		x.owed[now.ID] = now.Time
	}
	switch {
	case now.Unreadable && !unreadable:
		x.unreadable++
	case !now.Unreadable && unreadable:
		x.unreadable--
	}
}

// dropped takes a message the account no longer has out of the picture.
func (x *indexSession) dropped(before stored, had bool) {
	if !had {
		return
	}
	delete(x.owed, before.ID)
	if before.settled() {
		x.bodies--
	}
	if before.Unreadable {
		x.unreadable--
	}
}

// counts hands what the run has kept to the state beside the log, which is what
// `index list` reads and what a search says its answer covers.
//
// Every message has a body to fetch, so what the index owes text for is the
// whole of what it holds.
func (x *indexSession) counts() {
	x.log.State.Bodies, x.log.State.Unreadable = x.bodies, x.unreadable
	x.log.State.Texts = x.log.State.Indexed
}

// counted reports whether writing this record is worth reporting as indexed.
//
// A message is counted once a run, when the index takes in something about it
// that it did not have. An envelope written for a message whose body is still
// owed is half an arrival, and the half worth a line is the body: counting both
// would report a mailbox of ten thousand as twenty.
func counted(now stored) bool { return now.settled() }

// owing reports whether the summary beside the log says bodies are still to be
// fetched.
//
// The summary is written after the work it summarises, so a run stopped between
// the two leaves one that says less than the log holds - never more. One that
// says nothing is owed can be taken at its word; one that says something is has
// to be checked against the log, which is what a survey does.
func (x *indexSession) owing() bool { return x.log.State.Bodies < x.log.State.Indexed }

// held is what the index says about one message.
func (x *indexSession) held(id string) (stored, bool) {
	rec, ok := x.log.Get(id)
	if !ok {
		return stored{}, false
	}
	var in stored
	if err := json.Unmarshal(rec.Data, &in); err != nil {
		return stored{}, false
	}
	return in, true
}

// save writes the state file, with the counts this run has kept, and with the
// log read through first where the summary claims there is work to do.
func (x *indexSession) save(ctx context.Context) error {
	if x.owing() {
		x.survey(ctx)
	}
	if x.surveyed {
		x.counts()
	}
	return x.log.Save(time.Now().Unix())
}

// walkMailbox reads every message's envelope, newest first, and leaves the index
// holding exactly what the account holds.
//
// It is a reconciliation rather than a continuation: what is there is compared
// with what came back, what changed is written again, and what the mailbox no
// longer has is taken out. That is what makes it the answer to a feed that
// could not say what happened - following a feed can only apply what it is
// told, and the one thing it has said is that it cannot tell.
//
// The cursor into the change feed is taken before the first page rather than
// after the last, so a message that arrives during a walk is caught by the sync
// that follows instead of falling into the gap between the two.
func (x *indexSession) walkMailbox(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log := x.log
	if log.State.Cursor == "" {
		cursor, err := x.s.LatestEventID(ctx)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		if err := x.save(ctx); err != nil {
			return search.Result{}, err
		}
	}
	x.survey(ctx)
	// How many there are is asked without an anchor, because a page of the walk
	// is answered with how many remain from where it starts: a walk that took
	// its count from its last page would report a mailbox of fifty-five messages
	// having indexed ten thousand.
	total, err := x.s.mailboxCount(ctx)
	if err != nil {
		return search.Result{}, err
	}
	log.State.Total = total
	// How much there is to index is written down as soon as it is known, so a
	// walk interrupted on its first page still leaves an index that can say how
	// little of the mailbox it covers.
	if err := x.save(ctx); err != nil {
		return search.Result{}, err
	}
	progress.Counting(sink, "messages")
	sink.Start(int64(total), "Indexing mail")

	var done search.Result
	seen := make(map[string]bool, total)
	anchor := ""
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		page, _, err := x.s.indexPageOf(ctx, anchor, indexPage)
		if err != nil {
			return done, err
		}
		if len(page) == 0 {
			break
		}
		written, err := x.writeEnvelopes(page, seen)
		done = search.Total(done, written)
		if err != nil {
			return done, err
		}
		last := page[len(page)-1]
		anchor = anchorOf(last)
		if oldest := last.Time; oldest > 0 && (log.State.Oldest == 0 || oldest < log.State.Oldest) {
			log.State.Oldest = oldest
		}
		sink.Add(int64(len(page)))
		if err := x.save(ctx); err != nil {
			return done, err
		}
		if len(page) < indexPage {
			break
		}
	}

	// Only a walk that reached the end of the mailbox knows that what it did not
	// see is not there any more.
	gone, err := x.tombstoneUnseen(seen)
	done = search.Total(done, gone)
	if err != nil {
		return done, err
	}
	log.State.Complete, log.State.Stale = true, false
	sink.Done()
	return done, x.save(ctx)
}

// writeEnvelopes writes down what one page of the walk says, leaving alone the
// messages the index already says the same thing about.
func (x *indexSession) writeEnvelopes(page []rawListMessage, seen map[string]bool) (search.Result, error) {
	var done search.Result
	records := make([]search.Record, 0, len(page))
	for _, m := range page {
		seen[m.ID] = true
		before, had := x.held(m.ID)
		in := restated(before, m)
		if had && before.envelopeIs(in) {
			continue
		}
		rec, err := record(in)
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		x.reckon(before, had, in)
		if counted(in) {
			done.Indexed++
		}
	}
	return done, x.log.Append(records...)
}

// tombstoneUnseen takes out what the account no longer has.
//
// A message deleted while nothing was following the feed leaves no trace to
// apply: the only thing that says it is gone is that a reading of the whole
// mailbox did not come across it.
func (x *indexSession) tombstoneUnseen(seen map[string]bool) (search.Result, error) {
	var done search.Result
	var records []search.Record
	for _, rec := range x.log.Records() {
		if seen[rec.ID] {
			continue
		}
		before, had := x.held(rec.ID)
		records = append(records, search.Record{ID: rec.ID, Gone: true})
		x.dropped(before, had)
		done.Removed++
	}
	return done, x.log.Append(records...)
}

// fetchOwedBodies downloads the text of every message the index holds without
// it, newest first.
//
// An index that owes nothing is left unread. Working out what is owed means
// reading every record, and the commonest run there is - a catch-up on an index
// that is already current - would otherwise pay for the whole mailbox to find
// out that there is nothing to do.
func (x *indexSession) fetchOwedBodies(ctx context.Context, sink progress.Sink) (search.Result, error) {
	if x.owing() {
		x.survey(ctx)
	}
	wanted := x.owedNow()
	if len(wanted) == 0 {
		return search.Result{}, nil
	}
	progress.Counting(sink, "bodies")
	sink.Start(int64(x.log.State.Indexed), "Indexing mail bodies")
	progress.Resume(sink, int64(x.log.State.Indexed-len(wanted)))

	var done search.Result
	for len(wanted) > 0 {
		batch := wanted[:min(indexPage, len(wanted))]
		wanted = wanted[len(batch):]
		got, err := x.fetchBodies(ctx, batch, sink)
		done = search.Total(done, got)
		if err != nil {
			return done, err
		}
	}
	sink.Done()
	return done, x.save(ctx)
}

// owedNow is what the index is still owed a body for, newest first, as the
// envelopes to write once the text is there. A message stays owed until its
// text is written, so one whose fetch failed is asked for again by the next
// run, whether that is a new process or the next poll of the same one.
func (x *indexSession) owedNow() []stored {
	out := make([]stored, 0, len(x.owed))
	for id := range x.owed {
		in, had := x.held(id)
		if !had || in.settled() {
			delete(x.owed, id)
			continue
		}
		out = append(out, in)
	}
	slices.SortFunc(out, func(a, b stored) int {
		return cmp.Or(cmp.Compare(b.Time, a.Time), cmp.Compare(a.ID, b.ID))
	})
	return out
}

// fetchBodies downloads the text of the messages it is given and writes them
// down whole.
//
// What it is given is the envelope the index will hold, so one worker covers a
// build filling in what it walked and a catch-up taking in what just changed.
func (x *indexSession) fetchBodies(ctx context.Context, want []stored, sink progress.Sink) (search.Result, error) {
	u, err := x.s.keys(ctx)
	if err != nil {
		return search.Result{}, err
	}
	filled := make([]stored, len(want))
	unreadable := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, bodiesAtOnce)
	var failure error
	for i, in := range want {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			got, ok, err := x.fetchBody(ctx, u, in)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil && failure == nil && ctx.Err() == nil:
				failure = err
			case err == nil && ok:
				filled[i] = got
				if got.Unreadable {
					unreadable++
				}
			}
			if err == nil {
				sink.Add(1)
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		return search.Result{}, failure
	}

	var done search.Result
	records := make([]search.Record, 0, len(filled))
	for _, in := range filled {
		if in.ID == "" {
			continue
		}
		rec, err := record(in)
		if err != nil {
			return done, err
		}
		records = append(records, rec)
		before, had := x.held(in.ID)
		x.reckon(before, had, in)
		if counted(in) {
			done.Indexed++
		}
	}
	done.Unreadable = unreadable
	if err := x.log.Append(records...); err != nil {
		return done, err
	}
	// What was fetched before the run was stopped is kept: every record of it is
	// whole, and the next run is owed only what is still missing.
	return done, ctx.Err()
}

// fetchBody is one message's text, as the record that will hold it.
//
// A body that will not open is not a reason to leave the message out. The
// alternative is a mailbox where a message exists in a listing and not in a
// search, which is the shape of a search nobody can trust; indexed without its
// text, it is still found by its subject, its sender and its date, and `index
// list` says how many are in that state.
//
// A body that could not be asked for at all is a different thing, and is left
// owed rather than written off: the message keeps its place in the index and
// the next run asks again.
func (x *indexSession) fetchBody(ctx context.Context, u *keys.Unlocked, in stored) (stored, bool, error) {
	raw, err := x.s.fetchMessageRaw(ctx, in.ID)
	if err != nil {
		if ctx.Err() != nil {
			return stored{}, false, err
		}
		// Recorded and counted: the message stays in the index by its envelope,
		// and what is missing is the text a keyword would have matched.
		skip.Record(ctx, skip.KindMessage, in.ID, skip.Unreadable, err)
		return stored{}, false, nil
	}
	in.To, in.CC, in.BCC = addressees(raw.ToList), addressees(raw.CCList), addressees(raw.BCCList)
	body, _, err := x.s.openBody(ctx, u, *raw, false)
	if err != nil {
		// Recorded and not counted: nothing is missing from an answer that does
		// not already say so. The message is indexed and searchable by everything
		// but its text, and how many are in that state is on the screen of
		// `index list` and of the build that put them there.
		slog.DebugContext(ctx, "mail: a message body would not open for the index",
			"kind", string(skip.KindMessage), "reason", string(u.Shut(sealed(raw.Body))),
			"ref", in.ID, "error", err)
		in.Body, in.Opened, in.Unreadable = "", false, true
		return in, true, nil
	}
	in.Body, in.Opened, in.Unreadable = x.indexText(in, raw, body), true, false
	return in, true, nil
}

// mailboxCount is how many messages All Mail holds, which is what a build is
// counting towards.
func (s *Service) mailboxCount(ctx context.Context) (int, error) {
	_, total, err := s.indexPageOf(ctx, "", 1)
	return total, err
}

// anchorOf is where the next page of a walk starts: the oldest message of this
// one, named by both the time and the ID, since a second holds more than one
// message and a page boundary can fall inside it.
func anchorOf(m rawListMessage) string {
	return strconv.FormatInt(m.Time, 10) + "|" + m.ID
}

// indexPageOf reads one page of metadata, oldest-ward of the anchor.
//
// Paging by an anchor rather than by a page number is what makes a walk survive
// mail arriving while it runs: an arrival shifts every numbered page, and a walk
// that asked for page nine twice would skip whatever moved across the boundary
// in between.
func (s *Service) indexPageOf(ctx context.Context, anchor string, size int) ([]rawListMessage, int, error) {
	q := url.Values{}
	q.Set("LabelID", labelAllMail)
	q.Set("Sort", "Time")
	q.Set("Desc", "1")
	q.Set("Page", "0")
	q.Set("PageSize", strconv.Itoa(max(size, 1)))
	if at, id, ok := strings.Cut(anchor, "|"); ok {
		q.Set("End", at)
		q.Set("EndID", id)
	}
	var r struct {
		Total    int
		Messages []rawListMessage
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages", Query: q}, &r); err != nil {
		return nil, 0, err
	}
	return r.Messages, r.Total, nil
}

// quoted reports whether an earlier message of the same thread is in the index,
// which is what makes leaving a quote out lossless.
func (x *indexSession) quoted(in stored) bool {
	if in.ConversationID == "" {
		return false
	}
	at, ok := x.oldest[in.ConversationID]
	return ok && at < in.Time
}

// indexText is the body as it will be searched: the text of it, with a quoted
// reply left out when the message it quotes is in the index anyway.
//
// Quoting is how a thread carries its own history, so indexing it whole stores
// every message of a thread once per later message, and makes a search for a
// word in the first one match all of them. Proton's own client leaves a quote
// out for the same reason, and keeps it when nothing else covers it.
func (x *indexSession) indexText(in stored, raw *rawMessage, body string) string {
	quoted := x.quoted(in)
	if mailtext.IsHTML(raw.MIMEType) {
		if quoted {
			body = mailtext.StripHTMLQuotes(body)
		}
		return mailtext.HTMLToText(body)
	}
	if quoted {
		return mailtext.StripPlaintextQuotes(body)
	}
	return body
}

// restated is the message as the index will hold it: the envelope Proton just
// gave, over whatever the index already holds about its body.
func restated(before stored, m rawListMessage) stored {
	in := awaiting(before, m)
	in.Body, in.Opened, in.Unreadable = before.Body, before.Opened, before.Unreadable
	return in
}

// awaiting is the message as the index holds it while its text is fetched: the
// envelope Proton just gave, owed a body. Whatever text the index held is not
// carried over, because the event that brings a message here says the text is
// what changed.
//
// Who it was addressed to is kept where the envelope says nothing about it, so
// that a listing row without the addressees cannot take away what a message
// carried - --to matches a name in them, and a search that lost them would
// answer with less than it did before.
func awaiting(before stored, m rawListMessage) stored {
	in := indexedFrom(m)
	if len(in.To)+len(in.CC)+len(in.BCC) == 0 {
		in.To, in.CC, in.BCC = before.To, before.CC, before.BCC
	}
	return in
}

// indexedFrom is what a listing row says about a message, as the index holds it.
func indexedFrom(m rawListMessage) stored {
	return stored{
		ID: m.ID, ConversationID: m.ConversationID, Subject: m.Subject,
		SenderName: m.Sender.Name, SenderAddress: m.Sender.Address,
		To: addressees(m.ToList), CC: addressees(m.CCList), BCC: addressees(m.BCCList),
		Time: m.Time, Order: m.Order, Size: m.Size, Unread: m.Unread, Flags: m.Flags,
		Labels: m.LabelIDs, Attachments: m.NumAttachments, Expires: m.ExpirationTime,
	}
}

func addressees(list []map[string]any) []addressee {
	out := make([]addressee, 0, len(list))
	for _, p := range list {
		name, _ := p["Name"].(string)
		address, _ := p["Address"].(string)
		out = append(out, addressee{Name: name, Address: address})
	}
	return out
}

func record(in stored) (search.Record, error) {
	data, err := json.Marshal(in)
	if err != nil {
		return search.Record{}, err
	}
	return search.Record{ID: in.ID, Data: data}, nil
}

// syncIndex applies the change feed to the index.
//
// It is the same feed a watch reads, asked from where the index left off rather
// than from now: what the index is for is answering a question about the past,
// so it catches up rather than starting again.
func (x *indexSession) syncIndex(ctx context.Context) (search.Result, error) {
	log := x.log
	if log.State.Cursor == "" {
		cursor, err := x.s.LatestEventID(ctx)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		return search.Result{}, x.save(ctx)
	}

	var done search.Result
	for page := 0; page < maxDrain; page++ {
		var batch indexBatch
		if err := x.s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/core/v5/events/" + log.State.Cursor,
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			// The account changed more than the feed can describe, so what is
			// indexed can no longer be trusted to be all of it, and no event will
			// ever say what was missed. Reading the mailbox is what establishes
			// that, so the index is marked as owing one and the run walks it.
			slog.WarnContext(ctx, "The mail index missed part of the account's history and will be read from Proton again.",
				"kind", string(skip.KindMessage), "reason", string(skip.Unreadable))
			log.State.Stale = true
			cursor, err := x.s.LatestEventID(ctx)
			if err != nil {
				return done, err
			}
			log.State.Cursor = cursor
			done.Refreshed = true
			return done, x.save(ctx)
		}
		applied, err := x.applyEvents(ctx, batch)
		done = search.Total(done, applied)
		if err != nil {
			return done, err
		}
		// The cursor moves once the page is in the index, so a page that could not
		// be applied is asked for again rather than skipped - by the next run, or by
		// the next poll of a watch that keeps this session open.
		log.State.Cursor = batch.EventID
		if batch.More == 0 {
			break
		}
	}
	return done, x.save(ctx)
}

// indexBatch is one page of the feed, reduced to what an index needs: every
// message that changed, and what happened to it.
type indexBatch struct {
	EventID  string
	More     int
	Refresh  int
	Messages []indexEvent
}

// indexEvent is one thing the feed says happened to one message.
type indexEvent struct {
	ID      string
	Action  int
	Message *rawListMessage
}

// The feed's actions, as Proton numbers them.
const (
	eventDelete = 0
	eventUpdate = 2
)

// applyEvents writes one page of changes into the index.
//
// A message whose flags or labels moved is rewritten from the event itself,
// which carries the whole envelope: no request, and the body already in the
// index is kept. A message that arrived or was edited goes in the way the walk
// puts one in - the envelope first, owed its text, and the text fetched after -
// so a fetch that fails leaves a message the index holds and will ask about
// again, rather than one it never heard of.
func (x *indexSession) applyEvents(ctx context.Context, batch indexBatch) (search.Result, error) {
	if len(batch.Messages) == 0 {
		return search.Result{}, nil
	}
	x.survey(ctx)
	var done search.Result
	var records []search.Record
	var fetch []stored
	for _, e := range latest(batch.Messages) {
		before, had := x.held(e.ID)
		switch {
		case e.Action == eventDelete:
			records = append(records, search.Record{ID: e.ID, Gone: true})
			x.dropped(before, had)
			done.Removed++
		case e.Message == nil:
			// Nothing to write it from and nothing to fetch it by name with; the
			// next event about it carries the envelope.
			slog.DebugContext(ctx, "mail: an event carried no message to index",
				"kind", string(skip.KindMessage), "reason", string(skip.Unreadable), "ref", e.ID)
		case had && e.Action != eventUpdate:
			in := restated(before, *e.Message)
			if before.envelopeIs(in) {
				continue
			}
			rec, err := record(in)
			if err != nil {
				return done, err
			}
			records = append(records, rec)
			x.reckon(before, had, in)
			if counted(in) {
				done.Indexed++
			}
		default:
			in := awaiting(before, *e.Message)
			rec, err := record(in)
			if err != nil {
				return done, err
			}
			records = append(records, rec)
			x.reckon(before, had, in)
			fetch = append(fetch, in)
		}
	}
	if err := x.log.Append(records...); err != nil {
		return done, err
	}
	if len(fetch) == 0 {
		return done, nil
	}
	fetched, err := x.fetchBodies(ctx, fetch, progress.Nop{})
	return search.Total(done, fetched), err
}

// latest keeps the last event about each message, in the order they arrived: a
// message the feed reports twice is applied once, and the last word about it is
// the one that is true.
func latest(events []indexEvent) []indexEvent {
	at := make(map[string]int, len(events))
	out := make([]indexEvent, 0, len(events))
	for _, e := range events {
		if i, ok := at[e.ID]; ok {
			out[i] = e
			continue
		}
		at[e.ID] = len(out)
		out = append(out, e)
	}
	return out
}

// Indexed reports whether this app has an index to answer from.
func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppMail) }

// records is what the index holds, as messages.
func (x *indexSession) records(ctx context.Context) []stored {
	out := make([]stored, 0, len(x.log.Records()))
	for _, r := range x.log.Records() {
		var in stored
		if err := json.Unmarshal(r.Data, &in); err != nil {
			skip.Record(ctx, skip.KindMessage, r.ID, skip.Malformed, err)
			continue
		}
		out = append(out, in)
	}
	return out
}
