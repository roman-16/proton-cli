package keys

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/crypto/bip39"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Bringing keys back is the one thing this package does that no live test can
// reach: only a password reset leaves a key locked, and no test account may be
// reset. So what goes up is pinned here, key material and all - a locked
// hierarchy is built, opened with the secret, and the request that results is
// taken apart again.

// reset is an account a password reset has been through: the keys it has now,
// and the ones it had before, which Proton still holds and nothing opens.
type reset struct {
	// previous is what a person would have typed before the reset.
	previous string
	// keyPass is the passphrase the account's current keys opened with, which is
	// what a reactivated key has to come back locked under.
	keyPass string
	// oldSalt is the salt Proton holds for the locked account key.
	oldSalt string

	// oldUser is the account key the reset locked, and oldAddr the address key
	// whose token is sealed to it.
	oldUser, oldAddr *pgp.Key

	unlocked *Unlocked
	api      *reactivationAPI
}

// reactivationAPI answers the requests a reactivation makes and keeps what it
// was given.
type reactivationAPI struct {
	salts     []salt
	mnemonic  []mnemonicKey
	puts      map[string]map[string]any
	requested []string
}

func (a *reactivationAPI) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *reactivationAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requested = append(a.requested, req.Method+" "+req.Path)
	switch {
	case req.Path == "/core/v4/keys/salts":
		return reply(map[string]any{"KeySalts": a.salts}, out)
	case req.Path == "/core/v4/settings/mnemonic":
		return reply(map[string]any{"MnemonicUserKeys": a.mnemonic}, out)
	case strings.HasPrefix(req.Path, "/core/v4/keys/user/"):
		body, _ := req.Body.(map[string]any)
		if a.puts == nil {
			a.puts = map[string]map[string]any{}
		}
		a.puts[strings.TrimPrefix(req.Path, "/core/v4/keys/user/")] = body
		return nil
	}
	return nil
}

func reply(body map[string]any, out any) error {
	body["Code"] = 1000
	answer, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return json.Unmarshal(answer, out)
}

// afterReset builds an account whose current keys open and whose previous ones
// do not: an account key locked with the previous password's stretched form, and
// an address key locked with a token sealed to it.
func afterReset(t *testing.T) *reset {
	t.Helper()
	const previous, keyPass = "the password from before", "the key password now"
	oldSalt := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef"))
	stretched, err := stretch(previous, oldSalt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}

	oldUser := generated(t, "old-user")
	oldUserKR, err := pgp.NewKeyRing(oldUser)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	token, err := newAddressKeyToken(oldUserKR)
	if err != nil {
		t.Fatalf("newAddressKeyToken: %v", err)
	}
	oldAddr := generated(t, "old-address")

	current := generated(t, "current-address")
	currentKR, err := pgp.NewKeyRing(current)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	userKR, err := pgp.NewKeyRing(generated(t, "current-user"))
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}

	live := Key{ID: "current-address-key", PrivateKey: armoredOf(t, current), Primary: 1, Active: 1, Flags: 3}
	lockedAddr := Key{
		ID: "old-address-key", PrivateKey: locked(t, oldAddr, token.passphrase),
		Token: token.sealed, Signature: token.signature, Flags: 3,
	}
	addr := Address{
		ID: "address", Email: "me@proton.me", Status: 1,
		Keys:          []Key{live, lockedAddr},
		SignedKeyList: &SignedKeyList{Data: listNaming(t, current)},
	}
	lockedUser := Key{ID: "old-user-key", PrivateKey: locked(t, oldUser, stretched)}

	u := &Unlocked{
		UserKR:    userKR,
		AddrKRs:   map[string]Rings{addr.ID: {Read: currentKR, Write: currentKR}},
		Addresses: []Address{addr},
		UserKeys:  []Key{{ID: "current-user-key", Primary: 1, Active: 1}, lockedUser},
		keyPass:   []byte(keyPass),
		lockedID:  map[uint64]bool{},
	}
	u.noteLocked(context.Background(), LockedKey{Key: lockedUser})
	u.noteLocked(context.Background(), LockedKey{Key: lockedAddr, AddressID: addr.ID, Email: addr.Email})

	return &reset{
		previous: previous, keyPass: keyPass, oldSalt: oldSalt,
		oldUser: oldUser, oldAddr: oldAddr, unlocked: u,
		api: &reactivationAPI{salts: []salt{{ID: lockedUser.ID, KeySalt: oldSalt}}},
	}
}

// listNaming is a published key list that names exactly the keys given, which is
// what Proton serves for an address whose list is current.
func listNaming(t *testing.T, keys ...*pgp.Key) string {
	t.Helper()
	items := make([]keyListEntry, 0, len(keys))
	for i, key := range keys {
		primary := 0
		if i == 0 {
			primary = 1
		}
		items = append(items, keyListEntry{
			Primary: primary, Flags: mailKeyFlags,
			Fingerprint: key.GetFingerprint(), SHA256Fingerprints: key.GetSHA256Fingerprints(),
		})
	}
	data, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("compose a key list: %v", err)
	}
	return string(data)
}

// The whole of what a reactivation sends: the account key back under the
// password the account has now, the address key it opens named by fingerprint,
// and the address's list naming it as a key that is sealed to nothing new.
func TestReactivateSendsTheKeyBackUnderTheCurrentPassword(t *testing.T) {
	acct := afterReset(t)

	out, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword(acct.previous))
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if len(out.Reactivated) != 2 || len(out.StillLocked) != 0 || len(out.Unsupported) != 0 {
		t.Fatalf("reactivated %d, left %d locked and %d unsupported; want the account key and its address key",
			len(out.Reactivated), len(out.StillLocked), len(out.Unsupported))
	}

	put := acct.api.puts["old-user-key"]
	if put == nil {
		t.Fatalf("requests were %v; want a PUT for the locked account key", acct.api.requested)
	}
	armored, _ := put["PrivateKey"].(string)
	back, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the key sent back is not readable armour: %v", err)
	}
	if _, err := back.Unlock([]byte(acct.keyPass)); err != nil {
		t.Errorf("the key sent back does not open with the account's current password: %v", err)
	}
	if back.GetFingerprint() != acct.oldUser.GetFingerprint() {
		t.Error("the key sent back is not the key that was locked")
	}

	fingerprints, _ := put["AddressKeyFingerprints"].([]string)
	if len(fingerprints) != 1 || fingerprints[0] != acct.oldAddr.GetFingerprint() {
		t.Errorf("AddressKeyFingerprints = %v, want the locked address key's", fingerprints)
	}
}

// The address's published list gains the key coming back and loses nothing.
func TestReactivateNamesTheKeyInTheAddressList(t *testing.T) {
	acct := afterReset(t)
	if _, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword(acct.previous)); err != nil {
		t.Fatalf("Reactivate: %v", err)
	}

	lists, _ := acct.api.puts["old-user-key"]["SignedKeyLists"].(map[string]map[string]string)
	list, ok := lists["address"]
	if !ok {
		t.Fatalf("SignedKeyLists = %v, want one for the address", lists)
	}
	var items []keyListEntry
	if err := json.Unmarshal([]byte(list["Data"]), &items); err != nil {
		t.Fatalf("the published list is not readable: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("the list names %d keys, want the live one and the one coming back", len(items))
	}
	if items[0].Primary != 1 || items[0].Flags != mailKeyFlags {
		t.Errorf("the live key's entry changed: %+v", items[0])
	}
	back := items[1]
	if back.Fingerprint != acct.oldAddr.GetFingerprint() {
		t.Errorf("the added entry names %s, want the reactivated key", back.Fingerprint)
	}
	if back.Primary != 0 {
		t.Error("a reactivated key is named primary")
	}
	if back.Flags&keyNotObsolete != 0 {
		t.Errorf("flags = %d, want the obsolete bit set so nothing new is sealed to it", back.Flags)
	}
	assertKeyListSignature(t, keyOf(t, acct.unlocked, "address"), list["Data"], list["Signature"])
}

func keyOf(t *testing.T, u *Unlocked, addressID string) *pgp.Key {
	t.Helper()
	rings, ok := u.AddrRings(addressID)
	if !ok {
		t.Fatalf("the address %s did not open", addressID)
	}
	keys := rings.Write.GetKeys()
	if len(keys) == 0 {
		t.Fatalf("the address %s writes with nothing", addressID)
	}
	return keys[0]
}

// A secret that opens nothing is the common mistake, and it says so rather than
// reporting a reactivation of none.
func TestReactivateRefusesASecretThatOpensNothing(t *testing.T) {
	acct := afterReset(t)
	_, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword("some other password"))
	if err == nil {
		t.Fatal("a password that opens nothing was accepted")
	}
	if !strings.Contains(err.Error(), "did not open any of the locked keys") {
		t.Errorf("err = %v, want it to say the password opened nothing", err)
	}
	if acct.api.puts != nil {
		t.Error("a key went up after nothing opened")
	}
}

// An account with nothing locked has nothing to do, and address keys alone
// cannot be brought back without the account key that holds them.
func TestReactivateRefusesWhatItCannotOpen(t *testing.T) {
	acct := afterReset(t)
	acct.unlocked.UserKeys = []Key{{ID: "current-user-key", Primary: 1, Active: 1}}
	acct.unlocked.Addresses[0].Keys = acct.unlocked.Addresses[0].Keys[:1]
	if _, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword(acct.previous)); !errors.Is(err, ErrNoAccountKeyLocked) {
		t.Errorf("with nothing locked, err = %v, want ErrNoAccountKeyLocked", err)
	}

	acct = afterReset(t)
	acct.unlocked.UserKeys = []Key{{ID: "current-user-key", Primary: 1, Active: 1}}
	if _, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword(acct.previous)); !errors.Is(err, ErrNoAccountKeyLocked) {
		t.Errorf("with only an address key locked, err = %v, want ErrNoAccountKeyLocked", err)
	}
}

// The recovery phrase opens the copy Proton keeps for it, and what goes up is
// the same key under the same current password.
func TestReactivateWithARecoveryPhrase(t *testing.T) {
	const phrase = "legal winner thank year wave sausage worth useful legal winner thank yellow"
	acct := afterReset(t)
	acct.unlocked.recoveryPhrase = true

	entropy, err := bip39.Entropy(phrase)
	if err != nil {
		t.Fatalf("Entropy: %v", err)
	}
	mnemonicSalt := base64.StdEncoding.EncodeToString([]byte("fedcba9876543210"))
	stretched, err := stretch(base64.StdEncoding.EncodeToString(entropy), mnemonicSalt)
	if err != nil {
		t.Fatalf("stretch: %v", err)
	}
	acct.api.mnemonic = []mnemonicKey{{
		ID: "old-user-key", Salt: mnemonicSalt, PrivateKey: locked(t, acct.oldUser, stretched),
	}}

	secret, err := Phrase(phrase)
	if err != nil {
		t.Fatalf("Phrase: %v", err)
	}
	out, err := acct.unlocked.Reactivate(context.Background(), acct.api, secret)
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if len(out.Reactivated) != 2 {
		t.Fatalf("reactivated %d keys, want the account key and its address key", len(out.Reactivated))
	}
	armored, _ := acct.api.puts["old-user-key"]["PrivateKey"].(string)
	back, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("the key sent back is not readable armour: %v", err)
	}
	if _, err := back.Unlock([]byte(acct.keyPass)); err != nil {
		t.Errorf("the key sent back does not open with the account's current password: %v", err)
	}
}

// A phrase is judged before Proton is asked anything, and each way of getting it
// wrong is told apart: the count, a word, and the checksum.
func TestPhraseRefusesWhatIsNotOne(t *testing.T) {
	for _, tc := range []struct{ phrase, want string }{
		{"zoo zoo zoo", "twelve words"},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo proton", "not one a recovery phrase can hold"},
		{"zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo zoo", "one of its words is wrong"},
	} {
		_, err := Phrase(tc.phrase)
		if err == nil {
			t.Errorf("%q was accepted as a recovery phrase", tc.phrase)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: err = %v, want it to say %q", tc.phrase, err, tc.want)
		}
	}
}

// The recovery file is opened with the secret Proton keeps beside the keys, and
// the keys inside it are matched to the locked records by fingerprint.
func TestReactivateWithARecoveryFile(t *testing.T) {
	acct := afterReset(t)
	const secret = "cmVjb3Zlcnktc2VjcmV0LXRoaXJ0eS10d28tYnl0ZXM="
	withSecret(acct.unlocked, secret)

	binary, err := acct.oldUser.Serialize()
	if err != nil {
		t.Fatalf("serialize the key that was locked: %v", err)
	}
	message, err := pgp.EncryptMessageWithPassword(pgp.NewPlainMessage(binary), []byte(secret))
	if err != nil {
		t.Fatalf("make a recovery file: %v", err)
	}
	file, err := message.GetArmored()
	if err != nil {
		t.Fatalf("armor the recovery file: %v", err)
	}

	out, err := acct.unlocked.Reactivate(context.Background(), acct.api, RecoveryFile(file))
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if len(out.Reactivated) != 2 {
		t.Fatalf("reactivated %d keys, want the account key and its address key", len(out.Reactivated))
	}

	other := afterReset(t)
	withSecret(other.unlocked, "another account's secret")
	if _, err := other.unlocked.Reactivate(context.Background(), other.api, RecoveryFile(file)); err == nil ||
		!strings.Contains(err.Error(), "not made for this account") {
		t.Errorf("err = %v, want it to say the file is another account's", err)
	}

	none := afterReset(t)
	if _, err := none.unlocked.Reactivate(context.Background(), none.api, RecoveryFile(file)); err == nil ||
		!strings.Contains(err.Error(), "no recovery file to recover with") {
		t.Errorf("err = %v, want it to say the account has no recovery file", err)
	}
}

// withSecret gives the account's live key the recovery secret a recovery file
// was encrypted under, which is where Proton keeps it.
func withSecret(u *Unlocked, secret string) {
	for i, k := range u.UserKeys {
		if k.Active != 0 {
			u.UserKeys[i].RecoverySecret = secret
		}
	}
}

// An address key whose passphrase is not a token is one only a Proton client
// brings back, and it is named rather than dropped.
func TestReactivateNamesAKeyItCannotBringBack(t *testing.T) {
	acct := afterReset(t)
	legacy := Key{ID: "legacy-address-key", PrivateKey: locked(t, generated(t, "legacy"), "an old password")}
	acct.unlocked.Addresses[0].Keys = append(acct.unlocked.Addresses[0].Keys, legacy)
	acct.unlocked.noteLocked(context.Background(),
		LockedKey{Key: legacy, AddressID: "address", Email: "me@proton.me"})

	out, err := acct.unlocked.Reactivate(context.Background(), acct.api, PreviousPassword(acct.previous))
	if err != nil {
		t.Fatalf("Reactivate: %v", err)
	}
	if len(out.Unsupported) != 1 || out.Unsupported[0].Key.ID != legacy.ID {
		t.Errorf("unsupported = %v, want the key with no token", out.Unsupported)
	}
	fingerprints, _ := acct.api.puts["old-user-key"]["AddressKeyFingerprints"].([]string)
	if len(fingerprints) != 1 {
		t.Errorf("AddressKeyFingerprints = %v, want only the key that opened", fingerprints)
	}
}
