package proton

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/roman-16/proton-cli/internal/errs"
)

// Scope elevation, mirroring how the web clients do it.
//
// Proton guards its most destructive endpoints behind an elevated session scope.
// A request that needs one and does not have it comes back as HTTP 403 with
// Proton code 9100, and the client is expected to prove the user is present by
// re-running SRP against a scope-granting endpoint, retry, and then drop the
// scope again.
//
// WebClients handles this in the API layer rather than in each feature: the
// api helper raises an "unlock" event, a password dialog appears, srpAuth calls
// queryUnlock(), and the original request is retried
// (packages/shared/lib/api/helpers/apiErrorHelper.ts,
// packages/components/containers/api/ApiModals.tsx).
//
// This client does the same, which is why no command in the CLI knows that any
// particular operation is password-guarded. The alternative - a per-command
// check - can only ever cover the operations someone remembered to annotate,
// and gets the scope wrong as easily as right.
//
// Pass is the third scope and the one that behaves differently: it is refused
// with a code of its own, answered with the password protecting Pass rather than
// the account's, and once granted it stays. The shape is the same all the same,
// which is why it is handled here and not in the Pass commands - see
// extrapassword.go for the exchange.

// Scope names an elevated capability a session can hold.
type Scope string

const (
	// ScopeLocked guards destructive data operations: deleting a calendar,
	// exporting a private key, reading a secret back out.
	ScopeLocked Scope = "locked"
	// ScopePassword guards changes to the credentials themselves: the password,
	// two-factor settings, the recovery phrase.
	ScopePassword Scope = "password"
	// ScopePass guards every Pass endpoint for an account whose Pass is protected
	// with an extra password. Unlike the other two it is proved with that password
	// rather than the account's, it is granted for the life of the session, and
	// nothing takes it back.
	ScopePass Scope = "pass"
)

// endpoint is the SRP-guarded call that grants the scope. Mirrors queryUnlock
// and unlockPasswordChanges in WebClients (packages/shared/lib/api/user.ts).
//
// ScopePass has none: it is granted by an exchange of its own, in
// extrapassword.go.
func (s Scope) endpoint() string {
	if s == ScopePassword {
		return "/core/v4/users/password"
	}
	return "/core/v4/users/unlock"
}

// lockPath drops every elevated scope again. WebClients calls this after a
// sensitive operation finishes (lockSensitiveSettings); leaving a session
// elevated for the rest of its life would make the elevation pointless.
const lockPath = "/core/v4/users/lock"

// scopesPath reports the scopes a session currently holds.
const scopesPath = "/core/v4/auth/scopes"

// ScopeCredentials is what an elevation needs from the person. A second factor
// is not part of it: whether Proton wants one is answered by the parameters the
// exchange fetches, so it is asked for then and not before.
//
// Username is what the account password is proved for, and is empty for the Pass
// extra password: SRP stopped hashing the username at version 3, and the
// verifier that one is checked against was written by a client at version 4.
type ScopeCredentials struct {
	Username string
	Password []byte
}

// ScopeResolver supplies credentials when the server reports a missing scope.
// Returning an error aborts the operation with that error, so a resolver that
// cannot ask (no terminal, --no-input) simply explains why.
type ScopeResolver func(ctx context.Context, s Scope) (ScopeCredentials, error)

// SetScopeResolver installs the callback used to elevate a session on demand.
func (c *Client) SetScopeResolver(r ScopeResolver) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.scopeResolver = r
}

func (c *Client) getScopeResolver() ScopeResolver {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.scopeResolver
}

// The codes Proton refuses an unelevated session with.
//
// missingScopeCode is SCOPE_MISSING_UNEXPECTED (packages/shared/lib/errors.ts).
// passScopeCode is what every Pass endpoint answers when the account has an
// extra password the session has not been given: WebClients reads it on its own
// (PassErrorCode.MISSING_SCOPE, packages/pass/lib/api/errors.ts).
const (
	missingScopeCode = 9100
	passScopeCode    = 9108
)

// isMissingScope reports whether a response is the server asking for an elevated
// session.
//
// WebClients matches on 9100 alone, which is not enough here: deleting a
// calendar answers 403 with code 9101 and no name for it anywhere in their
// source. What both answers share is Details.MissingScopes - the server saying
// which scope it wants - so that is the surer signal, and the code is only a
// fallback for an answer that names nothing.
//
// The Pass refusal is read from its code alone, whatever the status came out as.
// It is the one scope no status is known for - the web clients match it on the
// code and never look - and being wrong about the status would leave an account
// with an extra password unable to reach Pass at all.
func isMissingScope(status int, body []byte) bool {
	var env struct {
		Code    int
		Details struct{ MissingScopes []string }
	}
	if json.Unmarshal(body, &env) != nil {
		return false
	}
	if env.Code == passScopeCode {
		return true
	}
	if status != http.StatusForbidden {
		return false
	}
	return len(env.Details.MissingScopes) > 0 || env.Code == missingScopeCode
}

// Elevate re-authenticates within the current session to obtain s.
//
// The server's proof is verified here as it is at login. SRP authenticates both
// directions, and discarding the server's half would throw away the guarantee
// that we are talking to something which knows the verifier.
func (c *Client) Elevate(ctx context.Context, s Scope, cr ScopeCredentials) error {
	// The Pass exchange is not wrapped: it says what it is about in its own words,
	// and a sentence about the extra password reads worse with a scope name in
	// front of it.
	if s == ScopePass {
		return c.unlockPass(ctx, cr.Password)
	}
	if _, err := c.exchange(ctx, srpExchange{
		method: "PUT", path: s.endpoint(),
		username: cr.Username, password: cr.Password,
		scope: s, secondFactor: c.answerSecondFactor(ctx),
	}); err != nil {
		return fmt.Errorf("elevate to %s scope: %w", s, err)
	}
	return nil
}

// answerSecondFactor answers the challenge an elevation is met with.
//
// An account with a second factor is asked for one here rather than up front,
// because until the parameters arrive there is nothing to say Proton wants one -
// and asking anyway would spend a code on a guess, or a touch on nothing.
func (c *Client) answerSecondFactor(ctx context.Context) func(twoFA) (map[string]any, error) {
	return func(t twoFA) (map[string]any, error) {
		if t.Enabled == 0 {
			return nil, nil
		}
		offer := t.offer(c.host())
		if !offer.TOTP && offer.SecurityKey == nil {
			return nil, errs.Problemf("This account uses a two-factor method proton does not support (0x%x).",
				t.Enabled).Exit(2)
		}
		resolver := c.getSecondFactorResolver()
		if resolver == nil {
			return nil, errs.Problemf("This account needs a second factor, and there is nothing here to ask.").Exit(2)
		}
		answer, err := resolver(ctx, offer)
		if err != nil {
			return nil, err
		}
		return offer.answer(answer)
	}
}

// Relock drops every elevated scope. Best-effort: the operation it protects has
// already finished, so a failure here is worth a log line and nothing more.
func (c *Client) Relock(ctx context.Context) {
	if err := c.Decode(ctx, Request{Method: "PUT", Path: lockPath}, nil); err != nil {
		c.log.Debug("relock failed; the session stays elevated until it expires", "error", err)
	}
}

// Scopes returns the scopes the session currently holds, for reporting what a
// saved session can actually do.
func (c *Client) Scopes(ctx context.Context) ([]string, error) {
	var r struct{ Scopes []string }
	if err := c.Decode(ctx, Request{Method: "GET", Path: scopesPath}, &r); err != nil {
		return nil, err
	}
	return r.Scopes, nil
}

// elevateAndRetry handles a missing-scope response: ask for credentials, elevate,
// run the request again, then drop the scope.
//
// The retry is marked so a second 403 cannot start the cycle again - an account
// whose password no longer works would otherwise loop.
func (c *Client) elevateAndRetry(ctx context.Context, req Request, resp *Response) (*Response, error) {
	resolver := c.getScopeResolver()
	if resolver == nil || req.elevated {
		_, apiErr := classifyErrorBody(resp.Status, resp.Body)
		return resp, apiErr
	}

	// The body names the scope when it can; ScopeLocked is the one every data
	// operation needs, so it is the safe assumption.
	scope := scopeFromBody(resp.Body)

	cr, err := resolver(ctx, scope)
	if err != nil {
		return resp, err
	}
	if err := c.Elevate(ctx, scope, cr); err != nil {
		return resp, err
	}
	// The Pass scope is not an elevation to be given back: it is what the session
	// holds from here on, there is no endpoint that takes it away, and asking for
	// the extra password again on the next request is the one outcome to avoid.
	if scope != ScopePass {
		defer c.Relock(ctx)
	}

	retry := req
	retry.elevated = true
	return c.Do(ctx, retry)
}

// scopeFromBody reads the scope the server said was missing. Proton reports it
// inconsistently across endpoints, so anything unrecognised falls back to the
// weakest of them: elevating further than necessary is worse than being asked
// again.
//
// Pass is read first and from either signal, because it is the one scope the
// account password cannot buy: guessing at it would ask for the wrong secret and
// then fail anyway. Which one is looked for is decided here rather than by the
// order Proton happened to list them in.
func scopeFromBody(body []byte) Scope {
	var env struct {
		Code    int
		Details struct {
			MissingScopes []string
		}
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return ScopeLocked
	}
	named := env.Details.MissingScopes
	switch {
	case env.Code == passScopeCode, slices.Contains(named, string(ScopePass)):
		return ScopePass
	case slices.Contains(named, string(ScopePassword)):
		return ScopePassword
	}
	return ScopeLocked
}

// ErrScopeUnavailable reports that an elevation was needed but could not be
// performed. Resolvers wrap their own reason; this exists so callers can detect
// the class.
var ErrScopeUnavailable = errors.New("session cannot be elevated")
