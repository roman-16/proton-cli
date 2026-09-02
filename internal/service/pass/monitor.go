package pass

import (
	"context"
	"sort"
	"time"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Pass Monitor: which of your addresses have turned up in a data breach.
//
// Proton watches the addresses on the account and the ones you add yourself, and
// says how many breaches each has appeared in and what was exposed. It is a
// paid feature, and a free account is told so by Proton rather than by a guess
// made here.
//
// Nothing about this writes: it reports what somebody else already leaked.

// MonitoredAddress is one address Proton watches, and what it found.
type MonitoredAddress struct {
	AddressID string `json:"address_id"`
	Email     string `json:"email"`
	// Breaches is how many breaches the address has appeared in.
	Breaches int `json:"breaches"`
	// LastBreach is when the most recent one happened, as a Unix time, or zero
	// when the address is clean.
	LastBreach int64 `json:"last_breach,omitempty"`
	// Monitored is false for an address whose watching was switched off.
	Monitored bool `json:"monitored"`
	// Custom marks an address added by hand rather than one the account owns.
	Custom bool `json:"custom"`
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
	BreachCounter  int
	LastBreachTime *int64
}

// Monitored lists every address Proton watches for this account, the ones it
// owns and the ones added by hand, worst first.
func (s *Service) Monitored(ctx context.Context) ([]MonitoredAddress, error) {
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
			id := a.AddressID
			if group.custom {
				id = a.CustomEmailID
			}
			row := MonitoredAddress{
				AddressID: id, Email: a.Email, Breaches: a.BreachCounter,
				Monitored: a.Flags&monitoringDisabled == 0, Custom: group.custom,
			}
			if a.LastBreachTime != nil {
				row.LastBreach = *a.LastBreachTime
			}
			out = append(out, row)
		}
	}
	// Worst first, because the reason to run this is to find what to deal with.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Breaches > out[j].Breaches })
	return out, nil
}

// BreachesFor lists the breaches one watched address appeared in, newest first.
//
// An address the account owns and one added by hand are asked for at different
// paths, so which it is has to be known: Monitored says.
func (s *Service) BreachesFor(ctx context.Context, address MonitoredAddress) ([]Breach, error) {
	var r struct {
		Breaches struct {
			Breaches []struct {
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
	// The two paths are written out rather than chosen into a variable, because
	// the guard that checks every request the CLI can send is reachable reads
	// them off the source and cannot see through a variable.
	req := proton.Request{
		Method: "GET", Path: "/pass/v1/breach/address/" + address.AddressID + "/breaches",
	}
	if address.Custom {
		req = proton.Request{
			Method: "GET", Path: "/pass/v1/breach/custom_email/" + address.AddressID + "/breaches",
		}
	}
	if err := s.C.Decode(ctx, req, &r); err != nil {
		return nil, err
	}

	out := make([]Breach, 0, len(r.Breaches.Breaches))
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
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Published > out[j].Published })
	return out, nil
}
