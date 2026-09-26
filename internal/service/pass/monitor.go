package pass

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"time"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/fetch"
	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/skip"
)

// Pass Monitor's other half: which of your addresses have turned up in a data
// breach.
//
// Proton watches three kinds of address - the ones the account owns, the
// hide-my-email aliases in your vaults, and the ones you add yourself - and says
// how many breaches each has appeared in and what was exposed. The full detail
// is a paid feature, and an account without it is told so by Proton rather than
// by a guess made here.
//
// An alias is not an address Proton holds a record of: it is an item, and what
// is known about it comes off the item's own flags. So a listing that includes
// aliases reads the vaults, which is what `items list` costs.

// The three kinds of watched address, which decide what a reference means and
// which endpoint answers for it.
const (
	AddressProton = "proton"
	AddressCustom = "custom"
	AddressAlias  = "alias"
)

// MonitoredAddress is one address Proton watches, and what it found.
type MonitoredAddress struct {
	// AddressID is what addresses this row at Proton: an address ID, a custom
	// email ID, or - for an alias, which is an item - its share and item IDs as
	// one token.
	AddressID string `json:"address_id"`
	Email     string `json:"email"`
	// Type is which of the three kinds this is.
	Type string `json:"type"`
	// Breaches is how many breaches the address has appeared in, and nil where
	// the count did not come back.
	Breaches *int `json:"breaches"`
	// LastBreach is when the most recent one happened, as a Unix time, or zero
	// when the address is clean.
	LastBreach int64 `json:"last_breach,omitempty"`
	// Monitored is false for an address whose watching was switched off.
	Monitored bool `json:"monitored"`
	// Verified is whether an address added by hand has had its code handed back.
	// Proton watches nothing until it has.
	Verified bool `json:"verified"`
	// State is the one word for what has to happen next, if anything, before
	// Proton is watching this address: watched, paused or unverified.
	State string `json:"state"`

	// shareID and itemID are the alias behind this row, for the endpoints that
	// address an alias rather than an address.
	shareID, itemID string
}

// The three states a watched address can be in.
const (
	StatePaused     = "paused"
	StateUnverified = "unverified"
	StateWatched    = "watched"
)

// settle works out the row's state, and is the only thing that does.
//
// It is called once, where rows leave this package, rather than at each of the
// three places one is built - a state worked out in three places is three places
// that can come to disagree about what "paused" means.
func (a *MonitoredAddress) settle() {
	switch {
	case !a.Verified:
		a.State = StateUnverified
	case !a.Monitored:
		a.State = StatePaused
	default:
		a.State = StateWatched
	}
}

// monitoringDisabled is the flag Proton sets on an address it has been told to
// stop watching.
const monitoringDisabled = 1 << 0

// Breach is one leak an address appeared in.
type Breach struct {
	ID string `json:"id"`
	// Name is the breach as Proton names it, usually the service that lost the
	// data.
	Name  string `json:"name"`
	Email string `json:"email"`
	// Severity is Proton's judgement, as one of low, medium or high. It arrives
	// as a number on a scale and is banded here, because a number between zero
	// and one is not something to act on.
	Severity string `json:"severity"`
	// Published is when the breach happened, as far as Proton knows, as a Unix
	// time.
	Published int64 `json:"published,omitempty"`
	// Exposed is what was leaked - an address, a password, a date of birth.
	Exposed []string `json:"exposed"`
	// Source is where it came from, when that is known.
	Source string `json:"source,omitempty"`
	// Size is roughly how many records the breach held.
	Size int `json:"size,omitempty"`
	// PasswordTail is the last few characters of the leaked password, when it
	// was leaked in the clear. It is what tells you which password to change.
	PasswordTail string `json:"password_tail,omitempty"`
	// Resolved is whether the alert has been dealt with.
	Resolved bool `json:"resolved"`
}

// BreachReport is what one address's breaches came back as, including the part
// Proton declined to send.
type BreachReport struct {
	Breaches []Breach
	// Withheld is how many breaches Proton counts for this address but would not
	// describe, because the plan does not include the detail. Anything above
	// zero means the list beside it is short.
	Withheld int
}

// severityBands are Proton's own boundaries on a nought-to-one scale.
func severityWord(v float64) string {
	switch {
	case v < 0.33:
		return "low"
	case v < 0.67:
		return "medium"
	default:
		return "high"
	}
}

// breachAlertResolved is the state Proton uses for an alert somebody has dealt
// with; the other two are unread and read.
const breachAlertResolved = 3

type rawAddress struct {
	AddressID      string
	CustomEmailID  string
	Email          string
	Flags          int
	Verified       bool
	BreachCounter  int
	LastBreachTime *int64
}

// Monitored lists every address Proton watches for this account - the ones it
// owns, the aliases in its vaults, and the ones added by hand - worst first.
func (s *Service) Monitored(ctx context.Context) ([]MonitoredAddress, error) {
	var (
		addresses []MonitoredAddress
		aliases   []MonitoredAddress
	)
	err := fetch.Together(ctx,
		func(ctx context.Context) error {
			var err error
			addresses, err = s.watchedAddresses(ctx)
			return err
		},
		func(ctx context.Context) error {
			var err error
			aliases, err = s.watchedAliases(ctx)
			return err
		},
	)
	if err != nil {
		return nil, err
	}

	out := append(addresses, aliases...)
	for i := range out {
		out[i].settle()
	}
	// Worst first, because the reason to run this is to find what to deal with.
	slices.SortStableFunc(out, ByBreaches)
	return out, nil
}

func ByBreaches(a, b MonitoredAddress) int { return cmp.Compare(breachRank(b), breachRank(a)) }

func breachRank(a MonitoredAddress) int {
	switch {
	case a.Breaches == nil:
		return 1
	case *a.Breaches == 0:
		return 0
	}
	return *a.Breaches + 1
}

func known(count int) *int { return &count }

// watchedAddresses is the addresses Proton holds a record of: the account's own
// and the ones added by hand.
func (s *Service) watchedAddresses(ctx context.Context) ([]MonitoredAddress, error) {
	var r struct {
		Breaches struct {
			Addresses    []rawAddress
			CustomEmails []rawAddress
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/pass/v1/breach"}, &r); err != nil {
		return nil, err
	}

	out := make([]MonitoredAddress, 0,
		len(r.Breaches.Addresses)+len(r.Breaches.CustomEmails))
	for _, group := range []struct {
		rows   []rawAddress
		custom bool
	}{{r.Breaches.Addresses, false}, {r.Breaches.CustomEmails, true}} {
		for _, a := range group.rows {
			id, kind, verified := a.AddressID, AddressProton, true
			if group.custom {
				id, kind, verified = a.CustomEmailID, AddressCustom, a.Verified
			}
			row := MonitoredAddress{
				AddressID: id, Email: a.Email, Type: kind, Breaches: known(a.BreachCounter),
				Monitored: a.Flags&monitoringDisabled == 0, Verified: verified,
			}
			if a.LastBreachTime != nil {
				row.LastBreach = *a.LastBreachTime
			}
			out = append(out, row)
		}
	}
	return out, nil
}

// watchedAliases is the hide-my-email aliases in this account's vaults.
//
// Whether an alias is watched and whether it has been in a breach are both flags
// on the item, so the listing itself costs no request beyond reading the vaults.
// How many breaches, and when, is only on the far side of a request per address,
// so it is asked for the aliases that have actually been in one - which is
// almost none of them.
func (s *Service) watchedAliases(ctx context.Context) ([]MonitoredAddress, error) {
	items, err := s.ItemsList(ctx, "")
	if err != nil {
		return nil, err
	}
	rows := aliasRows(items)
	s.fillAliasBreaches(ctx, rows)
	return rows, nil
}

func aliasRows(items []Item) []MonitoredAddress {
	var out []MonitoredAddress
	for _, it := range items {
		if it.Type != "alias" || it.Alias == "" {
			continue
		}
		row := MonitoredAddress{
			AddressID: ref.Join(it.ShareID, it.ItemID), Email: it.Alias, Type: AddressAlias,
			Monitored: !it.Excluded, Verified: true,
			shareID: it.ShareID, itemID: it.ItemID,
		}
		if !it.breached {
			row.Breaches = known(0)
		}
		out = append(out, row)
	}
	return out
}

func (s *Service) fillAliasBreaches(ctx context.Context, rows []MonitoredAddress) {
	var counts []func(context.Context) error
	for i := range rows {
		if rows[i].Breaches == nil {
			counts = append(counts, s.countAliasBreaches(&rows[i]))
		}
	}
	_ = fetch.Together(ctx, counts...)
}

// countAliasBreaches fills in how many breaches an alias is in and when the last
// one was, which is on the far side of a request of its own.
//
// An alias whose count will not come back is still an alias that has been in a
// breach, so the row stays with its count unknown: dropping it, or calling the
// count zero, would turn a failed request into an address that looks clean.
func (s *Service) countAliasBreaches(row *MonitoredAddress) func(context.Context) error {
	return func(ctx context.Context) error {
		report, err := s.BreachesFor(ctx, *row)
		if err != nil {
			// Recorded and not counted: the row is listed, and its count reads as
			// unknown on the screen and in JSON.
			slog.DebugContext(ctx, "pass: an alias's breach count did not come back",
				"kind", string(skip.KindAddress), "reason", string(skip.Unreadable),
				"ref", row.AddressID, "error", err)
			return nil
		}
		row.Breaches = known(len(report.Breaches) + report.Withheld)
		for _, b := range report.Breaches {
			if b.Published > row.LastBreach {
				row.LastBreach = b.Published
			}
		}
		return nil
	}
}

// BreachesFor lists the breaches one watched address appeared in, newest first.
//
// The three kinds of address are asked for at three different paths, so which it
// is has to be known: Monitored says.
//
// Proton refuses the question outright for an address whose code has not been
// handed back, so the caller settles that before asking.
//
// Proton sends the detail only to an account whose plan includes it, and answers
// the rest with a count and a handful of samples. That count is carried back
// rather than dropped: a list of no breaches under an address with three of them
// is a wrong answer, and only the count says so.
func (s *Service) BreachesFor(ctx context.Context, address MonitoredAddress) (*BreachReport, error) {
	var r struct {
		Breaches struct {
			IsEligible bool
			Count      int
			Breaches   []struct {
				ID                string
				Email             string
				ResolvedState     int
				Severity          float64
				Name              string
				PublishedAt       string
				Size              *int
				PasswordLastChars *string
				ExposedData       []struct{ Name string }
				Source            struct {
					IsAggregated bool
					Domain       *string
				}
			}
		}
	}
	// The three paths are written out rather than chosen into a variable,
	// because the guard that checks every request the CLI can send is reachable
	// reads them off the source and cannot see through a variable.
	req := proton.Request{
		Method: "GET", Path: "/pass/v1/breach/address/" + address.AddressID + "/breaches",
	}
	switch address.Type {
	case AddressCustom:
		req = proton.Request{
			Method: "GET", Path: "/pass/v1/breach/custom_email/" + address.AddressID + "/breaches",
		}
	case AddressAlias:
		req = proton.Request{
			Method: "GET",
			Path:   "/pass/v1/share/" + address.shareID + "/alias/" + address.itemID + "/breaches",
		}
	}
	if err := s.C.Decode(ctx, req, &r); err != nil {
		return nil, err
	}

	report := &BreachReport{Breaches: make([]Breach, 0, len(r.Breaches.Breaches))}
	for _, b := range r.Breaches.Breaches {
		row := Breach{
			ID: b.ID, Name: b.Name, Email: b.Email,
			Severity: severityWord(b.Severity),
			Resolved: b.ResolvedState == breachAlertResolved,
		}
		if at, err := time.Parse(time.RFC3339, b.PublishedAt); err == nil {
			row.Published = at.Unix()
		}
		for _, e := range b.ExposedData {
			row.Exposed = append(row.Exposed, e.Name)
		}
		switch {
		case b.Source.Domain != nil && *b.Source.Domain != "":
			row.Source = *b.Source.Domain
		case b.Source.IsAggregated:
			row.Source = "several sources"
		}
		if b.Size != nil {
			row.Size = *b.Size
		}
		if b.PasswordLastChars != nil {
			row.PasswordTail = *b.PasswordLastChars
		}
		report.Breaches = append(report.Breaches, row)
	}
	sort.SliceStable(report.Breaches, func(i, j int) bool {
		return report.Breaches[i].Published > report.Breaches[j].Published
	})
	if !r.Breaches.IsEligible && r.Breaches.Count > len(report.Breaches) {
		report.Withheld = r.Breaches.Count - len(report.Breaches)
	}
	return report, nil
}

// WatchAddress asks Proton to watch an address this account does not own.
//
// Proton emails the address a code and watches nothing until the code comes
// back, which is what VerifyAddress is for.
func (s *Service) WatchAddress(ctx context.Context, email string) (*MonitoredAddress, error) {
	var r struct{ Email rawAddress }
	err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/pass/v1/breach/custom_email",
		Body: map[string]string{"Email": email},
	}, &r)
	if err != nil {
		return nil, errs.Naming(email, err)
	}
	added := &MonitoredAddress{
		AddressID: r.Email.CustomEmailID, Email: r.Email.Email, Type: AddressCustom,
		Breaches: known(r.Email.BreachCounter),
		Verified: r.Email.Verified, Monitored: r.Email.Flags&monitoringDisabled == 0,
	}
	added.settle()
	return added, nil
}

// VerifyAddress hands back the code Proton emailed an address added by hand.
func (s *Service) VerifyAddress(ctx context.Context, address MonitoredAddress, code string) error {
	if err := s.custom(address, "verify"); err != nil {
		return err
	}
	_, err := s.C.Do(ctx, proton.Request{
		Method: "PUT", Path: "/pass/v1/breach/custom_email/" + address.AddressID + "/verify",
		Body: map[string]string{"Code": code},
	})
	return err
}

// ResendVerification asks Proton to email the code again.
func (s *Service) ResendVerification(ctx context.Context, address MonitoredAddress) error {
	if err := s.custom(address, "resend the code to"); err != nil {
		return err
	}
	_, err := s.C.Do(ctx, proton.Request{
		Method: "POST",
		Path:   "/pass/v1/breach/custom_email/" + address.AddressID + "/resend_verification",
	})
	return err
}

// StopWatching removes an address added by hand, and the breach history with it.
func (s *Service) StopWatching(ctx context.Context, address MonitoredAddress) error {
	if err := s.custom(address, "stop watching"); err != nil {
		return err
	}
	_, err := s.C.Do(ctx, proton.Request{
		Method: "DELETE", Path: "/pass/v1/breach/custom_email/" + address.AddressID,
	})
	return err
}

// custom refuses the three commands that only an address added by hand answers
// to, before any of them reaches the network.
func (s *Service) custom(address MonitoredAddress, action string) error {
	if address.Type == AddressCustom {
		return nil
	}
	return errs.Naming(address.Email, errs.Problemf(
		"only an address you added yourself can be %s; this one is %s",
		action, addressOrigin(address.Type)))
}

func addressOrigin(kind string) string {
	if kind == AddressAlias {
		return "an alias in one of your vaults"
	}
	return "an address on your account"
}

// SetMonitored turns Proton's watching of one address on or off.
//
// An alias has no record of its own: watching it is an item flag, and it is the
// same flag that excludes a login from the password checks. So this is the one
// place that writes it, and anything else that wants to is this.
func (s *Service) SetMonitored(ctx context.Context, address MonitoredAddress, on bool) error {
	switch address.Type {
	case AddressAlias:
		return s.SetHealthChecked(ctx, address.shareID, address.itemID, on)
	case AddressCustom:
		_, err := s.C.Do(ctx, proton.Request{
			Method: "PUT", Path: "/pass/v1/breach/custom_email/" + address.AddressID + "/monitor",
			Body: map[string]bool{"Monitor": on},
		})
		return err
	default:
		_, err := s.C.Do(ctx, proton.Request{
			Method: "PUT", Path: "/pass/v1/breach/address/" + address.AddressID + "/monitor",
			Body: map[string]bool{"Monitor": on},
		})
		return err
	}
}

// SetHealthChecked writes the one item flag that decides whether Proton's
// security checks apply to an item - which for an alias is the same thing as
// whether its address is watched for breaches.
func (s *Service) SetHealthChecked(ctx context.Context, shareID, itemID string, on bool) error {
	var r struct{ Item json.RawMessage }
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT",
		Path:   fmt.Sprintf("/pass/v1/share/%s/item/%s/flags", shareID, itemID),
		Body:   map[string]bool{"SkipHealthCheck": !on},
	}, &r)
}
