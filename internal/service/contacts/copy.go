package contacts

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/roman-16/proton-cli/internal/export"
	"github.com/roman-16/proton-cli/internal/localfile"
	"github.com/roman-16/proton-cli/internal/progress"
	"github.com/roman-16/proton-cli/internal/vcard"
)

const (
	copyFile    = "contacts/contacts.vcf"
	copyFileKey = "file"
)

type copyPart struct{ s *Service }

func (s *Service) CopyPart() export.Part { return copyPart{s: s} }

func (copyPart) App() export.App { return export.AppContacts }

func (copyPart) Noun() string { return "contacts" }

func (p copyPart) Holds(ctx context.Context) ([]export.Held, error) {
	all, err := p.s.List(ctx)
	if err != nil {
		return nil, err
	}
	return []export.Held{{Count: len(all), Noun: "contacts"}}, nil
}

func (p copyPart) Run(ctx context.Context, c *export.Copy, sink progress.Sink) (res export.Result, err error) {
	ledger, err := c.Ledger(export.AppContacts)
	if err != nil {
		return res, err
	}
	defer func() {
		if closed := ledger.Close(); err == nil {
			err = closed
		}
	}()
	all, err := p.s.List(ctx)
	if err != nil {
		return res, err
	}
	groups, err := p.s.Membership(ctx)
	if err != nil {
		return res, err
	}
	now, _, err := ledger.Place(copyFileKey, copyFile)
	if err != nil {
		return res, err
	}
	before, err := os.ReadFile(c.Path(now))
	present := err == nil
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return res, err
	}
	written := map[string]string{}
	previous := splitCards(string(before))
	for _, card := range previous {
		written[cardKey(card)] = card
	}
	cards := make([]string, 0, len(all)+len(previous))
	held := make(map[string]bool, len(all))
	changed := 0
	for _, ct := range all {
		card := vcard.Document(ct.Cards, groups[ct.ID])
		cards = append(cards, card)
		held[cardKey(card)] = true
		if written[cardKey(card)] != card {
			changed++
		}
	}
	for _, card := range previous {
		if !held[cardKey(card)] {
			cards = append(cards, card)
		}
	}
	var doc strings.Builder
	for _, card := range cards {
		doc.WriteString(card)
		doc.WriteString("\r\n")
	}
	if err := ledger.Retain(func(id string) bool { return id == copyFileKey }); err != nil {
		return res, err
	}
	if present && doc.String() == string(before) {
		return res, nil
	}
	if !present {
		changed = len(all)
	}
	res.Owed = changed
	progress.Counting(sink, "contacts")
	sink.Start(int64(len(cards)), "Exporting contacts")
	defer sink.Done()
	if err := localfile.Publish(c.Path(now), []byte(doc.String()), time.Time{}); err != nil {
		return res, err
	}
	sink.Add(int64(len(cards)))
	res.Exported = res.Owed
	return res, ledger.Put(export.Entry{ID: copyFileKey, Path: now, Want: copyFile})
}

func cardKey(card string) string {
	if uid := vcard.Field(card, "UID"); uid != "" {
		return uid
	}
	return card
}

func splitCards(text string) []string {
	var out, card []string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		switch {
		case strings.EqualFold(line, "BEGIN:VCARD"):
			card = []string{line}
		case card == nil:
		case strings.EqualFold(line, "END:VCARD"):
			out = append(out, strings.Join(append(card, line), "\r\n"))
			card = nil
		default:
			card = append(card, line)
		}
	}
	return out
}
