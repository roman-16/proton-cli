package mail

import (
	"context"

	"github.com/roman-16/proton-cli/internal/proton"
)

// sieveVersion is the Sieve dialect version Proton's filter endpoints expect.
const sieveVersion = 2

type Filter struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  int    `json:"status"`
	Version int    `json:"version"`
}

// FilterDetail is one filter in full: what it is called, whether it is running,
// and what it does - as a rule where that can be said, and as the script Proton
// runs either way.
type FilterDetail struct {
	Filter
	Sieve string `json:"sieve"`
	// Rule is how the filter reads as conditions and actions, and is absent for a
	// script that says more than those can.
	Rule *FilterRule `json:"rule,omitempty"`
}

// FilterGet reads one filter.
func (s *Service) FilterGet(ctx context.Context, id string) (*FilterDetail, error) {
	var r struct {
		Filter struct {
			ID, Name, Sieve string
			Status, Version int
			Tree            []any
		}
	}
	if err := s.C.Decode(ctx, proton.Request{
		Method: "GET", Path: "/mail/v4/filters/" + id,
	}, &r); err != nil {
		return nil, err
	}
	out := &FilterDetail{
		Filter: Filter{ID: r.Filter.ID, Name: r.Filter.Name, Status: r.Filter.Status, Version: r.Filter.Version},
		Sieve:  r.Filter.Sieve,
	}
	if rule, ok := RuleOf(r.Filter.Tree); ok {
		out.Rule = &rule
	}
	return out, nil
}

func (s *Service) FiltersList(ctx context.Context) ([]Filter, error) {
	var r struct{ Filters []Filter }
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/filters"}, &r); err != nil {
		return nil, err
	}
	return r.Filters, nil
}

// FilterCreate adds a filter, turned on.
//
// Proton has no say over that at creation - the endpoint takes a name, a script
// and a version, and nothing else - so a filter that should start off is turned
// off afterwards with FilterDisable.
func (s *Service) FilterCreate(ctx context.Context, name, sieve string) (string, error) {
	var r struct{ Filter struct{ ID string } }
	if err := s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/filters",
		Body: map[string]any{"Name": name, "Sieve": sieve, "Version": sieveVersion},
	}, &r); err != nil {
		return "", err
	}
	return r.Filter.ID, nil
}

// FilterUpdate changes a filter's name and/or sieve script, preserving its
// current enabled/disabled status (use FilterEnable/FilterDisable for that).
func (s *Service) FilterUpdate(ctx context.Context, id, name, sieve string) error {
	var cur struct {
		Filter struct {
			Name, Sieve     string
			Version, Status int
		}
	}
	if err := s.C.Decode(ctx, proton.Request{Method: "GET", Path: "/mail/v4/filters/" + id}, &cur); err != nil {
		return err
	}
	if name == "" {
		name = cur.Filter.Name
	}
	if sieve == "" {
		sieve = cur.Filter.Sieve
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/filters/" + id,
		Body: map[string]any{"Name": name, "Sieve": sieve, "Version": sieveVersion, "Status": cur.Filter.Status},
	}, nil)
}

// FilterUpdateRule rewrites what a filter matches and does.
//
// The whole rule is replaced rather than patched, because half of a rule is not
// one - the same reason FilterReorder takes every filter. Its place in the order
// and whether it is running are Proton's to keep, and it keeps them.
func (s *Service) FilterUpdateRule(ctx context.Context, id, name string, rule FilterRule) error {
	current, err := s.FilterGet(ctx, id)
	if err != nil {
		return err
	}
	if name == "" {
		name = current.Name
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/filters/" + id,
		Body: map[string]any{
			"Name": name, "Version": sieveVersion,
			"Status": current.Status, "Tree": rule.tree(),
		},
	}, nil)
}

func (s *Service) FilterDelete(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "DELETE", Path: "/mail/v4/filters/" + id}, nil)
}

func (s *Service) FilterEnable(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/mail/v4/filters/" + id + "/enable"}, nil)
}

func (s *Service) FilterDisable(ctx context.Context, id string) error {
	return s.C.Decode(ctx, proton.Request{Method: "PUT", Path: "/mail/v4/filters/" + id + "/disable"}, nil)
}

// FilterApply runs filters over mail that is already in the mailbox.
//
// A filter ordinarily acts once, as mail arrives, so a rule written today does
// nothing about what came yesterday. This is how the web client's "apply to
// existing messages" catches up - the same rules, run again over what is
// already there.
//
// With no IDs it runs every enabled filter; with them, only those.
func (s *Service) FilterApply(ctx context.Context, ids []string) error {
	body := map[string]any{
		"AllFilters": 1,
		"FilterIDs":  []string{},
	}
	if len(ids) > 0 {
		body["AllFilters"] = 0
		body["FilterIDs"] = ids
	}
	return s.C.Decode(ctx, proton.Request{
		Method: "POST", Path: "/mail/v4/messages/apply-filters", Body: body,
		Transient: []proton.Refusal{mailboxBusy},
	}, nil)
}

// FilterReorder sets the order filters run in.
//
// Order decides the outcome: the first rule to file a message wins, so moving one
// above another changes where mail lands. The list is the whole order, not a
// patch, which is why this takes every filter rather than a pair to swap.
func (s *Service) FilterReorder(ctx context.Context, ids []string) error {
	return s.C.Decode(ctx, proton.Request{
		Method: "PUT", Path: "/mail/v4/filters/order",
		Body: map[string]any{"FilterIDs": ids},
	}, nil)
}
