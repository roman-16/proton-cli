package mail

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/account/plan"
	"github.com/roman-16/proton-cli/internal/dns"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// The domain Proton hands back, as it writes it. Its states are the passing
// ones, so a test that wants a failing check moves the one it is about.
const rawDomainJSON = `{
  "ID": "dom-1",
  "DomainName": "example.com",
  "State": 1,
  "CheckTime": 1789377630,
  "VerifyCode": "protonmail-verification=abc123",
  "VerifyState": 2,
  "MxState": 3,
  "SpfState": 3,
  "DmarcState": 3,
  "DKIM": {
    "State": 4,
    "Config": [
      {"Hostname": "protonmail._domainkey", "CNAME": "protonmail.domainkey.k1.domains.proton.ch."},
      {"Hostname": "protonmail2._domainkey", "CNAME": "protonmail2.domainkey.k1.domains.proton.ch."},
      {"Hostname": "protonmail3._domainkey", "CNAME": "protonmail3.domainkey.k1.domains.proton.ch."}
    ]
  }
}`

func decodeRawDomain(t *testing.T) rawDomain {
	t.Helper()
	var raw rawDomain
	if err := json.Unmarshal([]byte(rawDomainJSON), &raw); err != nil {
		t.Fatalf("decode the domain: %v", err)
	}
	return raw
}

// The entries a domain needs are the same five groups whatever state it is in,
// in the order somebody sets them up: prove you own it, then receive, then be
// believed when you send.
func TestADomainCarriesEveryEntryItNeeds(t *testing.T) {
	d := decodeRawDomain(t).domain()

	var names []string
	for _, g := range d.DNS {
		names = append(names, g.Name)
	}
	want := []string{"verification", "mx", "spf", "dkim", "dmarc"}
	if !slices.Equal(names, want) {
		t.Errorf("checks = %v, want %v", names, want)
	}

	entries := map[string][]dns.Record{}
	for _, g := range d.DNS {
		entries[g.Name] = g.Records
	}
	if got := entries["verification"]; len(got) != 1 ||
		got[0] != (dns.Record{Type: "TXT", Host: "@", Value: "protonmail-verification=abc123"}) {
		t.Errorf("verification entry = %+v", got)
	}
	if got := entries["mx"]; len(got) != 2 ||
		got[0] != (dns.Record{Type: "MX", Host: "@", Value: "mail.protonmail.ch", Priority: 10}) ||
		got[1] != (dns.Record{Type: "MX", Host: "@", Value: "mailsec.protonmail.ch", Priority: 20}) {
		t.Errorf("MX entries = %+v", got)
	}
	if got := entries["spf"]; len(got) != 1 || got[0].Value != "v=spf1 include:_spf.protonmail.ch ~all" {
		t.Errorf("SPF entry = %+v", got)
	}
	if got := entries["dkim"]; len(got) != 3 ||
		got[2] != (dns.Record{Type: "CNAME", Host: "protonmail3._domainkey",
			Value: "protonmail3.domainkey.k1.domains.proton.ch."}) {
		t.Errorf("DKIM entries = %+v", got)
	}
	if got := entries["dmarc"]; len(got) != 1 ||
		got[0] != (dns.Record{Type: "TXT", Host: "_dmarc", Value: "v=DMARC1; p=quarantine"}) {
		t.Errorf("DMARC entry = %+v", got)
	}
}

func TestADomainThatPassesEveryCheckSaysSo(t *testing.T) {
	d := decodeRawDomain(t).domain()
	if d.Status != "active" {
		t.Errorf("status = %q, want active", d.Status)
	}
	if failing := d.Failing(); len(failing) != 0 {
		t.Errorf("failing = %v, want none", failing)
	}
	for _, g := range d.DNS {
		if g.Status != dns.Ok {
			t.Errorf("%s = %q, want ok", g.Name, g.Status)
		}
	}
}

// Each check counts from its own numbers, so the same number means different
// things to different checks and reading one with another's table would report a
// pass that never happened.
func TestEachCheckReadsItsOwnNumbers(t *testing.T) {
	for _, tt := range []struct {
		name  string
		apply func(*rawDomain)
		want  map[string]string
	}{
		{"nothing in DNS at all", func(r *rawDomain) {
			r.State, r.VerifyState, r.MxState, r.SpfState, r.DmarcState = 0, 0, 0, 0, 0
			r.DKIM.State = 0
		}, map[string]string{
			"verification": "missing", "mx": "missing", "spf": "missing",
			"dkim": "missing", "dmarc": "missing",
		}},
		{"one short of each", func(r *rawDomain) {
			r.VerifyState, r.MxState, r.SpfState, r.DmarcState = 1, 2, 2, 2
			r.DKIM.State = 6
		}, map[string]string{
			"verification": "wrong", "mx": "wrong-priority", "spf": "duplicate",
			"dkim": "warning", "dmarc": "duplicate",
		}},
		{"passing in the ways that are not plain ok", func(r *rawDomain) {
			r.MxState, r.DmarcState = 4, 4
			r.DKIM.State = 5
		}, map[string]string{"mx": "backup", "dkim": "delegated", "dmarc": "relaxed"}},
		{"a number no check declares", func(r *rawDomain) {
			r.VerifyState, r.MxState, r.SpfState, r.DmarcState = 99, 99, 99, 99
			r.DKIM.State = 99
		}, map[string]string{
			"verification": "unknown", "mx": "unknown", "spf": "unknown",
			"dkim": "unknown", "dmarc": "unknown",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw := decodeRawDomain(t)
			tt.apply(&raw)
			got := map[string]string{}
			for _, g := range raw.domain().DNS {
				got[g.Name] = g.Status
			}
			for check, want := range tt.want {
				if got[check] != want {
					t.Errorf("%s = %q, want %q", check, got[check], want)
				}
			}
		})
	}
}

// A domain nobody has verified reads as unverified rather than as one more
// failing check, because it is the one state that says no address can go on it.
func TestADomainStateHasItsOwnWords(t *testing.T) {
	for state, want := range map[int]string{0: "unverified", 1: "active", 2: "warning", 7: "unknown"} {
		raw := decodeRawDomain(t)
		raw.State = state
		if got := raw.domain().Status; got != want {
			t.Errorf("state %d = %q, want %q", state, got, want)
		}
	}
}

// Failing names what to go and fix, in the order the entries are shown, so a
// listing's one cell reads as a list of jobs.
func TestFailingNamesTheChecksInTheOrderTheyAreShown(t *testing.T) {
	raw := decodeRawDomain(t)
	raw.DmarcState, raw.MxState = 0, 1
	if got := raw.domain().Failing(); !slices.Equal(got, []string{"mx", "dmarc"}) {
		t.Errorf("failing = %v, want [mx dmarc]", got)
	}
}

// How many addresses a domain has and which of them catches stray mail are both
// facts Proton files with the address, so they are read off the address listing
// rather than asked for one domain at a time.
func TestADomainCountsOnlyItsOwnAddresses(t *testing.T) {
	d := Domain{ID: "dom-1"}
	countAddresses(&d, []Address{
		{Email: "work@example.com", DomainID: "dom-1"},
		{Email: "stray@example.com", DomainID: "dom-1", CatchAll: true},
		{Email: "other@elsewhere.test", DomainID: "dom-2", CatchAll: true},
		{Email: "me@proton.me"},
	})
	if d.Addresses != 2 {
		t.Errorf("addresses = %d, want 2", d.Addresses)
	}
	if d.CatchAll != "stray@example.com" {
		t.Errorf("catch-all = %q, want stray@example.com", d.CatchAll)
	}
}

func TestADomainWithNoCatchAllNamesNobody(t *testing.T) {
	d := Domain{ID: "dom-1"}
	countAddresses(&d, []Address{{Email: "work@example.com", DomainID: "dom-1"}})
	if d.CatchAll != "" {
		t.Errorf("catch-all = %q, want nothing", d.CatchAll)
	}
}

// domainAPI answers the listing with whatever Proton would, and says whether the
// account is in an organization.
type domainAPI struct {
	refusal error
	// organization is what /core/v4/organizations answers with, or nil for the
	// account that is not in one.
	organization error
	maxDomains   int
	paths        []string
}

func (a *domainAPI) Do(context.Context, proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (a *domainAPI) Decode(_ context.Context, req proton.Request, out any) error {
	a.paths = append(a.paths, req.Path)
	switch req.Path {
	case "/domains":
		if a.refusal != nil {
			return a.refusal
		}
		return json.Unmarshal([]byte(`{"Code":1000,"Domains":[]}`), out)
	case "/core/v4/organizations":
		if a.organization != nil {
			return a.organization
		}
		return json.Unmarshal(fmt.Appendf(nil,
			`{"Code":1000,"Organization":{"MaxDomains":%d}}`, a.maxDomains), out)
	}
	return json.Unmarshal([]byte(`{"Code":1000,"Addresses":[]}`), out)
}

// An account with no plan is in no organization, and Proton answers its listing
// with a server error - which says nothing a person can act on. What is put in
// its place is the one thing that is true of every such account.
func TestADomainListingWithoutAPlanSaysWhatIsMissing(t *testing.T) {
	api := &domainAPI{
		refusal:      &proton.APIError{HTTPStatus: 500, Message: "Internal server error"},
		organization: &proton.APIError{HTTPStatus: 422, Code: plan.NoOrganization, Message: "not a member"},
	}
	_, err := New(api, nil).DomainsList(context.Background())
	var problem *errs.Problem
	if !errors.As(err, &problem) {
		t.Fatalf("DomainsList = %v, want a refusal somebody can act on", err)
	}
	if !strings.Contains(problem.Error(), "paid Mail plan") {
		t.Errorf("the refusal does not say what is missing: %v", problem)
	}
}

// The plan is asked about only once the listing has already failed, so an
// account that has one pays nothing for the question.
func TestADomainListingThatWorksNeverAsksAboutThePlan(t *testing.T) {
	api := &domainAPI{}
	if _, err := New(api, nil).DomainsList(context.Background()); err != nil {
		t.Fatalf("DomainsList = %v", err)
	}
	if slices.Contains(api.paths, "/core/v4/organizations") {
		t.Errorf("the plan was asked about for nothing: %v", api.paths)
	}
}

// An account that has a plan and still could not list is Proton having a bad
// moment, and its own sentence is the better one.
func TestADomainListingThatFailsWithAPlanKeepsProtonsAnswer(t *testing.T) {
	refusal := &proton.APIError{HTTPStatus: 500, Message: "Internal server error"}
	api := &domainAPI{refusal: refusal, maxDomains: 3}
	_, err := New(api, nil).DomainsList(context.Background())
	if !errors.Is(err, error(refusal)) {
		t.Errorf("DomainsList = %v, want Proton's own answer", err)
	}
}
