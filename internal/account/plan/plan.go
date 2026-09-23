// Package plan reads what the account's plan allows.
//
// Proton keeps a plan on the organization an account belongs to, and a free
// account belongs to none. Two questions are asked of it - how many domains a
// plan allows, and which plan it is - and both are answered by one request, so
// the request and the way Proton says "no organization" are written once, here.
package plan

import (
	"context"
	"errors"

	"github.com/roman-16/proton-cli/internal/proton"
)

// Plan is the part of the account's organization a command decides by.
type Plan struct {
	// Name is Proton's name for the plan, such as "bundle2022". It is empty for an
	// account on no plan.
	Name string `json:"PlanName"`
	// MaxDomains is how many domains of its own the plan lets the account bring.
	MaxDomains int
}

// NoOrganization is Proton's answer for an account that is not in one, which is
// every account without a plan.
const NoOrganization = 2501

// Read asks for the account's plan. An account on none reads as the zero Plan,
// since that is an answer about the account and not a failure to get one.
func Read(ctx context.Context, c proton.Doer) (Plan, error) {
	var r struct{ Organization Plan }
	err := c.Decode(ctx, proton.Request{Method: "GET", Path: "/core/v4/organizations"}, &r)
	var api *proton.APIError
	if errors.As(err, &api) && api.Code == NoOrganization {
		return Plan{}, nil
	}
	if err != nil {
		return Plan{}, err
	}
	return r.Organization, nil
}
