package mail

import (
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

func TestBuildInlinePackageFlattensHTMLAndWrapsKeys(t *testing.T) {
	recKR, recPub := testKeyRing(t, "rec", "rec@ext.com")
	sndKR, _ := testKeyRing(t, "snd", "snd@proton.me")

	attSK, err := pgp.GenerateSessionKey()
	if err != nil {
		t.Fatalf("attachment session key: %v", err)
	}
	atts := []*draftAttachment{{ID: "att-1", SessionKey: attSK}}

	c := Content{
		From: &Sender{Address: keys.Address{Email: "snd@proton.me"}, Keys: keys.Rings{Read: sndKR, Write: sndKR}},
		Body: "<p>hello <b>world</b></p>", HTML: true,
	}
	plans := []plannedRecipient{{email: "bob@ext.com", scheme: schemeExternalInline, armoredKey: recPub}}

	pkg, ok, err := New(nil, testKeys(nil)).buildInlinePackage(c, atts, plans)
	if err != nil {
		t.Fatalf("buildInlinePackage: %v", err)
	}
	if !ok {
		t.Fatal("expected an inline package")
	}
	if pkg["Type"] != pkgPGPInline {
		t.Errorf("package Type = %v, want %d", pkg["Type"], pkgPGPInline)
	}
	if pkg["MIMEType"] != mimeTypePlain {
		t.Errorf("MIMEType = %v, want %s", pkg["MIMEType"], mimeTypePlain)
	}

	addr := pkg["Addresses"].(map[string]any)["bob@ext.com"].(map[string]any)
	if addr["Type"] != pkgPGPInline {
		t.Errorf("address Type = %v, want %d", addr["Type"], pkgPGPInline)
	}

	// The body key packet is wrapped to the recipient: decrypt it, then the body,
	// and confirm the recipient reads a flattened plaintext body.
	recKP, err := base64.StdEncoding.DecodeString(addr["BodyKeyPacket"].(string))
	if err != nil {
		t.Fatalf("decode BodyKeyPacket: %v", err)
	}
	sk, err := recKR.DecryptSessionKey(recKP)
	if err != nil {
		t.Fatalf("recipient DecryptSessionKey: %v", err)
	}
	bodyData, err := base64.StdEncoding.DecodeString(pkg["Body"].(string))
	if err != nil {
		t.Fatalf("decode Body: %v", err)
	}
	dec, err := sk.Decrypt(bodyData)
	if err != nil {
		t.Fatalf("decrypt inline body: %v", err)
	}
	if got := dec.GetString(); !strings.Contains(got, "hello world") || strings.Contains(got, "<b>") {
		t.Errorf("inline body should be flattened plaintext, got %q", got)
	}

	// The attachment session key is wrapped to the recipient as well.
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
}

func TestBuildInlinePackageIsSkippedWithoutInlineRecipients(t *testing.T) {
	sndKR, _ := testKeyRing(t, "snd", "snd@proton.me")
	c := Content{From: &Sender{Keys: keys.Rings{Read: sndKR, Write: sndKR}}, Body: "hi"}
	plans := []plannedRecipient{{email: "a@proton.me", scheme: schemeInternal}}

	_, ok, err := New(nil, testKeys(nil)).buildInlinePackage(c, nil, plans)
	if err != nil {
		t.Fatalf("buildInlinePackage: %v", err)
	}
	if ok {
		t.Error("no recipient uses PGP-Inline, so no package should be built")
	}
}

func TestBuildBodyPackagesSplitsPerScheme(t *testing.T) {
	recKR, recPub := testKeyRing(t, "rec", "rec@proton.me")
	sndKR, _ := testKeyRing(t, "snd", "snd@proton.me")

	c := Content{
		From: &Sender{Address: keys.Address{Email: "snd@proton.me"}, Keys: keys.Rings{Read: sndKR, Write: sndKR}},
		Body: "hello",
	}
	plans := []plannedRecipient{
		{email: "internal@proton.me", scheme: schemeInternal, armoredKey: recPub},
		{email: "clear@example.com", scheme: schemeClear},
	}

	pkgs, err := New(nil, testKeys(nil)).buildBodyPackages(c, Delivery{}, nil, plans, proton.Modulus{})
	if err != nil {
		t.Fatalf("buildBodyPackages: %v", err)
	}
	if len(pkgs) != 2 {
		t.Fatalf("got %d packages, want one internal and one cleartext", len(pkgs))
	}

	byType := map[int]map[string]any{}
	for _, p := range pkgs {
		byType[p["Type"].(int)] = p
	}
	internal, ok := byType[pkgInternal]
	if !ok {
		t.Fatal("no internal package")
	}
	clear, ok := byType[pkgClear]
	if !ok {
		t.Fatal("no cleartext package")
	}
	// Both packages reference the same encrypted body; only the key handling
	// differs, which is the whole point of sharing one session key.
	if internal["Body"] != clear["Body"] {
		t.Error("internal and cleartext packages should share one encrypted body")
	}
	// The cleartext package hands over the raw session key; the internal one
	// wraps it to the recipient.
	if _, has := clear["BodyKey"]; !has {
		t.Error("a cleartext package must carry the session key itself")
	}
	addr := internal["Addresses"].(map[string]any)["internal@proton.me"].(map[string]any)
	kp, err := base64.StdEncoding.DecodeString(addr["BodyKeyPacket"].(string))
	if err != nil {
		t.Fatalf("decode BodyKeyPacket: %v", err)
	}
	sk, err := recKR.DecryptSessionKey(kp)
	if err != nil {
		t.Fatalf("recipient DecryptSessionKey: %v", err)
	}
	bodyData, err := base64.StdEncoding.DecodeString(internal["Body"].(string))
	if err != nil {
		t.Fatalf("decode Body: %v", err)
	}
	dec, err := sk.Decrypt(bodyData)
	if err != nil {
		t.Fatalf("decrypt body: %v", err)
	}
	if dec.GetString() != "hello" {
		t.Errorf("decrypted body = %q, want hello", dec.GetString())
	}
}
