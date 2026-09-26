package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/errs"
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

	icon := "wallet"
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
	if got.Display.Icon != pb.VaultIcon(IconValue("wallet")) {
		t.Errorf("icon = %v, want the enum for wallet", got.Display.Icon)
	}
	if got.Display.Color != 5 {
		t.Errorf("setting an icon dropped the colour: %v", got.Display.Color)
	}
}

// The names a person writes and the enum Pass stores have to agree in both
// directions, or a vault reads back as a different colour than it was set to.
func TestDisplayNamesRoundTrip(t *testing.T) {
	for _, name := range VaultIcons() {
		if got := IconName(IconValue(name)); got != name {
			t.Errorf("icon %q round-tripped as %q", name, got)
		}
	}
	for _, name := range VaultColors() {
		if got := ColorName(ColorValue(name)); got != name {
			t.Errorf("colour %q round-tripped as %q", name, got)
		}
	}
	// A vault that never chose reads as nothing chosen, not as the first swatch.
	if got := IconName(0); got != "" {
		t.Errorf("an unset icon read as %q, want nothing", got)
	}
	if got := ColorName(1); got != "" {
		t.Errorf("a custom colour read as %q, want nothing", got)
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
		AddrKRs:   map[string]keys.Rings{"addr": {Read: ring(t, "me"), Write: ring(t, "me")}},
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

// shareDoer serves a list of vault shares and nothing else, and records every
// request, from whichever goroutine sent it.
type shareDoer struct {
	shares string
	mu     sync.Mutex
	sent   []proton.Request
}

func (d *shareDoer) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	d.record(r)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *shareDoer) Decode(_ context.Context, r proton.Request, out any) error {
	d.record(r)
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/share":
		return json.Unmarshal([]byte(d.shares), out)
	case r.Method == "PUT":
		return nil
	}
	return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
}

func (d *shareDoer) record(r proton.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.sent = append(d.sent, r)
}

func (d *shareDoer) asked(path string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.sent {
		if r.Path == path {
			return true
		}
	}
	return false
}

const archiveHiddenWorkVisible = `{"Shares":[
	{"ShareID":"archive","VaultID":"v1","TargetType":1,"Owner":true,"ShareRoleID":"1","Flags":1},
	{"ShareID":"work","VaultID":"v2","TargetType":1,"Owner":true,"ShareRoleID":"1","Flags":0}
]}`

func hiddenVaultService(shares string) (*Service, *shareDoer) {
	d := &shareDoer{shares: shares}
	return New(d, testKeys(&keys.Unlocked{})), d
}

func TestAVaultIsHiddenByItsShareFlag(t *testing.T) {
	s, _ := hiddenVaultService(archiveHiddenWorkVisible)
	vaults, err := s.VaultsList(context.Background())
	if err != nil {
		t.Fatalf("VaultsList: %v", err)
	}
	hidden := map[string]bool{}
	for _, v := range vaults {
		hidden[v.ShareID] = v.Hidden
	}
	if !hidden["archive"] || hidden["work"] {
		t.Errorf("hidden = %v, want archive alone", hidden)
	}
}

// A read across vaults leaves the hidden ones out, and so never opens them; one
// named, or a read that has to cover the whole account, reads them too.
func TestAHiddenVaultIsReadOnlyWhenNamedOrWhenEverythingIs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		filter string
		reach  reach
		opened bool
	}{
		{"a read across vaults", "", browsed, false},
		{"the hidden vault named", "archive", browsed, true},
		{"a read of everything", "", everything, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, d := hiddenVaultService(archiveHiddenWorkVisible)
			if _, err := s.itemsFull(context.Background(), tc.filter, tc.reach, false); err != nil {
				t.Fatalf("itemsFull: %v", err)
			}
			if got := d.asked("/pass/v1/share/archive/key"); got != tc.opened {
				t.Errorf("opened the hidden vault = %v, want %v", got, tc.opened)
			}
			if tc.filter == "" && !d.asked("/pass/v1/share/work/key") {
				t.Error("the vault that is not hidden was never read")
			}
		})
	}
}

func TestANewItemGoesToTheFirstVaultThatIsNotHidden(t *testing.T) {
	s, _ := hiddenVaultService(archiveHiddenWorkVisible)
	got, err := s.ResolveVault(context.Background(), "")
	if err != nil {
		t.Fatalf("ResolveVault: %v", err)
	}
	if got != "work" {
		t.Errorf("ResolveVault = %q, want work", got)
	}
	if got, err := s.ResolveVault(context.Background(), "archive"); err != nil || got != "archive" {
		t.Errorf("ResolveVault(archive) = %q, %v; a hidden vault can still be named", got, err)
	}
}

func TestWhenEveryVaultIsHiddenNoneIsChosen(t *testing.T) {
	s, _ := hiddenVaultService(`{"Shares":[{"ShareID":"archive","VaultID":"v1","TargetType":1,"Flags":1}]}`)
	_, err := s.ResolveVault(context.Background(), "")
	var problem *errs.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("err = %v, want a refusal", err)
	}
}

// Both lists are sent every time, empty rather than null, which is how Pass
// itself asks.
func TestHidingSendsBothLists(t *testing.T) {
	d := &capturingDoer{}
	if err := New(d, testKeys(nil)).VaultsSetHidden(context.Background(), []string{"archive"}, nil); err != nil {
		t.Fatalf("VaultsSetHidden: %v", err)
	}
	body, err := json.Marshal(d.body)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(body), `{"SharesToHide":["archive"],"SharesToUnhide":[]}`; got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}
