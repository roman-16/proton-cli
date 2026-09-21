// Package fido holds Proton's two security-key ceremonies from a terminal:
// registering a key with the account, and signing in with one.
//
// Both are WebAuthn, and a browser is only one way to hold them. What the key
// signs is its own authenticator data followed by the hash of a clientDataJSON
// the client writes itself, so nothing in either ceremony needs a page, a
// window or a JavaScript engine - which is why Proton's own Go client performs
// the sign-in the same way.
//
// What a browser does that matters is refuse to ask a key about a relying party
// the visited site has no claim to. That refusal is the whole of WebAuthn's
// phishing resistance, and with no browser in the picture it has to live here:
// the rpId Proton names is checked against the host that named it before any key
// is asked anything. A client that signed whatever rpId an answer carried would
// be a relay for whoever could forge that answer.
package fido

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
)

// Request is a security-key ceremony as Proton stated it.
type Request struct {
	// Options is Proton's challenge exactly as it arrived - an
	// AuthenticationOptions for a sign-in, a RegistrationOptions for a
	// registration. It is echoed back alongside the answer, so it is carried
	// rather than rebuilt: re-encoding a structure the server wrote is a way of
	// disagreeing with it about what it said.
	Options json.RawMessage
	// Host is the API host that sent the challenge. The relying party the key is
	// asked about must belong to it.
	Host string
}

// Assertion is what the key answered a sign-in with, in the shape Proton asks
// for it back.
type Assertion struct {
	ClientData        []byte
	AuthenticatorData []byte
	Signature         []byte
	CredentialID      []byte
}

// Attestation is the credential a key has just made, in the shape Proton asks
// for it back.
type Attestation struct {
	ClientData        []byte
	AttestationObject []byte
	CredentialID      []byte
	// Transports is how this key can be reached, as WebAuthn names them. Proton
	// keeps it against the credential so a later sign-in can say where to look.
	Transports []string
}

// Prompts is how a ceremony reaches the person holding the key.
//
// Touching a key is not something a program can do on somebody's behalf, so the
// wait has to be announced or it reads as a hang. What to announce comes from
// here rather than from the caller, because what happens next differs by
// platform: a key waiting for a finger, or a dialog Windows is about to draw.
// PIN is consulted only when the key insists on one; a key that verifies by
// fingerprint, or not at all, never asks.
type Prompts struct {
	Touch func(instruction string)
	PIN   func() (string, error)
}

func (p Prompts) touch(instruction string) {
	if p.Touch != nil {
		p.Touch(instruction)
	}
}

func (p Prompts) pin() (string, error) {
	if p.PIN == nil {
		return "", ErrPINRequired
	}
	return p.PIN()
}

// What can go wrong on the way to an answer. Each is a different sentence to
// the person in front of the terminal, so they are distinguishable here rather
// than being one opaque failure.
var (
	// ErrNoDevice means nothing that could answer is plugged in.
	ErrNoDevice = errors.New("no security key is connected")
	// ErrPermission means a key is there and this user may not open it, which on
	// Linux is a missing udev rule rather than anything the person did.
	ErrPermission = errors.New("a security key is connected but cannot be opened")
	// ErrNoCredential means the key works and holds nothing for this account.
	ErrNoCredential = errors.New("this security key holds no credential for the account")
	// ErrRegistered is the other half of that: the key holds one already, and a
	// second registration of the same key is what the account was asked to avoid.
	ErrRegistered = errors.New("this security key is already registered with the account")
	// ErrDenied covers a key that was never touched and a ceremony called off.
	ErrDenied = errors.New("the security key was not touched")
	// ErrPINRequired means the key will not answer without its PIN and there was
	// nobody to ask for one.
	ErrPINRequired = errors.New("this security key needs its PIN")
	// ErrPINWrong is a PIN the key rejected. The key counts these and locks
	// itself after enough of them, which is why nothing here tries twice.
	ErrPINWrong = errors.New("that is not the PIN of this security key")
	// ErrPINBlocked means the key has locked itself and only a reset will do.
	ErrPINBlocked = errors.New("this security key has locked itself after too many wrong PINs")
	// ErrUnsupported means this build on this platform cannot talk to a key.
	ErrUnsupported = errors.New("security keys are not available on this platform")
)

// Assert asks a key to answer req, and returns the answer Proton wants.
func Assert(ctx context.Context, req Request, p Prompts) (Assertion, error) {
	a, err := parseAuthentication(req.Options)
	if err != nil {
		return Assertion{}, err
	}
	if err := a.belongsTo(req.Host); err != nil {
		return Assertion{}, err
	}
	return a.ceremony(ctx, assert, p)
}

// Register asks a key to make a credential for the account, and returns what
// Proton stores against it.
func Register(ctx context.Context, req Request, p Prompts) (Attestation, error) {
	c, err := parseCreation(req.Options)
	if err != nil {
		return Attestation{}, err
	}
	if err := c.belongsTo(req.Host); err != nil {
		return Attestation{}, err
	}
	return c.ceremony(ctx, register, p)
}

// signer is a key being asked to sign, and enroller a key being asked to make a
// credential, once the challenge has been understood and found to be about
// Proton. There is one of each per platform.
type (
	signer   func(context.Context, authentication, []byte, Prompts) (Assertion, error)
	enroller func(context.Context, creation, []byte, Prompts) (Attestation, error)
)

// The two ceremonies WebAuthn defines, as the clientDataJSON names them. The
// name is signed along with everything else, so a challenge answered under the
// wrong one is refused by the relying party.
const (
	ceremonyGet    = "webauthn.get"
	ceremonyCreate = "webauthn.create"
)

// ceremony is everything around the signature: what the key is given to sign,
// and what has to be true of what comes back.
func (a authentication) ceremony(ctx context.Context, sign signer, p Prompts) (Assertion, error) {
	clientData, err := a.clientData(ceremonyGet)
	if err != nil {
		return Assertion{}, err
	}
	answer, err := sign(ctx, a, clientData, p)
	if err != nil {
		return Assertion{}, err
	}
	if answer.CredentialID, err = a.credential(answer.CredentialID); err != nil {
		return Assertion{}, err
	}
	answer.ClientData = clientData
	return answer, nil
}

// ceremony is the same for a registration, and differs in what a key that has
// answered still owes: the credential it just made names itself, and how the
// key is reached is part of what Proton keeps.
func (c creation) ceremony(ctx context.Context, enrol enroller, p Prompts) (Attestation, error) {
	clientData, err := c.clientData(ceremonyCreate)
	if err != nil {
		return Attestation{}, err
	}
	answer, err := enrol(ctx, c, clientData, p)
	if err != nil {
		return Attestation{}, err
	}
	if len(answer.CredentialID) == 0 {
		return Attestation{}, errors.New("the security key did not say which credential it made")
	}
	if len(answer.Transports) == 0 {
		answer.Transports = []string{transportUSB}
	}
	answer.ClientData = clientData
	return answer, nil
}

// transportUSB is where a key that says nothing about itself was reached: both
// platforms get here over the wire a roaming key is plugged into.
const transportUSB = "usb"

// credential names the credential that answered.
//
// A key given exactly one to choose from is allowed to answer without saying
// which it used, and most sign-ins are that case - Proton names one registered
// key per challenge. Proton still wants to be told, so the one it offered is the
// answer.
func (a authentication) credential(reported []byte) ([]byte, error) {
	switch {
	case len(reported) > 0:
		return reported, nil
	case len(a.allowCredentials) == 1:
		return a.allowCredentials[0], nil
	}
	return nil, errors.New("the security key did not say which credential answered")
}

// terms is what both ceremonies are given: who is asking, what is to be signed,
// how sure the key has to be that somebody is there, and how long the wait may
// be.
type terms struct {
	rpID             string
	challenge        []byte
	userVerification string
	milliseconds     uint32
}

// authentication is the part of Proton's AuthenticationOptions a sign-in uses.
type authentication struct {
	terms
	allowCredentials [][]byte
}

// creation is the part of Proton's RegistrationOptions a registration uses.
type creation struct {
	terms
	rpName string
	user   account
	// algorithms are the COSE identifiers of the signature algorithms the
	// relying party will accept, in its order of preference.
	algorithms []int
	// exclude are the credentials already registered, which the key refuses to
	// make a second of. It is what makes registering the same key twice an
	// answer rather than a duplicate.
	exclude [][]byte
	// attestation is how much the relying party wants to be told about where
	// the key came from. WebAuthn's default, and Proton's ask, is nothing.
	attestation string
}

// account is the person the credential is made for, as the key files them. The
// name is shown by a key that lists what it holds; the ID is Proton's own
// handle for the account and means nothing outside it.
type account struct {
	id          []byte
	name        string
	displayName string
}

// octets is a byte string written as an array of numbers, which is how Proton
// serialises the binary parts of a WebAuthn challenge.
type octets []byte

func (o *octets) UnmarshalJSON(b []byte) error {
	var numbers []uint8
	if err := json.Unmarshal(b, &numbers); err != nil {
		return err
	}
	*o = numbers
	return nil
}

func parseAuthentication(raw json.RawMessage) (authentication, error) {
	var doc struct {
		PublicKey struct {
			RPID             string `json:"rpId"`
			Challenge        octets `json:"challenge"`
			AllowCredentials []struct {
				ID octets `json:"id"`
			} `json:"allowCredentials"`
			UserVerification string `json:"userVerification"`
			Timeout          uint32 `json:"timeout"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return authentication{}, fmt.Errorf("unreadable security-key challenge: %w", err)
	}
	a := authentication{terms: terms{
		rpID:             doc.PublicKey.RPID,
		challenge:        doc.PublicKey.Challenge,
		userVerification: doc.PublicKey.UserVerification,
		milliseconds:     doc.PublicKey.Timeout,
	}}
	for _, c := range doc.PublicKey.AllowCredentials {
		if len(c.ID) > 0 {
			a.allowCredentials = append(a.allowCredentials, c.ID)
		}
	}
	if err := a.stated(); err != nil {
		return authentication{}, err
	}
	if len(a.allowCredentials) == 0 {
		return authentication{}, ErrNoCredential
	}
	return a, nil
}

func parseCreation(raw json.RawMessage) (creation, error) {
	var doc struct {
		PublicKey struct {
			RP struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"rp"`
			User struct {
				ID          octets `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
			} `json:"user"`
			Challenge        octets `json:"challenge"`
			PubKeyCredParams []struct {
				Algorithm int `json:"alg"`
			} `json:"pubKeyCredParams"`
			ExcludeCredentials []struct {
				ID octets `json:"id"`
			} `json:"excludeCredentials"`
			AuthenticatorSelection struct {
				UserVerification string `json:"userVerification"`
			} `json:"authenticatorSelection"`
			Attestation string `json:"attestation"`
			Timeout     uint32 `json:"timeout"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return creation{}, fmt.Errorf("unreadable security-key challenge: %w", err)
	}
	key := doc.PublicKey
	c := creation{
		terms: terms{
			rpID:             key.RP.ID,
			challenge:        key.Challenge,
			userVerification: key.AuthenticatorSelection.UserVerification,
			milliseconds:     key.Timeout,
		},
		rpName: key.RP.Name,
		user: account{
			id:          key.User.ID,
			name:        key.User.Name,
			displayName: key.User.DisplayName,
		},
		attestation: key.Attestation,
	}
	for _, p := range key.PubKeyCredParams {
		c.algorithms = append(c.algorithms, p.Algorithm)
	}
	for _, e := range key.ExcludeCredentials {
		if len(e.ID) > 0 {
			c.exclude = append(c.exclude, e.ID)
		}
	}
	if err := c.stated(); err != nil {
		return creation{}, err
	}
	if len(c.algorithms) == 0 {
		return creation{}, errors.New("the security-key challenge names no algorithm a key could sign with")
	}
	return c, nil
}

// stated refuses a challenge that is missing what either ceremony cannot do
// without.
func (t terms) stated() error {
	switch {
	case t.rpID == "":
		return errors.New("the security-key challenge names no relying party")
	case len(t.challenge) == 0:
		return errors.New("the security-key challenge carries nothing to sign")
	}
	return nil
}

// belongsTo refuses a challenge that asks about somebody else's relying party.
//
// This is the check a browser makes on every ceremony, and the reason a key
// cannot be phished through one. Proton names the rpId in its own answer, so
// taking that name on trust would mean a forged or altered answer could have
// this program collect an assertion for any site the key holds a credential for,
// or register a key of the attacker's choosing somewhere else. The rule is
// WebAuthn's: the relying party is the host that sent the challenge, or a domain
// that host sits under.
func (t terms) belongsTo(host string) error {
	name := strings.ToLower(strings.TrimSuffix(t.rpID, "."))
	h := strings.ToLower(strings.TrimSuffix(host, "."))
	if h != "" && strings.Contains(name, ".") &&
		(h == name || strings.HasSuffix(h, "."+name)) {
		return nil
	}
	return fmt.Errorf("refusing to ask the security key about %q, which %q has no claim to", t.rpID, host)
}

// clientData is what the key signs the hash of, and what Proton checks the
// signature against. Its shape is WebAuthn's and its origin is the one Proton's
// own clients present for this relying party.
func (t terms) clientData(ceremony string) ([]byte, error) {
	return json.Marshal(struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
	}{
		Type:      ceremony,
		Challenge: base64.RawURLEncoding.EncodeToString(t.challenge),
		Origin:    "https://" + t.rpID,
	})
}

// needsVerification reports whether the relying party asked for the person to be
// verified, rather than merely present.
func (t terms) needsVerification() bool {
	return strings.EqualFold(t.userVerification, "required")
}

// timeout is how long the relying party is willing to wait, for the one platform
// that runs the ceremony on a clock of its own. A challenge that names no
// deadline gets the two minutes a person needs to find a key in a drawer.
func (t terms) timeout() time.Duration {
	if t.milliseconds == 0 {
		return 2 * time.Minute
	}
	return time.Duration(t.milliseconds) * time.Millisecond
}

// formatNone is the attestation statement of a credential that says nothing
// about the key it was made on.
const formatNone = "none"

// wantsAttestation reports whether the relying party asked to be told what made
// the credential. WebAuthn's default is that it did not, so a challenge that
// leaves the field out is asking for none.
func (c creation) wantsAttestation() bool {
	return c.attestation != "" && !strings.EqualFold(c.attestation, formatNone)
}

// attestationObject is the half of the answer a relying party stores: the
// authenticator data the key produced, and what the key said about where it came
// from, in the one CBOR structure WebAuthn defines for the pair.
//
// A relying party that asked for no attestation is given none. The key has
// already answered with whatever it attests with, and passing that on would hand
// over a statement identifying the make and model of key somebody carries, which
// nothing asked for - so the statement is dropped and the model identifier zeroed
// out of the authenticator data, which is what a browser does here.
func (c creation) attestationObject(
	ctx context.Context, format string, statement map[string]any, authData []byte,
) ([]byte, error) {
	if !c.wantsAttestation() && format != formatNone {
		slog.DebugContext(ctx, "dropping an attestation nothing asked for", "format", format)
		format, statement, authData = formatNone, nil, anonymised(authData)
	}
	if statement == nil {
		statement = map[string]any{}
	}
	return cbor.Marshal(struct {
		Format    string         `cbor:"fmt"`
		AuthData  []byte         `cbor:"authData"`
		Statement map[string]any `cbor:"attStmt"`
	}{Format: format, AuthData: authData, Statement: statement})
}

// anonymised is authenticator data with the model identifier taken out of it,
// which is the one part that says which make of key answered. It sits at a fixed
// offset: the relying party hash, the flags and the counter come first.
func anonymised(authData []byte) []byte {
	const model, length = 37, 16
	if len(authData) < model+length {
		return authData
	}
	out := append([]byte{}, authData...)
	clear(out[model : model+length])
	return out
}
