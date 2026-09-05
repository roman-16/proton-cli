package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
	"google.golang.org/protobuf/proto"
)

func vaultKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func storedVault(t *testing.T, key []byte, v *pb.Vault) string {
	t.Helper()
	raw, err := proto.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	ct, err := aead.Encrypt(key, raw, []byte(aead.TagVaultContent))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(ct)
}

// An edit replaces the whole stored vault, so everything it did not name has to
// survive. Rebuilding from an empty vault is how a rename loses a description.
func TestPatchedVaultKeepsEverythingElse(t *testing.T) {
	key := vaultKey()
	content := storedVault(t, key, &pb.Vault{
		Name:        "Personal",
		Description: "cards and logins",
		Display:     &pb.VaultDisplayPreferences{Icon: 3, Color: 5},
	})

	name := "Private"
	out, err := patchedVault(content, key, VaultPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	var got pb.Vault
	if err := proto.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Private" {
		t.Errorf("name = %q", got.Name)
	}
	if got.Description != "cards and logins" {
		t.Errorf("the rename dropped the description: %q", got.Description)
	}
	if got.Display == nil || got.Display.Icon != 3 || got.Display.Color != 5 {
		t.Errorf("the rename dropped the display settings: %+v", got.Display)
	}
}

func TestPatchedVaultRefusesContentItCannotRead(t *testing.T) {
	key := vaultKey()
	other := make([]byte, 32)
	name := "Private"
	patch := VaultPatch{Name: &name}

	if _, err := patchedVault("", key, patch); err == nil {
		t.Error("a vault with no stored content was renamed anyway")
	}
	content := storedVault(t, key, &pb.Vault{Name: "Personal", Description: "keep me"})
	if _, err := patchedVault(content, other, patch); err == nil {
		t.Error("a vault whose content could not be decrypted was renamed anyway")
	}
	if _, err := patchedVault("not base64!!", key, patch); err == nil {
		t.Error("content that is not base64 was renamed anyway")
	}
}

// A patch changes only what it names: an icon set on a vault leaves its name and
// description exactly as they were.
func TestPatchedVaultChangesOnlyWhatItNames(t *testing.T) {
	key := vaultKey()
	content := storedVault(t, key, &pb.Vault{
		Name: "Personal", Description: "cards and logins",
		Display: &pb.VaultDisplayPreferences{Icon: 3, Color: 5},
	})

	icon := 7
	out, err := patchedVault(content, key, VaultPatch{Icon: &icon})
	if err != nil {
		t.Fatal(err)
	}
	var got pb.Vault
	if err := proto.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got.Name != "Personal" || got.Description != "cards and logins" {
		t.Errorf("setting an icon changed something else: %+v", &got)
	}
	if got.Display.Icon != pb.VaultIcon(DisplayValue(7)) {
		t.Errorf("icon = %v, want the enum for 7", got.Display.Icon)
	}
	if got.Display.Color != 5 {
		t.Errorf("setting an icon dropped the colour: %v", got.Display.Color)
	}
}

// The numbers a person writes and the enum Pass stores are offset, and the two
// have to agree in both directions or a vault reads back as a different colour
// than it was set to.
func TestDisplayNumbersRoundTrip(t *testing.T) {
	for n := 1; n <= 30; n++ {
		if got := DisplayNumber(DisplayValue(n)); got != n {
			t.Errorf("%d round-tripped as %d", n, got)
		}
	}
	// A vault that never chose reads as nothing chosen, not as number one.
	if got := DisplayNumber(0); got != 0 {
		t.Errorf("an unset display read as %d, want 0", got)
	}
	if got := DisplayNumber(1); got != 0 {
		t.Errorf("a custom display read as %d, want 0", got)
	}
}

// The rotation sent has to name the key the content was encrypted with, so the
// newest key is what a write uses.
func TestShareKeysLatestPicksTheHighestRotation(t *testing.T) {
	sk := &shareKeys{keys: map[int][]byte{1: {1}, 3: {3}, 2: {2}}}
	key, rotation := sk.latest()
	if rotation != 3 || len(key) != 1 || key[0] != 3 {
		t.Errorf("latest = %v, %d; want the key for rotation 3", key, rotation)
	}
	empty := &shareKeys{keys: map[int][]byte{}}
	if key, rotation := empty.latest(); key != nil || rotation != -1 {
		t.Errorf("latest on no keys = %v, %d", key, rotation)
	}
}

// A new vault's key is sealed to the primary user key and to nothing else.
//
// Every key the account ever had has to open what it sealed, so reading uses the
// whole ring - but sealing something new under all of them puts it under keys
// the owner has retired, and is not what Proton's own client sends.
func TestVaultCreateSealsTheKeyToThePrimaryUserKeyAlone(t *testing.T) {
	users, err := pgp.NewKeyRing(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"primary", "retired"} {
		key, err := pgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
		if err != nil {
			t.Fatalf("GenerateKey: %v", err)
		}
		if err := users.AddKey(key); err != nil {
			t.Fatalf("AddKey: %v", err)
		}
	}
	u := &keys.Unlocked{
		UserKR:    users,
		Addresses: []keys.Address{{ID: "addr", Email: "me@proton.me"}},
		AddrKRs:   map[string]*pgp.KeyRing{"addr": ring(t, "me")},
	}
	d := &capturingDoer{}
	if _, err := New(d, testKeys(u)).VaultCreate(context.Background(), "Work"); err != nil {
		t.Fatalf("VaultCreate: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(d.body["EncryptedVaultKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	msg := pgp.NewPGPMessage(raw)
	if ids, ok := msg.GetHexEncryptionKeyIDs(); !ok || len(ids) != 1 {
		t.Fatalf("the vault key is sealed to %v, want exactly one key", ids)
	}
	one := func(i int) *pgp.KeyRing {
		kr, err := pgp.NewKeyRing(users.GetKeys()[i])
		if err != nil {
			t.Fatal(err)
		}
		return kr
	}
	if _, err := one(0).Decrypt(msg, nil, pgp.GetUnixTime()); err != nil {
		t.Errorf("the primary user key cannot open the vault key it should have sealed: %v", err)
	}
	if _, err := one(1).Decrypt(msg, nil, pgp.GetUnixTime()); err == nil {
		t.Error("the vault key was sealed to a key that is no longer primary")
	}
}

// capturingDoer answers every request with success and keeps the last body.
type capturingDoer struct{ body map[string]any }

func (d *capturingDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *capturingDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.body, _ = r.Body.(map[string]any)
	if out != nil {
		return json.Unmarshal([]byte(`{"Share":{"ShareID":"share"}}`), out)
	}
	return nil
}
