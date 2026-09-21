package mail

import (
	"context"
	"errors"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/proton"
)

// noOrganization is Proton's answer for an account that is not in one, which is
// every account without a plan.
const noOrganization = 2501

// orNoPlan replaces Proton's refusal when the account could not have had the
// thing in the first place.
//
// A plan puts the account in an organization of its own and a free account in
// none, so the question is asked only once a request has already failed, and
// only to say which kind of failure it was. Anything else is left to say what it
// is: a refusal nobody can act on is worse than Proton's own sentence.
//
// How many domains the plan allows is the signal, because that is what the
// things asking are: a custom domain, and a token that sends from an address on
// one. What names them opens the sentence, so it is plural and capitalised.
func (s *Service) orNoPlan(ctx context.Context, refusal error, what string) error {
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
		return errs.Problemf("%s need a paid Mail plan.", what)
	}
	// Recorded and not counted: Proton's own refusal is on the screen either way,
	// and this line is what says why it was not improved on.
	slog.DebugContext(ctx, "the account's plan could not be read", "error", err.Error())
	return refusal
}
