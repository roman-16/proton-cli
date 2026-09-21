package mail

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"sort"

	"github.com/roman-16/proton-cli/internal/dns"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// A custom domain is one the account owns and carries its own mail under -
// Proton's "Domain names" settings page.
//
// The DNS entries it needs are carried with the domain rather than asked for:
// three of the five groups are the same for every account, and the two that are
// not - the verification code and the DKIM hostnames - arrive with the domain
// itself. What Proton last saw in their place is the group's status, which is
// why the entries and the verdicts are one answer and not two.

// The entries Proton expects at every custom domain.
const (
	mxPrimary   = "mail.protonmail.ch"
	mxSecondary = "mailsec.protonmail.ch"
	spfValue    = "v=spf1 include:_spf.protonmail.ch ~all"
	dmarcHost   = "_dmarc"
	dmarcValue  = "v=DMARC1; p=quarantine"
)

// The words each check reports, from Proton's own state numbers. A number
// outside the set reads as unknown rather than as a pass: a check whose verdict
// cannot be read is not a check that succeeded.
var (
	domainStates = map[int]string{0: "unverified", 1: "active", 2: "warning"}
	verifyStates = map[int]string{0: "missing", 1: "wrong", 2: "ok"}
	mxStates     = map[int]string{0: "missing", 1: "wrong", 2: "wrong-priority", 3: "ok", 4: "backup"}
	spfStates    = map[int]string{0: "missing", 1: "wrong", 2: "duplicate", 3: "ok"}
	dkimStates   = map[int]string{0: "missing", 1: "wrong", 2: "wrong", 3: "error", 4: "ok", 5: "delegated", 6: "warning"}
	dmarcStates  = map[int]string{0: "missing", 1: "wrong", 2: "duplicate", 3: "ok", 4: "relaxed"}
)

// Domain is one custom domain, its DNS entries and how each of them stands.
type Domain struct {
	ID     string `json:"id"`
	Domain string `json:"domain"`
	Status string `json:"status"`
	// Addresses is how many of the account's addresses are on the domain, and
	// CatchAll is whichever of them takes mail sent to a name it has not got.
	Addresses int    `json:"addresses"`
	CatchAll  string `json:"catch_all,omitempty"`
	// Checked is when Proton last read the domain's DNS.
	Checked int64       `json:"checked,omitempty"`
	DNS     []dns.Check `json:"dns"`
}

// Failing names the checks that are not passing, in the order they are shown.
func (d Domain) Failing() []string { return dns.Failing(d.DNS) }

// rawDomain is the domain as Proton writes it, with its own numbers.
type rawDomain struct {
	ID          string
	DomainName  string
	State       int
	CheckTime   int64
	VerifyCode  string
	VerifyState int
	MxState     int
	SpfState    int
	DmarcState  int
	DKIM        struct {
		State  int
		Config []struct {
			Hostname string
			CNAME    string
		}
	}
}

func (r rawDomain) domain() Domain {
	return Domain{
		ID:      r.ID,
		Domain:  r.DomainName,
		Status:  stateWord(domainStates, r.State),
		Checked: r.CheckTime,
		DNS:     r.dns(),
	}
}

func (r rawDomain) dns() []dns.Check {
	dkim := make([]dns.Record, 0, len(r.DKIM.Config))
	for _, c := range r.DKIM.Config {
		dkim = append(dkim, dns.Record{Type: "CNAME", Host: c.Hostname, Value: c.CNAME})
	}
	return []dns.Check{{
		Name: "verification", Status: stateWord(verifyStates, r.VerifyState),
		Records: []dns.Record{{Type: "TXT", Host: dns.Apex, Value: r.VerifyCode}},
	}, {
		Name: "mx", Status: stateWord(mxStates, r.MxState),
		Records: []dns.Record{
			{Type: "MX", Host: dns.Apex, Value: mxPrimary, Priority: 10},
			{Type: "MX", Host: dns.Apex, Value: mxSecondary, Priority: 20},
		},
	}, {
		Name: "spf", Status: stateWord(spfStates, r.SpfState),
		Records: []dns.Record{{Type: "TXT", Host: dns.Apex, Value: spfValue}},
	}, {
		Name: "dkim", Status: stateWord(dkimStates, r.DKIM.State), Records: dkim,
	}, {
		Name: "dmarc", Status: stateWord(dmarcStates, r.DmarcState),
		Records: []dns.Record{{Type: "TXT", Host: dmarcHost, Value: dmarcValue}},
	}}
}

func stateWord(words map[int]string, n int) string {
	if w, ok := words[n]; ok {
		return w
	}
	return "unknown"
}

// DomainsList is every custom domain on the account, as the last DNS check left
// it.
//
// The address listing is read too, because it carries which domain each address
// is on and which one catches mail addressed to nobody. Both are facts about a
// domain that Proton files with the address, so taking them from a listing the
// account already needs is what keeps this two requests rather than one a
// domain.
func (s *Service) DomainsList(ctx context.Context) ([]Domain, error) {
	var r struct{ Domains []rawDomain }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/domains"}, &r); err != nil {
		return nil, s.orNoPlanForDomains(ctx, err)
	}
	addrs, err := s.AddressesList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Domain, 0, len(r.Domains))
	for _, raw := range r.Domains {
		d := raw.domain()
		countAddresses(&d, addrs)
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

// DomainRefresh reads a domain with its DNS looked at again, rather than as the
// last check left it.
//
// What it is handed back keeps the addresses of the domain it was given: the
// re-check is about DNS, and the addresses were read by whatever resolved the
// reference.
func (s *Service) DomainRefresh(ctx context.Context, d Domain) (Domain, error) {
	var r struct{ Domain rawDomain }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/domains/" + d.ID,
		Query: url.Values{"Refresh": {"1"}},
	}, &r); err != nil {
		return Domain{}, err
	}
	fresh := r.Domain.domain()
	fresh.Addresses, fresh.CatchAll = d.Addresses, d.CatchAll
	return fresh, nil
}

// DomainCreate adds a custom domain to the account.
//
// The domain carries no mail until the entries it comes back with are in its
// zone, so the domain it returns is the answer rather than its ID alone.
func (s *Service) DomainCreate(ctx context.Context, name string) (Domain, error) {
	var r struct{ Domain rawDomain }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/domains",
		Body: map[string]any{"Name": name, "AllowedForMail": true, "AllowedForSSO": false},
	}, &r); err != nil {
		return Domain{}, err
	}
	return r.Domain.domain(), nil
}

// DomainDelete gives a custom domain up. Every address on it stops sending and
// receiving.
func (s *Service) DomainDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/domains/" + id}, nil)
}

// DomainCatchAll points a domain at the address that takes mail sent to a name
// it has not got. A nil address turns it off, and such mail is refused again.
func (s *Service) DomainCatchAll(ctx context.Context, domainID string, addressID *string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/domains/" + domainID + "/catchall",
		Body: map[string]any{"AddressID": addressID},
	}, nil)
}

// noOrganization is Proton's answer for an account that is not in one, which is
// every account without a plan.
const noOrganization = 2501

// orNoPlanForDomains replaces Proton's refusal when the account could not have
// had a custom domain in the first place.
//
// A plan puts the account in an organization of its own and a free account in
// none, so the question is asked only once a listing has already failed, and
// only to say which kind of failure it was. Anything else is left to say what it
// is: a refusal nobody can act on is worse than Proton's own sentence.
func (s *Service) orNoPlanForDomains(ctx context.Context, refusal error) error {
	var r struct {
		Organization struct{ MaxDomains int }
	}
	err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/core/v4/organizations", Reads: true,
	}, &r)
	var api *proton.APIError
	switch {
	case err == nil && r.Organization.MaxDomains > 0:
		return refusal
	case err == nil, errors.As(err, &api) && api.Code == noOrganization:
		return errs.Problemf("Custom domains need a paid Mail plan.")
	}
	// Recorded and not counted: Proton's own refusal is on the screen either way,
	// and this line is what says why it was not improved on.
	slog.DebugContext(ctx, "domains: the account's plan could not be read", "error", err.Error())
	return refusal
}

// countAddresses fills in what the address listing says about a domain.
func countAddresses(d *Domain, addrs []Address) {
	for _, a := range addrs {
		if a.DomainID != d.ID {
			continue
		}
		d.Addresses++
		if a.CatchAll {
			d.CatchAll = a.Email
		}
	}
}
