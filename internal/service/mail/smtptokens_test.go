package mail

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// smtpAPI answers the two listings a token listing is made of, and keeps what
// was asked of it.
type smtpAPI struct {
	tokens    string
	addresses string
	created   string
	refusal   error
	paths     []string
	bodies    []any
}

func (a *smtpAPI) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *smtpAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.paths = append(a.paths, req.Method+" "+req.Path)
	switch {
	case req.Method == "POST" && req.Path == "/mail/v4/smtptokens":
		a.bodies = append(a.bodies, req.Body)
		return json.Unmarshal([]byte(a.created), out)
	case req.Method == "GET" && req.Path == "/mail/v4/smtptokens":
		if a.refusal != nil {
			return a.refusal
		}
		return json.Unmarshal([]byte(a.tokens), out)
	case req.Path == "/core/v4/addresses":
		return json.Unmarshal([]byte(a.addresses), out)
	case req.Path == "/core/v4/organizations":
		return &proton.APIError{HTTPStatus: 422, Code: noOrganization, Message: "not a member"}
	}
	return nil
}

// A token carries the ID of the address it sends from, and the address is what
// somebody typed into the device - so the two listings are one answer. The
// newest token is the one a person just made, so it comes first.
func TestASMTPTokenIsNamedByTheAddressItSendsFrom(t *testing.T) {
	api := &smtpAPI{
		tokens: `{"Code":1000,"SmtpTokens":[
		  {"SmtpTokenID":"tok-2","AddressID":"addr-1","Name":"Status page",
		   "CreateTime":1789291230,"LastUsedTime":null},
		  {"SmtpTokenID":"tok-1","AddressID":"addr-1","Name":"Office printer",
		   "CreateTime":1789377630,"LastUsedTime":1789464030}]}`,
		addresses: `{"Code":1000,"Addresses":[
		  {"ID":"addr-1","Email":"billing@example.com","Type":3,"Status":1}]}`,
	}

	rows, err := New(api, nil).SMTPTokens(context.Background())
	if err != nil {
		t.Fatalf("SMTPTokens = %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("tokens = %d, want 2", len(rows))
	}
	if rows[0].Name != "Office printer" {
		t.Errorf("the first token is %q, want the newest", rows[0].Name)
	}
	if rows[0].Address != "billing@example.com" {
		t.Errorf("address = %q, want the address the token sends from", rows[0].Address)
	}
	if rows[0].LastUsed != 1789464030 {
		t.Errorf("last used = %d, want when something last sent with it", rows[0].LastUsed)
	}
	if rows[1].LastUsed != 0 {
		t.Errorf("a token nothing has sent with was last used at %d", rows[1].LastUsed)
	}
	for _, token := range rows {
		if token.Secret != "" {
			t.Errorf("a listing carries a token: %q", token.Secret)
		}
	}
}

// An address the account no longer holds has nothing to be named by, and the
// row says which one it was rather than leaving the cell blank.
func TestASMTPTokenWhoseAddressIsGoneNamesItsID(t *testing.T) {
	api := &smtpAPI{
		tokens: `{"Code":1000,"SmtpTokens":[
		  {"SmtpTokenID":"tok-1","AddressID":"addr-gone","Name":"Old printer","CreateTime":1789377630}]}`,
		addresses: `{"Code":1000,"Addresses":[]}`,
	}

	rows, err := New(api, nil).SMTPTokens(context.Background())
	if err != nil {
		t.Fatalf("SMTPTokens = %v", err)
	}
	if rows[0].Address != "addr-gone" {
		t.Errorf("address = %q, want the ID standing in for it", rows[0].Address)
	}
}

// An account with no plan has no custom domain to send from, and Proton's own
// refusal says nothing a person can act on.
func TestASMTPTokenListingWithoutAPlanSaysWhatIsMissing(t *testing.T) {
	api := &smtpAPI{refusal: &proton.APIError{HTTPStatus: 500, Message: "Internal server error"}}

	_, err := New(api, nil).SMTPTokens(context.Background())
	var problem *errs.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("SMTPTokens = %v, want a refusal somebody can act on", err)
	}
	if !strings.Contains(problem.Error(), "SMTP tokens need a paid Mail plan") {
		t.Errorf("the refusal does not say what is missing: %v", problem)
	}
}

// The token is the answer, and the address it was made for travels with it:
// together they are the password and the username a device is set up with.
func TestMakingASMTPTokenHandsItBackInTheClear(t *testing.T) {
	api := &smtpAPI{created: `{"Code":1000,"SmtpTokenCode":"9Kd2mQxT9wLpN4vRs8kZc"}`}

	token, err := New(api, nil).SMTPTokenCreate(context.Background(),
		Address{ID: "addr-1", Email: "billing@example.com"}, "Office printer")
	if err != nil {
		t.Fatalf("SMTPTokenCreate = %v", err)
	}
	if token.Secret != "9Kd2mQxT9wLpN4vRs8kZc" {
		t.Errorf("token = %q", token.Secret)
	}
	if token.Address != "billing@example.com" || token.Name != "Office printer" {
		t.Errorf("the token was made for %q as %q", token.Address, token.Name)
	}
	body, _ := api.bodies[0].(map[string]any)
	if body["AddressID"] != "addr-1" || body["Name"] != "Office printer" {
		t.Errorf("the request asked for %v", body)
	}
}

// A creation Proton answers without a token leaves nothing to give the device,
// and saying so beats reporting a token that is the empty string.
func TestASMTPTokenProtonKeepsToItselfIsAFailure(t *testing.T) {
	api := &smtpAPI{created: `{"Code":1000}`}

	if _, err := New(api, nil).SMTPTokenCreate(context.Background(),
		Address{ID: "addr-1", Email: "billing@example.com"}, "Office printer"); err == nil {
		t.Error("SMTPTokenCreate reported success without a token")
	}
}
