package proton

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/ProtonMail/go-srp"
	"github.com/roman-16/proton-cli/internal/errs"
)

// The Pass extra password.
//
// Pass can be protected with a password of its own, on top of the account's
// (https://proton.me/support/pass-extra-password). A session that has not
// answered it holds no `pass` scope, and every Pass endpoint refuses it - so this
// is an elevation rather than a command, the same shape as the two scopes in
// scope.go with a different secret and an exchange of its own.
//
// The exchange is SRP again, and deliberately not the one in auth.go: the
// parameters come from a GET rather than from /core/v4/auth/info, they name the
// session differently, and no second factor is ever asked for. Mirrors
// verifyExtraPassword in WebClients (packages/pass/lib/auth/password.ts) and the
// same flow in Proton's own Pass CLI (protonpass/pass-cli,
// pass-auth/src/extra_password.rs).

const (
	extraPasswordInfoPath = "/pass/v1/user/srp/info"
	extraPasswordAuthPath = "/pass/v1/user/srp/auth"
)

// The codes Proton answers a proof it did not accept with. A wrong answer is
// counted, and the session is ended once too many have been given
// (PassErrorCode.NOT_ALLOWED and SRP_ERROR, packages/pass/lib/api/errors.ts).
const (
	extraPasswordWrongCode = 2011
	extraPasswordSpentCode = 2026
)

// extraPasswordChallenge is what Proton offers a client that wants to prove the
// extra password.
type extraPasswordChallenge struct {
	Modulus         string
	ServerEphemeral string
	SrpSessionID    string
	SrpSalt         string
	Version         int
}

// check refuses a challenge nothing can be proved against, before anything tries.
//
// A modulus is read by verifying Proton's signature on it, and go-srp hands an
// empty one to a code path that panics - so an answer that carries no challenge
// has to be caught here rather than crash a command. A version below 3 hashes the
// username into the password, which this exchange deliberately has none of, and
// would come back as a wrong password rather than as the mismatch it is.
func (ch extraPasswordChallenge) check() error {
	switch {
	case ch.Modulus == "", ch.ServerEphemeral == "", ch.SrpSalt == "", ch.SrpSessionID == "":
		return errors.New("proton offered nothing to prove the extra password against")
	case ch.Version < 3:
		return fmt.Errorf("proton asked for SRP version %d, which no extra password is stored under", ch.Version)
	}
	return nil
}

// ProveExtraPassword answers Proton's challenge with the extra password.
//
// It is what the scope is bought with, and it is also how a change to the extra
// password itself is authorised: Proton wants the current one proved before it
// will take one off, the same as its own clients do.
func (c *Client) ProveExtraPassword(ctx context.Context, password []byte) error {
	// The pair is the unit that gets another go, as at sign-in: a challenge is
	// spent by the attempt that answered it, so a second attempt needs a second
	// challenge.
	_, err := c.retrying(ctx,
		Request{Method: "POST", Path: extraPasswordAuthPath, Repeatable: true},
		func() (*Response, error) {
			challenge, err := c.extraPasswordChallenge(ctx)
			if err != nil {
				return nil, err
			}
			return c.proveExtraPassword(ctx, challenge, password)
		})
	return err
}

// unlockPass proves the extra password, so the session may reach Pass.
//
// The tokens are renewed afterwards, which is what Proton's own Pass CLI does and
// what carries the scope into the session file: a later run then reaches Pass
// without asking for anything. It goes through the guarded renewal rather than
// refreshing directly, because a refresh token is single-use - and a session
// another process has already renewed is taken up instead of spent again.
func (c *Client) unlockPass(ctx context.Context, password []byte) error {
	_, spent, _ := c.Tokens()
	if err := c.ProveExtraPassword(ctx, password); err != nil {
		return err
	}
	if err := c.renewSession(ctx, spent); err != nil {
		return fmt.Errorf("renew the session after unlocking Pass: %w", err)
	}
	return nil
}

func (c *Client) extraPasswordChallenge(ctx context.Context) (extraPasswordChallenge, error) {
	resp, err := c.authCall(ctx, Request{Method: "GET", Path: extraPasswordInfoPath})
	if err != nil {
		return extraPasswordChallenge{}, fmt.Errorf("ask Proton how to prove the extra password: %w", err)
	}
	var r struct {
		SRPData extraPasswordChallenge
	}
	if err := readAnswer(resp.Body, &r); err != nil {
		return extraPasswordChallenge{}, err
	}
	return r.SRPData, nil
}

// proveExtraPassword answers a challenge and checks the answer it gets back.
//
// The server's proof is verified as it is everywhere else here: SRP
// authenticates both directions, and taking Proton's word for it would throw
// away the half that says we are talking to something which knows the verifier.
func (c *Client) proveExtraPassword(ctx context.Context, ch extraPasswordChallenge, password []byte) (*Response, error) {
	if err := ch.check(); err != nil {
		return nil, err
	}
	// No username: the verifier this is checked against was written at SRP
	// version 4, which does not hash one.
	auth, err := srp.NewAuth(ch.Version, "", password, ch.SrpSalt, ch.Modulus, ch.ServerEphemeral)
	if err != nil {
		return nil, fmt.Errorf("SRP setup: %w", err)
	}
	proofs, err := auth.GenerateProofs(2048)
	if err != nil {
		return nil, fmt.Errorf("SRP proofs: %w", err)
	}
	resp, err := c.authCall(ctx, Request{
		Method: "POST", Path: extraPasswordAuthPath,
		Body: map[string]any{
			"ClientEphemeral": base64.StdEncoding.EncodeToString(proofs.ClientEphemeral),
			"ClientProof":     base64.StdEncoding.EncodeToString(proofs.ClientProof),
			"SrpSessionID":    ch.SrpSessionID,
		},
	})
	if err != nil {
		return nil, extraPasswordRefusal(err)
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

// extraPasswordRefusal says what Proton refused and what to do about it.
//
// A wrong answer is worth saying plainly, because the alternative is somebody
// reading a code and concluding their Pass is broken. That Proton counts the
// wrong ones is worth saying with it: the next thing a person does is try again,
// and a few more tries end the session.
func extraPasswordRefusal(err error) error {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	switch apiErr.Code {
	case extraPasswordWrongCode:
		return errs.Problemf("That is not this account's Pass extra password.").
			Hint("try again - Proton ends the session after a few wrong answers").Exit(2)
	case extraPasswordSpentCode:
		return errs.Problemf("Proton ended this session after too many wrong extra passwords.").
			Hint("proton account login").Exit(2)
	}
	return err
}
