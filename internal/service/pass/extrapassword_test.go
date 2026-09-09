package pass

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

// answers stands in for Proton, keyed by the request each answer belongs to, and
// records what was sent so a write can be read back.
type answers struct {
	body map[string]json.RawMessage
	sent []proton.Request
}

func (a *answers) Do(_ context.Context, r proton.Request) (*proton.Response, error) {
	a.sent = append(a.sent, r)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *answers) Decode(_ context.Context, r proton.Request, out any) error {
	a.sent = append(a.sent, r)
	answer, ok := a.body[r.Method+" "+r.Path]
	if !ok {
		return fmt.Errorf("nothing to answer %s %s with", r.Method, r.Path)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(answer, out)
}

// Whether Pass has an extra password is answered without asking anybody for one,
// which is what makes it safe to ask before every one of these commands. A
// session Proton has withheld the Pass scope from belongs to an account that has
// one - and asking Pass itself would be asking an endpoint that refuses until the
// password is handed over.
func TestExtraPasswordIsReadFromTheScopeBeforePass(t *testing.T) {
	for _, tc := range []struct {
		name    string
		scopes  string
		hasSRP  bool
		want    bool
		visited []string
	}{{
		name:    "a session without the Pass scope",
		scopes:  `["self","mail","parent"]`,
		want:    true,
		visited: []string{"GET /core/v4/auth/scopes"},
	}, {
		name:    "a session holding it, over an account with one",
		scopes:  `["self","mail","pass"]`,
		hasSRP:  true,
		want:    true,
		visited: []string{"GET /core/v4/auth/scopes", "GET /pass/v1/user/srp"},
	}, {
		name:    "a session holding it, over an account with none",
		scopes:  `["self","mail","pass"]`,
		want:    false,
		visited: []string{"GET /core/v4/auth/scopes", "GET /pass/v1/user/srp"},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			a := &answers{body: map[string]json.RawMessage{
				"GET /core/v4/auth/scopes": json.RawMessage(`{"Scopes":` + tc.scopes + `}`),
				"GET /pass/v1/user/srp":    json.RawMessage(fmt.Sprintf(`{"HasSRP":%v}`, tc.hasSRP)),
			}}
			got, err := New(a, testKeys(nil)).ExtraPassword(context.Background())
			if err != nil {
				t.Fatalf("ExtraPassword: %v", err)
			}
			if got.Enabled != tc.want {
				t.Errorf("Enabled = %v, want %v", got.Enabled, tc.want)
			}
			var visited []string
			for _, r := range a.sent {
				visited = append(visited, r.Method+" "+r.Path)
			}
			if fmt.Sprint(visited) != fmt.Sprint(tc.visited) {
				t.Errorf("asked %v, want %v", visited, tc.visited)
			}
		})
	}
}

// Setting one sends a verifier and never the password, which is the whole of what
// Proton is trusted with here.
func TestSettingAnExtraPasswordSendsAVerifier(t *testing.T) {
	// A modulus Proton signed, because go-srp checks the signature before it will
	// derive anything from one. It is the fixture from go-srp's own tests.
	modulus, err := os.ReadFile("testdata/modulus.asc")
	if err != nil {
		t.Fatalf("modulus fixture: %v", err)
	}
	a := &answers{body: map[string]json.RawMessage{
		"GET /core/v4/auth/modulus": json.RawMessage(
			`{"Modulus":` + string(mustJSON(string(modulus))) + `,"ModulusID":"modulus-id"}`),
		"POST /pass/v1/user/srp": json.RawMessage(`{}`),
	}}
	if err := New(a, testKeys(nil)).ExtraPasswordSet(context.Background(), "correct horse"); err != nil {
		t.Fatalf("ExtraPasswordSet: %v", err)
	}

	last := a.sent[len(a.sent)-1]
	if last.Method != "POST" || last.Path != "/pass/v1/user/srp" {
		t.Fatalf("sent %s %s", last.Method, last.Path)
	}
	body, ok := last.Body.(map[string]any)
	if !ok {
		t.Fatalf("body is %T", last.Body)
	}
	for _, field := range []string{"SrpModulusID", "SrpSalt", "SrpVerifier"} {
		if v, _ := body[field].(string); v == "" {
			t.Errorf("%s is empty", field)
		}
	}
	for field, value := range body {
		if text, _ := value.(string); text == "correct horse" {
			t.Errorf("%s carries the password itself", field)
		}
	}
	if salt, _ := body["SrpSalt"].(string); salt != "" {
		if _, err := base64.StdEncoding.DecodeString(salt); err != nil {
			t.Errorf("SrpSalt is not base64: %v", err)
		}
	}
}

func TestRemovingAnExtraPasswordDeletesIt(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{"DELETE /pass/v1/user/srp": json.RawMessage(`{}`)}}
	if err := New(a, testKeys(nil)).ExtraPasswordRemove(context.Background()); err != nil {
		t.Fatalf("ExtraPasswordRemove: %v", err)
	}
	if last := a.sent[len(a.sent)-1]; last.Method != "DELETE" || last.Path != "/pass/v1/user/srp" {
		t.Errorf("sent %s %s", last.Method, last.Path)
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
