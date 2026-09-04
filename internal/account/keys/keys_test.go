package keys

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ProtonMail/go-srp"
	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/localkey"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/skip"
)

func unlocked(addrs ...Address) *Unlocked {
	u := &Unlocked{Addresses: addrs, AddrKRs: map[string]*pgp.KeyRing{}}
	for _, a := range addrs {
		if len(a.Keys) > 0 {
			u.AddrKRs[a.ID] = &pgp.KeyRing{}
		}
	}
	return u
}

var withKey = []Key{{ID: "k"}}

func TestFirstAddrSkipsAddressesWithoutKeys(t *testing.T) {
	u := unlocked(
		Address{ID: "locked", Email: "locked@example.com"},
		Address{ID: "open", Email: "open@example.com", DisplayName: "Open", Keys: withKey},
	)
	kr, addr, err := u.FirstAddr()
	if err != nil {
		t.Fatalf("FirstAddr: %v", err)
	}
	if kr == nil {
		t.Error("FirstAddr returned a nil key ring")
	}
	if addr.ID != "open" || addr.DisplayName != "Open" {
		t.Errorf("FirstAddr = %+v, want the address whose keys unlocked", addr)
	}
}

func TestFirstAddrWithoutUnlockableAddresses(t *testing.T) {
	_, _, err := unlocked(Address{ID: "locked", Email: "locked@example.com"}).FirstAddr()
	if err == nil {
		t.Error("FirstAddr with no unlockable address should fail")
	}
}

// Writing goes under one key even on an account that holds several, because
// sealing something new under a key the owner has retired is not a favour.
func TestPrimaryUserKeyIsOneKeyAndTheFirst(t *testing.T) {
	ring, err := pgp.NewKeyRing(nil)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	for _, name := range []string{"primary", "retired"} {
		key, err := pgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		if err := ring.AddKey(key); err != nil {
			t.Fatalf("AddKey: %v", err)
		}
	}

	write, err := (&Unlocked{UserKR: ring}).PrimaryUserKey()
	if err != nil {
		t.Fatalf("PrimaryUserKey: %v", err)
	}
	if write.CountEntities() != 1 {
		t.Fatalf("the write key holds %d keys, want 1", write.CountEntities())
	}
	if got, want := write.GetKeys()[0].GetFingerprint(), ring.GetKeys()[0].GetFingerprint(); got != want {
		t.Errorf("wrote under %s, want the primary %s", got, want)
	}
}

func TestPrimaryAddrPrefersProtonDomains(t *testing.T) {
	tests := []struct {
		name  string
		addrs []Address
		want  string
	}{
		{
			"custom domain first, proton.me second",
			[]Address{
				{ID: "custom", Email: "me@example.com", Keys: withKey},
				{ID: "proton", Email: "me@proton.me", Keys: withKey},
			},
			"proton",
		},
		{
			"pm.me counts as a Proton domain",
			[]Address{
				{ID: "custom", Email: "me@example.com", Keys: withKey},
				{ID: "pm", Email: "me@pm.me", Keys: withKey},
			},
			"pm",
		},
		{
			"protonmail.com counts as a Proton domain",
			[]Address{
				{ID: "custom", Email: "me@example.com", Keys: withKey},
				{ID: "legacy", Email: "me@protonmail.com", Keys: withKey},
			},
			"legacy",
		},
		{
			"falls back to the first unlockable address",
			[]Address{{ID: "custom", Email: "me@example.com", Keys: withKey}},
			"custom",
		},
		{
			"ignores a Proton address whose keys stayed locked",
			[]Address{
				{ID: "custom", Email: "me@example.com", Keys: withKey},
				{ID: "proton", Email: "me@proton.me"},
			},
			"custom",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, addr, err := unlocked(tc.addrs...).PrimaryAddr()
			if err != nil {
				t.Fatalf("PrimaryAddr: %v", err)
			}
			if addr.ID != tc.want {
				t.Errorf("PrimaryAddr = %q, want %q", addr.ID, tc.want)
			}
		})
	}
}

// Which of an account's secrets opens its keys is the whole of two-password
// mode, and no test with an account can prove it: the accounts the live suite
// signs in are Proton's to configure, and a wrong secret there fails as a wrong
// password rather than as the wrong question. So the hierarchy is built here -
// real keys, a real salt, Proton's own bcrypt - and served to the client.

// account is a Proton account as the unlock path sees it: a user key and an
// address key, both locked with a passphrase stretched from one secret.
type account struct {
	// secret is what a person would type.
	secret string
	// keySalt is the salt Proton holds for the primary user key, base64 as the
	// API returns it.
	keySalt string
	// twoPassword is the mode /core/v4/settings reports.
	twoPassword bool
	// salts are what /core/v4/keys/salts answers, in the order it answers them.
	salts []salt
	// clientKey is what the session's local key answers with, empty for a
	// session Proton holds none for.
	clientKey []byte

	userKey, addrKey string
	// addrToken is the token and signature the address key's passphrase is
	// sealed in, for the hierarchy that cannot open its own address keys.
	addrToken string
}

// newAccount locks a fresh hierarchy with the passphrase the secret stretches
// into.
func newAccount(t *testing.T, secret string, twoPassword bool) *account {
	t.Helper()
	raw := []byte("0123456789abcdef")
	keySalt := base64.StdEncoding.EncodeToString(raw)
	stretched, err := stretch(secret, keySalt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	a := &account{
		secret:      secret,
		keySalt:     keySalt,
		twoPassword: twoPassword,
		salts:       []salt{{ID: "user-key", KeySalt: keySalt}},
		userKey:     lockedKey(t, "user", stretched),
		addrKey:     lockedKey(t, "address", stretched),
	}
	return a
}

func lockedKey(t *testing.T, name, passphrase string) string {
	t.Helper()
	key, err := pgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("generate %s key: %v", name, err)
	}
	locked, err := key.Lock([]byte(passphrase))
	if err != nil {
		t.Fatalf("lock %s key: %v", name, err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatalf("armor %s key: %v", name, err)
	}
	return armored
}

// serve answers the four requests a first unlock makes, and the two a seal does.
func (a *account) serve(t *testing.T) *proton.Client {
	t.Helper()
	return client(t, func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{"Code": 1000}
		switch r.URL.Path {
		case "/core/v4/users":
			body["User"] = map[string]any{
				"ID": "user", "Name": "test",
				"Keys": []map[string]any{{"ID": "user-key", "PrivateKey": a.userKey, "Primary": 1, "Active": 1}},
			}
		case "/core/v4/addresses":
			body["Addresses"] = []map[string]any{{
				"ID": "address", "Email": "me@proton.me", "Status": 1, "Send": 1, "Receive": 1,
				"Keys": []map[string]any{{
					"ID": "address-key", "PrivateKey": a.addrKey, "Primary": 1, "Active": 1,
					"Token": a.addrToken, "Signature": a.addrToken,
				}},
			}}
		case "/core/v4/keys/salts":
			body["KeySalts"] = a.salts
		case "/core/v4/settings":
			mode := 1
			if a.twoPassword {
				mode = PasswordModeTwo
			}
			body["UserSettings"] = map[string]any{"Password": map[string]any{"Mode": mode}}
		case "/auth/v4/sessions/local/key":
			if r.Method == http.MethodGet && a.clientKey != nil {
				body["ClientKey"] = base64.StdEncoding.EncodeToString(a.clientKey)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	})
}

// client points a session at a server that answers for Proton.
func client(t *testing.T, handler http.HandlerFunc) *proton.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c := proton.New(proton.Options{BaseURL: srv.URL, Logger: slog.New(slog.DiscardHandler)})
	c.SetTokens("uid", "access", "refresh")
	return c
}

// asked records what the unlock asked for, which is the thing under test.
type asked struct {
	twoPassword bool
	times       int
}

func (a *asked) answer(secret string) KeyPassword {
	return func(twoPassword bool) (string, error) {
		a.twoPassword, a.times = twoPassword, a.times+1
		return secret, nil
	}
}

func TestUnlockAsksForTheSecondPasswordInTwoPasswordMode(t *testing.T) {
	acct := newAccount(t, "the second password", true)
	var got asked
	if _, err := Unlock(context.Background(), acct.serve(t), got.answer(acct.secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if !got.twoPassword {
		t.Error("the unlock asked for the password, want the second password")
	}
	if got.times != 1 {
		t.Errorf("asked %d times, want once", got.times)
	}
}

func TestUnlockAsksForThePasswordInOnePasswordMode(t *testing.T) {
	acct := newAccount(t, "the password", false)
	var got asked
	if _, err := Unlock(context.Background(), acct.serve(t), got.answer(acct.secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.twoPassword {
		t.Error("the unlock asked for the second password, want the password")
	}
}

// A secret that does not open the keys says which secret it was, because in
// two-password mode the other one has just been proved over SRP.
func TestUnlockSaysWhichSecretDidNotOpenTheKeys(t *testing.T) {
	for _, tc := range []struct {
		name        string
		twoPassword bool
		want        string
	}{
		{"two-password mode blames the second password", true, "Incorrect second password."},
		{"one-password mode blames the password", false, "Your password did not unlock your keys."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			acct := newAccount(t, "the right one", tc.twoPassword)
			var got asked
			_, err := Unlock(context.Background(), acct.serve(t), got.answer("the wrong one"))
			if err == nil {
				t.Fatal("Unlock with the wrong secret succeeded")
			}
			if err.Error() != tc.want {
				t.Errorf("error = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

// Proton salts every key of its own and locks the hierarchy with the primary
// key's salt, so an account with more than one key is where taking the first
// salt that arrives stops working.
func TestUnlockStretchesTheSecretWithThePrimaryKeysSalt(t *testing.T) {
	acct := newAccount(t, "the password", false)
	other := base64.StdEncoding.EncodeToString([]byte("fedcba9876543210"))
	acct.salts = []salt{{ID: "retired-key", KeySalt: other}, {ID: "user-key", KeySalt: acct.keySalt}}

	var got asked
	if _, err := Unlock(context.Background(), acct.serve(t), got.answer(acct.secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

// A key from an auth version that predates salts is locked with the secret
// itself, which is what Proton's own clients fall back to.
func TestUnlockUsesTheSecretItselfWhenTheKeyHasNoSalt(t *testing.T) {
	secret := "the password"
	acct := &account{
		secret:  secret,
		userKey: lockedKey(t, "user", secret),
		addrKey: lockedKey(t, "address", secret),
	}
	var got asked
	if _, err := Unlock(context.Background(), acct.serve(t), got.answer(secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
}

// The sealed key password is a cache of something derivable, so a seal that no
// longer opens the keys - what a password changed elsewhere leaves behind - is a
// miss rather than a dead profile.
func TestUnlockDerivesAgainWhenTheSealedKeyPasswordIsStale(t *testing.T) {
	acct := newAccount(t, "the password", false)
	acct.clientKey = make([]byte, 32)
	c := acct.serve(t)

	stale, err := localkey.Wrap("no longer the key password", acct.clientKey)
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	c.SetEncKeyBlob(stale)

	var got asked
	if _, err := Unlock(context.Background(), c, got.answer(acct.secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.times != 1 {
		t.Errorf("asked %d times, want the stale seal replaced by asking once", got.times)
	}
}

// Signing in over a session that no longer works leaves the old session's seal
// behind, and Proton holds no client key for the new one. Deriving is what makes
// that sign-in the recovery it is advertised as.
func TestUnlockDerivesWhenTheSessionHoldsNoClientKey(t *testing.T) {
	acct := newAccount(t, "the password", false)
	c := acct.serve(t)

	orphaned, err := localkey.Wrap("whatever the old session sealed", make([]byte, 32))
	if err != nil {
		t.Fatalf("wrap: %v", err)
	}
	c.SetEncKeyBlob(orphaned)

	var got asked
	if _, err := Unlock(context.Background(), c, got.answer(acct.secret)); err != nil {
		t.Fatalf("Unlock: %v", err)
	}
	if got.times != 1 {
		t.Errorf("asked %d times, want the orphaned seal replaced by asking once", got.times)
	}
}

// Stretching is Proton's own bcrypt, and getting it wrong would open nothing at
// all - so it is pinned against go-srp rather than against a recorded string.
func TestStretchIsProtonsKeyPassword(t *testing.T) {
	raw := []byte("0123456789abcdef")
	got, err := stretch("the password", base64.StdEncoding.EncodeToString(raw))
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	want, err := srp.MailboxPassword([]byte("the password"), raw)
	if err != nil {
		t.Fatalf("MailboxPassword: %v", err)
	}
	if got != string(want[len(want)-31:]) {
		t.Errorf("stretch = %q, want the last 31 bytes of Proton's own derivation", got)
	}
}

// ── the failure nobody at a terminal can cause ──

// The user keys open and not one address key follows. The four causes are told
// apart only by what the unlocking loop writes down, so a run that says nothing
// about which one it was cannot be acted on from a report.
//
// This is the shape of it that is actually diagnosable - an address key whose
// token this account's user key cannot decrypt - and what the run has to say
// about it afterwards.
func unopenableAccount(t *testing.T, secret string, twoPassword bool) *account {
	t.Helper()
	a := newAccount(t, secret, twoPassword)
	// A token and a signature the user key has nothing to do with, which is what
	// a key hierarchy in trouble looks like from here.
	a.addrToken = lockedKey(t, "someone-else", "another passphrase")
	return a
}

func TestUnlockSaysNoneOfTheAddressKeysOpenedAndWhy(t *testing.T) {
	acct := unopenableAccount(t, "the password", false)
	var got asked

	ctx, tally := skip.With(context.Background())
	var records bytes.Buffer
	restore := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(restore) })

	_, err := Unlock(ctx, acct.serve(t), got.answer(acct.secret))
	if err == nil {
		t.Fatal("the unlock succeeded with no openable address key")
	}

	// The screen says what happened rather than leaving Go's words on it.
	if !strings.Contains(err.Error(), "None of your addresses' keys could be opened.") {
		t.Errorf("the failure is not phrased for a person: %v", err)
	}
	// And it is this CLI's fault, not the caller's - the code a script reads, and
	// what makes the screen ask for the report that carries the log.
	var coder errs.ExitCoder
	if !errors.As(err, &coder) {
		t.Fatalf("the failure carries no exit code: %v", err)
	}
	if coder.ExitCode() != errs.ExitBug {
		t.Errorf("the failure exits %d, want %d", coder.ExitCode(), errs.ExitBug)
	}

	// The log says which of the four causes it was, per key and per address.
	log := records.String()
	for _, want := range []string{
		`msg="not shown" kind=key reason="token not decryptable by user key"`,
		`msg="not shown" kind=address reason="token not decryptable by user key"`,
		"addresses=1", "addresses_active=1", "user_keys=1", "address_keys=1", "opened=0",
		"two_password=false",
	} {
		if !strings.Contains(log, want) {
			t.Errorf("the log does not say %q:\n%s", want, log)
		}
	}
	if tally.Count() == 0 {
		t.Error("nothing was counted as unshowable")
	}
}

// The one fact the original report turned on. Somebody saying "I used 2FA"
// usually means this, and it changes the advice, so the run has to record it
// whichever way it got here - including from a session that already held a
// usable key password, which is the path that never asks.
func TestUnlockRecordsWhetherTheAccountKeepsTwoPasswords(t *testing.T) {
	for _, twoPassword := range []bool{true, false} {
		acct := unopenableAccount(t, "the second password", twoPassword)
		var got asked
		var records bytes.Buffer
		restore := slog.Default()
		slog.SetDefault(slog.New(slog.NewTextHandler(&records, &slog.HandlerOptions{Level: slog.LevelDebug})))

		_, err := Unlock(context.Background(), acct.serve(t), got.answer(acct.secret))
		slog.SetDefault(restore)

		if err == nil {
			t.Fatal("the unlock succeeded with no openable address key")
		}
		want := fmt.Sprintf("two_password=%v", twoPassword)
		if !strings.Contains(records.String(), want) {
			t.Errorf("the log does not say %q:\n%s", want, records.String())
		}
		advice := strings.Contains(err.Error(), "second password") ||
			hasHint(err, "second password")
		if advice != twoPassword {
			t.Errorf("two-password %v: the advice about the second password is %v", twoPassword, advice)
		}
	}
}

func hasHint(err error, substr string) bool {
	var hinter errs.Hinter
	if !errors.As(err, &hinter) {
		return false
	}
	for _, h := range hinter.Hints() {
		if strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

// ── post-quantum keys ──

// An account that turned on post-quantum encryption holds a v6 ML-DSA-65
// primary with an ML-KEM-768 subkey. Reading one depends on the -proton tags of
// go-crypto and gopenpgp, which carry the algorithms the upstream releases do
// not - and which sort *below* the plain tags in semver, so an ordinary
// dependency update drops them without a word. These two tests are what notices.

func TestUnlockOpensAPostQuantumAccount(t *testing.T) {
	acct := newAccount(t, "the password", false)
	stretched, err := stretch(acct.secret, acct.keySalt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	acct.userKey = postQuantumKey(t, "postquantum-private.asc", stretched)
	acct.addrKey = postQuantumKey(t, "postquantum-private.asc", stretched)

	var got asked
	u, err := Unlock(context.Background(), acct.serve(t), got.answer(acct.secret))
	if err != nil {
		t.Fatalf("a post-quantum account did not unlock: %v", err)
	}
	if u.UserKR.CountEntities() == 0 {
		t.Error("the user key ring is empty")
	}
	if _, ok := u.AddrKRs["address"]; !ok {
		t.Error("the address key did not open")
	}
}

// The other half of it is somebody else's key: a share, an invitation and a
// sent message all go to the key Proton publishes for them.
func TestPublishedReadsAPostQuantumRecipient(t *testing.T) {
	kr, err := Published(context.Background(), publishing(t, publishedKey(t, "postquantum-public.asc")), "them@proton.me")
	if err != nil {
		t.Fatalf("a post-quantum recipient's key did not come back: %v", err)
	}
	if kr == nil || kr.CountEntities() == 0 {
		t.Fatal("the recipient came back with no key ring")
	}
}

// ── what a published key can be ──

// An address Proton publishes no key for is one outside Proton, and the caller
// phrases that itself. An address whose key will not parse is a real Proton
// address, and calling it the first thing is a false statement about the one
// thing the sender cannot work around.

// Asking with InternalOnly makes Proton refuse an address outside Proton rather
// than answer with an empty list, so both shapes have to mean the same thing.
// Getting this wrong is how a calendar invitation to somebody's work address
// stops going out.
func TestPublishedReturnsNothingForAnAddressOutsideProton(t *testing.T) {
	for name, c := range map[string]*proton.Client{
		"an empty key list": publishing(t),
		"Proton's refusal":  refusing(t),
	} {
		t.Run(name, func(t *testing.T) {
			kr, err := Published(context.Background(), c, "them@example.com")
			if err != nil {
				t.Fatalf("Published: %v", err)
			}
			if kr != nil {
				t.Errorf("an address Proton holds no key for came back with a ring: %v", kr)
			}
		})
	}
}

// refusing answers the way Proton does for an address it does not hold.
func refusing(t *testing.T) *proton.Client {
	t.Helper()
	return client(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Code": 33103, "Error": "This address does not exist. Please try again",
		})
	})
}

func TestPublishedNamesAKeyItCannotRead(t *testing.T) {
	kr, err := Published(context.Background(), publishing(t, "-----BEGIN PGP PUBLIC KEY BLOCK-----\n\nnope\n-----END PGP PUBLIC KEY BLOCK-----\n"), "them@proton.me")
	if err == nil {
		t.Fatalf("an unreadable key came back as a ring: %v", kr)
	}
	if want := "Proton publishes a key for them@proton.me that this build cannot read."; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
	var coder errs.ExitCoder
	if !errors.As(err, &coder) || coder.ExitCode() != errs.ExitBug {
		t.Errorf("the failure does not exit %d: %v", errs.ExitBug, err)
	}
}

// publishing answers the key lookup with the primary keys given, or with an
// address Proton holds none for when given nothing.
func publishing(t *testing.T, armored ...string) *proton.Client {
	t.Helper()
	keys := make([]map[string]any, 0, len(armored))
	for _, a := range armored {
		keys = append(keys, map[string]any{"PublicKey": a, "Primary": 1})
	}
	return client(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"Code":    1000,
			"Address": map[string]any{"Keys": keys},
		})
	})
}

// publishedKey is a sample key of the kind Proton generates on opt-in, as it is
// published.
func publishedKey(t *testing.T, name string) string {
	t.Helper()
	armored, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(armored)
}

// postQuantumKey is the same key locked with the passphrase the account's secret
// stretches into, which is the shape Proton hands one back in.
func postQuantumKey(t *testing.T, name, passphrase string) string {
	t.Helper()
	key, err := pgp.NewKeyFromArmored(publishedKey(t, name))
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	locked, err := key.Lock([]byte(passphrase))
	if err != nil {
		t.Fatalf("lock %s: %v", name, err)
	}
	armored, err := locked.Armor()
	if err != nil {
		t.Fatalf("armor %s: %v", name, err)
	}
	return armored
}
