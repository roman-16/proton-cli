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

const (
	indexPage    = pageMax
	bodiesAtOnce = 10
	indexShape   = 1
)

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
	AddressID      string      `json:"address,omitempty"`
	Expires        int64       `json:"expires,omitempty"`
	Body           string      `json:"body,omitempty"`
	Opened         bool        `json:"opened,omitempty"`
	Unreadable     bool        `json:"unreadable,omitempty"`
}

type addressee struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address,omitempty"`
}

func (s stored) settled() bool { return s.Opened || s.Unreadable }

func (s stored) message() Message {
	v := verdicts(s.Flags)
	return Message{
		ID: s.ID, ConversationID: s.ConversationID, Subject: s.Subject,
		FromName: s.SenderName, FromAddress: s.SenderAddress,
		Time: s.Time, Unread: s.Unread, NumAttachments: s.Attachments, Labels: s.Labels,
		Size: s.Size, Expires: s.Expires, ExpiryFixed: s.Flags&flagExpiryFixed != 0,
		ExpiryByRetention: s.Flags&flagExpiryByRetention != 0,
		DMARCFailed:       v.dmarcFailed, MarkedLegitimate: v.markedLegitimate,
		Phishing: v.phishing, Suspicious: v.suspicious,
	}
}

func (s stored) recipients() []string {
	out := make([]string, 0, 2*(len(s.To)+len(s.CC)+len(s.BCC)))
	for _, list := range [][]addressee{s.To, s.CC, s.BCC} {
		for _, p := range list {
			out = append(out, p.Name, p.Address)
		}
	}
	return out
}

func (s stored) envelopeIs(other stored) bool {
	return s.ID == other.ID && s.ConversationID == other.ConversationID &&
		s.Subject == other.Subject && s.SenderName == other.SenderName &&
		s.SenderAddress == other.SenderAddress && s.Time == other.Time &&
		s.Order == other.Order && s.Unread == other.Unread && s.Flags == other.Flags &&
		s.Attachments == other.Attachments && s.Expires == other.Expires &&
		s.AddressID == other.AddressID && s.Size == other.Size &&
		sameLabels(s.Labels, other.Labels) &&
		slices.Equal(s.To, other.To) && slices.Equal(s.CC, other.CC) &&
		slices.Equal(s.BCC, other.BCC)
}

func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

func (s *Service) SetIndex(store *search.Store) { s.index = store }

type indexPart struct{ s *Service }

func (s *Service) IndexPart() search.Part { return indexPart{s: s} }

func (indexPart) App() search.App { return search.AppMail }
func (indexPart) Noun() string    { return "messages" }

func (p indexPart) Open(ctx context.Context) (search.Session, error) { return p.s.openIndex(ctx) }

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

type indexSession struct {
	s   *Service
	log *search.Log

	surveyed   bool
	oldest     map[string]int64
	bodies     int
	unreadable int
	owed       map[string]int64
}

func (s *Service) openIndex(ctx context.Context) (*indexSession, error) {
	log, err := s.index.Load(ctx, search.AppMail)
	if err != nil {
		return nil, err
	}
	if log.State.Complete && log.State.Shape != indexShape {
		slog.DebugContext(ctx, "mail: the index holds records of another shape, so it will be read again")
		log.State.Stale = true
	}
	return &indexSession{s: s, log: log, oldest: map[string]int64{}, owed: map[string]int64{}}, nil
}

func (x *indexSession) Status() search.Status {
	if x.surveyed {
		x.counts()
	}
	return x.log.Status()
}

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

func (x *indexSession) Sync(ctx context.Context) (search.Result, error) { return x.syncIndex(ctx) }

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

func (x *indexSession) counts() {
	x.log.State.Bodies, x.log.State.Unreadable = x.bodies, x.unreadable
	x.log.State.Texts = x.log.State.Indexed
}

func counted(now stored) bool { return now.settled() }

func (x *indexSession) owing() bool { return x.log.State.Bodies < x.log.State.Indexed }

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

func (x *indexSession) save(ctx context.Context) error {
	if x.owing() {
		x.survey(ctx)
	}
	if x.surveyed {
		x.counts()
	}
	return x.log.Save(time.Now().Unix())
}

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
	total, err := x.s.mailboxCount(ctx)
	if err != nil {
		return search.Result{}, err
	}
	log.State.Total = total
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

	gone, err := x.tombstoneUnseen(seen)
	done = search.Total(done, gone)
	if err != nil {
		return done, err
	}
	log.State.Complete, log.State.Stale, log.State.Shape = true, false, indexShape
	sink.Done()
	return done, x.save(ctx)
}

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
	return done, ctx.Err()
}

func (x *indexSession) fetchBody(ctx context.Context, u *keys.Unlocked, in stored) (stored, bool, error) {
	raw, err := x.s.fetchMessageRaw(ctx, in.ID)
	if err != nil {
		if ctx.Err() != nil {
			return stored{}, false, err
		}
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

func (s *Service) mailboxCount(ctx context.Context) (int, error) {
	_, total, err := s.indexPageOf(ctx, "", 1)
	return total, err
}

func anchorOf(m rawListMessage) string {
	return strconv.FormatInt(m.Time, 10) + "|" + m.ID
}

func (s *Service) indexPageOf(ctx context.Context, anchor string, size int) ([]rawListMessage, int, error) {
	raws, total, err := s.metadataPage(ctx, anchor, size)
	if err != nil {
		return nil, 0, err
	}
	out := make([]rawListMessage, 0, len(raws))
	for _, raw := range raws {
		var m rawListMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, nil
}

func (s *Service) metadataPage(ctx context.Context, anchor string, size int) ([]json.RawMessage, int, error) {
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
		Messages []json.RawMessage
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/messages", Query: q}, &r); err != nil {
		return nil, 0, err
	}
	return r.Messages, r.Total, nil
}

func (x *indexSession) quoted(in stored) bool {
	if in.ConversationID == "" {
		return false
	}
	at, ok := x.oldest[in.ConversationID]
	return ok && at < in.Time
}

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

func restated(before stored, m rawListMessage) stored {
	in := awaiting(before, m)
	in.Body, in.Opened, in.Unreadable = before.Body, before.Opened, before.Unreadable
	return in
}

func awaiting(before stored, m rawListMessage) stored {
	in := indexedFrom(m)
	if len(in.To)+len(in.CC)+len(in.BCC) == 0 {
		in.To, in.CC, in.BCC = before.To, before.CC, before.BCC
	}
	return in
}

func indexedFrom(m rawListMessage) stored {
	return stored{
		ID: m.ID, ConversationID: m.ConversationID, Subject: m.Subject,
		SenderName: m.Sender.Name, SenderAddress: m.Sender.Address,
		To: addressees(m.ToList), CC: addressees(m.CCList), BCC: addressees(m.BCCList),
		Time: m.Time, Order: m.Order, Size: m.Size, Unread: m.Unread, Flags: m.Flags,
		Labels: m.LabelIDs, Attachments: m.NumAttachments, AddressID: m.AddressID,
		Expires: m.ExpirationTime,
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
		log.State.Cursor = batch.EventID
		if batch.More == 0 {
			break
		}
	}
	return done, x.save(ctx)
}

type indexBatch struct {
	EventID  string
	More     int
	Refresh  int
	Messages []indexEvent
}

type indexEvent struct {
	ID      string
	Action  int
	Message *rawListMessage
}

const (
	eventDelete = 0
	eventUpdate = 2
)

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

func (s *Service) Indexed() bool { return s.index != nil && s.index.Exists(search.AppMail) }

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
