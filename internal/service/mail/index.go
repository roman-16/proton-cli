package mail

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
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
// The one thing Proton cannot do for a search is read a body, so the only way to
// search one is to have it here, decrypted. The build walks All Mail newest
// first, which is the order the web client's own indexer uses: the half of a
// mailbox somebody is going to search for is the recent half, so a build that is
// interrupted at any point has covered the part that matters most, and a search
// over it says how far back it reaches.
//
// A body is fetched per message and there is no bulk endpoint for one, so this
// is one request each and the reason a first build of a large mailbox takes
// hours. Everything after it costs what changed.

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
}

type addressee struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
}

// message is the row a listing shows, rebuilt from what was indexed.
func (s stored) message() Message {
	v := verdicts(s.Flags)
	return Message{
		ID: s.ID, ConversationID: s.ConversationID, Subject: s.Subject,
		FromName: s.SenderName, FromAddress: s.SenderAddress,
		Time: s.Time, Unread: s.Unread, NumAttachments: s.Attachments, Labels: s.Labels,
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

// SetIndex hands the service the profile's index directory, which is the one
// thing about it decided outside: where this machine keeps its files.
func (s *Service) SetIndex(store *search.Store) { s.index = store }

// indexPart is the mail app as the index commands see it.
type indexPart struct{ s *Service }

// IndexPart is what `search` drives to keep the mail index current.
func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

func (indexPart) App() search.App { return search.AppMail }
func (indexPart) Noun() string    { return "messages" }

func (p indexPart) Status() (search.Status, error) { return p.s.index.Status(search.AppMail) }

// Count is how many messages a run would index: what All Mail holds, less what
// is in the index already. It is the first page of the walk a build would do, so
// a preview costs one request rather than doing the work to find out.
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
	return max(total-status.Indexed, 0), nil
}

// Build indexes every message not indexed yet, newest first.
func (p indexPart) Build(ctx context.Context, sink progress.Sink) (search.Result, error) {
	return p.s.buildIndex(ctx, progress.Of(sink))
}

// Sync applies everything that has happened to the mailbox since the index was
// last brought up to date.
func (p indexPart) Sync(ctx context.Context) (search.Result, error) {
	return p.s.syncIndex(ctx)
}

// buildIndex walks All Mail from where the last run stopped.
//
// The cursor into the change feed is taken before the first page rather than
// after the last, so a message that arrives during a build is caught by the
// first sync instead of falling into the gap between the two.
func (s *Service) buildIndex(ctx context.Context, sink progress.Sink) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppMail)
	if err != nil {
		return search.Result{}, err
	}
	s.noteLostRecords(ctx, log)
	if log.State.Complete {
		return search.Result{}, nil
	}
	// The cursor is written down before the walk rather than after it. A build
	// that is interrupted on its first page has still established the moment it
	// started from, and taking a fresh one later would step over everything that
	// happened in between.
	if log.State.Cursor == "" {
		cursor, err := s.LatestEventID(ctx)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		if err := log.Save(time.Now().Unix()); err != nil {
			return search.Result{}, err
		}
	}

	var done search.Result
	writing := s.open(ctx, log)
	mark := log.Mark()
	// The bar is opened once, at what is already indexed, so a build that is
	// carried on shows how much of the mailbox is covered rather than starting
	// from nothing every time it is run.
	counting := false
	for {
		if err := ctx.Err(); err != nil {
			return done, err
		}
		page, _, err := s.indexPageOf(ctx, mark, indexPage)
		if err != nil {
			return done, err
		}
		if len(page) == 0 {
			log.State.Complete = true
			break
		}
		if !counting {
			// How many there are is asked without the anchor, because a page of
			// the walk is answered with how many remain from where it starts: a
			// build that took its count from its last page would report a mailbox
			// of fifty-five messages having indexed ten thousand.
			total, err := s.mailboxCount(ctx)
			if err != nil {
				return done, err
			}
			log.State.Total = total
			// How much there is to index is written down as soon as it is known,
			// so a build interrupted on its first page still leaves an index that
			// can say how little of the mailbox it covers.
			if err := log.Save(time.Now().Unix()); err != nil {
				return done, err
			}
			sink.Start(int64(total), "Indexing mail")
			progress.Resume(sink, int64(log.State.Indexed))
			counting = true
		}

		// A page that was not indexed to the end leaves the mark where it was.
		// The mark is what the next run carries on from, so advancing it over a
		// page that is only half in would skip the other half for good.
		indexed, err := s.indexMessages(ctx, writing, page, sink)
		done.Indexed += indexed.Indexed
		done.Unreadable += indexed.Unreadable
		if err != nil {
			return done, err
		}
		last := page[len(page)-1]
		mark = anchor(last)
		if oldest := last.Time; oldest > 0 && (log.State.Oldest == 0 || oldest < log.State.Oldest) {
			log.State.Oldest = oldest
		}
		if err := log.Append(search.Record{Mark: mark}); err != nil {
			return done, err
		}
		if err := log.Save(time.Now().Unix()); err != nil {
			return done, err
		}
		if len(page) < indexPage {
			log.State.Complete = true
			break
		}
	}
	sink.Done()
	return done, log.Save(time.Now().Unix())
}

// mailboxCount is how many messages All Mail holds, which is what a build is
// counting towards.
func (s *Service) mailboxCount(ctx context.Context) (int, error) {
	_, total, err := s.indexPageOf(ctx, "", 1)
	return total, err
}

// anchor is where the next page of a build starts: the oldest message of this
// one, named by both the time and the ID, since a second holds more than one
// message and a page boundary can fall inside it.
func anchor(m rawListMessage) string {
	return strconv.FormatInt(m.Time, 10) + "|" + m.ID
}

// indexPageOf reads one page of metadata, oldest-ward of the anchor.
//
// Paging by an anchor rather than by a page number is what makes a build
// survive its own writes: mail arriving while it runs shifts every numbered
// page, and a walk that asked for page nine twice would skip whatever moved
// across the boundary in between.
func (s *Service) indexPageOf(ctx context.Context, mark string, size int) ([]rawListMessage, int, error) {
	q := url.Values{}
	q.Set("LabelID", labelAllMail)
	q.Set("Sort", "Time")
	q.Set("Desc", "1")
	q.Set("Page", "0")
	q.Set("PageSize", strconv.Itoa(max(size, 1)))
	if at, id, ok := strings.Cut(mark, "|"); ok {
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

// run is the index being written, and the one thing about what is already in it
// that writing needs to know: which threads it holds, and from when.
//
// It is read once, when the run opens, because the alternative is reading every
// record again for every message - which is the whole index per page, and an
// hour of a build spent deciding whether to keep a quote.
type run struct {
	log *search.Log
	// oldest is the time of the earliest message each thread has in the index.
	oldest map[string]int64
}

func (s *Service) open(ctx context.Context, log *search.Log) *run {
	r := &run{log: log, oldest: make(map[string]int64, len(log.Records()))}
	for _, rec := range log.Records() {
		var in stored
		if err := json.Unmarshal(rec.Data, &in); err != nil {
			skip.Record(ctx, skip.KindMessage, rec.ID, skip.Malformed, err)
			continue
		}
		r.note(in)
	}
	return r
}

// note remembers a message the index now holds.
func (r *run) note(in stored) {
	if in.ConversationID == "" {
		return
	}
	if at, ok := r.oldest[in.ConversationID]; !ok || in.Time < at {
		r.oldest[in.ConversationID] = in.Time
	}
}

// quoted reports whether an earlier message of the same thread is already
// indexed, which is what makes leaving a quote out lossless.
func (r *run) quoted(raw *rawMessage) bool {
	at, ok := r.oldest[raw.ConversationID]
	return ok && at < raw.Time
}

// indexMessages fetches and seals the messages of one page that are not in the
// index yet, and returns how many went in.
func (s *Service) indexMessages(ctx context.Context, r *run, page []rawListMessage, sink progress.Sink) (search.Result, error) {
	log := r.log
	wanted := make([]rawListMessage, 0, len(page))
	for _, m := range page {
		if !log.Has(m.ID) {
			wanted = append(wanted, m)
		}
	}
	if len(wanted) == 0 {
		return search.Result{}, nil
	}
	u, err := s.keys(ctx)
	if err != nil {
		return search.Result{}, err
	}

	records := make([]search.Record, len(wanted))
	unreadable := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	slots := make(chan struct{}, bodiesAtOnce)
	var failure error
	for i, m := range wanted {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			rec, opened, err := s.indexOne(ctx, u, r, m)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err != nil && failure == nil && ctx.Err() == nil:
				failure = err
			case err == nil:
				records[i] = rec
				if !opened {
					unreadable++
				}
				sink.Add(1)
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		return search.Result{}, failure
	}

	written := make([]search.Record, 0, len(records))
	for i, rec := range records {
		if rec.ID == "" {
			continue
		}
		written = append(written, rec)
		r.note(indexedFrom(wanted[i], nil, "", false))
	}
	log.State.Unreadable += unreadable
	if err := log.Append(written...); err != nil {
		return search.Result{}, err
	}
	// What was fetched before the run was stopped is kept: every record of it is
	// whole, and the walk that carries on will pass this page again and fetch
	// only what is still missing.
	return search.Result{Indexed: len(written), Unreadable: unreadable}, ctx.Err()
}

// indexOne is one message as a sealed record: its metadata, and its body when
// the body opens.
//
// A body that will not open is not a reason to leave the message out. The
// alternative is a mailbox where a message exists in a listing and not in a
// search, which is the shape of a search nobody can trust; indexed without its
// text, it is still found by its subject, its sender and its date, and `index
// list` says how many are in that state.
func (s *Service) indexOne(ctx context.Context, u *keys.Unlocked, r *run, m rawListMessage) (search.Record, bool, error) {
	raw, err := s.fetchMessageRaw(ctx, m.ID)
	if err != nil {
		if ctx.Err() != nil {
			return search.Record{}, false, err
		}
		skip.Record(ctx, skip.KindMessage, m.ID, skip.Unreadable, err)
		rec, rerr := record(indexedFrom(m, nil, "", false))
		return rec, false, rerr
	}
	body, _, err := s.openBody(ctx, u, *raw, false)
	if err != nil {
		// Recorded and not counted: nothing is missing from an answer that does
		// not already say so. The message is indexed and searchable by everything
		// but its text, and how many are in that state is on the screen of
		// `index list` and of the build that put them there.
		slog.DebugContext(ctx, "mail: a message body would not open for the index",
			"kind", string(skip.KindMessage), "reason", string(u.Shut(sealed(raw.Body))),
			"ref", m.ID, "error", err)
		rec, rerr := record(indexedFrom(m, raw, "", false))
		return rec, false, rerr
	}
	rec, rerr := record(indexedFrom(m, raw, indexText(r, raw, body), true))
	return rec, true, rerr
}

// indexText is the body as it will be searched: the text of it, with a quoted
// reply left out when the message it quotes is in the index already.
//
// Quoting is how a thread carries its own history, so indexing it whole stores
// every message of a thread once per later message, and makes a search for a
// word in the first one match all of them. Proton's own client leaves a quote
// out for the same reason, and keeps it when nothing else covers it.
func indexText(r *run, raw *rawMessage, body string) string {
	quoted := raw.ConversationID != "" && r.quoted(raw)
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

// indexedFrom folds what a listing knows and what the message itself knows into
// the one shape the index holds.
func indexedFrom(m rawListMessage, raw *rawMessage, body string, opened bool) stored {
	in := stored{
		ID: m.ID, ConversationID: m.ConversationID, Subject: m.Subject,
		SenderName: m.Sender.Name, SenderAddress: m.Sender.Address,
		Time: m.Time, Order: m.Order, Unread: m.Unread, Flags: m.Flags,
		Labels: m.LabelIDs, Attachments: m.NumAttachments, Expires: m.ExpirationTime,
		Body: body, Opened: opened,
	}
	if raw != nil {
		in.To, in.CC, in.BCC = addressees(raw.ToList), addressees(raw.CCList), addressees(raw.BCCList)
	}
	return in
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
func (s *Service) syncIndex(ctx context.Context) (search.Result, error) {
	log, err := s.index.Load(ctx, search.AppMail)
	if err != nil {
		return search.Result{}, err
	}
	s.noteLostRecords(ctx, log)
	if log.State.Cursor == "" {
		cursor, err := s.LatestEventID(ctx)
		if err != nil {
			return search.Result{}, err
		}
		log.State.Cursor = cursor
		return search.Result{}, log.Save(time.Now().Unix())
	}

	var done search.Result
	writing := s.open(ctx, log)
	for page := 0; page < maxDrain; page++ {
		var batch indexBatch
		if err := s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/core/v5/events/" + log.State.Cursor,
		}, &batch); err != nil {
			return done, err
		}
		if batch.Refresh != 0 {
			// The account changed more than the feed can describe, so what is
			// indexed can no longer be trusted to be all of it. The walk is the
			// thing that establishes that, and it skips what is already here, so
			// this costs metadata rather than bodies.
			slog.WarnContext(ctx, "The mail index missed part of the account's history and will be rebuilt from Proton.",
				"kind", string(skip.KindMessage), "reason", string(skip.Unreadable))
			log.State.Complete = false
			cursor, err := s.LatestEventID(ctx)
			if err != nil {
				return done, err
			}
			log.State.Cursor = cursor
			return done, log.Save(time.Now().Unix())
		}
		log.State.Cursor = batch.EventID
		applied, err := s.applyEvents(ctx, writing, batch)
		done = search.Total(done, applied)
		if err != nil {
			return done, err
		}
		if batch.More == 0 {
			break
		}
	}
	return done, log.Save(time.Now().Unix())
}

// indexBatch is one page of the feed, reduced to what an index needs: every
// message that changed, and what happened to it.
type indexBatch struct {
	EventID  string
	More     int
	Refresh  int
	Messages []struct {
		ID      string
		Action  int
		Message *rawListMessage
	}
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
// index is kept. Anything else is fetched, because a message that was edited is
// a body that changed.
func (s *Service) applyEvents(ctx context.Context, r *run, batch indexBatch) (search.Result, error) {
	log := r.log
	var done search.Result
	var fetch []rawListMessage
	var records []search.Record
	for _, e := range batch.Messages {
		switch {
		case e.Action == eventDelete:
			records = append(records, search.Record{ID: e.ID, Gone: true})
			done.Removed++
		case e.Message == nil:
			// Nothing to write it from and nothing to fetch it by name with; the
			// next event about it carries the envelope.
			slog.DebugContext(ctx, "mail: an event carried no message to index",
				"kind", string(skip.KindMessage), "reason", string(skip.Unreadable), "ref", e.ID)
		case e.Action == eventFlags && log.Has(e.ID):
			rec, err := reindexed(log, *e.Message)
			if err != nil {
				return done, err
			}
			records = append(records, rec)
			done.Indexed++
		case e.Action == eventCreate || e.Action == eventUpdate || e.Action == eventFlags:
			fetch = append(fetch, *e.Message)
		}
	}
	if err := log.Append(records...); err != nil {
		return done, err
	}
	if len(fetch) == 0 {
		return done, nil
	}
	// A message the feed reports twice is fetched once: the last word about it
	// is the one that is true.
	indexed, err := s.indexMessages(ctx, r, newest(fetch), progress.Nop{})
	done.Indexed += indexed.Indexed
	done.Unreadable += indexed.Unreadable
	return done, err
}

// newest keeps the last event about each message, in the order they arrived.
func newest(msgs []rawListMessage) []rawListMessage {
	at := make(map[string]int, len(msgs))
	out := make([]rawListMessage, 0, len(msgs))
	for _, m := range msgs {
		if i, ok := at[m.ID]; ok {
			out[i] = m
			continue
		}
		at[m.ID] = len(out)
		out = append(out, m)
	}
	return out
}

// reindexed rewrites an indexed message's envelope, keeping the body that is
// already there. Moving a message between folders is the commonest thing that
// happens to a mailbox, and it changes nothing a body search would read.
func reindexed(log *search.Log, m rawListMessage) (search.Record, error) {
	old, _ := log.Get(m.ID)
	var in stored
	if len(old.Data) > 0 {
		if err := json.Unmarshal(old.Data, &in); err != nil {
			return search.Record{}, err
		}
	}
	fresh := indexedFrom(m, nil, in.Body, in.Opened)
	fresh.To, fresh.CC, fresh.BCC = in.To, in.CC, in.BCC
	return record(fresh)
}

// noteLostRecords says when the end of a log did not read back, which is what an
// interrupted write leaves behind.
func (s *Service) noteLostRecords(ctx context.Context, log *search.Log) {
	if log.Lost == 0 {
		return
	}
	// Recorded and not counted: the records are written again by the build that
	// carries on, and what is in the index is what `index list` reports.
	slog.DebugContext(ctx, "mail: the end of the index did not read back",
		"kind", string(skip.KindMessage), "reason", string(skip.Malformed), "bytes", log.Lost)
}

// Indexed reports whether this app has an index to answer from.
func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppMail) }

// indexRecords is what the index holds, as messages.
func (s *Service) indexRecords(ctx context.Context) ([]stored, error) {
	log, err := s.index.Load(ctx, search.AppMail)
	if err != nil {
		return nil, err
	}
	s.noteLostRecords(ctx, log)
	out := make([]stored, 0, len(log.Records()))
	for _, r := range log.Records() {
		var in stored
		if err := json.Unmarshal(r.Data, &in); err != nil {
			skip.Record(ctx, skip.KindMessage, r.ID, skip.Malformed, err)
			continue
		}
		out = append(out, in)
	}
	return out, nil
}
