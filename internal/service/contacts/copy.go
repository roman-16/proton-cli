package contacts

import (
	"context"
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
	contactKey  = "contact:"
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
	cards := make([]string, 0, len(all))
	versions := make(map[string]string, len(all))
	ordered := make([]string, 0, len(all))
	changed := 0
	for _, ct := range all {
		card := vcard.Document(ct.Cards, groups[ct.ID])
		cards = append(cards, card)
		version := export.Version(card)
		versions[contactKey+ct.ID] = version
		ordered = append(ordered, version)
		if had, ok := ledger.Get(contactKey + ct.ID); !ok || had.Version != version {
			changed++
		}
	}
	var gone []string
	for _, id := range ledger.IDs() {
		if _, held := versions[id]; strings.HasPrefix(id, contactKey) && !held {
			gone = append(gone, id)
		}
	}
	fileVersion := export.Version(ordered)
	now, _, err := ledger.Place(copyFileKey, copyFile)
	if err != nil {
		return res, err
	}
	file, _ := ledger.Get(copyFileKey)
	present := localfile.Exists(c.Path(now))
	if present && file.Version == fileVersion {
		return res, nil
	}
	if !present {
		changed = len(all)
	}
	res.Owed = changed + len(gone)
	progress.Counting(sink, "contacts")
	sink.Start(int64(len(all)), "Exporting contacts")
	defer sink.Done()
	var doc strings.Builder
	for _, card := range cards {
		doc.WriteString(card)
		doc.WriteString("\r\n")
	}
	if err := localfile.Publish(c.Path(now), []byte(doc.String()), time.Time{}); err != nil {
		return res, err
	}
	for id, version := range versions {
		if err := ledger.Put(export.Entry{ID: id, Version: version}); err != nil {
			return res, err
		}
	}
	for _, id := range gone {
		if err := ledger.Forget(id); err != nil {
			return res, err
		}
	}
	sink.Add(int64(len(all)))
	res.Exported = res.Owed
	return res, ledger.Put(export.Entry{ID: copyFileKey, Version: fileVersion, Path: now, Want: copyFile})
}
