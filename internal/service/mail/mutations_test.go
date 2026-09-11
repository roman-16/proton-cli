package mail

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Marking a message legitimate is one request per message, because that is what
// Proton takes: the verdict is about the message rather than about its sender.
func TestMarkLegitimateAsksAboutEachMessage(t *testing.T) {
	api := &recordingAPI{}
	if err := New(api, testKeys(nil)).MarkLegitimate(context.Background(), []string{"a", "b"}); err != nil {
		t.Fatalf("MarkLegitimate: %v", err)
	}
	want := []string{"PUT /mail/v4/messages/a/mark/ham", "PUT /mail/v4/messages/b/mark/ham"}
	if got := api.calls(); !equalStrings(got, want) {
		t.Errorf("requests = %v, want %v", got, want)
	}
}

// Reporting phishing hands Proton the body it cannot read for itself, and then
// files the message where the reader's own verdict says it belongs.
func TestReportPhishingSendsTheBodyAndFilesTheMessage(t *testing.T) {
	kr := genMailKeyRing(t)
	api := &recordingAPI{message: encryptedMessage(t, kr, "Click here to verify your account", mimeTypeHTML)}
	s := New(api, testKeys(unlockedRings("addr-1", kr)))

	if err := s.ReportPhishing(context.Background(), "m1"); err != nil {
		t.Fatalf("ReportPhishing: %v", err)
	}

	report := api.body("/core/v4/reports/phishing")
	if report == nil {
		t.Fatal("nothing was reported")
	}
	if report["MessageID"] != "m1" {
		t.Errorf("MessageID = %v, want m1", report["MessageID"])
	}
	if report["Body"] != "Click here to verify your account" {
		t.Errorf("Body = %v, want the decrypted body - Proton holds no key to read it with", report["Body"])
	}
	if report["MIMEType"] != mimeTypeHTML {
		t.Errorf("MIMEType = %v, want %v", report["MIMEType"], mimeTypeHTML)
	}
	if got := bodyLabelID(t, api.requests[len(api.requests)-1]); got != labelSpam {
		t.Errorf("the message was filed under %q, want spam", got)
	}
}

// A plain-text message is reported as one, so the person reading the report sees
// what arrived rather than markup that was never there.
func TestReportPhishingKeepsTheMessagesOwnType(t *testing.T) {
	kr := genMailKeyRing(t)
	api := &recordingAPI{message: encryptedMessage(t, kr, "plain words", mimeTypePlain)}
	s := New(api, testKeys(unlockedRings("addr-1", kr)))

	if err := s.ReportPhishing(context.Background(), "m1"); err != nil {
		t.Fatalf("ReportPhishing: %v", err)
	}
	if got := api.body("/core/v4/reports/phishing")["MIMEType"]; got != mimeTypePlain {
		t.Errorf("MIMEType = %v, want %v", got, mimeTypePlain)
	}
}

// A body that will not open is refused rather than reported as the armour it
// still is: a report nobody at Proton can read is not a report, and the message
// would have been filed as spam on the strength of it.
func TestReportPhishingRefusesABodyItCannotOpen(t *testing.T) {
	api := &recordingAPI{message: encryptedMessage(t, genMailKeyRing(t), "secret", mimeTypePlain)}
	s := New(api, testKeys(unlockedRings("addr-1", genMailKeyRing(t))))

	if err := s.ReportPhishing(context.Background(), "m1"); err == nil {
		t.Fatal("a message that would not decrypt was reported anyway")
	}
	for _, call := range api.calls() {
		if strings.Contains(call, "reports/phishing") || strings.Contains(call, "messages/label") {
			t.Errorf("%s happened despite the body never being opened", call)
		}
	}
}

// ── the seam ──

// recordingAPI answers a message read and records everything sent.
type recordingAPI struct {
	// message is what GET /mail/v4/messages/{id} answers with.
	message  map[string]any
	requests []proton.Request
}

func (a *recordingAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *recordingAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	if out == nil {
		return nil
	}
	answer := map[string]any{"Code": 1000}
	if strings.HasPrefix(req.Path, "/mail/v4/messages/") && req.Method == "GET" {
		answer["Message"] = a.message
	}
	b, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func (a *recordingAPI) calls() []string {
	out := make([]string, 0, len(a.requests))
	for _, r := range a.requests {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}

func (a *recordingAPI) body(path string) map[string]any {
	for _, r := range a.requests {
		if r.Path == path {
			body, _ := r.Body.(map[string]any)
			return body
		}
	}
	return nil
}

// encryptedMessage is a stored message as Proton hands one back.
func encryptedMessage(t *testing.T, kr *pgp.KeyRing, body, mimeType string) map[string]any {
	t.Helper()
	enc, err := kr.Encrypt(pgp.NewPlainMessageFromString(body), nil)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	armored, err := enc.GetArmored()
	if err != nil {
		t.Fatalf("GetArmored: %v", err)
	}
	return map[string]any{"ID": "m1", "AddressID": "addr-1", "Body": armored, "MIMEType": mimeType}
}

func unlockedRings(addressID string, kr *pgp.KeyRing) *keys.Unlocked {
	return &keys.Unlocked{
		Addresses: []keys.Address{{ID: addressID}},
		AddrKRs:   map[string]keys.Rings{addressID: {Read: kr, Write: kr}},
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
