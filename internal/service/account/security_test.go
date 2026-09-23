package account

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/roman-16/proton-cli/internal/proton"
)

// answers is Proton, for the reading under test: what it says to each request,
// and what it was asked.
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

// serving is an account as Proton describes it: its settings, and the part of
// the account record the credentials live in.
func serving(settings, user string) *answers {
	return &answers{body: map[string]json.RawMessage{
		"GET /core/v4/settings": json.RawMessage(`{"UserSettings":` + settings + `}`),
		"GET /core/v4/users":    json.RawMessage(`{"User":` + user + `}`),
	}}
}

// The reading is what every `get` in this tree answers with, and what each
// write checks itself against, so the whole of it is read from one pair of
// requests rather than one pair per question.
func TestSecurityReadsWhatTheAccountIsProtectedWith(t *testing.T) {
	a := serving(`{
		"Email": {"Value": "jane.roe@example.com", "Status": 1, "Reset": 1},
		"Phone": {"Value": "+43 660 1234567", "Status": 0, "Reset": 0},
		"Password": {"Mode": 2},
		"Mnemonic": {"UpdateTime": 1776000000},
		"HighSecurity": {"Value": 1},
		"2FA": {"Enabled": 3, "RegisteredKeys": [{"Name": "the yubikey", "CredentialID": [255, 239, 191]}]}
	}`, `{"MnemonicStatus": 3}`)

	got, err := New(a, nil).Security(context.Background())
	if err != nil {
		t.Fatalf("Security: %v", err)
	}
	want := Security{
		TwoFactor: TwoFactor{AuthenticatorApp: true, SecurityKeys: []SecurityKey{
			{ID: "_--_", Name: "the yubikey"},
		}},
		TwoPasswordMode: true,
		RecoveryEmail:   RecoveryEmail{Address: "jane.roe@example.com", Verified: true, AllowRecovery: true},
		RecoveryPhone:   RecoveryPhone{Number: "+43 660 1234567"},
		RecoveryPhrase:  RecoveryPhrase{Status: PhraseOn, Changed: 1776000000},
		Sentinel:        true,
	}
	if fmt.Sprint(*got) != fmt.Sprint(want) {
		t.Errorf("read\n  %v\nwant\n  %v", *got, want)
	}
}

// A security key registered without the authenticator app is its own state, and
// reporting either as "two-factor is on" would hide which of them the account
// would be asked for.
func TestSecurityTellsTheTwoSecondFactorsApart(t *testing.T) {
	for _, tc := range []struct {
		enabled int
		app     bool
	}{{enabled: 0, app: false}, {enabled: 1, app: true}, {enabled: 2, app: false}, {enabled: 3, app: true}} {
		a := serving(fmt.Sprintf(`{"2FA": {"Enabled": %d}}`, tc.enabled), `{}`)
		got, err := New(a, nil).Security(context.Background())
		if err != nil {
			t.Fatalf("Security: %v", err)
		}
		if got.TwoFactor.AuthenticatorApp != tc.app {
			t.Errorf("Enabled %d: authenticator app = %v, want %v",
				tc.enabled, got.TwoFactor.AuthenticatorApp, tc.app)
		}
	}
}

// The four words a phrase's state is reported in. The one distinction that
// changes what a person does is `outdated`: those words still open the data
// they were made for and will not get the account back.
func TestTheRecoveryPhraseStatesAreNamed(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
		set    bool
	}{
		{status: 0, want: PhraseOff},
		{status: 1, want: PhraseNotSet},
		{status: 2, want: PhraseOutdated, set: true},
		{status: 3, want: PhraseOn, set: true},
		{status: 4, want: PhraseNotSet},
	} {
		got := phrase(tc.status, 1776000000)
		if got.Status != tc.want {
			t.Errorf("status %d reads as %q, want %q", tc.status, got.Status, tc.want)
		}
		if got.Set() != tc.set {
			t.Errorf("status %d: set = %v, want %v", tc.status, got.Set(), tc.set)
		}
		// A time only means something where there is a phrase to have been set.
		if wantTime := int64(0); !tc.set && got.Changed != wantTime {
			t.Errorf("status %d: changed = %d, want %d", tc.status, got.Changed, wantTime)
		}
	}
}

// Taking a second factor or a recovery phrase away carries the proof of the
// password in the request rather than answering for it afterwards, which is
// what Proton wants for both.
func TestTakingACredentialAwayProvesThePasswordInTheRequest(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Service) error
		path string
		// accountHost is where the endpoint answers: the recovery phrase is the
		// account app's and is Path not found at the host Mail is served from.
		accountHost bool
	}{{
		name: "the authenticator app",
		call: func(s *Service) error { return s.TOTPDisable(context.Background()) },
		path: "/core/v4/settings/2fa/totp",
	}, {
		name:        "the recovery phrase",
		call:        func(s *Service) error { return s.DisableRecoveryPhrase(context.Background()) },
		path:        "/core/v4/settings/mnemonic/disable",
		accountHost: true,
	}} {
		t.Run(tc.name, func(t *testing.T) {
			a := &answers{body: map[string]json.RawMessage{
				"PUT " + tc.path:  json.RawMessage(`{}`),
				"POST " + tc.path: json.RawMessage(`{}`),
			}}
			if err := tc.call(New(a, nil)); err != nil {
				t.Fatalf("call: %v", err)
			}
			if len(a.sent) != 1 {
				t.Fatalf("sent %d requests, want one", len(a.sent))
			}
			if !a.sent[0].Proves {
				t.Error("the request does not carry the proof of the password")
			}
			if a.sent[0].AccountHost != tc.accountHost {
				t.Errorf("account host = %v, want %v", a.sent[0].AccountHost, tc.accountHost)
			}
		})
	}
}

// A recovery address or number is removed by writing an empty one, which is
// what the CLI's `none` means here.
func TestRemovingARecoveryAddressWritesAnEmptyOne(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"PUT /core/v4/settings/email": json.RawMessage(`{}`),
		"PUT /core/v4/settings/phone": json.RawMessage(`{}`),
	}}
	s := New(a, nil)
	if err := s.SetRecoveryEmail(context.Background(), ""); err != nil {
		t.Fatalf("SetRecoveryEmail: %v", err)
	}
	if err := s.SetRecoveryPhone(context.Background(), ""); err != nil {
		t.Fatalf("SetRecoveryPhone: %v", err)
	}
	for i, field := range []string{"Email", "Phone"} {
		body, _ := a.sent[i].Body.(map[string]any)
		if value, ok := body[field]; !ok || value != "" {
			t.Errorf("%s = %v, want an empty one", field, value)
		}
	}
}

// The code and the number travel together in the header a human verification
// travels in, with the spacing a person types stripped out.
func TestVerifyingAPhoneCarriesTheNumberAndTheCode(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"POST /core/v4/verify/phone": json.RawMessage(`{}`),
	}}
	if err := New(a, nil).VerifyPhone(context.Background(), "+43 660 1234567", "482 913"); err != nil {
		t.Fatalf("VerifyPhone: %v", err)
	}
	if got, want := a.sent[0].HVToken, "+436601234567:482913"; got != want {
		t.Errorf("token = %q, want %q", got, want)
	}
	if got := a.sent[0].HVType; got != "sms" {
		t.Errorf("type = %q, want sms", got)
	}
}
