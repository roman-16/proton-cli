package mail

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/roman-16/proton-cli/internal/export"
)

func listedMessage(t *testing.T, id string, unread int, labels ...string) listed {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"ID": id, "AddressID": "addr-1", "Subject": "Invoice #2291", "Time": 1775000000,
		"Size": 2048, "Unread": unread, "LabelIDs": labels, "Flags": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	var m rawListMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	return listed{meta: m, raw: raw}
}

func openCopy(t *testing.T) (*export.Copy, *export.Ledger) {
	t.Helper()
	c, err := export.Open(filepath.Join(t.TempDir(), "copy"), "user-1", "you@proton.me")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	l, err := c.Ledger(export.AppMail)
	if err != nil {
		t.Fatal(err)
	}
	return c, l
}

func written(t *testing.T, c *export.Copy, l *export.Ledger, m listed) {
	t.Helper()
	stem := m.want()
	for _, suffix := range []string{emlSuffix, sidecarSuffix} {
		path := c.Path(stem + suffix)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(`{"Version":1,"Payload":{"Headers":"From: a"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := l.Put(export.Entry{ID: m.meta.ID, Version: m.version(), Path: stem, Want: stem}); err != nil {
		t.Fatal(err)
	}
}

func TestAMessageIsOwedOnlyWhatChanged(t *testing.T) {
	c, l := openCopy(t)
	held := listedMessage(t, "held", 0, "0")
	relabelled := listedMessage(t, "relabelled", 0, "0")
	written(t, c, l, held)
	written(t, c, l, relabelled)
	relabelled = listedMessage(t, "relabelled", 1, "0", "10")
	fresh := listedMessage(t, "fresh", 0, "0")

	due := copyPart{}.due(c, l, []listed{held, relabelled, fresh})
	if len(due) != 2 {
		t.Fatalf("owed %d messages, want the relabelled one and the new one", len(due))
	}
	byID := map[string]owed{}
	for _, d := range due {
		byID[d.meta.ID] = d
	}
	if d, ok := byID["relabelled"]; !ok || d.whole {
		t.Errorf("a message whose labels moved is owed its sidecar alone: %+v", d)
	}
	if d, ok := byID["fresh"]; !ok || !d.whole {
		t.Errorf("a new message is not owed in whole: %+v", d)
	}
}

func TestAMessageWhoseFilesWentIsOwedAgain(t *testing.T) {
	c, l := openCopy(t)
	m := listedMessage(t, "gone", 0, "0")
	written(t, c, l, m)
	if err := os.Remove(c.Path(m.want() + emlSuffix)); err != nil {
		t.Fatal(err)
	}
	due := copyPart{}.due(c, l, []listed{m})
	if len(due) != 1 || !due[0].whole {
		t.Errorf("owed %+v, want the whole message again", due)
	}
}

func TestASidecarCarriesWhatRestoringReads(t *testing.T) {
	m := listedMessage(t, "msg-1", 1, "0", "10")
	doc, err := sidecarOf(m.raw, &rawMessage{MIMEType: "text/plain", Header: "From: a@b\r\n"})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Version int
		Payload struct {
			ID, AddressID, MIMEType, Headers string
			LabelIDs                         []string
			Unread                           int
			Time                             int64
			Attachments                      []any
		}
	}
	if err := json.Unmarshal(doc, &got); err != nil {
		t.Fatal(err)
	}
	p := got.Payload
	if got.Version != 1 || p.ID != "msg-1" || p.AddressID != "addr-1" || p.Unread != 1 || p.Time != 1775000000 ||
		len(p.LabelIDs) != 2 || p.MIMEType != "text/plain" || p.Headers != "From: a@b\r\n" || p.Attachments == nil {
		t.Errorf("sidecar = %s", doc)
	}
}

func TestRestatingKeepsWhatOnlyTheWholeMessageSaid(t *testing.T) {
	c, l := openCopy(t)
	m := listedMessage(t, "msg-1", 0, "0")
	written(t, c, l, m)
	moved := listedMessage(t, "msg-1", 1, "0", "10")
	if err := (copyPart{}).restate(c, m.want(), moved); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(c.Path(m.want() + sidecarSuffix))
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Payload struct {
			Headers  string
			Unread   int
			LabelIDs []string
		}
	}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Payload.Headers != "From: a" || got.Payload.Unread != 1 || len(got.Payload.LabelIDs) != 2 {
		t.Errorf("restated sidecar = %s", raw)
	}
}
