package mail

import (
	"context"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
)

func TestClassifyRecipient(t *testing.T) {
	const pw = "hunter2"
	internalKey := apiPublicKey{PublicKey: "INTERNAL", Flags: 3, Source: 0}         // NOT_OBSOLETE|NOT_COMPROMISED, Proton
	noEncryptKey := apiPublicKey{PublicKey: "DISABLED", Flags: 3 | 4, Source: 0}    // e2ee disabled for mail
	wkdKey := apiPublicKey{PublicKey: "WKD", Flags: 3, Source: 1}                   // external WKD key
	unverifiedInternal := apiPublicKey{PublicKey: "UNVER-INT", Flags: 3, Source: 0} // unverified Proton key

	tests := []struct {
		name       string
		resp       keysAllResponse
		eoPassword string
		wantScheme sendScheme
		wantKey    string
	}{
		{
			name:       "internal address key",
			resp:       resp(apiKeys(internalKey), nil),
			wantScheme: schemeInternal, wantKey: "INTERNAL",
		},
		{
			name:       "internal via unverified proton key",
			resp:       resp(nil, []apiPublicKey{unverifiedInternal}),
			wantScheme: schemeInternal, wantKey: "UNVER-INT",
		},
		{
			name:       "external PGP from unverified WKD key",
			resp:       resp(nil, []apiPublicKey{wkdKey}),
			wantScheme: schemeExternalPGP, wantKey: "WKD",
		},
		{
			name:       "no key + eo password => EO",
			resp:       resp(nil, nil),
			eoPassword: pw,
			wantScheme: schemeEO,
		},
		{
			name:       "no key, no password => cleartext",
			resp:       resp(nil, nil),
			wantScheme: schemeClear,
		},
		{
			name:       "e2ee-disabled internal key is not mail-capable => cleartext",
			resp:       resp(apiKeys(noEncryptKey), nil),
			wantScheme: schemeClear,
		},
		{
			name:       "e2ee-disabled internal key + password => EO",
			resp:       resp(apiKeys(noEncryptKey), nil),
			eoPassword: pw,
			wantScheme: schemeEO,
		},
		{
			name:       "internal wins over an external WKD key",
			resp:       resp(apiKeys(internalKey), []apiPublicKey{wkdKey}),
			wantScheme: schemeInternal, wantKey: "INTERNAL",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotScheme, gotKey := classifyRecipient(tc.resp, tc.eoPassword)
			if gotScheme != tc.wantScheme {
				t.Errorf("scheme = %d, want %d", gotScheme, tc.wantScheme)
			}
			if gotKey != tc.wantKey {
				t.Errorf("key = %q, want %q", gotKey, tc.wantKey)
			}
		})
	}
}

// apiKeys is sugar for a key list in a keysAllResponse.
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

func TestPinEncrypts(t *testing.T) {
	no, yes := false, true
	if !pinEncrypts(&PinnedRecipient{}) {
		t.Error("nil Encrypt should default to true (pinned key present)")
	}
	if !pinEncrypts(&PinnedRecipient{Encrypt: &yes}) {
		t.Error("explicit true should encrypt")
	}
	if pinEncrypts(&PinnedRecipient{Encrypt: &no}) {
		t.Error("explicit false should opt out")
	}
}

func TestPlanPinnedRecipient(t *testing.T) {
	pubA, _ := genArmoredPubKey(t)
	pubB, _ := genArmoredPubKey(t)

	t.Run("external no server key encrypts to pinned key (PGP/MIME)", func(t *testing.T) {
		pin := &PinnedRecipient{ArmoredKeys: []string{pubA}, SignatureVerified: true}
		p, err := planPinnedRecipient(context.Background(), "bob@ext.com", schemeClear, "", pin)
		if err != nil {
			t.Fatalf("planPinnedRecipient: %v", err)
		}
		if p.scheme != schemeExternalPGP {
			t.Errorf("scheme = %d, want schemeExternalPGP", p.scheme)
		}
		if p.armoredKey != pubA {
			t.Error("expected the pinned key to be used as the send key")
		}
	})

	t.Run("pgp-inline scheme selects the inline path", func(t *testing.T) {
		pin := &PinnedRecipient{ArmoredKeys: []string{pubA}, Scheme: "pgp-inline", SignatureVerified: true}
		p, err := planPinnedRecipient(context.Background(), "bob@ext.com", schemeClear, "", pin)
		if err != nil {
			t.Fatalf("planPinnedRecipient: %v", err)
		}
		if p.scheme != schemeExternalInline {
			t.Errorf("scheme = %d, want schemeExternalInline", p.scheme)
		}
		if p.armoredKey != pubA {
			t.Error("inline should still send to the pinned key")
		}
	})

	t.Run("internal recipient uses pinned copy of the primary key", func(t *testing.T) {
		// The primary API key is pinned (same key material), so we send to it.
		pin := &PinnedRecipient{ArmoredKeys: []string{pubA}, SignatureVerified: true}
		p, err := planPinnedRecipient(context.Background(), "alice@proton.me", schemeInternal, pubA, pin)
		if err != nil {
			t.Fatalf("planPinnedRecipient: %v", err)
		}
		if p.scheme != schemeInternal || p.armoredKey != pubA {
			t.Errorf("got scheme=%d key-match=%v, want internal + pinned copy", p.scheme, p.armoredKey == pubA)
		}
	})

	t.Run("internal recipient whose primary key is not pinned errors", func(t *testing.T) {
		pin := &PinnedRecipient{ArmoredKeys: []string{pubB}, SignatureVerified: true}
		if _, err := planPinnedRecipient(context.Background(), "alice@proton.me", schemeInternal, pubA, pin); err == nil {
			t.Error("expected PRIMARY_NOT_PINNED-style error when the primary key is not pinned")
		}
	})

	t.Run("unverified contact signature refuses to send", func(t *testing.T) {
		pin := &PinnedRecipient{ArmoredKeys: []string{pubA}, SignatureVerified: false}
		if _, err := planPinnedRecipient(context.Background(), "bob@ext.com", schemeClear, "", pin); err == nil {
			t.Error("expected an error for an unverified contact signature")
		}
	})

	t.Run("no parseable pinned key errors", func(t *testing.T) {
		pin := &PinnedRecipient{ArmoredKeys: []string{"not-a-key"}, SignatureVerified: true}
		if _, err := planPinnedRecipient(context.Background(), "bob@ext.com", schemeClear, "", pin); err == nil {
			t.Error("expected an error when no pinned key is valid for sending")
		}
	})
}

func TestMailCapable(t *testing.T) {
	if !mailCapable(3) {
		t.Error("flags 3 (NOT_OBSOLETE|NOT_COMPROMISED) should be mail-capable")
	}
	if mailCapable(3 | keyFlagEmailNoEncrypt) {
		t.Error("FLAG_EMAIL_NO_ENCRYPT should make a key non-mail-capable")
	}
}
