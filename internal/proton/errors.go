package proton

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnauthorized signals that auth failed and token refresh did not recover.
//
// It carries its own exit code rather than being recognised by the layer that
// classifies errors. An expired session is the most ordinary failure there is,
// and a classifier that has to know this particular value by sight is one that
// answers "a bug in the CLI" for it the moment anything else asks the question.
var ErrUnauthorized error = unauthorized{}

type unauthorized struct{}

func (unauthorized) Error() string { return "unauthorized: session expired" }
func (unauthorized) ExitCode() int { return 2 }

// NetworkError wraps a transport-level failure (DNS, connection refused, TLS,
// timeout) where the request never received an HTTP response. It carries exit
// code 5, matching the documented "network/server" contract - distinct from an
// APIError (a response with a non-2xx status).
type NetworkError struct{ Err error }

func (e *NetworkError) Error() string { return "request failed: " + e.Err.Error() }
func (e *NetworkError) Unwrap() error { return e.Err }
func (e *NetworkError) ExitCode() int { return 5 }

// APIError is returned for non-2xx responses that carry a Proton error code.
type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
	RawBody    []byte
}

func (e *APIError) Error() string {
	if e.Code != 0 {
		return fmt.Sprintf("[HTTP %d] %d: %s", e.HTTPStatus, e.Code, e.Message)
	}
	return fmt.Sprintf("[HTTP %d] %s", e.HTTPStatus, e.Message)
}

// The codes Proton answers with when the thing named does not exist: an ID it
// does not recognise, a resource that is gone, and a conversation in particular.
// The web clients treat all three as the same answer
// (isNotExistError, packages/shared/lib/api/helpers/apiErrorHelper.ts).
//
// They arrive with HTTP 400 or 422, which on their own read as a bad request or a
// conflict, so the Proton code has the final say. Anything that matched no
// resource has to exit 3, because that is the code scripts branch on.
const (
	invalidIDCode            = 2061
	notFoundCode             = 2501
	conversationNotFoundCode = 20052
)

// invalidLoginCode is what Proton answers when the credentials are refused: a
// wrong password, or a wrong two-factor code. The web clients read the one code
// for both (PASSWORD_WRONG_ERROR, packages/shared/lib/api/auth.ts).
const invalidLoginCode = 8002

// Succeeded reports whether a Proton code is one of the ways of saying it worked:
// done, one answer per item, or accepted and being carried out in the background.
//
// It is exported because the per-item codes inside a batch answer are read by
// the service that sent the batch, and which codes mean success is this
// package's to know rather than each caller's to remember.
func Succeeded(code int) bool {
	switch code {
	case okCode, multiCode, acceptedCode:
		return true
	}
	return false
}

const (
	okCode       = 1000
	multiCode    = 1001
	acceptedCode = 1002
)

// The code Proton answers with when the name is already taken where it was to
// be written. The web clients read the same code to know a duplicate was found
// and ask what to do about it (useUploadFile.ts).
const (
	alreadyExistsCode        = 2500
	tooManyActiveFiltersCode = 50016
	noSuchAddressCode        = 33103
)

// NoSuchAddress reports whether err is Proton saying it holds no address by that
// name, which is how an address outside Proton answers a key lookup made with
// InternalOnly.
//
// It is not a failure everywhere it happens: to somebody asking whether a
// message can be encrypted it is the answer, and only the caller knows whether
// having no key is a refusal or an ordinary outcome.
func NoSuchAddress(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == noSuchAddressCode
}

// DoesNotExist reports whether err is Proton saying that what was named is not
// there, so a caller holding the reference can say so in its own words.
func DoesNotExist(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case invalidIDCode, notFoundCode, conversationNotFoundCode:
		return true
	}
	return apiErr.HTTPStatus == 404
}

// AlreadyExists reports whether err is Proton refusing to write a second thing
// under a name that is taken, so a caller that knows which name and which folder
// can say so instead of passing the code on.
func AlreadyExists(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == alreadyExistsCode
}

// AtFilterLimit reports whether err is Proton refusing to have another filter
// running.
//
// It writes the filter anyway and leaves it turned off, so a caller can say that
// rather than let a refusal imply nothing happened. The refusal carries no ID,
// which is why the caller has to send somebody to the listing to find it.
func AtFilterLimit(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == tooManyActiveFiltersCode
}

// ExitCode maps an HTTP failure to the CLI's exit-code scheme.
func (e *APIError) ExitCode() int {
	switch e.Code {
	case invalidIDCode, notFoundCode, conversationNotFoundCode:
		return 3
	case invalidLoginCode:
		return 2
	}
	switch e.HTTPStatus {
	case 401, 403:
		return 2
	case 404:
		return 3
	case 409, 422:
		return 4
	case 429:
		// Proton asking for room is a server problem rather than something the
		// caller got wrong, and 5 is the code a script waits on.
		return 5
	}
	if e.HTTPStatus >= 500 {
		return 5
	}
	return 1
}

// HumanVerificationError is returned when the API requires human verification
// (Proton code 9001). Callers typically display WebURL, run the resolver, and
// retry with Token + Methods[0] set on the next Request.
type HumanVerificationError struct {
	Token   string
	Methods []string
	WebURL  string
	// Refused marks a challenge raised against a request that already carried a
	// verification. The two failures need different words: one asks somebody to
	// verify, the other says the verification they did was not accepted.
	Refused bool
}

func (e *HumanVerificationError) Error() string {
	return fmt.Sprintf("human verification required: %s", e.WebURL)
}

// hvDetails is the shared shape of the Details object Proton returns on a
// 9001 (human-verification) response.
type hvDetails struct {
	HumanVerificationToken   string
	HumanVerificationMethods []string
	WebUrl                   string
}

// classifyErrorBody decodes a non-2xx Proton response body into either a
// *HumanVerificationError (Code 9001) or a generic *APIError.
func classifyErrorBody(status int, body []byte) (*HumanVerificationError, *APIError) {
	var env struct {
		Code    int
		Error   string
		Details *hvDetails
	}
	_ = json.Unmarshal(body, &env)
	if env.Code == 9001 && env.Details != nil {
		return &HumanVerificationError{
			Token:   env.Details.HumanVerificationToken,
			Methods: env.Details.HumanVerificationMethods,
			WebURL:  env.Details.WebUrl,
		}, nil
	}
	msg := env.Error
	if msg == "" {
		msg = httpStatusText(status)
	}
	return nil, &APIError{HTTPStatus: status, Code: env.Code, Message: msg, RawBody: body}
}

// responseError is the refusal a response carries, or nil when it carries none.
//
// Both halves of the answer have a say. Proton names its own reason in a code,
// and the thousands are the ways of succeeding: 1000 done, 1001 a response per
// item, 1002 accepted and being carried out in the background (which is how
// restoring a revision answers, and what the web client's own type says it
// means). Anything else is a refusal, whatever the status line said. And the
// status line has the say when there is no code to read at all - an HTML error
// page from the edge carries none - which is what keeps a bad moment upstream
// from being reported as unreadable JSON.
//
// The status the answer arrived with is kept on the error, because that is what
// separates Proton failing from Proton refusing, and it is the difference between
// telling somebody to wait and telling them they are wrong.
func responseError(resp *Response) error {
	if resp.Status >= 200 && resp.Status < 300 {
		var env struct{ Code int }
		if json.Unmarshal(resp.Body, &env) != nil || env.Code == 0 || Succeeded(env.Code) {
			return nil
		}
	}
	hvErr, apiErr := classifyErrorBody(resp.Status, resp.Body)
	if hvErr != nil {
		return hvErr
	}
	return apiErr
}
