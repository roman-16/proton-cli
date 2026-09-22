package cli

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Every request this CLI can send is one the live suite sends.
//
// The live suite is the only thing that would notice Proton changing an answer,
// and it can only notice for requests it actually makes. So the set it reaches is
// recorded - `just coverage` writes tests/api-coverage.golden from a real run -
// and this reads the source to find every request the CLI is able to send, and
// fails on one the suite has never sent.
//
// It is checked here rather than by the suite because it needs no account and no
// network: what the CLI can send is a property of the code. The other half, what
// the suite did send, needs a run against Proton, which is why it is a recorded
// file rather than a check.
//
// A request that appears here after a change means one of two things: a test for
// it is missing, or it belongs in unreachable below with the reason nobody can
// reach it.

// coverageGolden is what a live run reached, recorded by `just coverage`.
const coverageGolden = "../../tests/api-coverage.golden"

// unresolved stands for a part of a path that is not known until the command
// runs.
const (
	unresolved = "\x00"
	// numeric marks a segment a format string will fill with a number, which
	// the recording end reduces to {n} rather than {id}.
	numeric = "\x00n"
)

// unreachable are the requests the suite cannot make, and why. They are the
// exceptions that have to be argued for, not a list to grow when a test is
// inconvenient to write.
//
// "The accounts do not have the plan for it" is not one of them any more: the
// suite signs a paid account in on every run. What is left is what no run of
// anything could do.
var unreachable = map[string]string{
	"DELETE /auth/v4/sessions": "revoking every other session would end the run",

	"GET /core/v4/keys/salts": "only a first unlock derives the key password, and the suite resumes a session",

	"POST /core/v4/auth/2fa": "only a sign-in answers a second factor there, and no test signs in from nothing",

	// Everything about registering a security key. Reaching any of the four means
	// holding the ceremony, and the ceremony is a person touching a key: a run on
	// a machine with one plugged in would block until somebody did, and if
	// somebody did it would enrol their key on a test account for good. A key that
	// is not plugged in fails before the request. So what is tested is the listing,
	// the preview, and every refusal that comes before the network; the ceremony
	// itself is covered offline, against a key made of software.
	"GET /core/v4/settings/2fa/register":     "registering a key is a person touching one, which no run can do or should",
	"POST /core/v4/settings/2fa/register":    "the same: there is nothing to hand back without a key somebody touched",
	"PUT /core/v4/settings/2fa/{id}/rename":  "renaming needs a registered key, and no run may register one",
	"POST /core/v4/settings/2fa/{id}/remove": "removing needs a registered key, and removing somebody's real one is worse",

	// The organization's key, asked about only when an administrator changes a
	// password - to refuse the change if that key is locked with it. The one test
	// account with an organization is somebody's own, and it refuses every
	// command that would change a credential.
	"GET /core/v4/organizations/keys": "only an administrator's password change asks, and the account with an organization refuses that command",

	// Everything a password reset leaves behind. Only a reset locks a key or a
	// volume, and a run that reset a test account's password would lock that
	// account's Drive for good and spend keys nothing can put back - so no test
	// may make the state these requests answer, and every test account is in the
	// state where they are refused before the network. What the CLI builds is
	// covered offline instead, against the shape Proton documents.
	"GET /core/v4/settings/mnemonic":        "only a key a password reset locked is opened with a recovery phrase, and no test may reset a password",
	"PUT /core/v4/keys/user/{id}":           "the same: nothing but a reset leaves a key to reactivate",
	"PUT /drive/volumes/{id}/restore":       "the same: nothing but a reset leaves a volume locked",
	"PUT /drive/volumes/{id}/delete_locked": "the same, and deleting a locked volume cannot be undone by a run",
	"GET /drive/volumes/{id}":               "only a locked volume the volume listing left out is read alone, and no test account has one",

	"POST /drive/volumes": "a volume is made once in an account's life, and every test account is long past that moment",

	// Everything about reading a password-protected message. Proton seals a
	// message to a password only for a recipient it has no keys for, so no address
	// any test account holds can be sent one - and the link that names it arrives
	// in that outside mailbox and nowhere else, which is a mailbox no run can
	// read. A run can reach the first of these with an id nobody issued, and a
	// refusal is not coverage. What is tested instead is the sending end, which is
	// where these messages come from, and the decryption, against messages built
	// the way Proton builds them.
	"GET /mail/v4/eo/token/{id}":      "only a mailbox outside Proton is sent such a link, and no run can read one",
	"GET /mail/v4/eo/message":         "the same: without a link there is no token to ask with",
	"GET /mail/v4/eo/attachment/{id}": "the same",
	"POST /mail/v4/eo/reply":          "the same, and an answer would go to a real mailbox",

	// Asking what the account's plan allows. It is sent only once a listing of
	// custom domains has already failed, and the only account whose listing fails
	// is one with no organization - which answers this with a refusal rather than
	// an answer, and a refusal is not coverage. An account that has an
	// organization never fails the listing, so no run can be in the state where
	// this is both sent and answered.
	"GET /core/v4/organizations": "only a failed domain listing asks, and an account whose listing fails has no organization to answer about",
}

// untested are the requests a run could make and does not. Each is a gap somebody
// chose to leave, so it is named here and reported on every run rather than
// passing quietly. The list is something to shorten.
var untested = map[string]string{
	// Both halves of confirming a mailbox. Proton meters everything under a
	// mailbox's /verify the way a brute-force guard does, so a suite that spends
	// a code attempt on every run fails on that quota rather than on anything
	// real - and it stays failing for the best part of an hour, which is the
	// opposite of what a test is for. The flow itself was walked by hand against
	// the live API; what is left out is repeating it every run.
	"GET /pass/v1/user/alias/mailbox/{n}/verify":  "sending the confirmation code again",
	"POST /pass/v1/user/alias/mailbox/{n}/verify": "handing the confirmation code back",

	// Confirming a watched address, which is the same guard a third time. A run
	// adds one address and spends the one verification email that costs; asking
	// for the email again is what the quota is counted in, and the code itself
	// arrives at a domain that takes no mail, so there is nothing to hand back
	// even if spending an attempt every run were free.
	//
	// The last two follow from it. Proton is not watching an address nobody has
	// verified, so there is nothing there to read and nothing to pause - it
	// refuses both, and so does the CLI, before the request. What is tested is
	// adding the address, the three refusals, and removing it.
	"POST /pass/v1/breach/custom_email/{id}/resend_verification": "asking for a second verification email is what Proton's quota counts",
	"PUT /pass/v1/breach/custom_email/{id}/verify":               "handing back a code that arrives in a mailbox no run can read",
	"GET /pass/v1/breach/custom_email/{id}/breaches":             "needs a verified address added by hand, and no run can verify one",
	"PUT /pass/v1/breach/custom_email/{id}/monitor":              "the same: Proton is not watching an unverified address, so there is nothing to pause",

	// The auto-reply is a paid feature, so only the paid account could reach it -
	// and it is the one setting a run cannot put back. Proton keeps the last
	// message even while the auto-reply is off and offers no way to clear it
	// (`set --message ""` is refused), so a test would leave its own text in
	// somebody's real settings for good. Turning it off restores the behaviour
	// and not the state, which is not the same thing.
	"PUT /mail/v4/settings/autoresponder": "writing an auto-reply cannot be undone, and only a real account has the plan for one",

	// Wiping the security log. The events in it are the only ones the accounts
	// have - Proton records a sign-in from a named client and never one from this
	// CLI, so nothing a run does puts an event back, and a run that wiped once
	// would leave the listing with nothing to be checked against for good. What is
	// tested is the listing, the state, both toggles and the preview of the wipe.
	"DELETE /core/v4/logs/auth": "the events it removes are ones no run can cause Proton to record again",

	// Reporting phishing hands the message, decrypted, to the people at Proton
	// who read reports. A suite that ran it every run would file its own test mail
	// as an attack on somebody's desk, over and over, and no run could take one
	// back. The flow was walked by hand against the live API instead.
	"POST /core/v4/reports/phishing": "a report is read by a person at Proton and cannot be withdrawn",

	// A refresh happens when a session expires mid-run, which is Proton's to
	// decide and no test can arrange - so whether a run reaches it is luck. It
	// stays listed rather than being relied on: a golden line that appears and
	// disappears with the age of a saved session is not coverage. The exchange
	// itself is covered by internal/proton's own tests against a stub, and a run
	// that does happen to refresh will report this entry as stale.
	"POST /auth/v4/refresh": "only an expiring session causes one, and a run cannot make its session expire",

	// Everything about a computer. Only the Proton Drive desktop app registers
	// one - the web client cannot, so neither can this CLI - and no test account
	// has one for a run to find, which a test cannot arrange and cannot skip over.
	// What the CLI does with the answers is covered offline instead, by
	// internal/service/drive/computers_test.go against the shape Proton documents;
	// what is not covered anywhere is that the live answers have that shape.
	"GET /drive/devices":         "needs an account with a computer, and only the Proton Drive desktop app makes one",
	"PUT /drive/devices/{id}":    "the same, and it fires only for a computer named before Proton moved the name onto the root",
	"DELETE /drive/devices/{id}": "the same, and deleting the one computer an account has cannot be undone by a run",

	// Leaving a mailing list, from either end: the message that arrived, or the
	// sender behind it. Proton records a subscription when mail carrying List-
	// headers arrives from a real sender, and nothing a run does puts one there -
	// a message these accounts send each other has no such headers, so the record
	// never appears and the four requests that act on one cannot be reached. The
	// listing is tested, and so is every refusal that is settled before a request.
	"POST /mail/v4/messages/{id}/unsubscribe":                 "reaching it needs a message from a real mailing list carrying a List-Unsubscribe header, which no seeding can put on these accounts",
	"PUT /mail/v4/messages/mark/unsubscribed":                 "the same message: only one from a real list is left by sending mail or opening a link",
	"POST /mail/v4/newsletter-subscriptions/{id}":             "only a real list leaves a subscription record to mark as left",
	"POST /mail/v4/newsletter-subscriptions/{id}/unsubscribe": "the same record, which no run can cause Proton to create",
	"POST /mail/v4/newsletter-subscriptions/{id}/filter":      "the same record: a standing rule needs a list to put it on",
	"DELETE /mail/v4/newsletter-subscriptions/{id}":           "the same record, which has to exist before it can be forgotten",
	"GET /calendar/v1/{id}/events/{id}/attendees":             "reaching it needs an event with more attendees than a page holds, which would mean inviting a hundred addresses from these accounts",

	// Deleting an address. Proton allows one a year, and the paid account - the
	// one account that may delete at all - refuses the command outright, so
	// nothing here spends it.
	"PUT /core/v4/addresses/{id}/delete":          "Proton allows one address deletion a year, and no test may spend it",
	"GET /core/v4/addresses/allowAddressDeletion": "only deleting an address asks whether the year's allowance is still there",

	// Adding one. The forwarding fixture is the only address a run makes, and the
	// deletion above is why it is minted once and then kept: an address made per
	// run would spend the year's allowance and leave nothing to spend it on. So
	// the only run that reaches this is the first one on an account that has not
	// got the fixture yet, and every test account has had it for a while.
	"POST /core/v4/addresses": "the fixture address is minted once in an account's life, and every test account is past that moment",

	// Both halves of verifying a recovery phone. Proton texts a code to a real
	// number, and no test account has one: a number a run can set is one nobody
	// receives the code at, so the send is all there is and the code cannot come
	// back. Setting and removing the number is tested; the flow was walked by
	// hand against the live API with a real phone.
	"POST /core/v4/users/code":   "Proton texts a code to a real phone, and no test account has one",
	"POST /core/v4/verify/phone": "handing back a code that arrives on a phone no run can read",

	// Handing the keys to somebody who was offered a vault before they had a
	// Proton account. Reaching it needs the mailbox the offer went to to become a
	// Proton account between one command and the next, which is somebody at a
	// signup form and not something a run can arrange - and once it happened the
	// account would exist for good, so the state could not be made again.
	//
	// Everything up to it is tested: the offer is made, listed as waiting,
	// refused for confirmation while it is, and withdrawn. Drive's half of the
	// same feature is covered the same way, and its conversion is one request the
	// suite already sends - an ordinary invitation - so what is untested here is
	// the Pass request alone.
	"POST /pass/v1/share/{id}/invite/new_user/{id}/keys": "reaching it needs a mailbox to become a Proton account mid-run, which nobody can arrange",

	// Turning on the short-domain address. An account has one for its lifetime
	// and the paid account's is already on, so the only account a run could turn
	// one on for is a free one, which Proton does not let have it. The refusal on
	// each side is tested instead: the free plan is refused before the request,
	// and the paid account is told the address is already there.
	"POST /core/v4/addresses/setup": "the short domain is turned on once in an account's life, and every test account is past that moment",

	// Everything a verified alias domain has. Proving a domain is yours means a
	// TXT entry in a real zone, which no run can add, so the domains a run makes
	// under the paid account's own name stay unverified - and Proton keeps the
	// settings from an unverified domain, so the CLI refuses to change them before
	// the request. Adding a domain, checking its DNS, choosing the default and
	// deleting it are tested; what is left is reached only through a domain
	// somebody verified by hand.
	"GET /pass/v1/user/alias/custom_domain/{n}/settings":               "needs a custom alias domain somebody verified by hand, and no run can put an entry in a real zone",
	"PUT /pass/v1/user/alias/custom_domain/{n}/settings/catch_all":     "the same: only a verified domain has a catch-all to switch",
	"PUT /pass/v1/user/alias/custom_domain/{n}/settings/mailboxes":     "the same: only a verified domain has mailboxes to point stray mail at",
	"PUT /pass/v1/user/alias/custom_domain/{n}/settings/name":          "the same: only a verified domain has a display name to set",
	"PUT /pass/v1/user/alias/custom_domain/{n}/settings/random_prefix": "the same: only a verified domain has a random prefix to switch",
}

func TestEveryRequestTheCLICanSendIsOneTheSuiteSends(t *testing.T) {
	exercised, err := readCoverage(coverageGolden)
	if err != nil {
		t.Fatalf("read the recorded API surface: %v\n\nRun `just coverage` to record it.", err)
	}
	emitted := emittableRequests(t)
	if len(emitted) == 0 {
		t.Fatal("found no requests in the source; the extractor is broken")
	}

	var missing, known, closed []string
	for _, req := range emitted {
		switch {
		case exercised[req]:
			if unreachable[req] != "" || untested[req] != "" {
				closed = append(closed, req)
			}
		case unreachable[req] != "":
		case untested[req] != "":
			known = append(known, req)
		default:
			missing = append(missing, req)
		}
	}
	sort.Strings(missing)
	sort.Strings(known)
	sort.Strings(closed)
	for _, req := range missing {
		t.Errorf("the CLI can send %s but the live suite never has;\n"+
			"\twrite a test that reaches it, or say why nobody can in `unreachable`,\n"+
			"\tor record it in `untested` if it is a gap you are leaving open", req)
	}
	// A gap that has closed has to be taken off the list. Both lists are arguments
	// for why the suite does not reach something, and one the recording contradicts
	// is an argument nobody should read again - a list of gaps is only worth having
	// while every line in it is still true.
	for _, req := range closed {
		t.Errorf("the live suite reaches %s, so the entry excusing it is stale;\n"+
			"\tdelete it from `unreachable` or `untested`", req)
	}
	// The gaps already known about are said out loud on every run, so the list is
	// something to shorten rather than somewhere to put things.
	for _, req := range known {
		t.Logf("not covered by the live suite: %s (%s)", req, untested[req])
	}
}

func readCoverage(path string) (map[string]bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	out := map[string]bool{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out[line] = true
	}
	return out, scanner.Err()
}

// emittableRequests reads every proton.Request the CLI builds and renders it as a
// method and a path template.
//
// The raw `api` command is left out on purpose: its whole purpose is to send a
// request nothing else models, so it can emit anything and covers nothing.
func emittableRequests(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	roots := []string{"../account", "../service", "../proton", "../selfmanage"}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return err
			}
			for _, req := range requestsIn(t, path) {
				seen[req] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	out := make([]string, 0, len(seen))
	for req := range seen {
		out = append(out, req)
	}
	sort.Strings(out)
	return out
}

func requestsIn(t *testing.T, path string) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	consts := constantsIn(file)

	var out []string
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok || !isRequestLit(lit) {
			return true
		}
		var method, p string
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			key, ok := kv.Key.(*ast.Ident)
			if !ok {
				continue
			}
			switch key.Name {
			case "Method":
				method = strings.ToUpper(strings.Trim(render(kv.Value, consts), `"`))
			case "Path":
				p = render(kv.Value, consts)
			}
		}
		// A path that is a variable in its entirety names no endpoint that can be
		// read off the source - the two there are, a cross-table probe and the
		// settings tables, are built elsewhere and covered by the commands that use
		// them. Reading source has limits, which is why the other half of this is a
		// recording of a real run.
		//
		// The limit worth knowing: a path chosen into a variable and then handed
		// over is invisible here. The recording covers that for a path some test
		// reaches, so only a path that is both built in a variable and never
		// exercised escapes both halves. Write the path in the request literal.
		if method == "" || p == "" || p == unresolved || strings.Contains(method, unresolved) {
			return true
		}
		out = append(out, method+" "+pathTemplate(p))
		return true
	})
	return out
}

func isRequestLit(lit *ast.CompositeLit) bool {
	switch t := lit.Type.(type) {
	case *ast.Ident:
		return t.Name == "Request"
	case *ast.SelectorExpr:
		return t.Sel.Name == "Request"
	}
	return false
}

// constantsIn collects the file's own string constants, so a path written as a
// name resolves to what it holds.
func constantsIn(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				if i >= len(vs.Values) {
					continue
				}
				if s, ok := literal(vs.Values[i]); ok {
					out[name.Name] = s
				}
			}
		}
	}
	return out
}

func literal(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// render turns a path expression into a string, with anything that is not known
// at compile time standing in as a placeholder.
func render(e ast.Expr, consts map[string]string) string {
	switch v := e.(type) {
	case *ast.BasicLit:
		if s, ok := literal(v); ok {
			return s
		}
	case *ast.Ident:
		if s, ok := consts[v.Name]; ok {
			return s
		}
		return unresolved
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			return render(v.X, consts) + render(v.Y, consts)
		}
	case *ast.CallExpr:
		// fmt.Sprintf("/a/%s/b", …) - the format is the shape.
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			if s, ok := literal(v.Args[0]); ok {
				return s
			}
		}
	}
	return unresolved
}

// template normalises a rendered path: whatever was not a literal, and whatever a
// format string left a verb for, is the same placeholder an exercised path is
// reduced to.
//
// A number is its own placeholder, because the recording end can tell one from
// an opaque ID by looking and does. %d is what says a segment is a number here,
// since nothing else in these paths is formatted with it.
func pathTemplate(p string) string {
	p = strings.ReplaceAll(p, "%d", numeric)
	for _, verb := range []string{"%s", "%v", "%q"} {
		p = strings.ReplaceAll(p, verb, unresolved)
	}
	segments := strings.Split(p, "/")
	for i, s := range segments {
		switch {
		case strings.Contains(s, numeric):
			segments[i] = "{n}"
		case strings.Contains(s, unresolved):
			segments[i] = "{id}"
		}
	}
	return strings.Join(segments, "/")
}
