package redact

// Fields is every name a log record may carry an attribute under, and what may
// be written for it.
//
// It is the vocabulary of the diagnostic log, declared here the way the command
// vocabulary is declared in kit: closed, in one place, and checked. A log line
// using a name that is not in here does not compile past the conformance test,
// which is what turns "the log holds nothing sensitive" from a habit somebody
// has to keep into a property of the build.
//
// So the question to answer when adding a line is not "is this safe to log" but
// "what is the name for this, and what may be written for that name" - and the
// answer is written down where the next person can read it.
//
// It holds the names in use and no others. A name nothing writes is a decision
// nobody has had to make yet, and leaving it here to be found later is how a
// vocabulary comes to describe a program that no longer exists; the conformance
// test checks both directions for that reason.
var Fields = map[string]Policy{
	// ── the run itself ──
	"run":       Keep, // the four-character name for one invocation
	"command":   Keep, // the command path, without its arguments
	"flags":     Keep, // the names of the flags that were set, never their values
	"version":   Keep,
	"revision":  Keep,
	"go":        Keep,
	"platform":  Keep,
	"install":   Keep, // how this binary arrived: release, go install, unknown
	"profile":   Keep, // the user's own word for one account, validated to be one
	"exit":      Keep,
	"panic":     Text,   // the value a panic carried
	"stack":     Text,   // the goroutine's stack at the panic
	"tty":       Keep,   // whether the answer was going to a terminal
	"term":      Keep,   // the terminfo name the terminal calls itself
	"colorterm": Keep,   // what the terminal advertises about colour depth
	"width":     Keep,   // the column budget the output was laid out against
	"output":    Keep,   // text, json, yaml
	"zone":      Keep,   // an IANA zone name, which names a region and not a person
	"error":     Text,   // the chain of what was being attempted, and what stopped it
	"duration":  Keep,   // how long the run took, in milliseconds
	"reason":    Keep,   // one of a fixed set of words, declared where it is raised
	"kind":      Keep,   // the singular noun for what a skip was about
	"ref":       Handle, // the thing a skip was about, whatever kind it is
	"path":      Route,  // an API path

	// ── counts, which describe a shape and never a person ──
	"count":  Keep,
	"opened": Keep,

	// ── the request ──
	"method":      Keep,
	"status":      Keep,
	"bytes":       Keep,
	"duration_ms": Keep,
	"wait_ms":     Keep,
	"attempt":     Keep,
	"dry_run":     Keep,

	// ── the account's shape, which is counts and never names ──
	"two_password":     Keep,
	"addresses":        Keep,
	"addresses_active": Keep,
	"user_keys":        Keep,
	"address_keys":     Keep,

	// ── the things an account holds ──
	"calendar": Handle,
	"item":     Handle,
	"link":     Handle,
	"parent":   Handle,
	"share":    Handle,
	"vault":    Handle,

	// ── who signed something ──
	"signer": Address,
	"result": Keep, // a signature verdict: ok, not verified, no signer
}
