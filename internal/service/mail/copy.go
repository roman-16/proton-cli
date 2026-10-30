package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/skip"
)

const (
	copiesAtOnce    = 6
	sidecarVersion  = 1
	emlSuffix       = ".eml"
	sidecarSuffix   = ".metadata.json"
	labelsFile      = "mail/labels.json"
	mailDir         = "mail/"
	stateSeparation = "/"
)

type copyPart struct{ s *Service }

func (s *Service) CopyPart() export.Part { return copyPart{s: s} }

func (copyPart) App() export.App { return export.AppMail }

func (copyPart) Noun() string { return "messages" }

func (p copyPart) Holds(ctx context.Context) ([]export.Held, error) {
	total, err := p.s.mailboxCount(ctx)
	if err != nil {
		return nil, err
	}
	return []export.Held{{Count: total, Noun: "messages"}}, nil
}

type listed struct {
	meta rawListMessage
	raw  json.RawMessage
}

func (l listed) content() string { return fmt.Sprintf("%d.%d", l.meta.Time, l.meta.Size) }

func (l listed) state() string {
	labels := slices.Clone(l.meta.LabelIDs)
	slices.Sort(labels)
	return export.Version(labels, l.meta.Unread, l.meta.Flags)
}

func (l listed) version() string { return l.content() + stateSeparation + l.state() }

func (l listed) want() string {
	stamp := time.Unix(l.meta.Time, 0).Local().Format("2006-01-02 1504")
	return mailDir + localfile.SafeName(stamp+" "+l.meta.Subject)
}

type owed struct {
	listed
	whole bool
}

func (p copyPart) Run(ctx context.Context, c *export.Copy, sink progress.Sink) (res export.Result, err error) {
	ledger, err := c.Ledger(export.AppMail)
	if err != nil {
		return res, err
	}
	defer func() {
		if closed := ledger.Close(); err == nil {
			err = closed
		}
	}()
	if err := p.writeLabels(ctx, c); err != nil {
		return res, err
	}
	messages, err := p.walk(ctx, sink)
	if err != nil {
		return res, err
	}
	due := p.due(c, ledger, messages)
	res.Owed = len(due)
	if len(due) == 0 {
		return res, nil
	}
	progress.Counting(sink, "messages")
	sink.Start(int64(len(due)), "Exporting mail")
	defer sink.Done()

	var mu sync.Mutex
	var failure error
	var wg sync.WaitGroup
	slots := make(chan struct{}, copiesAtOnce)
	for _, m := range due {
		if ctx.Err() != nil {
			break
		}
		mu.Lock()
		stop := failure != nil
		mu.Unlock()
		if stop {
			break
		}
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			sealedBody, err := p.copyOne(ctx, c, ledger, m)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				res.Exported++
				if sealedBody {
					res.Unreadable++
				}
				sink.Add(1)
			case export.Stops(ctx, err):
				if failure == nil {
					failure = err
				}
			case export.Gone(err):
				// Recorded and not counted: the message left the account while the
				// run was under way, so there is nothing of it to be short of.
				slog.DebugContext(ctx, "mail: a message went before it was copied", "ref", m.meta.ID)
				sink.Add(1)
			default:
				skip.Record(ctx, skip.KindMessage, m.meta.ID, skip.Undecryptable, err)
				sink.Add(1)
			}
		}()
	}
	wg.Wait()
	if failure != nil {
		return res, failure
	}
	return res, ctx.Err()
}

func (p copyPart) walk(ctx context.Context, sink progress.Sink) ([]listed, error) {
	total, err := p.s.mailboxCount(ctx)
	if err != nil {
		return nil, err
	}
	progress.Counting(sink, "messages")
	sink.Start(int64(total), "Reading mail")
	defer sink.Done()
	seen := make(map[string]bool, total)
	out := make([]listed, 0, total)
	anchor := ""
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		page, _, err := p.s.metadataPage(ctx, anchor, indexPage)
		if err != nil {
			return nil, err
		}
		var last rawListMessage
		for _, raw := range page {
			var m rawListMessage
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, err
			}
			last = m
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			out = append(out, listed{meta: m, raw: raw})
		}
		sink.Add(int64(len(page)))
		if len(page) < indexPage {
			return out, nil
		}
		anchor = anchorOf(last)
	}
}

func (p copyPart) due(c *export.Copy, ledger *export.Ledger, messages []listed) []owed {
	var out []owed
	for _, m := range messages {
		e, ok := ledger.Get(m.meta.ID)
		switch {
		case !ok || !e.Done() || !p.present(c, e.Path):
			out = append(out, owed{listed: m, whole: true})
		case e.Version == m.version():
		case contentOf(e.Version) != m.content():
			out = append(out, owed{listed: m, whole: true})
		default:
			out = append(out, owed{listed: m})
		}
	}
	return out
}

func contentOf(version string) string {
	content, _, _ := strings.Cut(version, stateSeparation)
	return content
}

func (copyPart) present(c *export.Copy, stem string) bool {
	return localfile.Exists(c.Path(stem+emlSuffix)) && localfile.Exists(c.Path(stem+sidecarSuffix))
}

func (p copyPart) copyOne(ctx context.Context, c *export.Copy, ledger *export.Ledger, m owed) (bool, error) {
	if !m.whole {
		e, _ := ledger.Get(m.meta.ID)
		if err := p.restate(c, e.Path, m.listed); err != nil {
			return false, err
		}
		return false, ledger.Put(export.Entry{ID: m.meta.ID, Version: m.version(), Path: e.Path, Want: e.Want})
	}
	r, err := p.s.render(ctx, m.meta.ID, true)
	if err != nil {
		return false, err
	}
	sidecar, err := sidecarOf(m.raw, r.raw)
	if err != nil {
		return false, err
	}
	want := m.want()
	now, was, err := ledger.Place(m.meta.ID, want)
	if err != nil {
		return false, err
	}
	when := time.Unix(m.meta.Time, 0)
	if err := localfile.Publish(c.Path(now+emlSuffix), r.doc, when); err != nil {
		return false, err
	}
	if err := localfile.Publish(c.Path(now+sidecarSuffix), sidecar, when); err != nil {
		return false, err
	}
	if was != "" && was != now {
		_ = os.Remove(c.Path(was + emlSuffix))
		_ = os.Remove(c.Path(was + sidecarSuffix))
	}
	return r.sealed, ledger.Put(export.Entry{ID: m.meta.ID, Version: m.version(), Path: now, Want: want})
}

func (copyPart) restate(c *export.Copy, stem string, m listed) error {
	at := c.Path(stem + sidecarSuffix)
	raw, err := os.ReadFile(at)
	if err != nil {
		return err
	}
	var doc struct {
		Version int
		Payload map[string]json.RawMessage
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	var listing map[string]json.RawMessage
	if err := json.Unmarshal(m.raw, &listing); err != nil {
		return err
	}
	for key, value := range listing {
		doc.Payload[key] = value
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return localfile.Publish(at, out, time.Unix(m.meta.Time, 0))
}

func sidecarOf(listing json.RawMessage, full *rawMessage) ([]byte, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(listing, &payload); err != nil {
		return nil, err
	}
	attachments := full.Attachments
	if attachments == nil {
		attachments = []rawAttachment{}
	}
	for key, value := range map[string]any{
		"Attachments": attachments,
		"MIMEType":    full.MIMEType,
		"Headers":     full.Header,
		"WriterType":  0,
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		payload[key] = encoded
	}
	return json.MarshalIndent(struct {
		Version int
		Payload map[string]json.RawMessage
	}{Version: sidecarVersion, Payload: payload}, "", "  ")
}

func (p copyPart) writeLabels(ctx context.Context, c *export.Copy) error {
	var rows []rawLabel
	for _, t := range []int{labelTypeFolder, labelTypeLabel} {
		labels, err := p.s.rawLabels(ctx, t)
		if err != nil {
			return err
		}
		rows = append(rows, labels...)
	}
	doc, err := json.MarshalIndent(struct {
		Version int
		Payload []rawLabel
	}{Version: sidecarVersion, Payload: rows}, "", "  ")
	if err != nil {
		return err
	}
	at := c.Path(labelsFile)
	if was, err := os.ReadFile(at); err == nil && bytes.Equal(was, doc) {
		return nil
	}
	return localfile.Publish(at, doc, time.Time{})
}
