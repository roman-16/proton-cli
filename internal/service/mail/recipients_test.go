package mail

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/vcard"
)

func TestDestinationFrom(t *testing.T) {
	internalKey := apiPublicKey{PublicKey: "INTERNAL", Flags: 3, Source: 0}
	noEncryptKey := apiPublicKey{PublicKey: "DISABLED", Flags: 3 | 4, Source: 0}
	wkdKey := apiPublicKey{PublicKey: "WKD", Flags: 3, Source: 1}
	unverifiedInternal := apiPublicKey{PublicKey: "UNVER-INT", Flags: 3, Source: 0}

	tests := []struct {
		name    string
		resp    keysAllResponse
		want    Destination
		wantKey string
	}{
		{"internal address key", resp(apiKeys(internalKey), nil), Destination{Proton: true}, "INTERNAL"},
		{"internal via unverified proton key", resp(nil, []apiPublicKey{unverifiedInternal}), Destination{Proton: true}, "UNVER-INT"},
		{"external WKD key", resp(nil, []apiPublicKey{wkdKey}), Destination{ProviderKey: true}, "WKD"},
		{"no key", resp(nil, nil), Destination{}, ""},
		{"e2ee-disabled internal key is not mail-capable", resp(apiKeys(noEncryptKey), nil), Destination{}, ""},
		{"internal wins over an external WKD key", resp(apiKeys(internalKey), []apiPublicKey{wkdKey}), Destination{Proton: true}, "INTERNAL"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, key := destinationFrom(tc.resp)
			if got != tc.want || key != tc.wantKey {
				t.Errorf("destinationFrom = %+v %q, want %+v %q", got, key, tc.want, tc.wantKey)
			}
		})
	}
}

func apiKeys(keys ...apiPublicKey) []apiPublicKey { return keys }

func resp(addressKeys, unverifiedKeys []apiPublicKey) keysAllResponse {
	var r keysAllResponse
	r.Address.Keys = addressKeys
	r.Unverified.Keys = unverifiedKeys
	return r
}

func genArmoredPubKey(t *testing.T) (armored, fingerprint string) {
	t.Helper()
	key, err := pgp.GenerateKey("pin", "pin@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	armored, err = key.GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("GetArmoredPublicKey: %v", err)
	}
	return armored, key.GetFingerprint()
}

func ptr[T any](v T) *T { return &v }

// The rules Proton's clients send by, one row each.
func TestResolve(t *testing.T) {
	outside, provider, proton := Destination{}, Destination{ProviderKey: true}, Destination{Proton: true}
	pinned := []string{"KEY"}
	tests := []struct {
		name     string
		dest     Destination
		contact  ContactSettings
		defaults SendDefaults
		want     Preferences
	}{
		{"a Proton address is encrypted and signed", proton, ContactSettings{Sign: ptr(false)}, SendDefaults{},
			Preferences{Encrypt: true, Sign: true}},
		{"a Proton address takes the format", proton, ContactSettings{PlainText: true}, SendDefaults{},
			Preferences{Encrypt: true, Sign: true, PlainText: true}},
		{"a provider key is encrypted to by default", provider, ContactSettings{}, SendDefaults{},
			Preferences{Encrypt: true, Sign: true}},
		{"a contact can turn encryption off", provider, ContactSettings{Encrypt: ptr(false)}, SendDefaults{},
			Preferences{}},
		{"a pinned key is encrypted to by default", outside, ContactSettings{Keys: pinned}, SendDefaults{},
			Preferences{Encrypt: true, Sign: true}},
		{"nothing to encrypt to", outside, ContactSettings{Encrypt: ptr(true)}, SendDefaults{},
			Preferences{}},
		{"the account signs by default", outside, ContactSettings{}, SendDefaults{Sign: true},
			Preferences{Sign: true}},
		{"a contact overrides the account", outside, ContactSettings{Sign: ptr(false)}, SendDefaults{Sign: true},
			Preferences{}},
		{"encryption signs whatever the contact says", provider, ContactSettings{Sign: ptr(false)}, SendDefaults{},
			Preferences{Encrypt: true, Sign: true}},
		{"PGP/Inline signing is plain text", outside, ContactSettings{Sign: ptr(true), Scheme: vcard.SchemeInline}, SendDefaults{},
			Preferences{Sign: true, Inline: true, PlainText: true}},
		{"PGP/MIME signing keeps the format", outside, ContactSettings{Sign: ptr(true), PlainText: true}, SendDefaults{},
			Preferences{Sign: true}},
		{"the account's scheme applies", outside, ContactSettings{Sign: ptr(true)}, SendDefaults{Inline: true},
			Preferences{Sign: true, Inline: true, PlainText: true}},
		{"a contact's scheme overrides the account's", outside, ContactSettings{Sign: ptr(true), Scheme: vcard.SchemeMIME}, SendDefaults{Inline: true},
			Preferences{Sign: true}},
		{"unsigned mail takes the format", outside, ContactSettings{PlainText: true}, SendDefaults{},
			Preferences{PlainText: true}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Resolve(tc.dest, tc.contact, tc.defaults); got != tc.want {
				t.Errorf("Resolve = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// keysDoer answers the keys lookup with a fixed response.
type keysDoer struct {
	fakeDoer
	keys keysAllResponse
}

func (d *keysDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.last = r
	b, err := json.Marshal(d.keys)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestPlanRecipient(t *testing.T) {
	wkd := resp(nil, []apiPublicKey{{PublicKey: "WKD", Flags: 3, Source: 1}})
	internal := resp(apiKeys(apiPublicKey{PublicKey: "INTERNAL", Flags: 3}), nil)
	tests := []struct {
		name     string
		keys     keysAllResponse
		eo       string
		contact  *ContactSettings
		defaults SendDefaults
		composer string
		want     plannedRecipient
	}{
		{"a Proton address", internal, "", nil, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgInternal, mimeType: mimeTypeHTML, armoredKey: "INTERNAL"}},
		{"a Proton address that wants plain text", internal, "", &ContactSettings{PlainText: true}, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgInternal, mimeType: mimeTypePlain, armoredKey: "INTERNAL"}},
		{"a provider key", wkd, "", nil, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgPGPMIME, mimeType: mimeTypeMultipart, armoredKey: "WKD"}},
		{"a provider key under PGP/Inline", wkd, "", &ContactSettings{Scheme: vcard.SchemeInline}, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgPGPInline, mimeType: mimeTypePlain, armoredKey: "WKD"}},
		{"a password", resp(nil, nil), "hunter22", nil, SendDefaults{Sign: true}, mimeTypeHTML,
			plannedRecipient{kind: pkgEO, mimeType: mimeTypeHTML}},
		{"signed with PGP/MIME", resp(nil, nil), "", nil, SendDefaults{Sign: true}, mimeTypeHTML,
			plannedRecipient{kind: pkgClearMIME, signature: true, mimeType: mimeTypeMultipart}},
		{"signed with PGP/Inline", resp(nil, nil), "", &ContactSettings{Sign: ptr(true), Scheme: vcard.SchemeInline}, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgClear, signature: true, mimeType: mimeTypePlain}},
		{"in the clear, as plain text", resp(nil, nil), "", &ContactSettings{PlainText: true}, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgClear, mimeType: mimeTypePlain}},
		{"in the clear, as written", resp(nil, nil), "", nil, SendDefaults{}, mimeTypeHTML,
			plannedRecipient{kind: pkgClear, mimeType: mimeTypeHTML}},
		{"a provider key the contact will not encrypt to", wkd, "", &ContactSettings{Encrypt: ptr(false)}, SendDefaults{}, mimeTypePlain,
			plannedRecipient{kind: pkgClear, mimeType: mimeTypePlain}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{C: &keysDoer{keys: tc.keys}}
			got, err := svc.planRecipient(context.Background(), "bob@example.com", tc.eo, tc.contact, tc.defaults, tc.composer)
			if err != nil {
				t.Fatalf("planRecipient: %v", err)
			}
			tc.want.email = "bob@example.com"
			if got != tc.want {
				t.Errorf("planRecipient = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestPinnedSendKey(t *testing.T) {
	pubA, _ := genArmoredPubKey(t)
	pubB, _ := genArmoredPubKey(t)
	ctx := context.Background()

	t.Run("no key of its own encrypts to the pinned key", func(t *testing.T) {
		key, err := pinnedSendKey(ctx, "bob@ext.com", "", ContactSettings{Keys: []string{pubA}, SignatureVerified: true})
		if err != nil || key != pubA {
			t.Errorf("pinnedSendKey = %v, want the pinned key", err)
		}
	})
	t.Run("its own key must be the pinned one", func(t *testing.T) {
		key, err := pinnedSendKey(ctx, "alice@proton.me", pubA, ContactSettings{Keys: []string{pubA}, SignatureVerified: true})
		if err != nil || key != pubA {
			t.Errorf("pinnedSendKey = %v, want the pinned copy", err)
		}
	})
	t.Run("its own key not pinned refuses", func(t *testing.T) {
		_, err := pinnedSendKey(ctx, "alice@proton.me", pubA, ContactSettings{Keys: []string{pubB}, SignatureVerified: true})
		if err == nil || !strings.Contains(err.Error(), "do not match") {
			t.Errorf("err = %v, want the primary-not-pinned refusal", err)
		}
	})
	t.Run("an unverified contact refuses", func(t *testing.T) {
		if _, err := pinnedSendKey(ctx, "bob@ext.com", "", ContactSettings{Keys: []string{pubA}}); err == nil {
			t.Error("expected an error for an unverified contact signature")
		}
	})
	t.Run("no key that can encrypt refuses", func(t *testing.T) {
		_, err := pinnedSendKey(ctx, "bob@ext.com", "", ContactSettings{Keys: []string{"not-a-key"}, SignatureVerified: true})
		var problem *errs.Problem
		if !errors.As(err, &problem) {
			t.Fatalf("err = %v, want a refusal", err)
		}
		if hint := strings.Join(problem.Hints(), " "); !strings.Contains(hint, "proton contacts keys untrust bob@ext.com") {
			t.Errorf("the refusal points at %q, which is not a command", hint)
		}
	})
}

// A pin that cannot be seen is not the same as no pin. The send stops before it
// asks Proton for a key, since a message it will not send has no use for one -
// and says so in a sentence the sender can act on, not one that asks for a
// report.
func TestAPinThatCannotBeSeenStopsTheSend(t *testing.T) {
	d := &fakeDoer{}
	svc := &Service{C: d}
	_, err := svc.planRecipient(context.Background(), "bob@example.com", "", &ContactSettings{Unknown: true}, SendDefaults{}, mimeTypePlain)
	if err == nil {
		t.Fatal("a recipient whose pins could not be read was planned for sending")
	}
	if d.last.Path != "" {
		t.Errorf("a stopped send still asked Proton for keys at %s", d.last.Path)
	}
	var coder errs.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("the refusal is not a plain user error: %v", err)
	}
	if !strings.Contains(err.Error(), "Nothing was sent.") {
		t.Errorf("the refusal does not say nothing went out: %v", err)
	}
}

func TestMailCapable(t *testing.T) {
	if !mailCapable(3) {
		t.Error("flags 3 (NOT_OBSOLETE|NOT_COMPROMISED) should be mail-capable")
	}
	if mailCapable(3 | keyFlagEmailNoEncrypt) {
		t.Error("FLAG_EMAIL_NO_ENCRYPT should make a key non-mail-capable")
	}
}

// errDoer answers every request with one refusal.
type errDoer struct {
	fakeDoer
	err error
}

func (d *errDoer) Decode(context.Context, proton.Request, any) error { return d.err }

// An address Proton holds no keys for is an address outside Proton with none,
// as Proton's own email-settings editor reads it; anything else stays an error.
func TestDestinationReadsNoKeysAsOutside(t *testing.T) {
	svc := &Service{C: &errDoer{err: &proton.APIError{HTTPStatus: 422, Code: 33102, Message: "no"}}}
	if d, err := svc.Destination(context.Background(), "nobody@example.invalid"); err != nil || d != (Destination{}) {
		t.Errorf("Destination = %+v, %v; want an address outside Proton", d, err)
	}
	svc = &Service{C: &errDoer{err: &proton.APIError{HTTPStatus: 500, Code: 2500, Message: "down"}}}
	if _, err := svc.Destination(context.Background(), "jane@example.com"); err == nil {
		t.Error("a failure that says nothing about the address was taken as an answer")
	}
}
