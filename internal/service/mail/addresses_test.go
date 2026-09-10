package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	pgp "github.com/ProtonMail/gopenpgp/v2/crypto"
	"github.com/roman-16/proton-cli/internal/account/keys"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// testSender is a stand-in sending identity for tests that only need an address,
// not working crypto.
func testSender() *Sender {
	return &Sender{Address: keys.Address{
		ID: "addr-1", Email: "sender@proton.me", DisplayName: "Sender",
		Status: 1, Send: 1, Receive: 1,
	}}
}

// unlockedWith builds an Unlocked whose every address has an (empty) key ring, so
// sender selection sees them all as usable.
func unlockedWith(addrs ...keys.Address) *keys.Unlocked {
	krs := map[string]keys.Rings{}
	for _, a := range addrs {
		kr, err := pgp.NewKeyRing(nil)
		if err != nil {
			panic(err)
		}
		krs[a.ID] = keys.Rings{Read: kr, Write: kr}
	}
	return &keys.Unlocked{AddrKRs: krs, Addresses: addrs}
}

func addr(id, email string, order int, opts ...func(*keys.Address)) keys.Address {
	a := keys.Address{ID: id, Email: email, Order: order, Status: 1, Send: 1, Receive: 1}
	for _, o := range opts {
		o(&a)
	}
	return a
}

func disabled(a *keys.Address)    { a.Status = 0 }
func sendOnly(a *keys.Address)    { a.Receive = 0 }
func receiveOnly(a *keys.Address) { a.Send = 0 }

func TestResolveSenderUsesAccountOrder(t *testing.T) {
	u := unlockedWith(
		addr("b", "second@proton.me", 2),
		addr("a", "first@proton.me", 1),
	)
	got, err := resolveSender(u, SenderRequest{})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.Email != "first@proton.me" {
		t.Errorf("sender = %q, want the lowest-Order address", got.Address.Email)
	}
}

func TestResolveSenderSkipsAddressesThatCannotSend(t *testing.T) {
	u := unlockedWith(
		addr("a", "disabled@proton.me", 1, disabled),
		addr("b", "receive-only@proton.me", 2, receiveOnly),
		addr("c", "send-only@proton.me", 3, sendOnly),
		addr("d", "usable@proton.me", 4),
	)
	got, err := resolveSender(u, SenderRequest{})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.Email != "usable@proton.me" {
		t.Errorf("sender = %q, want usable@proton.me", got.Address.Email)
	}
}

func TestResolveSenderExplicitByEmailAndID(t *testing.T) {
	u := unlockedWith(addr("a", "first@proton.me", 1), addr("b", "work@example.com", 2))
	for _, want := range []string{"work@example.com", "WORK@EXAMPLE.COM", "b"} {
		got, err := resolveSender(u, SenderRequest{Explicit: want})
		if err != nil {
			t.Fatalf("resolveSender(%q): %v", want, err)
		}
		if got.Address.ID != "b" {
			t.Errorf("resolveSender(%q) picked %q, want address b", want, got.Address.ID)
		}
	}
}

func TestResolveSenderExplicitPlusAliasKeepsTheAlias(t *testing.T) {
	u := unlockedWith(addr("a", "me@proton.me", 1))
	got, err := resolveSender(u, SenderRequest{Explicit: "me+shopping@proton.me"})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.ID != "a" {
		t.Errorf("alias resolved to %q, want the base address a", got.Address.ID)
	}
	if got.Address.Email != "me+shopping@proton.me" {
		t.Errorf("email = %q, want the alias preserved", got.Address.Email)
	}
}

func TestResolveSenderUnknownExplicitIsNotFound(t *testing.T) {
	u := unlockedWith(addr("a", "me@proton.me", 1))
	_, err := resolveSender(u, SenderRequest{Explicit: "nope@elsewhere.test"})
	if err == nil {
		t.Fatal("expected an error for an address that is not on the account")
	}
	if code := exitCodeOf(err); code != 3 {
		t.Errorf("exit code = %d, want 3 (not found)", code)
	}
}

func TestResolveSenderFollowsTheParentAddress(t *testing.T) {
	u := unlockedWith(addr("a", "first@proton.me", 1), addr("b", "work@example.com", 2))

	// A reply leaves from the address the parent arrived on, not the default.
	got, err := resolveSender(u, SenderRequest{ParentAddressID: "b"})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.ID != "b" {
		t.Errorf("sender = %q, want the parent's address b", got.Address.ID)
	}

	// Matching on the address the mail was sent to works too, for a parent whose
	// AddressID is no longer around.
	got, err = resolveSender(u, SenderRequest{ParentAddress: "work@example.com"})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.ID != "b" {
		t.Errorf("sender = %q, want b", got.Address.ID)
	}
}

func TestResolveSenderParentPlusAliasSendsAsTheAlias(t *testing.T) {
	u := unlockedWith(addr("a", "me@proton.me", 1))
	got, err := resolveSender(u, SenderRequest{ParentAddress: "me+newsletter@proton.me", ParentAddressID: "a"})
	if err != nil {
		t.Fatalf("ResolveSender: %v", err)
	}
	if got.Address.Email != "me+newsletter@proton.me" {
		t.Errorf("email = %q, want the alias the parent arrived on", got.Address.Email)
	}
}

func TestPlusAliasBase(t *testing.T) {
	tests := []struct{ in, want string }{
		{"me+tag@proton.me", "me@proton.me"},
		{"me@proton.me", "me@proton.me"},
		{"me+a+b@proton.me", "me@proton.me"},
		{"not-an-address", ""},
		{"weird+@proton.me", "weird@proton.me"},
	}
	for _, tt := range tests {
		if got := plusAliasBase(tt.in); got != tt.want {
			t.Errorf("plusAliasBase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// exitCodeOf reports the exit code an error carries, or 0.
func exitCodeOf(err error) int {
	type exitCoder interface{ ExitCode() int }
	if ec, ok := err.(exitCoder); ok {
		return ec.ExitCode()
	}
	return 0
}

// Adding an address is the one command that makes key material, so what it
// refuses matters as much as what it does: an account whose keys this build
// cannot make, and an address that already has one.

// addressAPI answers the reads adding an address makes and refuses the write,
// which is where every test here stops.
type addressAPI struct {
	postQuantum bool
	member      string
	// refusal is what Proton answers the create with.
	refusal error
	// protonDomains, customDomains and shortDomains are what the account may use.
	protonDomains []string
	customDomains []string
	shortDomains  []string
	requests      []proton.Request
}

func (a *addressAPI) Do(_ context.Context, req proton.Request) (*proton.Response, error) {
	a.requests = append(a.requests, req)
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *addressAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.requests = append(a.requests, req)
	answer := `{"Code":1000}`
	switch {
	case req.Path == "/core/v4/settings":
		flag := 0
		if a.postQuantum {
			flag = 1
		}
		answer = fmt.Sprintf(`{"Code":1000,"UserSettings":{"Flags":{"SupportPgpV6Keys":%d}}}`, flag)
	case req.Path == "/core/v4/members/me":
		answer = fmt.Sprintf(`{"Code":1000,"Member":{"ID":%q}}`, a.member)
	case req.Path == "/core/v4/addresses" && req.Method == "POST":
		return a.refusal
	case req.Path == "/core/v4/addresses/setup":
		return a.refusal
	case req.Path == "/domains/premium":
		listed, _ := json.Marshal(a.shortDomains)
		answer = fmt.Sprintf(`{"Code":1000,"Domains":%s}`, listed)
	case req.Path == "/domains/available":
		listed, _ := json.Marshal(a.protonDomains)
		answer = fmt.Sprintf(`{"Code":1000,"Domains":%s}`, listed)
	case req.Path == "/domains":
		var domains []map[string]any
		for _, name := range a.customDomains {
			domains = append(domains, map[string]any{"DomainName": name, "State": 1})
		}
		listed, _ := json.Marshal(domains)
		answer = fmt.Sprintf(`{"Code":1000,"Domains":%s}`, listed)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal([]byte(answer), out)
}

func (a *addressAPI) sent(method, path string) int {
	var n int
	for _, req := range a.requests {
		if req.Method == method && req.Path == path {
			n++
		}
	}
	return n
}

// body is what was sent to a path, or nil if nothing was.
func (a *addressAPI) body(path string) map[string]any {
	for _, req := range a.requests {
		if req.Path != path {
			continue
		}
		body, _ := req.Body.(map[string]any)
		return body
	}
	return nil
}

// hierarchy is an unlocked account holding the given addresses, as the service
// reads it: adding an address needs the user key and nothing else.
func hierarchy(t *testing.T, addrs ...keys.Address) keys.Get {
	t.Helper()
	key, err := pgp.GenerateKey("user", "user@example.invalid", "x25519", 0)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	userKR, err := pgp.NewKeyRing(key)
	if err != nil {
		t.Fatalf("NewKeyRing: %v", err)
	}
	u := &keys.Unlocked{UserKR: userKR, Addresses: addrs, AddrKRs: map[string]keys.Rings{}}
	return func(context.Context) (*keys.Unlocked, error) { return u, nil }
}

// account is a hierarchy that knows what it is called and what its plan covers,
// which is what turning on the short-domain address is decided from.
func account(t *testing.T, username string, paidMail bool, addrs ...keys.Address) keys.Get {
	t.Helper()
	get := hierarchy(t, addrs...)
	u, err := get(context.Background())
	if err != nil {
		t.Fatalf("hierarchy: %v", err)
	}
	u.Username, u.PaidMail = username, paidMail
	return get
}

// The account's own name on a short domain is its short-domain address, which
// Proton sets up through a door of its own - and it arrives carrying what the
// default address already says, so nobody retypes it.
func TestAddressCreateTurnsOnTheShortDomainAddress(t *testing.T) {
	api := &addressAPI{member: "member", shortDomains: []string{"pm.me"}}
	s := New(api, account(t, "alice", true, keys.Address{
		ID: "first", Email: "alice@proton.me", Order: 1,
		DisplayName: "Alice", Signature: "<p>Alice</p>",
		Keys: []keys.Key{{ID: "key", Active: 1, Primary: 1}},
	}))

	// The key that follows is generated against Proton's answer, which this stub
	// does not give; what the setup asked for is the whole of the question here.
	_, _ = s.AddressCreate(context.Background(), "Alice", "pm.me", "")

	if api.sent("POST", "/core/v4/addresses/setup") != 1 {
		t.Fatalf("the short-domain address was not set up: %v", api.requests)
	}
	if api.sent("POST", "/core/v4/addresses") != 0 {
		t.Error("the short-domain address was added as an ordinary one")
	}
	body := api.body("/core/v4/addresses/setup")
	if body["Domain"] != "pm.me" {
		t.Errorf("the setup names domain %v, want pm.me", body["Domain"])
	}
	if body["DisplayName"] != "Alice" || body["Signature"] != "<p>Alice</p>" {
		t.Errorf("the short-domain address does not take the default address's identity: %v", body)
	}
}

// A display name that was asked for is the one the address gets.
func TestAddressCreateKeepsTheDisplayNameTheShortDomainAddressWasGiven(t *testing.T) {
	api := &addressAPI{member: "member", shortDomains: []string{"pm.me"}}
	s := New(api, account(t, "alice", true, keys.Address{
		ID: "first", Email: "alice@proton.me", Order: 1, DisplayName: "Alice",
		Keys: []keys.Key{{ID: "key", Active: 1, Primary: 1}},
	}))

	_, _ = s.AddressCreate(context.Background(), "alice", "pm.me", "Alice at work")

	if got := api.body("/core/v4/addresses/setup")["DisplayName"]; got != "Alice at work" {
		t.Errorf("the address was set up as %v, want the name that was asked for", got)
	}
}

// Only a plan with Mail may turn it on, and the refusal costs no request: there
// is nothing else for the account to try.
func TestAddressCreateRefusesTheShortDomainWithoutAPaidPlan(t *testing.T) {
	api := &addressAPI{member: "member", shortDomains: []string{"pm.me"}}
	s := New(api, account(t, "alice", false, keys.Address{
		ID: "first", Email: "alice@proton.me", Order: 1,
		Keys: []keys.Key{{ID: "key", Active: 1, Primary: 1}},
	}))

	_, err := s.AddressCreate(context.Background(), "alice", "pm.me", "")
	if err == nil || !strings.Contains(err.Error(), "paid Mail plan") {
		t.Fatalf("AddressCreate = %v, want a refusal naming the plan", err)
	}
	if api.sent("POST", "/core/v4/addresses/setup")+api.sent("POST", "/core/v4/addresses") != 0 {
		t.Error("an address was asked for despite the refusal")
	}
}

// Any other name on a short domain is an ordinary additional address, and goes
// the ordinary way.
func TestAddressCreateAddsAnotherShortDomainAddressTheOrdinaryWay(t *testing.T) {
	api := &addressAPI{member: "member", shortDomains: []string{"pm.me"}}
	s := New(api, account(t, "alice", true, keys.Address{
		ID: "first", Email: "alice@proton.me", Order: 1,
		Keys: []keys.Key{{ID: "key", Active: 1, Primary: 1}},
	}))

	_, _ = s.AddressCreate(context.Background(), "bob", "pm.me", "")

	if api.sent("POST", "/core/v4/addresses") != 1 {
		t.Errorf("an address that is not the account's own name did not go the ordinary way: %v", api.requests)
	}
	if api.sent("POST", "/core/v4/addresses/setup") != 0 {
		t.Error("an ordinary address was set up as the short-domain one")
	}
}

// An account that holds post-quantum keys is refused before an address is made,
// because an address this build could make would be weaker than the rest and
// nothing afterwards would say so.
func TestAddressCreateRefusesAPostQuantumAccount(t *testing.T) {
	api := &addressAPI{postQuantum: true, member: "member"}
	s := New(api, hierarchy(t))

	_, err := s.AddressCreate(context.Background(), "work", "example.com", "")
	if err == nil {
		t.Fatal("an address was made on an account whose keys this cannot make")
	}
	var problem *errs.Problem
	if !errors.As(err, &problem) || problem.ExitCode() != errs.ExitUnsupported {
		t.Errorf("the refusal exits %v, want the unsupported code", err)
	}
	if api.sent("POST", "/core/v4/addresses") != 0 {
		t.Error("an address was created despite the refusal")
	}
}

// An address that already has a key is somebody else's answer to the same
// request, and saying so is more use than making a second one.
func TestAddressCreateRefusesAnAddressThatAlreadyHasAKey(t *testing.T) {
	api := &addressAPI{member: "member"}
	s := New(api, hierarchy(t, keys.Address{
		ID: "address", Email: "work@example.com", Keys: []keys.Key{{ID: "key", Active: 1, Primary: 1}},
	}))

	_, err := s.AddressCreate(context.Background(), "work", "example.com", "")
	var exists *errs.Exists
	if !errors.As(err, &exists) {
		t.Fatalf("AddressCreate = %v, want the address to be reported as already there", err)
	}
	if api.sent("POST", "/core/v4/addresses") != 0 {
		t.Error("a second address was created for the same email")
	}
}

// A domain the account cannot use is named as the problem, with what it could
// have been - and the list is only asked for once the create has failed.
func TestAddressCreateNamesTheDomainsAnAccountCanUse(t *testing.T) {
	api := &addressAPI{
		member:        "member",
		refusal:       &proton.APIError{HTTPStatus: 422, Code: 2001, Message: "Invalid domain"},
		protonDomains: []string{"proton.me", "protonmail.com"},
		customDomains: []string{"example.com"},
		shortDomains:  []string{"pm.me"},
	}
	s := New(api, account(t, "alice", true))

	_, err := s.AddressCreate(context.Background(), "work", "nowhere.example", "")
	if err == nil {
		t.Fatal("an address on a domain the account cannot use was accepted")
	}
	if !strings.Contains(err.Error(), "nowhere.example") {
		t.Errorf("the refusal does not name the domain: %v", err)
	}
	hints := strings.Join(err.(*errs.Problem).Hints(), " ")
	for _, domain := range []string{"proton.me", "protonmail.com", "example.com", "pm.me"} {
		if !strings.Contains(hints, domain) {
			t.Errorf("the refusal does not offer %s: %v", domain, hints)
		}
	}
}

// A short domain is only offered to an account that could put an address on
// one, so a free plan is not sent after something it cannot have.
func TestAddressCreateOffersTheShortDomainsOnlyToAPaidPlan(t *testing.T) {
	api := &addressAPI{
		member:        "member",
		refusal:       &proton.APIError{HTTPStatus: 422, Code: 2001, Message: "Invalid domain"},
		protonDomains: []string{"proton.me"},
		shortDomains:  []string{"pm.me"},
	}
	s := New(api, account(t, "alice", false))

	_, err := s.AddressCreate(context.Background(), "work", "nowhere.example", "")
	if err == nil {
		t.Fatal("an address on a domain the account cannot use was accepted")
	}
	if hints := strings.Join(err.(*errs.Problem).Hints(), " "); strings.Contains(hints, "pm.me") {
		t.Errorf("a free plan was offered a short domain: %v", hints)
	}
}

// The order is written whole, in the order it was given: the first address is
// the default, so a list that arrived rearranged would change which one that is.
func TestAddressesReorderWritesEveryAddressInTheOrderGiven(t *testing.T) {
	api := &addressAPI{}
	s := New(api, hierarchy(t))

	if err := s.AddressesReorder(context.Background(), []string{"c", "a", "b"}); err != nil {
		t.Fatalf("AddressesReorder: %v", err)
	}
	body := api.body("/core/v4/addresses/order")
	ids, _ := body["AddressIDs"].([]string)
	if len(ids) != 3 || ids[0] != "c" || ids[1] != "a" || ids[2] != "b" {
		t.Errorf("the order written is %v, want c, a, b", body["AddressIDs"])
	}
}

// A create Proton refuses for its own reasons keeps Proton's sentence: the
// domain list is only worth putting in front of somebody when the domain is
// what was wrong.
func TestAddressCreateKeepsProtonsRefusalWhenTheDomainIsFine(t *testing.T) {
	api := &addressAPI{
		member:        "member",
		refusal:       &proton.APIError{HTTPStatus: 422, Code: 2011, Message: "Maximum number of addresses reached"},
		protonDomains: []string{"proton.me"},
		customDomains: []string{"example.com"},
	}
	s := New(api, hierarchy(t))

	_, err := s.AddressCreate(context.Background(), "work", "example.com", "")
	if err == nil {
		t.Fatal("a create Proton refused came back as a success")
	}
	if !strings.Contains(err.Error(), "Maximum number of addresses") {
		t.Errorf("Proton's own reason was replaced: %v", err)
	}
}

// Proton takes an address down before it will delete it, so deleting one that
// is still enabled is two requests in the order Proton expects.
func TestAddressDeleteTakesTheAddressDownFirst(t *testing.T) {
	api := &addressAPI{}
	s := New(api, hierarchy(t))

	if err := s.AddressDelete(context.Background(), Address{ID: "a", Status: StatusEnabled}); err != nil {
		t.Fatalf("AddressDelete: %v", err)
	}
	if api.sent("PUT", "/core/v4/addresses/a/disable") != 1 {
		t.Error("an enabled address was deleted without being taken down first")
	}
	if api.sent("PUT", "/core/v4/addresses/a/delete") != 1 {
		t.Error("the address was not deleted")
	}

	api.requests = nil
	if err := s.AddressDelete(context.Background(), Address{ID: "a", Status: StatusDisabled}); err != nil {
		t.Fatalf("AddressDelete: %v", err)
	}
	if api.sent("PUT", "/core/v4/addresses/a/disable") != 0 {
		t.Error("an address that was already down was taken down again")
	}
}

// An address with no key is inert: it cannot send, whatever else Proton says
// about it.
func TestAnAddressWithoutAKeyCannotSend(t *testing.T) {
	usable := Address{Status: StatusEnabled, Send: 1, Receive: 1, HasKeys: true}
	if !usable.CanSend() {
		t.Error("an enabled address with a key cannot send")
	}
	inert := usable
	inert.HasKeys = false
	if inert.CanSend() {
		t.Error("an address with no key reports that it can send")
	}
}
