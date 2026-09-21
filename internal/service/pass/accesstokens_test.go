package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"google.golang.org/protobuf/proto"

	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/crypto/aead"
	"github.com/roman-16/proton-cli/internal/proton"
	pb "github.com/roman-16/proton-cli/internal/service/pass/proto"
)

// An access token has a key of its own that Proton never sees in the clear. It
// travels to Proton sealed to the account's user key and signed by it, and to
// the program beside the token, and a vault is opened to the token by sealing
// the vault's key under it.

// tokenDoer serves one vault's share key and one token, and captures what was
// sent to make a token and to hand it a vault.
type tokenDoer struct {
	free     bool
	shareKey string
	tokenKey string
	records  []map[string]any
	made     map[string]any
	granted  []map[string]any
	revoked  []string
}

func (d *tokenDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *tokenDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var payload any
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/user/access":
		plan := map[string]any{"Type": "plus"}
		if d.free {
			plan["Type"] = "free"
		}
		payload = map[string]any{"Access": map[string]any{"Plan": plan}}
	case r.Method == "GET" && r.Path == "/pass/v1/share":
		payload = map[string]any{"Shares": []map[string]any{{
			"ShareID": "share", "VaultID": "vault", "TargetType": targetVault, "Owner": true,
		}}}
	case r.Method == "GET" && strings.HasSuffix(r.Path, "/key"):
		payload = map[string]any{"ShareKeys": map[string]any{
			"Keys": []map[string]any{{"Key": d.shareKey, "KeyRotation": 1}},
		}}
	case r.Method == "POST" && r.Path == tokenBase:
		d.made, _ = r.Body.(map[string]any)
		payload = map[string]any{"PersonalAccessToken": map[string]any{
			"PersonalAccessTokenID": "pat", "Name": d.made["Name"], "Token": "tok",
			"PersonalAccessTokenKey": d.made["PersonalAccessTokenKey"],
			"ExpireTime":             d.made["ExpireTime"], "CreateTime": 1, "Flags": d.made["Flags"],
		}}
	case r.Method == "GET" && r.Path == tokenBase:
		payload = map[string]any{"PersonalAccessTokens": map[string]any{
			"PersonalAccessTokens": []map[string]any{{
				"PersonalAccessTokenID": "pat", "Name": "ci", "PersonalAccessTokenKey": d.tokenKey,
				"ExpireTime": time.Now().Unix() + 3600*24, "CreateTime": 1,
			}},
		}}
	case r.Method == "POST" && strings.HasSuffix(r.Path, "/access"):
		body, _ := r.Body.(map[string]any)
		d.granted = append(d.granted, body)
		return nil
	case r.Method == "GET" && strings.HasSuffix(r.Path, "/access"):
		payload = map[string]any{"Shares": []map[string]any{{
			"ShareID": "grant", "ParentShareID": "share", "CreateTime": 1,
		}}}
	case r.Method == "DELETE" && strings.Contains(r.Path, "/access/"):
		d.revoked = append(d.revoked, r.Path)
		return nil
	case r.Method == "GET" && strings.HasPrefix(r.Path, "/pass/v1/pat/monitor/"):
		payload = map[string]any{"Actions": map[string]any{"Records": d.records}}
	default:
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

// holder is an account with a primary user key and one retired one, holding a
// vault whose share key is sealed to the primary.
func holder(t *testing.T) (*keys.Unlocked, *pgp.KeyRing, string) {
	t.Helper()
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
	primary, err := users.FirstKey()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := primary.Encrypt(pgp.NewPlainMessage(vaultKey()), primary)
	if err != nil {
		t.Fatal(err)
	}
	u := &keys.Unlocked{UserKR: users, AddrKRs: map[string]keys.Rings{}}
	return u, primary, base64.StdEncoding.EncodeToString(sealed.GetBinary())
}

func TestAccessTokenCreateSealsItsKeyToTheAccountAndTheVaultKeyToTheToken(t *testing.T) {
	u, primary, shareKey := holder(t)
	d := &tokenDoer{shareKey: shareKey}

	token, err := New(d, testKeys(u)).AccessTokenCreate(context.Background(), NewAccessToken{
		Name: "ci", Life: 24 * time.Hour, Agent: true, ShareIDs: []string{"share"},
	})
	if err != nil {
		t.Fatalf("AccessTokenCreate: %v", err)
	}

	// What Proton holds is the token's key sealed to the primary user key and
	// signed by it, which is what lets a key nobody here made be refused later.
	sealed, err := base64.StdEncoding.DecodeString(d.made["PersonalAccessTokenKey"].(string))
	if err != nil {
		t.Fatal(err)
	}
	msg := pgp.NewPGPMessage(sealed)
	opened, err := primary.Decrypt(msg, primary, pgp.GetUnixTime())
	if err != nil {
		t.Fatalf("the primary user key cannot open and vouch for the token key: %v", err)
	}
	raw := opened.GetBinary()
	if len(raw) != aead.KeyLen {
		t.Fatalf("the token key is %d bytes, want %d", len(raw), aead.KeyLen)
	}
	if ids, ok := msg.GetHexEncryptionKeyIDs(); !ok || len(ids) != 1 {
		t.Errorf("the token key is sealed to %v, want the primary key alone", ids)
	}
	if products, _ := d.made["Products"].([]string); len(products) != 1 || products[0] != "pass" {
		t.Errorf("the token is for %v, want pass", d.made["Products"])
	}
	if flags, _ := d.made["Flags"].(map[string]any); flags["PassAgent"] != true {
		t.Errorf("an agent token was made with flags %v", d.made["Flags"])
	}

	// What the program gets is the token Proton minted and the key in the clear.
	want := "tok::" + base64.RawURLEncoding.EncodeToString(raw)
	if token.Secret != want {
		t.Errorf("secret = %q, want %q", token.Secret, want)
	}
	if token.ID != "pat" || !token.Agent || token.Status != TokenActive {
		t.Errorf("token = %+v", token)
	}

	// The vault is opened to the token by sealing its share key under the
	// token's key, read-only.
	if len(d.granted) != 1 {
		t.Fatalf("%d vaults handed over, want 1", len(d.granted))
	}
	grant := d.granted[0]
	if grant["ShareID"] != "share" || grant["TargetType"] != targetVault || grant["ShareRoleID"] != roleRead {
		t.Errorf("grant = %v", grant)
	}
	sent, _ := grant["Keys"].([]map[string]any)
	if len(sent) != 1 || sent[0]["KeyRotation"] != 1 {
		t.Fatalf("keys handed over = %v, want the one rotation", sent)
	}
	wrapped, err := base64.StdEncoding.DecodeString(sent[0]["Key"].(string))
	if err != nil {
		t.Fatal(err)
	}
	got, err := aead.Decrypt(raw, wrapped, []byte(aead.TagShareKey))
	if err != nil {
		t.Fatalf("the token key does not open the vault key it was handed: %v", err)
	}
	if string(got) != string(vaultKey()) {
		t.Error("what the token was handed is not the vault's key")
	}
}

// The free plan has no tokens, and is told so before anything is made.
func TestAccessTokenCreateRefusesTheFreePlanBeforeMakingAnything(t *testing.T) {
	u, _, shareKey := holder(t)
	d := &tokenDoer{free: true, shareKey: shareKey}
	_, err := New(d, testKeys(u)).AccessTokenCreate(context.Background(), NewAccessToken{
		Name: "ci", Life: time.Hour, ShareIDs: []string{"share"},
	})
	if err == nil || !strings.Contains(err.Error(), "paid Pass plan") {
		t.Fatalf("a free plan was refused with %v, want the plan named", err)
	}
	if d.made != nil {
		t.Error("a token was made for a plan that has none")
	}
}

// The key Proton hands back is only trusted when the account's own key signed
// it: one sealed to the account by somebody else opens nothing.
func TestOpenTokenKeyRefusesAKeyTheAccountDidNotSign(t *testing.T) {
	u, primary, shareKey := holder(t)
	mallory := ring(t, "mallory")
	raw := vaultKey()
	forged, err := publicRing(t, primary).Encrypt(pgp.NewPlainMessage(raw), mallory)
	if err != nil {
		t.Fatal(err)
	}
	d := &tokenDoer{shareKey: shareKey, tokenKey: base64.StdEncoding.EncodeToString(forged.GetBinary())}
	s := New(d, testKeys(u))
	tokens, err := s.AccessTokens(context.Background())
	if err != nil || len(tokens) != 1 {
		t.Fatalf("AccessTokens = %v, %v", tokens, err)
	}
	if _, err := s.openTokenKey(context.Background(), tokens[0]); err == nil {
		t.Fatal("a token key nobody here signed was opened")
	}
	if _, _, err := s.AccessTokenSetVaults(context.Background(), tokens[0], []string{"share", "other"}); err == nil {
		t.Fatal("a vault was handed to a token whose key nobody here signed")
	}
	if len(d.granted) != 0 {
		t.Error("a vault key was sealed under a key nobody vouched for")
	}
}

// Making the vaults exactly the ones named hands over what is missing and takes
// back what is no longer named, and leaves what was already there alone.
func TestAccessTokenSetVaultsGrantsWhatIsMissingAndRevokesTheRest(t *testing.T) {
	u, primary, shareKey := holder(t)
	raw := vaultKey()
	sealed, err := primary.Encrypt(pgp.NewPlainMessage(raw), primary)
	if err != nil {
		t.Fatal(err)
	}
	d := &tokenDoer{shareKey: shareKey, tokenKey: base64.StdEncoding.EncodeToString(sealed.GetBinary())}
	s := New(d, testKeys(u))
	tokens, err := s.AccessTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	granted, revoked, err := s.AccessTokenSetVaults(context.Background(), tokens[0], []string{"share"})
	if err != nil || granted != 0 || revoked != 0 {
		t.Errorf("naming the vault it already reads = %d granted, %d revoked, %v", granted, revoked, err)
	}
	granted, revoked, err = s.AccessTokenSetVaults(context.Background(), tokens[0], nil)
	if err != nil || granted != 0 || revoked != 1 {
		t.Errorf("naming no vault = %d granted, %d revoked, %v", granted, revoked, err)
	}
	if len(d.revoked) != 1 || !strings.HasSuffix(d.revoked[0], "/access/grant") {
		t.Errorf("revoked %v, want the grant's own ID", d.revoked)
	}
}

// What a program wrote down about an action opens with the token's key, and a
// note that will not open leaves the action on the list, marked shut.
func TestAccessTokenActivityOpensEachNoteAndMarksOneThatWillNot(t *testing.T) {
	u, primary, shareKey := holder(t)
	raw := vaultKey()
	sealed, err := primary.Encrypt(pgp.NewPlainMessage(raw), primary)
	if err != nil {
		t.Fatal(err)
	}
	note, err := proto.Marshal(&pb.ActionPayload{Content: &pb.ActionPayload_AgentAction{
		AgentAction: &pb.AgentAction{Reason: "to fill in the login form", VaultName: "Work", ItemName: "GitHub"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := aead.Encrypt(raw, note, []byte(aead.TagActionPayload))
	if err != nil {
		t.Fatal(err)
	}
	other := make([]byte, aead.KeyLen)
	shut, err := aead.Encrypt(other, note, []byte(aead.TagActionPayload))
	if err != nil {
		t.Fatal(err)
	}
	d := &tokenDoer{
		shareKey: shareKey, tokenKey: base64.StdEncoding.EncodeToString(sealed.GetBinary()),
		records: []map[string]any{
			{"PatMonitorRecordID": "r1", "VaultID": "vault", "ObjectID": "item", "Action": 31,
				"Payload": base64.StdEncoding.EncodeToString(wrapped), "ActionTime": 10},
			{"PatMonitorRecordID": "r2", "VaultID": "vault", "Action": 160, "ActionTime": 20},
			{"PatMonitorRecordID": "r3", "VaultID": "vault", "Action": 999,
				"Payload": base64.StdEncoding.EncodeToString(shut), "ActionTime": 30},
		},
	}
	s := New(d, testKeys(u))
	tokens, err := s.AccessTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	actions, err := s.AccessTokenActivity(context.Background(), tokens[0])
	if err != nil {
		t.Fatalf("AccessTokenActivity: %v", err)
	}
	if len(actions) != 3 {
		t.Fatalf("%d actions, want 3", len(actions))
	}
	// Newest first.
	if actions[0].ID != "r3" || actions[2].ID != "r1" {
		t.Errorf("order = %s, %s, %s; want newest first", actions[0].ID, actions[1].ID, actions[2].ID)
	}
	read := actions[2]
	if read.Action != "item read" || read.ItemID != "item" || read.Reason != "to fill in the login form" ||
		read.Vault != "Work" || read.Item != "GitHub" || read.Sealed {
		t.Errorf("the read action = %+v", read)
	}
	if actions[1].Action != "vault access granted" || actions[1].Sealed || actions[1].Reason != "" {
		t.Errorf("an action with no note = %+v", actions[1])
	}
	if !actions[0].Sealed || actions[0].Action != "action 999" || actions[0].Reason != "" {
		t.Errorf("an action whose note will not open = %+v", actions[0])
	}
}

func TestATokenIsActiveUntilItsLastHourAndExpiredAfter(t *testing.T) {
	now := int64(1_000_000)
	for _, tc := range []struct {
		expires int64
		want    string
	}{
		{now + 7200, TokenActive},
		{now + 3600, TokenExpiring},
		{now + 1, TokenExpiring},
		{now - 1, TokenExpired},
	} {
		if got := tokenStatus(tc.expires, now); got != tc.want {
			t.Errorf("tokenStatus(now%+d) = %q, want %q", tc.expires-now, got, tc.want)
		}
	}
}
