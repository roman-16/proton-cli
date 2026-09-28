package account

import (
	"encoding/json"
	"testing"
)

func TestAnAccountAnswersToItsUsernameAndEveryAddress(t *testing.T) {
	a := &answers{body: map[string]json.RawMessage{
		"GET /core/v4/users": json.RawMessage(`{"User": {"ID": "u1", "Name": "alice.smith", "Email": "alice@company.com"}}`),
		"GET /core/v4/addresses": json.RawMessage(`{"Addresses": [
			{"Email": "alice@company.com"}, {"Email": "alice.smith@proton.me"}, {"Email": "Alice@PM.me"}
		]}`),
	}}
	acct, err := New(a, nil).Get(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]bool{
		"alice.smith":           true,
		"alice@company.com":     true,
		"alice.smith@proton.me": true,
		"ALICE@COMPANY.COM":     true,
		"alice@pm.me":           true,
		"bob@company.com":       false,
		"":                      false,
	} {
		if got := acct.AnswersTo(name); got != want {
			t.Errorf("AnswersTo(%q) = %v, want %v", name, got, want)
		}
	}
}
