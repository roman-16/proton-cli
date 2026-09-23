package mail

import (
	"context"
	"log/slog"

	"github.com/roman-16/proton-cli/internal/account/plan"
	"github.com/roman-16/proton-cli/internal/errs"
)

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
	current, err := plan.Read(ctx, s.C)
	switch {
	case err == nil && current.MaxDomains > 0:
		return refusal
	case err == nil:
		return errs.Problemf("%s need a paid Mail plan.", what)
	}
	// Recorded and not counted: Proton's own refusal is on the screen either way,
	// and this line is what says why it was not improved on.
	slog.DebugContext(ctx, "the account's plan could not be read", "error", err.Error())
	return refusal
}
