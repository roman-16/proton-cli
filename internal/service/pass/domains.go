package pass

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/dns"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
)

// The domains an alias can be made on, and the ones the account brings itself.
//
// Proton offers a handful of its own. A paid account may add a domain it owns,
// which carries no alias until the entries Proton asks for are in its zone -
// Pass's "Domains" settings, catch-all and all. Both kinds are the account's
// rather than a vault's, so neither takes a share.

// The entries Proton expects at every custom alias domain. Only the value that
// proves the domain is yours differs from one domain to the next, and that one
// arrives with the domain.
const (
	aliasMXPrimary   = "mx1.alias.proton.me"
	aliasMXSecondary = "mx2.alias.proton.me"
	aliasSPF         = "v=spf1 include:alias.proton.me ~all"
	aliasDKIMTarget  = "alias.proton.me"
	aliasDMARCHost   = "_dmarc"
	aliasDMARC       = "v=DMARC1; p=quarantine; pct=100; adkim=s; aspf=s"
)

// aliasDKIMHosts are the three selectors Proton signs alias mail under.
var aliasDKIMHosts = []string{"dkim._domainkey", "dkim02._domainkey", "dkim03._domainkey"}

// The states a custom domain is reported in: nobody has proved it is theirs,
// it is theirs and no mail reaches it yet, or it carries aliases.
const (
	DomainUnverified   = "unverified"
	DomainUnconfigured = "unconfigured"
	DomainActive       = "active"
)

// The words a check reports when it is not passing: nothing where the entry
// should be, or something else.
const (
	checkMissing = "missing"
	checkWrong   = "wrong"
)

// Domain is a domain an alias can be made on: one of Proton's, or one of your
// own.
type Domain struct {
	Domain  string `json:"domain"`
	Default bool   `json:"default"`
	Premium bool   `json:"premium"`
	Custom  bool   `json:"custom"`
	// What follows is a custom domain's alone. One of Proton's has no number,
	// no state to be in and no count of its own.
	ID      int    `json:"id,omitempty"`
	Status  string `json:"status,omitempty"`
	Aliases int    `json:"aliases,omitempty"`
	Created int64  `json:"created,omitempty"`

	// verification is the value that proves the domain is yours, which the
	// ownership entry is made of.
	verification string
	verified     bool
}

// Ref is what a mutation reports acting on: the number Proton files a custom
// domain under, and nothing for one of Proton's own.
func (d Domain) Ref() string {
	if !d.Custom {
		return ""
	}
	return strconv.Itoa(d.ID)
}

// Verified reports whether Proton has seen the entry that proves the domain is
// yours, which is what its settings wait for.
func (d Domain) Verified() bool { return d.verified }

// DomainDetail is a custom domain in full: every entry its zone needs with the
// verdict Proton just reached on it, and the settings a verified one has.
type DomainDetail struct {
	Domain
	DNS []dns.Check `json:"dns"`
	// Settings is nil until the domain is verified: there is nothing to set on a
	// domain nobody has proved is theirs.
	Settings *DomainSettings `json:"settings,omitempty"`
}

// DomainSettings is what a verified custom domain does with mail to a name no
// alias has claimed, and how the aliases made on it are dressed.
type DomainSettings struct {
	// CatchAll says whether mail to any name at the domain makes an alias on
	// the spot, and Mailboxes are where such an alias forwards.
	CatchAll  bool     `json:"catch_all"`
	Mailboxes []string `json:"mailboxes"`
	// DisplayName is what recipients see on mail from an alias on the domain,
	// unless the alias says otherwise.
	DisplayName string `json:"display_name,omitempty"`
	// RandomPrefix says whether an alias made on the domain gets a random word
	// in front.
	RandomPrefix bool `json:"random_prefix"`
}

// customDomain is a domain of the account's own as Proton writes it.
type customDomain struct {
	ID                 int
	Domain             string
	VerificationRecord string
	OwnershipVerified  bool
	MxVerified         bool
	DkimVerified       bool
	SpfVerified        bool
	DmarcVerified      bool
	AliasCount         int
	CreateTime         int64
}

func (c customDomain) domain() Domain {
	return Domain{
		Domain: c.Domain, Custom: true, ID: c.ID, Aliases: c.AliasCount,
		Created: c.CreateTime, Status: customStatus(c.OwnershipVerified, c.MxVerified),
		verification: c.VerificationRecord, verified: c.OwnershipVerified,
	}
}

// customStatus is the one word a listing has for a custom domain, in the order
// its owner has to work through: prove it is theirs, then point its mail here.
func customStatus(ownership, mx bool) string {
	switch {
	case !ownership:
		return DomainUnverified
	case !mx:
		return DomainUnconfigured
	}
	return DomainActive
}

// Domains lists every domain an alias can be made on, and every domain of the
// account's own on its way to being one.
//
// Proton keeps the two apart - the domains that work, and the custom domains
// whatever their state - and a custom domain is in both once its mail arrives.
// They are read together and joined by name, so a domain nobody has verified yet
// is in the listing with the state it is in rather than missing from it.
func (s *Service) Domains(ctx context.Context) ([]Domain, error) {
	var offered struct {
		Domains []struct {
			Domain     string
			IsCustom   bool
			IsDefault  bool
			IsPremium  bool
			MXVerified bool
		}
	}
	var own []customDomain
	if err := fetch.Together(ctx, func(ctx context.Context) error {
		return s.C.Decode(ctx, proton.Request{
			Method: "GET", Path: "/pass/v1/user/alias/domain",
		}, &offered)
	}, func(ctx context.Context) error {
		var err error
		own, err = s.customDomains(ctx)
		return err
	}); err != nil {
		return nil, err
	}
	custom := make(map[string]Domain, len(own))
	for _, c := range own {
		custom[strings.ToLower(c.Domain)] = c.domain()
	}
	out := make([]Domain, 0, len(offered.Domains)+len(own))
	for _, o := range offered.Domains {
		d := Domain{Domain: o.Domain, Default: o.IsDefault, Premium: o.IsPremium, Custom: o.IsCustom}
		if c, ok := custom[strings.ToLower(o.Domain)]; ok {
			c.Default = o.IsDefault
			d = c
			delete(custom, strings.ToLower(o.Domain))
		}
		out = append(out, d)
	}
	for _, d := range custom {
		out = append(out, d)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}

// customDomains reads every domain of the account's own, page by page.
func (s *Service) customDomains(ctx context.Context) ([]customDomain, error) {
	var out []customDomain
	var lastID *int
	for {
		var r struct {
			CustomDomains struct {
				Domains []customDomain
				LastID  *int
				Total   int
			}
		}
		req := proton.Request{Method: "GET", Path: "/pass/v1/user/alias/custom_domain"}
		if lastID != nil {
			req.Query = proton.Query("LastID", strconv.Itoa(*lastID))
		}
		if err := s.C.Decode(ctx, req, &r); err != nil {
			return nil, err
		}
		out = append(out, r.CustomDomains.Domains...)
		if len(r.CustomDomains.Domains) == 0 || len(out) >= r.CustomDomains.Total || r.CustomDomains.LastID == nil {
			return out, nil
		}
		lastID = r.CustomDomains.LastID
	}
}

// DomainCheck reads a custom domain's DNS again and hands back every entry with
// the verdict Proton reached on it, and its settings once it is verified.
//
// Proton answers a check with what it found where each entry should be, which
// is the one thing that tells a value not yet propagated from a value typed
// wrong - so a failing check says which.
func (s *Service) DomainCheck(ctx context.Context, d Domain) (*DomainDetail, error) {
	var r struct {
		CustomDomainValidation struct {
			OwnershipVerified  bool
			VerificationErrors []string
			MxVerified         bool
			MxErrors           []string
			SpfVerified        bool
			SpfErrors          []string
			DkimVerified       bool
			DkimErrors         []string
			DmarcVerified      bool
			DmarcErrors        []string
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d", d.ID), Reads: true,
	}, &r); err != nil {
		return nil, err
	}
	v := r.CustomDomainValidation
	dkim := make([]dns.Record, 0, len(aliasDKIMHosts))
	for _, host := range aliasDKIMHosts {
		dkim = append(dkim, dns.Record{Type: "CNAME", Host: host, Value: host + "." + aliasDKIMTarget})
	}
	detail := &DomainDetail{Domain: d, DNS: []dns.Check{{
		Name: "ownership", Status: verdict(v.OwnershipVerified, v.VerificationErrors), Found: found(v.OwnershipVerified, v.VerificationErrors),
		Records: []dns.Record{{Type: "TXT", Host: dns.Apex, Value: d.verification}},
	}, {
		Name: "mx", Status: verdict(v.MxVerified, v.MxErrors), Found: found(v.MxVerified, v.MxErrors),
		Records: []dns.Record{
			{Type: "MX", Host: dns.Apex, Value: aliasMXPrimary, Priority: 10},
			{Type: "MX", Host: dns.Apex, Value: aliasMXSecondary, Priority: 20},
		},
	}, {
		Name: "spf", Status: verdict(v.SpfVerified, v.SpfErrors), Found: found(v.SpfVerified, v.SpfErrors),
		Records: []dns.Record{{Type: "TXT", Host: dns.Apex, Value: aliasSPF}},
	}, {
		Name: "dkim", Status: verdict(v.DkimVerified, v.DkimErrors), Found: found(v.DkimVerified, v.DkimErrors),
		Records: dkim,
	}, {
		Name: "dmarc", Status: verdict(v.DmarcVerified, v.DmarcErrors), Found: found(v.DmarcVerified, v.DmarcErrors),
		Records: []dns.Record{{Type: "TXT", Host: aliasDMARCHost, Value: aliasDMARC}},
	}}}
	detail.Status = customStatus(v.OwnershipVerified, v.MxVerified)
	detail.verified = v.OwnershipVerified
	if !v.OwnershipVerified {
		return detail, nil
	}
	settings, err := s.domainSettings(ctx, d.ID)
	if err != nil {
		return nil, err
	}
	detail.Settings = settings
	return detail, nil
}

// verdict is the word for one check: passing, or failing with nothing found, or
// failing with something else in the entry's place.
func verdict(verified bool, errors []string) string {
	switch {
	case verified:
		return dns.Ok
	case len(errors) > 0:
		return checkWrong
	}
	return checkMissing
}

// found is what Proton saw instead, which only a failing check has.
func found(verified bool, errors []string) []string {
	if verified || len(errors) == 0 {
		return nil
	}
	return errors
}

// domainSettings reads what a verified domain does with stray mail.
func (s *Service) domainSettings(ctx context.Context, id int) (*DomainSettings, error) {
	var r struct{ Settings rawDomainSettings }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d/settings", id),
	}, &r); err != nil {
		return nil, err
	}
	return r.Settings.settings(), nil
}

// rawDomainSettings is a domain's settings as Proton writes them.
type rawDomainSettings struct {
	CatchAll  bool
	Mailboxes []struct {
		ID    int
		Email string
	}
	DefaultDisplayName     *string
	RandomPrefixGeneration bool
}

func (r rawDomainSettings) settings() *DomainSettings {
	out := &DomainSettings{
		CatchAll: r.CatchAll, RandomPrefix: r.RandomPrefixGeneration,
		Mailboxes: make([]string, 0, len(r.Mailboxes)),
	}
	for _, m := range r.Mailboxes {
		out.Mailboxes = append(out.Mailboxes, m.Email)
	}
	if r.DefaultDisplayName != nil {
		out.DisplayName = *r.DefaultDisplayName
	}
	return out
}

// DomainCreate adds a domain of the account's own for aliases to be made on.
//
// The plan is asked first, because Proton's own refusal says nothing anybody
// can act on and a paid plan is the whole of what is missing. The domain
// carries no alias until the entry it comes back with is in its zone, so the
// domain is the answer rather than its number alone.
func (s *Service) DomainCreate(ctx context.Context, name string) (*DomainDetail, error) {
	limits, err := s.Limits(ctx)
	if err != nil {
		return nil, err
	}
	if !limits.AliasDomains {
		return nil, errs.Problemf("Custom domains need a paid Pass plan.")
	}
	var r struct{ CustomDomain customDomain }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/user/alias/custom_domain",
		Body: map[string]any{"Domain": name},
	}, &r); err != nil {
		return nil, err
	}
	d := r.CustomDomain.domain()
	return &DomainDetail{Domain: d, DNS: []dns.Check{{
		Name:    "ownership",
		Records: []dns.Record{{Type: "TXT", Host: dns.Apex, Value: d.verification}},
	}}}, nil
}

// DomainDelete gives a custom domain up, and every alias made on it with it.
func (s *Service) DomainDelete(ctx context.Context, id int) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "DELETE", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d", id),
	}, nil)
}

// DomainSetDefault picks the domain a new alias is made on when nothing says
// otherwise. Nil leaves none chosen.
func (s *Service) DomainSetDefault(ctx context.Context, domain *string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/user/alias/settings/default_alias_domain",
		Body: map[string]any{"DefaultAliasDomain": domain},
	}, nil)
}

// DomainCatchAll has mail to any name at the domain make an alias on the spot,
// forwarding to the mailboxes named. Naming none turns it off.
//
// Proton keeps the switch and the mailboxes behind two endpoints, so turning it
// on is two requests: where such mail goes, and then that it goes at all.
func (s *Service) DomainCatchAll(ctx context.Context, id int, mailboxIDs []int) error {
	if len(mailboxIDs) > 0 {
		if err := s.C.Decode(ctx, proton.Request{
			Method: "PUT", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d/settings/mailboxes", id),
			Body: map[string]any{"MailboxIDs": mailboxIDs},
		}, nil); err != nil {
			return err
		}
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d/settings/catch_all", id),
		Body: map[string]any{"CatchAll": len(mailboxIDs) > 0},
	}, nil)
}

// DomainDisplayName sets what recipients see on mail from an alias on the
// domain, unless the alias says otherwise. Empty takes it off.
func (s *Service) DomainDisplayName(ctx context.Context, id int, name string) error {
	var value *string
	if name != "" {
		value = &name
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d/settings/name", id),
		Body: map[string]any{"Name": value},
	}, nil)
}

// DomainRandomPrefix says whether an alias made on the domain gets a random word
// in front.
func (s *Service) DomainRandomPrefix(ctx context.Context, id int, on bool) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: fmt.Sprintf("/pass/v1/user/alias/custom_domain/%d/settings/random_prefix", id),
		Body: map[string]any{"RandomPrefixGeneration": on},
	}, nil)
}
