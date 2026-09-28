package mail

import (
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	pgphelper "github.com/roman-16/proton-cli/internal/crypto/pgp"
)

func TestNewContentID(t *testing.T) {
	cid, err := newContentID("alice@proton.me")
	if err != nil {
		t.Fatalf("newContentID: %v", err)
	}
	local, domain, ok := strings.Cut(cid, "@")
	if !ok {
		t.Fatalf("cid %q has no @", cid)
	}
	if domain != "proton.me" {
		t.Errorf("domain = %q, want proton.me", domain)
	}
	// 8 random bytes -> 16 hex chars.
	if len(local) != 16 {
		t.Errorf("local part = %q (len %d), want 16 hex chars", local, len(local))
	}
	if _, err := hex.DecodeString(local); err != nil {
		t.Errorf("local part %q is not hex: %v", local, err)
	}

	custom, _ := newContentID("bob@company.example")
	if !strings.HasSuffix(custom, "@company.example") {
		t.Errorf("cid = %q, want @company.example suffix", custom)
	}

	// A sender without a parseable domain falls back to proton.me.
	fallback, _ := newContentID("no-at-sign")
	if !strings.HasSuffix(fallback, "@proton.me") {
		t.Errorf("fallback cid = %q, want @proton.me suffix", fallback)
	}

	a, _ := newContentID("x@y.z")
	b, _ := newContentID("x@y.z")
	if a == b {
		t.Error("two content IDs came out identical; they must be random")
	}
}

func TestAssignInlineContentIDsNoInlineIsNoOp(t *testing.T) {
	c := &Content{From: testSender(), HTML: true, Body: "<p>x</p>"}
	if err := assignInlineContentIDs(c); err != nil {
		t.Fatalf("assignInlineContentIDs: %v", err)
	}
	if c.Body != "<p>x</p>" {
		t.Errorf("body must be untouched, got %q", c.Body)
	}
}

func TestAssignInlineContentIDsRequiresHTML(t *testing.T) {
	c := &Content{
		From: testSender(), HTML: false, Body: "plain",
		Attach: []LocalAttachment{{Filename: "a.png", Data: []byte("x"), Inline: true}},
	}
	if err := assignInlineContentIDs(c); err == nil {
		t.Error("expected an error when an inline attachment is used without an HTML body")
	}
}

func TestAssignInlineContentIDsEmbedsAndSetsContentID(t *testing.T) {
	c := &Content{
		From: testSender(), HTML: true, Body: "<p>hi</p>",
		Attach: []LocalAttachment{
			{Filename: "logo.png", MIMEType: "image/png", Data: []byte("PNGBYTES"), Inline: true},
			{Filename: "note.txt", MIMEType: "text/plain", Data: []byte("plain")},
		},
	}
	if err := assignInlineContentIDs(c); err != nil {
		t.Fatalf("assignInlineContentIDs: %v", err)
	}
	inline, regular := c.Attach[0], c.Attach[1]
	if inline.ContentID == "" {
		t.Error("ContentID must be set for an inline attachment")
	}
	if regular.ContentID != "" {
		t.Errorf("a regular attachment must not get a ContentID, got %q", regular.ContentID)
	}
	// The body keeps its original content and gains a cid: reference so the
	// image renders in place.
	if !strings.HasPrefix(c.Body, "<p>hi</p>") {
		t.Errorf("original body should be preserved as a prefix, got %q", c.Body)
	}
	if !strings.Contains(c.Body, `src="cid:`+inline.ContentID+`"`) {
		t.Errorf("body should reference the cid, got %q", c.Body)
	}
	if strings.Contains(c.Body, "note.txt") {
		t.Errorf("a regular attachment must not be referenced in the body, got %q", c.Body)
	}
}

func TestReadLocalAttachmentResolvesMIMEType(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logo.png")
	data := []byte("PNGBYTES")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ReadLocalAttachment(path, true)
	if err != nil {
		t.Fatalf("ReadLocalAttachment: %v", err)
	}
	if a.Filename != "logo.png" {
		t.Errorf("filename = %q, want logo.png", a.Filename)
	}
	if !strings.HasPrefix(a.MIMEType, "image/png") {
		t.Errorf("mime = %q, want image/png", a.MIMEType)
	}
	if !a.Inline {
		t.Error("inline flag was not carried through")
	}
	if string(a.Data) != string(data) {
		t.Errorf("data = %q, want %q", a.Data, data)
	}
}

func TestReadLocalAttachmentUnknownExtensionFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blob.zzzz")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := ReadLocalAttachment(path, false)
	if err != nil {
		t.Fatalf("ReadLocalAttachment: %v", err)
	}
	if a.MIMEType != "application/octet-stream" {
		t.Errorf("mime = %q, want application/octet-stream", a.MIMEType)
	}
}

// TestAttachmentPasswordKeyPacketsRoundTrip proves an EO attachment key packet
// decrypts back to the original session key with the password.
func TestAttachmentPasswordKeyPacketsRoundTrip(t *testing.T) {
	const pw = "correct horse battery staple"
	sk, err := pgphelper.PGP.GenerateSessionKey()
	if err != nil {
		t.Fatalf("GenerateSessionKey: %v", err)
	}
	atts := []*draftAttachment{{ID: "att-1", SessionKey: sk}}

	out, err := attachmentPasswordKeyPackets(atts, pw)
	if err != nil {
		t.Fatalf("attachmentPasswordKeyPackets: %v", err)
	}
	packetB64, ok := out["att-1"]
	if !ok {
		t.Fatalf("missing key packet for att-1: %v", out)
	}
	packet, err := base64.StdEncoding.DecodeString(packetB64)
	if err != nil {
		t.Fatalf("packet is not base64: %v", err)
	}
	got, err := pgphelper.DecryptSessionKeyWithPassword([]byte(pw), packet)
	if err != nil {
		t.Fatalf("DecryptSessionKeyWithPassword: %v", err)
	}
	if !reflect.DeepEqual(got.Key, sk.Key) {
		t.Error("round-tripped session key does not match the original")
	}
	// The wrong password must not recover the session key. That it also *errors*
	// cannot be asserted: a symmetric-key encrypted session key packet carries no
	// integrity check, so a wrong password yields bytes that now and then parse as
	// a valid session key. What the packet promises is that they are not this one,
	// and the data they would go on to decrypt is what carries the tag that fails.
	if wrong, err := pgphelper.DecryptSessionKeyWithPassword([]byte("wrong"), packet); err == nil && reflect.DeepEqual(wrong.Key, sk.Key) {
		t.Error("the wrong password recovered the session key")
	}
}

func TestAttachmentPasswordKeyPacketsEmpty(t *testing.T) {
	out, err := attachmentPasswordKeyPackets(nil, "pw")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil for no attachments, got %v", out)
	}
}
