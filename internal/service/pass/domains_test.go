package pass

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/dns"
	"github.com/roman-16/proton-cli/internal/proton"
)

// Proton keeps the domains that work apart from the custom domains whatever
// their state. The listing joins them, so a domain nobody has verified is in it
// with the state it is in, and one that has been is one row rather than two.

// domainDoer serves Proton's domains, the account's own, and one check, and
// captures every write.
type domainDoer struct {
	verified bool
	free     bool
	writes   []proton.Request
}

func (d *domainDoer) Do(_ context.Context, _ proton.Request) (*proton.Response, error) {
	return &proton.Response{Status: 200, Body: []byte(`{"Code":1000}`)}, nil
}

func (d *domainDoer) Decode(_ context.Context, r proton.Request, out any) error {
	var payload any
	switch {
	case r.Method == "GET" && r.Path == "/pass/v1/user/access":
		payload = map[string]any{"Access": map[string]any{"Plan": map[string]any{"ManageAlias": !d.free}}}
	case r.Method == "GET" && r.Path == "/pass/v1/user/alias/domain":
		payload = map[string]any{"Domains": []map[string]any{
			{"Domain": "passmail.net", "IsCustom": false, "IsDefault": true, "IsPremium": false, "MXVerified": true},
			{"Domain": "Mail.Example.com", "IsCustom": true, "IsDefault": false, "IsPremium": false, "MXVerified": true},
		}}
	case r.Method == "GET" && r.Path == "/pass/v1/user/alias/custom_domain":
		payload = map[string]any{"CustomDomains": map[string]any{
			"Domains": []map[string]any{
				{"ID": 7, "Domain": "mail.example.com", "VerificationRecord": "sl-verification=abc",
					"OwnershipVerified": true, "MxVerified": true, "AliasCount": 3, "CreateTime": 100},
				{"ID": 8, "Domain": "new.example.com", "VerificationRecord": "sl-verification=def",
					"OwnershipVerified": false, "MxVerified": false, "AliasCount": 0, "CreateTime": 200},
			},
			"LastID": 8, "Total": 2,
		}}
	case r.Method == "POST" && r.Path == "/pass/v1/user/alias/custom_domain":
		d.writes = append(d.writes, r)
		payload = map[string]any{"CustomDomain": map[string]any{
			"ID": 9, "Domain": r.Body.(map[string]any)["Domain"], "VerificationRecord": "sl-verification=new",
		}}
	case r.Method == "POST" && r.Path == "/pass/v1/user/alias/custom_domain/8":
		payload = map[string]any{"CustomDomainValidation": map[string]any{
			"OwnershipVerified": d.verified, "VerificationErrors": []string{},
			"MxVerified": false, "MxErrors": []string{"10 mail.protonmail.ch."},
			"SpfVerified": false, "SpfErrors": []string{},
			"DkimVerified": true, "DkimErrors": []string{},
			"DmarcVerified": false, "DmarcErrors": []string{},
		}}
	case r.Method == "GET" && r.Path == "/pass/v1/user/alias/custom_domain/8/settings":
		payload = map[string]any{"Settings": map[string]any{
			"ID": 8, "CatchAll": true, "Mailboxes": []map[string]any{{"ID": 1, "Email": "me@proton.me"}},
			"DefaultDisplayName": "Jane R", "RandomPrefixGeneration": false,
		}}
	case r.Method == "GET":
		return nil
	default:
		d.writes = append(d.writes, r)
		return nil
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}

func TestDomainsJoinProtonsWithTheAccountsOwn(t *testing.T) {
	rows, err := New(&domainDoer{}, testKeys(nil)).Domains(context.Background())
	if err != nil {
		t.Fatalf("Domains: %v", err)
	}
	var names []string
	for _, d := range rows {
		names = append(names, d.Domain)
	}
	if !slices.Equal(names, []string{"mail.example.com", "new.example.com", "passmail.net"}) {
		t.Fatalf("domains = %v", names)
	}
	own, fresh, proton := rows[0], rows[1], rows[2]
	if !own.Custom || own.ID != 7 || own.Aliases != 3 || own.Status != DomainActive || !own.Verified() || own.Ref() != "7" {
		t.Errorf("a verified custom domain reads %+v", own)
	}
	if !fresh.Custom || fresh.Status != DomainUnverified || fresh.Verified() || fresh.Ref() != "8" {
		t.Errorf("a custom domain nobody has verified reads %+v", fresh)
	}
	if proton.Custom || !proton.Default || proton.Ref() != "" || proton.Status != "" {
		t.Errorf("one of Proton's domains reads %+v", proton)
	}
}

// A check that failed says whether nothing was there or something else was,
// and only then does a domain have settings to read.
func TestDomainCheckReadsEveryVerdictAndWhatWasFoundInstead(t *testing.T) {
	d := &domainDoer{verified: true}
	s := New(d, testKeys(nil))
	rows, err := s.Domains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.DomainCheck(context.Background(), rows[1])
	if err != nil {
		t.Fatalf("DomainCheck: %v", err)
	}
	checks := map[string]dns.Check{}
	var order []string
	for _, c := range detail.DNS {
		checks[c.Name] = c
		order = append(order, c.Name)
	}
	if !slices.Equal(order, []string{"ownership", "mx", "spf", "dkim", "dmarc"}) {
		t.Errorf("checks = %v", order)
	}
	if got := checks["ownership"]; got.Status != dns.Ok || got.Records[0].Value != "sl-verification=def" {
		t.Errorf("ownership = %+v", got)
	}
	if got := checks["mx"]; got.Status != checkWrong || !slices.Equal(got.Found, []string{"10 mail.protonmail.ch."}) {
		t.Errorf("mx = %+v", got)
	}
	if got := checks["spf"]; got.Status != checkMissing || got.Found != nil {
		t.Errorf("spf = %+v", got)
	}
	if got := checks["dkim"]; got.Status != dns.Ok || len(got.Records) != 3 ||
		got.Records[1] != (dns.Record{Type: "CNAME", Host: "dkim02._domainkey", Value: "dkim02._domainkey.alias.proton.me"}) {
		t.Errorf("dkim = %+v", got)
	}
	if detail.Status != DomainUnconfigured {
		t.Errorf("a domain proved but not pointed here reads %q", detail.Status)
	}
	if detail.Settings == nil || !detail.Settings.CatchAll || detail.Settings.DisplayName != "Jane R" ||
		!slices.Equal(detail.Settings.Mailboxes, []string{"me@proton.me"}) {
		t.Errorf("settings = %+v", detail.Settings)
	}
}

func TestADomainNobodyHasVerifiedHasNoSettingsToRead(t *testing.T) {
	d := &domainDoer{verified: false}
	s := New(d, testKeys(nil))
	rows, err := s.Domains(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.DomainCheck(context.Background(), rows[1])
	if err != nil {
		t.Fatalf("DomainCheck: %v", err)
	}
	if detail.Settings != nil {
		t.Errorf("an unverified domain came back with settings: %+v", detail.Settings)
	}
	if detail.Status != DomainUnverified || detail.Verified() {
		t.Errorf("status = %q, verified = %v", detail.Status, detail.Verified())
	}
}

// Turning the catch-all on says where first and then that it is on; turning it
// off touches nothing but the switch.
func TestDomainCatchAllWritesWhereBeforeWhether(t *testing.T) {
	d := &domainDoer{}
	s := New(d, testKeys(nil))
	if err := s.DomainCatchAll(context.Background(), 8, []int{1, 2}); err != nil {
		t.Fatal(err)
	}
	if len(d.writes) != 2 || !strings.HasSuffix(d.writes[0].Path, "/settings/mailboxes") ||
		!strings.HasSuffix(d.writes[1].Path, "/settings/catch_all") {
		t.Fatalf("turning it on wrote %v", paths(d.writes))
	}
	if on, _ := d.writes[1].Body.(map[string]any)["CatchAll"].(bool); !on {
		t.Error("the switch was written off")
	}
	d.writes = nil
	if err := s.DomainCatchAll(context.Background(), 8, nil); err != nil {
		t.Fatal(err)
	}
	if len(d.writes) != 1 || !strings.HasSuffix(d.writes[0].Path, "/settings/catch_all") {
		t.Fatalf("turning it off wrote %v", paths(d.writes))
	}
	if on, _ := d.writes[0].Body.(map[string]any)["CatchAll"].(bool); on {
		t.Error("the switch was written on")
	}
}

// A display name taken off travels as nothing rather than as an empty word.
func TestDomainDisplayNameClearsWithNull(t *testing.T) {
	d := &domainDoer{}
	s := New(d, testKeys(nil))
	if err := s.DomainDisplayName(context.Background(), 8, ""); err != nil {
		t.Fatal(err)
	}
	if got := d.writes[0].Body.(map[string]any)["Name"]; got != (*string)(nil) {
		t.Errorf("clearing sent %#v, want nil", got)
	}
	if err := s.DomainDisplayName(context.Background(), 8, "Jane R"); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.writes[1].Body.(map[string]any)["Name"].(*string); got == nil || *got != "Jane R" {
		t.Errorf("setting sent %#v", d.writes[1].Body)
	}
}

// Adding a domain hands back the one entry that proves it is yours, and a plan
// that cannot have one is told so before anything is sent.
func TestDomainCreateHandsBackTheOwnershipEntryOrRefusesThePlan(t *testing.T) {
	d := &domainDoer{}
	s := New(d, testKeys(nil))
	created, err := s.DomainCreate(context.Background(), "new.example.com")
	if err != nil {
		t.Fatalf("DomainCreate: %v", err)
	}
	if len(created.DNS) != 1 || created.DNS[0].Name != "ownership" || created.DNS[0].Status != "" ||
		created.DNS[0].Records[0] != (dns.Record{Type: "TXT", Host: "@", Value: "sl-verification=new"}) {
		t.Errorf("a new domain comes with %+v, want the ownership entry alone", created.DNS)
	}
	if created.Ref() != "9" || created.Status != DomainUnverified {
		t.Errorf("a new domain reads %+v", created.Domain)
	}
	if len(d.writes) != 1 || d.writes[0].Body.(map[string]any)["Domain"] != "new.example.com" {
		t.Errorf("wrote %v", paths(d.writes))
	}

	free := &domainDoer{free: true}
	_, err = New(free, testKeys(nil)).DomainCreate(context.Background(), "new.example.com")
	if err == nil || !strings.Contains(err.Error(), "paid Pass plan") {
		t.Errorf("a free plan was refused with %v, want the plan named", err)
	}
	if len(free.writes) != 0 {
		t.Error("a domain was added for a plan that cannot have one")
	}
}

func TestACustomDomainsStateIsTheNextThingItsOwnerHasToDo(t *testing.T) {
	for _, tc := range []struct {
		ownership, mx bool
		want          string
	}{
		{false, false, DomainUnverified},
		{false, true, DomainUnverified},
		{true, false, DomainUnconfigured},
		{true, true, DomainActive},
	} {
		if got := customStatus(tc.ownership, tc.mx); got != tc.want {
			t.Errorf("customStatus(%v, %v) = %q, want %q", tc.ownership, tc.mx, got, tc.want)
		}
	}
}

func paths(reqs []proton.Request) []string {
	out := make([]string, 0, len(reqs))
	for _, r := range reqs {
		out = append(out, r.Method+" "+r.Path)
	}
	return out
}
