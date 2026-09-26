package mail

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
)

// testKeyRing generates a throwaway key pair, returning the ring and its armored
// public key.
func testKeyRing(t *testing.T, name, email string) (*pgp.KeyRing, string) {
	t.Helper()
	key, err := pgp.GenerateKey(name, email, "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey %s: %v", name, err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing %s: %v", name, err)
	}
	pub, err := key.GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("armor %s: %v", name, err)
	}
	return kr, pub
}

// openBody decrypts a package's body with the session key a recipient's entry
// wraps to kr.
func openBody(t *testing.T, pkg map[string]any, email string, kr *pgp.KeyRing) string {
	t.Helper()
	addr := pkg["Addresses"].(map[string]any)[email].(map[string]any)
	kp, err := base64.StdEncoding.DecodeString(addr["BodyKeyPacket"].(string))
	if err != nil {
		t.Fatalf("decode BodyKeyPacket: %v", err)
	}
	sk, err := kr.DecryptSessionKey(kp)
	if err != nil {
		t.Fatalf("DecryptSessionKey: %v", err)
	}
	body, err := base64.StdEncoding.DecodeString(pkg["Body"].(string))
	if err != nil {
		t.Fatalf("decode Body: %v", err)
	}
	dec, err := sk.Decrypt(body)
	if err != nil {
		t.Fatalf("decrypt body: %v", err)
	}
	return dec.GetString()
}

func packagesByFormat(pkgs []map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for _, p := range pkgs {
		out[p["MIMEType"].(string)] = p
	}
	return out
}

// Every recipient gets the package for the body their plan calls for, and a
// package's type is the union of the types of those it carries.
func TestBuildPackagesGivesEachRecipientTheBodyTheyNeed(t *testing.T) {
	internalKR, internalPub := testKeyRing(t, "internal", "internal@proton.me")
	inlineKR, inlinePub := testKeyRing(t, "inline", "inline@ext.com")
	mimeKR, mimePub := testKeyRing(t, "mime", "mime@ext.com")
	sndKR, _ := testKeyRing(t, "snd", "snd@proton.me")

	c := Content{
		From: &Sender{Address: keys.Address{Email: "snd@proton.me"}, Keys: keys.Rings{Read: sndKR, Write: sndKR}},
		Body: "<p>hello <b>world</b></p>", HTML: true,
	}
	plans := []plannedRecipient{
		{email: "internal@proton.me", kind: pkgInternal, mimeType: mimeTypeHTML, armoredKey: internalPub},
		{email: "clear@example.com", kind: pkgClear, mimeType: mimeTypeHTML},
		{email: "plain@proton.me", kind: pkgInternal, mimeType: mimeTypePlain, armoredKey: internalPub},
		{email: "inline@ext.com", kind: pkgPGPInline, mimeType: mimeTypePlain, armoredKey: inlinePub},
		{email: "signed-inline@example.com", kind: pkgClear, signature: true, mimeType: mimeTypePlain},
		{email: "mime@ext.com", kind: pkgPGPMIME, mimeType: mimeTypeMultipart, armoredKey: mimePub},
		{email: "signed-mime@example.com", kind: pkgClearMIME, signature: true, mimeType: mimeTypeMultipart},
	}

	pkgs, err := New(nil, testKeys(nil)).buildPackages(context.Background(), c, Delivery{}, nil, plans, proton.Modulus{})
	if err != nil {
		t.Fatalf("buildPackages: %v", err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("got %d packages, want one per body", len(pkgs))
	}
	byFormat := packagesByFormat(pkgs)

	html, plain, multipart := byFormat[mimeTypeHTML], byFormat[mimeTypePlain], byFormat[mimeTypeMultipart]
	for format, want := range map[string]int{
		mimeTypeHTML:      pkgInternal | pkgClear,
		mimeTypePlain:     pkgInternal | pkgPGPInline | pkgClear,
		mimeTypeMultipart: pkgPGPMIME | pkgClearMIME,
	} {
		if got := byFormat[format]["Type"]; got != want {
			t.Errorf("%s package Type = %v, want %d", format, got, want)
		}
	}
	for format, pkg := range byFormat {
		if _, has := pkg["BodyKey"]; !has {
			t.Errorf("the %s package carries a cleartext recipient but not the session key", format)
		}
	}

	if got := openBody(t, html, "internal@proton.me", internalKR); got != c.Body {
		t.Errorf("the HTML body = %q", got)
	}
	if got := openBody(t, plain, "inline@ext.com", inlineKR); !strings.Contains(got, "hello world") || strings.Contains(got, "<b>") {
		t.Errorf("the plain body is not flattened: %q", got)
	}
	if got := openBody(t, multipart, "mime@ext.com", mimeKR); !strings.Contains(got, "Content-Type: multipart/mixed") {
		t.Errorf("the MIME body is not a MIME message: %q", got)
	}

	for _, tc := range []struct {
		pkg   map[string]any
		email string
		kind  int
		sign  int
	}{
		{html, "clear@example.com", pkgClear, 0},
		{plain, "signed-inline@example.com", pkgClear, 1},
		{multipart, "signed-mime@example.com", pkgClearMIME, 1},
	} {
		addr := tc.pkg["Addresses"].(map[string]any)[tc.email].(map[string]any)
		if addr["Type"] != tc.kind || addr["Signature"] != tc.sign {
			t.Errorf("%s: entry %+v, want type %d signature %d", tc.email, addr, tc.kind, tc.sign)
		}
	}
}

// A body that refers to its attachments hands each recipient their keys; the
// MIME message carries the attachments inside and hands none.
func TestBuildPackagesWrapsAttachmentKeysForReferencedAttachments(t *testing.T) {
	recKR, recPub := testKeyRing(t, "rec", "rec@ext.com")
	sndKR, _ := testKeyRing(t, "snd", "snd@proton.me")
	attSK, err := pgp.GenerateSessionKey()
	if err != nil {
		t.Fatalf("attachment session key: %v", err)
	}
	atts := []*draftAttachment{{ID: "att-1", SessionKey: attSK}}
	c := Content{
		From: &Sender{Address: keys.Address{Email: "snd@proton.me"}, Keys: keys.Rings{Read: sndKR, Write: sndKR}},
		Body: "hello",
	}
	plans := []plannedRecipient{
		{email: "rec@ext.com", kind: pkgPGPInline, mimeType: mimeTypePlain, armoredKey: recPub},
		{email: "clear@example.com", kind: pkgClear, mimeType: mimeTypePlain},
	}
	pkgs, err := New(nil, testKeys(nil)).buildPackages(context.Background(), c, Delivery{}, atts, plans, proton.Modulus{})
	if err != nil {
		t.Fatalf("buildPackages: %v", err)
	}
	pkg := packagesByFormat(pkgs)[mimeTypePlain]
	addr := pkg["Addresses"].(map[string]any)["rec@ext.com"].(map[string]any)
	akp, ok := addr["AttachmentKeyPackets"].(map[string]string)
	if !ok {
		t.Fatalf("AttachmentKeyPackets missing or wrong type: %T", addr["AttachmentKeyPackets"])
	}
	attKP, err := base64.StdEncoding.DecodeString(akp["att-1"])
	if err != nil {
		t.Fatalf("decode attachment key packet: %v", err)
	}
	gotAttSK, err := recKR.DecryptSessionKey(attKP)
	if err != nil {
		t.Fatalf("decrypt attachment session key: %v", err)
	}
	if !reflect.DeepEqual(gotAttSK.Key, attSK.Key) {
		t.Error("attachment session key was not wrapped to the recipient")
	}
	if _, has := pkg["AttachmentKeys"]; !has {
		t.Error("a package with a cleartext recipient must hand over the attachment keys")
	}
}
