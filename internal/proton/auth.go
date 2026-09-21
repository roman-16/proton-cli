package proton

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"

	"github.com/ProtonMail/go-srp"
	"github.com/roman-16/proton-cli/internal/errs"
)

type authInfo struct {
	Code            int
	Modulus         string
	ServerEphemeral string
	Version         int
	Salt            string
	SRPSession      string
	// TwoFA is answered only when the session asking is already signed in, which
	// is how proving a session again learns what it may prove itself with.
	TwoFA twoFA `json:"2FA"`
}

type authResp struct {
	Code         int
	UID          string
	AccessToken  string
	RefreshToken string
	ServerProof  string
	TwoFA        twoFA `json:"2FA"`
}

// twoFA is what Proton says about an account's second factor, in the sign-in
// answer and again when a session is asked to prove itself a second time.
type twoFA struct {
	Enabled int
	FIDO2   struct {
		// AuthenticationOptions is a WebAuthn challenge, carried as it arrived: it
		// goes back with the answer, and re-encoding what the server wrote is a way
		// of disagreeing with it about what it said.
		AuthenticationOptions json.RawMessage
		RegisteredKeys        []struct {
			Name string
		}
	}
}

// offer is what this account can answer with, as a question for whoever is at
// the terminal. A security key that is enabled but registers no keys is Proton
// describing a door with nothing behind it, and is offered as no key at all.
func (t twoFA) offer(host string) SecondFactorOffer {
	offer := SecondFactorOffer{TOTP: t.Enabled&twoFAOTP != 0}
	if t.Enabled&twoFAFIDO2 != 0 && len(t.FIDO2.RegisteredKeys) > 0 {
		offer.SecurityKey = &SecurityKeyRequest{
			Options: t.FIDO2.AuthenticationOptions,
			Host:    host,
		}
	}
	return offer
}

// Two-factor methods, as the bitfield Proton returns in 2FA.Enabled.
// Mirrors SETTINGS_2FA_ENABLED in WebClients
// (packages/shared/lib/interfaces/UserSettings.ts).
const (
	twoFAOTP   = 1
	twoFAFIDO2 = 2
)

// SecondFactorFunc answers Proton's second-factor challenge.
//
// It is called only when the server says this account has one, and it is given
// what the server will accept rather than being asked for a particular kind:
// which of the two to use is a question for whoever is at the terminal, and only
// they know whether the key is in the room. Nothing is asked speculatively - a
// code lives thirty seconds and a touch cannot be taken back.
type SecondFactorFunc func(context.Context, SecondFactorOffer) (SecondFactorAnswer, error)

// SecondFactorOffer is what Proton will accept from this account.
type SecondFactorOffer struct {
	TOTP        bool
	SecurityKey *SecurityKeyRequest
}

// SecurityKeyRequest is a WebAuthn challenge and the host that sent it. Both
// halves matter: what a key is asked to sign, and who is entitled to ask.
type SecurityKeyRequest struct {
	Options json.RawMessage
	Host    string
}

// SecondFactorAnswer is what came back. Exactly one of the two is filled in.
type SecondFactorAnswer struct {
	TOTP        string
	SecurityKey *SecurityKeyAssertion
}

// SecurityKeyAssertion is what a key answered a challenge with.
type SecurityKeyAssertion struct {
	ClientData        []byte
	AuthenticatorData []byte
	Signature         []byte
	CredentialID      []byte
}

// SetSecondFactorResolver installs what answers a second-factor challenge, for
// signing in and for proving a session again later.
func (c *Client) SetSecondFactorResolver(r SecondFactorFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.secondFactor = r
}

func (c *Client) getSecondFactorResolver() SecondFactorFunc {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.secondFactor
}

// answer puts a second factor where the request that carries it wants it. Every
// endpoint that asks for one takes both kinds under the same two names, so this
// is written once.
func (offer SecondFactorOffer) answer(a SecondFactorAnswer) (map[string]any, error) {
	switch {
	case a.SecurityKey != nil:
		if offer.SecurityKey == nil {
			return nil, errors.New("a security key answered a challenge that did not ask for one")
		}
		// The credential is a JSON array of numbers, which is how the web clients
		// send it and so how Proton expects to read it.
		credential := make([]int, len(a.SecurityKey.CredentialID))
		for i, b := range a.SecurityKey.CredentialID {
			credential[i] = int(b)
		}
		return map[string]any{"FIDO2": map[string]any{
			"AuthenticationOptions": offer.SecurityKey.Options,
			"ClientData":            base64.StdEncoding.EncodeToString(a.SecurityKey.ClientData),
			"AuthenticatorData":     base64.StdEncoding.EncodeToString(a.SecurityKey.AuthenticatorData),
			"Signature":             base64.StdEncoding.EncodeToString(a.SecurityKey.Signature),
			"CredentialID":          credential,
		}}, nil
	case a.TOTP != "":
		return map[string]any{"TwoFactorCode": a.TOTP}, nil
	}
	return nil, errs.Problemf("This account needs a second factor to sign in, and none was given.").Exit(2)
}

// Login performs the full web-client auth flow (unauth session → SRP → 2FA).
// On a Proton 9001 (human-verification) response from the SRP step, Login
// consults the installed HVResolver (if any) and retries the auth POST once
// with HV headers, redoing getAuthInfo to obtain a fresh SRPSession.
//
// A second refusal is marked as one. Signing in mints a challenge of its own
// each time it is asked, so a verification that was not accepted here is a
// verification that will never be accepted, and saying "verify" again would
// send somebody round the same loop.
func (c *Client) Login(ctx context.Context, username string, password []byte) error {
	sess, err := c.createSession(ctx)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.mu.Lock()
	c.uid, c.acc, c.ref = sess.UID, sess.AccessToken, sess.RefreshToken
	c.mu.Unlock()

	auth, err := c.loginSRP(ctx, username, password, "", "")
	var hvErr *HumanVerificationError
	if errors.As(err, &hvErr) {
		if resolver := c.getHVResolver(); resolver != nil {
			token, kind, rerr := resolver(hvErr)
			switch {
			case rerr == nil && token != "":
				auth, err = c.loginSRP(ctx, username, password, token, kind)
				var refused *HumanVerificationError
				if errors.As(err, &refused) {
					refused.Refused = true
				}
			case errors.Is(rerr, ErrHVUnavailable):
				// keep err = hvErr, surfaced below
			case rerr != nil:
				return fmt.Errorf("login: hv resolver: %w", rerr)
			}
		}
	}
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	c.mu.Lock()
	c.uid, c.acc, c.ref = auth.UID, auth.AccessToken, auth.RefreshToken
	// A sealed key password belongs to the session it was wrapped for, and this is
	// a different session: Proton holds no client key for it, so the blob a
	// previous sign-in left behind can never be opened again. Dropping it here is
	// what keeps `account get` from reporting a profile as unlocked on the strength
	// of a blob nothing can read.
	c.encKeyBlob = ""
	c.mu.Unlock()

	if auth.TwoFA.Enabled == 0 {
		return nil
	}
	offer := auth.TwoFA.offer(c.Host())
	if !offer.TOTP && offer.SecurityKey == nil {
		return errs.Problemf("This account uses a two-factor method proton does not support (0x%x).",
			auth.TwoFA.Enabled).Exit(2)
	}
	resolver := c.getSecondFactorResolver()
	if resolver == nil {
		return fmt.Errorf("login: account requires a second factor")
	}
	answer, err := resolver(ctx, offer)
	if err != nil {
		return err
	}
	if err := c.auth2FA(ctx, offer, answer); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	return nil
}

// Host is the machine the API answers on, which is the only relying party a
// security key may be asked about.
func (c *Client) Host() string {
	u, err := url.Parse(c.base)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// srpExchange is one SRP exchange: where the parameters come from, the endpoint
// that answers the proof, who is proving what, and what the proof carries
// alongside itself.
type srpExchange struct {
	// parameters fetches one set of SRP parameters. It is asked once per attempt,
	// because the SRPSession a set carries is spent by the attempt that used it.
	parameters func(context.Context) (*authInfo, error)

	method string
	path   string

	username string
	password []byte

	// extra is what the endpoint wants alongside the proof.
	extra map[string]any
	// secondFactor answers the challenge the parameters come back with, for the
	// exchanges that carry one in the same request as the proof. It is consulted
	// after the parameters arrive, because only they say whether Proton wants one.
	secondFactor func(twoFA) (map[string]any, error)

	hvToken string
	hvType  string

	// accountHost sends the proof through Proton's account host. See
	// Request.AccountHost.
	accountHost bool

	// opensLink marks the exchange that proves a public link, which Proton answers
	// whether or not anybody is behind it. See Request.opensLink.
	opensLink bool
}

// repeatable reports whether the whole exchange may be run a second time.
//
// One carrying a second factor may not. A code is single-use, so a second run
// submits one Proton has already taken and is told it is wrong - which would put
// the blame on the person for a bad moment upstream - and a challenge a key has
// signed is spent the same way.
func (x srpExchange) repeatable() bool {
	return x.secondFactor == nil
}

// exchange runs an SRP exchange: fresh parameters, then the proof.
//
// The pair is the unit that gets another go, because the proof on its own cannot
// be sent twice - the SRPSession the parameters carried is spent by the first
// attempt, so a second attempt needs a second set. That is exactly what a person
// does on seeing the error, and it has the same consequence.
func (c *Client) exchange(ctx context.Context, x srpExchange) (*Response, error) {
	resp, err := c.retrying(ctx,
		Request{Method: x.method, Path: x.path, Repeatable: x.repeatable()},
		func() (*Response, error) {
			info, err := x.parameters(ctx)
			if err != nil {
				return nil, err
			}
			return c.srpCall(ctx, x, info)
		})
	return resp, refusedSecondFactor(err)
}

// refusedSecondFactor phrases the one thing an exchange can be refused for that
// the person can put right themselves.
//
// The code was asked for by this package, through its own resolver, so the
// answer to it is this package's to explain. Left bare it reaches the screen as
// a number under an invitation to file a bug, for a thirty-second code that had
// simply turned over.
func refusedSecondFactor(err error) error {
	if !WrongTwoFactorCode(err) {
		return err
	}
	return errs.Problemf("Proton did not accept that two-factor code.").
		Hint("a code lasts thirty seconds - run this again with the next one").Exit(2)
}

// srpCall proves the password against info and validates the server's proof.
//
// It is the single SRP code path: signing in and elevating a scope differ only
// in which endpoint they call and what they add to the body. Sharing it is what
// guarantees both verify the server's proof, which is the half of SRP that
// authenticates Proton to us.
//
// Mirrors srpAuth in WebClients (packages/shared/lib/srp.ts), whose
// callAndValidate rejects an unexpected server proof for every caller.
func (c *Client) srpCall(ctx context.Context, x srpExchange, info *authInfo) (*Response, error) {
	auth, err := srp.NewAuth(info.Version, x.username, x.password, info.Salt, info.Modulus, info.ServerEphemeral)
	if err != nil {
		return nil, fmt.Errorf("SRP setup: %w", err)
	}
	proofs, err := auth.GenerateProofs(2048)
	if err != nil {
		return nil, fmt.Errorf("SRP proofs: %w", err)
	}

	payload := map[string]any{
		"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
		"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
		"SRPSession":      info.SRPSession,
	}
	for k, v := range x.extra {
		payload[k] = v
	}
	if x.secondFactor != nil {
		answer, err := x.secondFactor(info.TwoFA)
		if err != nil {
			return nil, err
		}
		for k, v := range answer {
			payload[k] = v
		}
	}

	resp, err := c.authCall(ctx, Request{
		Method: x.method, Path: x.path, Body: payload,
		AccountHost: x.accountHost,
		HVToken:     x.hvToken, HVType: x.hvType, opensLink: x.opensLink,
	})
	if err != nil {
		return nil, err
	}
	var r struct{ ServerProof string }
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	serverProof, err := base64.StdEncoding.DecodeString(r.ServerProof)
	if err != nil {
		return nil, fmt.Errorf("server proof decode: %w", err)
	}
	if !bytes.Equal(serverProof, proofs.ExpectedServerProof) {
		return nil, fmt.Errorf("server proof verification failed")
	}
	return resp, nil
}

func (c *Client) createSession(ctx context.Context) (*authResp, error) {
	resp, err := c.authCall(ctx, Request{
		Method: "POST", Path: "/auth/v4/sessions", Body: []byte("{}"),
		headers: map[string]string{"x-enforce-unauthsession": "true"},
		// An unauthenticated session is scratch: one created twice leaves behind a
		// session nothing holds the tokens to use, which Proton reaps, and failing
		// the whole sign-in instead is the worse of the two outcomes.
		Repeatable: true,
	})
	if err != nil {
		return nil, err
	}
	var r authResp
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// accountParameters asks Proton what an account password is proved against, for
// the scope the exchange is for.
//
// accountHost sends the question through the same door as the proof that
// follows. They are two halves of one conversation - the second spends the
// SRPSession the first issued - so they are never split across hosts.
func (c *Client) accountParameters(username string, scope Scope, accountHost bool) func(context.Context) (*authInfo, error) {
	return func(ctx context.Context) (*authInfo, error) {
		return c.getAuthInfo(ctx, username, scope, accountHost)
	}
}

// getAuthInfo fetches the SRP parameters for username.
//
// Intent identifies the sign-in flow, and ReauthScope tells the server which
// elevation the following exchange is for; both are what the web clients send
// (packages/shared/lib/api/auth.ts getInfo). reauthScope is empty for a fresh
// sign-in.
//
// It is not marked repeatable, though asking twice spends nothing: it is only
// ever one half of an exchange, and the exchange is what gets another go. Two
// budgets over the same failure would multiply into minutes of waiting.
func (c *Client) getAuthInfo(ctx context.Context, username string, reauthScope Scope, accountHost bool) (*authInfo, error) {
	payload := map[string]any{"Intent": "Proton"}
	if username != "" {
		payload["Username"] = username
	}
	if reauthScope != "" {
		payload["ReauthScope"] = string(reauthScope)
	}
	resp, err := c.authCall(ctx, Request{
		Method: "POST", Path: "/core/v4/auth/info", Body: payload, AccountHost: accountHost,
	})
	if err != nil {
		return nil, err
	}
	var r authInfo
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// loginSRP proves the password to the endpoint that hands out a session.
// When hvToken/hvType are non-empty they're attached as HV headers on the proof.
func (c *Client) loginSRP(ctx context.Context, username string, password []byte, hvToken, hvType string) (*authResp, error) {
	resp, err := c.exchange(ctx, srpExchange{
		parameters: c.accountParameters(username, "", false),
		method:     "POST", path: "/core/v4/auth",
		username: username, password: password,
		extra:   map[string]any{"Username": username},
		hvToken: hvToken, hvType: hvType,
	})
	if err != nil {
		return nil, err
	}
	var r authResp
	if err := readAnswer(resp.Body, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// prove sends a request whose body carries the SRP proof of the account
// password. See Request.Proves.
//
// The credentials come from the same resolver an elevation asks, so the person
// is asked the same question whichever way Proton guards an endpoint, and a
// second factor is answered where every other exchange answers it: once the
// parameters have said Proton wants one. The parameters are asked for without a
// scope, because this proof buys none - it authorises the one request it is
// part of, which is what the web clients do here too (srpAuth in
// packages/shared/lib/srp.ts).
func (c *Client) prove(ctx context.Context, req Request) (*Response, error) {
	resolver := c.getScopeResolver()
	if resolver == nil {
		return nil, fmt.Errorf("%w: nothing can supply the password this request has to carry", ErrScopeUnavailable)
	}
	extra, err := provenBody(req.Body)
	if err != nil {
		return nil, err
	}
	cr, err := resolver(ctx, ScopePassword)
	if err != nil {
		return nil, err
	}
	return c.exchange(ctx, srpExchange{
		parameters: c.accountParameters(cr.Username, "", req.AccountHost),
		method:     req.Method, path: req.Path, accountHost: req.AccountHost,
		username: cr.Username, password: cr.Password,
		extra:        extra,
		secondFactor: c.answerSecondFactor(ctx),
	})
}

// provenBody is what a proving request wants sent alongside its proof. The
// proof is a JSON object, so anything the endpoint wants with it has to be one
// too - which is a fact about the request rather than about this run, so it is
// a programming error rather than something to phrase for a person.
func provenBody(body any) (map[string]any, error) {
	switch b := body.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return b, nil
	}
	return nil, fmt.Errorf("a request carrying its own proof needs a map body, got %T", body)
}

func (c *Client) auth2FA(ctx context.Context, offer SecondFactorOffer, answer SecondFactorAnswer) error {
	body, err := offer.answer(answer)
	if err != nil {
		return err
	}
	// Not repeatable, whatever goes wrong: a code is single-use and lives for
	// thirty seconds, so by the time a wait is over it is either spent or expired,
	// and submitting it again is answered with "incorrect code" - a sentence that
	// blames the person for something Proton did. A challenge a key has signed is
	// spent in the same way.
	_, err = c.authCall(ctx, Request{
		Method: "POST", Path: "/core/v4/auth/2fa", Body: body,
	})
	return refusedSecondFactor(err)
}

// authCall sends one request of the auth flow and returns the answer to it.
//
// The flow stands here rather than on Do, one layer up: refreshing a session,
// elevating a scope and refusing a dry run are all answers about a session, and
// during a sign-in there is not one yet. What it does share with every other
// request is the part that matters - the status is read before the body, and a
// bad moment upstream is waited out rather than parsed.
func (c *Client) authCall(ctx context.Context, req Request) (*Response, error) {
	resp, err := c.attempt(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := responseError(resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// readAnswer reads an auth-flow answer into out.
//
// A body that is not the JSON it should be, with a status that said the request
// succeeded, is Proton answering something nobody can act on. Saying that beats
// quoting the parser at somebody who cannot fix it.
func readAnswer(raw []byte, out any) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("unreadable answer from Proton: %w", err)
	}
	return nil
}
