package mail

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestAThreadActsOnItsInboxMessagesForATab(t *testing.T) {
	for scope, want := range map[string]string{"": labelAllMail, labelSocial: labelInbox, labelArchive: labelArchive} {
		f := &fakeDoer{}
		if _, err := New(f, testKeys(nil)).ConversationsDelete(context.Background(), []string{"c"}, scope); err != nil {
			t.Fatalf("ConversationsDelete: %v", err)
		}
		if got := bodyLabelID(t, f.last); got != want {
			t.Errorf("scope %q: LabelID %q, want %q", scope, got, want)
		}
	}
}

type batchAPI struct {
	sent      []map[string]any
	refuse    map[string]string
	anonymous bool
}

func (a *batchAPI) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *batchAPI) Decode(_ context.Context, req proton.Request, out any) error {
	body := req.Body.(map[string]any)
	a.sent = append(a.sent, body)
	var responses []map[string]any
	for _, id := range body["IDs"].([]string) {
		code, reason := 1000, ""
		if why, ok := a.refuse[id]; ok {
			code, reason = 2501, why
		}
		answer := map[string]any{"Response": map[string]any{"Code": code, "Error": reason}}
		if !a.anonymous {
			answer["ID"] = id
		}
		responses = append(responses, answer)
	}
	b, err := json.Marshal(map[string]any{"Code": 1001, "Responses": responses})
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestABatchGoesAHundredAndFiftyAtATime(t *testing.T) {
	ids := make([]string, 320)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%d", i)
	}
	api := &batchAPI{}
	if _, err := New(api, testKeys(nil)).Label(context.Background(), ids, labelArchive); err != nil {
		t.Fatal(err)
	}
	var sizes []int
	for _, body := range api.sent {
		sizes = append(sizes, len(body["IDs"].([]string)))
		if body["LabelID"] != labelArchive {
			t.Errorf("a batch lost its label: %v", body["LabelID"])
		}
	}
	if fmt.Sprint(sizes) != "[150 150 20]" {
		t.Errorf("batches of %v, want [150 150 20]", sizes)
	}
}

func TestABatchNamesWhatProtonRefused(t *testing.T) {
	for _, anonymous := range []bool{false, true} {
		api := &batchAPI{refuse: map[string]string{"b": "Message does not exist"}, anonymous: anonymous}
		refused, err := New(api, testKeys(nil)).SetExpiration(context.Background(), []string{"a", "b", "c"}, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(refused) != 1 || refused[0] != (Refused{ID: "b", Reason: "Message does not exist"}) {
			t.Errorf("anonymous %v: refused = %+v, want b alone", anonymous, refused)
		}
		if at, ok := api.sent[0]["ExpirationTime"]; !ok || at != nil {
			t.Errorf("never sent ExpirationTime %v, want null", at)
		}
	}
}
