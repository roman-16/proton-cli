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
	"DELETE /auth/v4/sessions":      "revoking every other session would end the run",
	"DELETE /auth/v4/sessions/{id}": "the only session there is to revoke is the one running",

	"GET /core/v4/keys/salts":         "only a first unlock derives the key password, and the suite resumes a session",
	"PUT /auth/v4/sessions/local/key": "written once, at the first unlock, before the suite runs",

	"POST /auth/v4/sessions": "only a first sign-in creates one, and no test signs out to force another",
	"POST /core/v4/auth/2fa": "no test account has two-factor enabled, so nothing is ever asked for a code",
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

	// The auto-reply is a paid feature, so only the paid account could reach it -
	// and it is the one setting a run cannot put back. Proton keeps the last
	// message even while the auto-reply is off and offers no way to clear it
	// (`set --message ""` is refused), so a test would leave its own text in
	// somebody's real settings for good. Turning it off restores the behaviour
	// and not the state, which is not the same thing.
	"PUT /mail/v4/settings/autoresponder": "writing an auto-reply cannot be undone, and only a real account has the plan for one",

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

	// Reading a breach needs an address that has been in one. TestPassBreaches...
	// asks for the list and stops when every watched address is clean, which is
	// the good outcome and an untestable one.
	"GET /pass/v1/breach/address/{id}/breaches":      "needs a watched address that has actually been breached",
	"GET /pass/v1/breach/custom_email/{id}/breaches": "needs a breached address added by hand, which is the same problem",
	"POST /mail/v4/messages/{id}/unsubscribe":        "reaching it needs a message from a real mailing list carrying a List-Unsubscribe header, which no seeding can put on these accounts",
	"GET /calendar/v1/{id}/events/{id}/attendees":    "reaching it needs an event with more attendees than a page holds, which would mean inviting a hundred addresses from these accounts",

	// Deleting an address. Proton allows one a year, and the paid account - the
	// one account that may delete at all - refuses the command outright, so
	// nothing here spends it. Adding one is reached instead by the run that mints
	// the fixture address, which is why it is not on this list.
	"PUT /core/v4/addresses/{id}/delete":          "Proton allows one address deletion a year, and no test may spend it",
	"GET /core/v4/addresses/allowAddressDeletion": "only deleting an address asks whether the year's allowance is still there",

	// Turning on the short-domain address. An account has one for its lifetime
	// and the paid account's is already on, so the only account a run could turn
	// one on for is a free one, which Proton does not let have it. The refusal on
	// each side is tested instead: the free plan is refused before the request,
	// and the paid account is told the address is already there.
	"POST /core/v4/addresses/setup": "the short domain is turned on once in an account's life, and every test account is past that moment",
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
