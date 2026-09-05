package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// An invitation is a vault key encrypted to the invited address and signed by
// the inviter under the invitation context. Proton cannot read it, so the
// signature is the only thing that says who put the key in the record - and an
// offer is opened against the inviter's published keys or not at all.

func ring(t *testing.T, name string) *pgp.KeyRing {
	t.Helper()
	key, err := pgp.GenerateKey(name, name+"@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	kr, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return kr
}

func publicRing(t *testing.T, kr *pgp.KeyRing) *pgp.KeyRing {
	t.Helper()
	armored, err := kr.GetKeys()[0].GetArmoredPublicKey()
	if err != nil {
		t.Fatalf("GetArmoredPublicKey: %v", err)
	}
	key, err := pgp.NewKeyFromArmored(armored)
	if err != nil {
		t.Fatalf("NewKeyFromArmored: %v", err)
	}
	pub, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	return pub
}

// offer seals a vault key to the invited address, signed by whoever signer is
// under the context given.
func offer(t *testing.T, invited, signer *pgp.KeyRing, context string) string {
	t.Helper()
	var sealed *pgp.PGPMessage
	var err error
	if context == "" {
		sealed, err = publicRing(t, invited).Encrypt(pgp.NewPlainMessage(vaultKey()), signer)
	} else {
		sealed, err = publicRing(t, invited).EncryptWithContext(pgp.NewPlainMessage(vaultKey()), signer, pgp.NewSigningContext(context, true))
	}
	if err != nil {
		t.Fatalf("seal the offer: %v", err)
	}
	return base64.StdEncoding.EncodeToString(sealed.GetBinary())
}

// inviteDoer serves one received invitation and the inviter's published keys,
// and captures what an acceptance sends back.
type inviteDoer struct {
	key      string
	content  string
	inviter  *pgp.KeyRing
	accepted map[string]any
}

func (d *inviteDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *inviteDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var payload any
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/invite":
		payload = map[string]any{"Invites": []map[string]any{{
			"InviteToken": "tok", "InvitedEmail": "me@proton.me", "InvitedAddressID": "addr",
			"InviterEmail": "alice@proton.me", "ShareRoleID": roleRead, "TargetType": targetVault,
			"Keys":      []map[string]any{{"Key": d.key, "KeyRotation": 1}},
			"VaultData": map[string]any{"Content": d.content, "ContentKeyRotation": 1, "ItemCount": 3},
		}}}
	case r.Method == "GET" && r.Path == "/core/v4/keys/all":
		armored, err := d.inviter.GetKeys()[0].GetArmoredPublicKey()
		if err != nil {
			return err
		}
		payload = map[string]any{"Address": map[string]any{"Keys": []map[string]any{{"PublicKey": armored, "Primary": 1}}}}
	case r.Method == "POST":
		d.accepted, _ = r.Body.(map[string]any)
		return nil
	default:
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func invited(t *testing.T) (*keys.Unlocked, *pgp.KeyRing) {
	t.Helper()
	addr := ring(t, "me")
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
	return &keys.Unlocked{UserKR: users, AddrKRs: map[string]*pgp.KeyRing{"addr": addr}}, addr
}

func TestAcceptRefusesAKeyTheInviterDidNotSign(t *testing.T) {
	u, addr := invited(t)
	alice, mallory := ring(t, "alice"), ring(t, "mallory")
	for name, key := range map[string]string{
		"signed by somebody else":        offer(t, addr, mallory, inviteContext),
		"signed for something else":      offer(t, addr, alice, "pass.something.else"),
		"signed under no context at all": offer(t, addr, alice, ""),
	} {
		t.Run(name, func(t *testing.T) {
			d := &inviteDoer{key: key, inviter: alice}
			err := New(d, testKeys(u)).InviteAccept(context.Background(), "tok")
			if err == nil {
				t.Fatal("an offer nobody vouched for was taken")
			}
			if !strings.Contains(err.Error(), "was not signed by alice@proton.me") {
				t.Errorf("the refusal does not say who failed to vouch: %v", err)
			}
			if d.accepted != nil {
				t.Error("the key was re-sealed and sent back all the same")
			}
		})
	}
}

// A key the inviter did sign is taken, and sealed to the primary user key alone
// on the way in - every key the account ever had opens old secrets, but a new
// one is not put under a retired key.
func TestAcceptTakesTheInvitersKeyAndSealsItToOneKey(t *testing.T) {
	u, addr := invited(t)
	alice := ring(t, "alice")
	d := &inviteDoer{key: offer(t, addr, alice, inviteContext), inviter: alice}
	if err := New(d, testKeys(u)).InviteAccept(context.Background(), "tok"); err != nil {
		t.Fatalf("InviteAccept: %v", err)
	}
	sent, _ := d.accepted["Keys"].([]map[string]any)
	if len(sent) != 1 {
		t.Fatalf("accepted with %d keys, want the one rotation offered", len(sent))
	}
	raw, err := base64.StdEncoding.DecodeString(sent[0]["Key"].(string))
	if err != nil {
		t.Fatal(err)
	}
	msg := pgp.NewPGPMessage(raw)
	if ids, ok := msg.GetHexEncryptionKeyIDs(); !ok || len(ids) != 1 {
		t.Fatalf("the vault key is sealed to %v, want exactly one key", ids)
	}
	primary, err := u.PrimaryUserKey()
	if err != nil {
		t.Fatal(err)
	}
	opened, err := primary.Decrypt(msg, nil, pgp.GetUnixTime())
	if err != nil {
		t.Fatalf("the primary user key cannot open the vault key it should have sealed: %v", err)
	}
	if string(opened.GetBinary()) != string(vaultKey()) {
		t.Error("what was re-sealed is not the vault key that was offered")
	}
}

// The listing shows the offer either way, with the vault's name only when the
// key that reveals it was signed by the sender.
func TestReceivedInvitationsNameTheVaultOnlyWhenTheInviterSigned(t *testing.T) {
	u, addr := invited(t)
	alice, mallory := ring(t, "alice"), ring(t, "mallory")
	for name, tc := range map[string]struct {
		signer *pgp.KeyRing
		named  bool
	}{
		"signed by the inviter":   {alice, true},
		"signed by somebody else": {mallory, false},
	} {
		t.Run(name, func(t *testing.T) {
			d := &inviteDoer{
				key: offer(t, addr, tc.signer, inviteContext), inviter: alice,
				content: storedVault(t, vaultKey(), &pb.Vault{Name: "Work"}),
			}
			invites, err := New(d, testKeys(u)).InvitesReceived(context.Background())
			if err != nil {
				t.Fatalf("InvitesReceived: %v", err)
			}
			if len(invites) != 1 {
				t.Fatalf("%d invitations, want the one offered", len(invites))
			}
			if want := map[bool]string{true: "Work", false: ""}[tc.named]; invites[0].Vault != want {
				t.Errorf("vault name = %q, want %q", invites[0].Vault, want)
			}
		})
	}
}
