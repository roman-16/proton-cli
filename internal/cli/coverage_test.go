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

const coverageGolden = "../../tests/api-coverage.golden"

const (
	unresolved = "\x00"
	numeric    = "\x00n"
)

var unreachable = map[string]string{
	"DELETE /auth/v4/sessions": "revoking every other session would end the run",

	"GET /core/v4/keys/salts": "only a first unlock derives the key password, and the suite resumes a session",

	"POST /core/v4/auth/2fa": "only a sign-in answers a second factor there, and no test signs in from nothing",

	"GET /core/v4/settings/2fa/register":     "registering a key is a person touching one, which no run can do or should",
	"POST /core/v4/settings/2fa/register":    "the same: there is nothing to hand back without a key somebody touched",
	"PUT /core/v4/settings/2fa/{id}/rename":  "renaming needs a registered key, and no run may register one",
	"POST /core/v4/settings/2fa/{id}/remove": "removing needs a registered key, and removing somebody's real one is worse",

	"GET /core/v4/organizations/keys": "only an administrator's password change asks, and the account with an organization refuses that command",

	"GET /core/v4/settings/mnemonic":        "only a key a password reset locked is opened with a recovery phrase, and no test may reset a password",
	"PUT /core/v4/keys/user/{id}":           "the same: nothing but a reset leaves a key to reactivate",
	"PUT /core/v4/keys/address/{id}":        "the same: only a key a reset locked comes back from a copy of itself",
	"PUT /drive/volumes/{id}/restore":       "the same: nothing but a reset leaves a volume locked",
	"PUT /drive/volumes/{id}/delete_locked": "the same, and deleting a locked volume cannot be undone by a run",
	"GET /drive/volumes/{id}":               "only a locked volume the volume listing left out is read alone, and no test account has one",

	"POST /drive/volumes": "a volume is made once in an account's life, and every test account is long past that moment",

	"PUT /core/v4/users/delete":                "deleting a test account would end every run after it; a dry run asks the check that comes first",
	"DELETE /core/v4/organizations/membership": "only a family plan's member leaves it on the way to deleting the account, and deleting ends the account",

	"GET /mail/v4/eo/token/{id}":      "only a mailbox outside Proton is sent such a link, and no run can read one",
	"GET /mail/v4/eo/message":         "the same: without a link there is no token to ask with",
	"GET /mail/v4/eo/attachment/{id}": "the same",
	"POST /mail/v4/eo/reply":          "the same, and an answer would go to a real mailbox",
}

var untested = map[string]string{
	"PUT /account/v1/access/{id}/trigger": "requesting emergency access notifies the granting account, which is always the paid one",
	"PUT /account/v1/access/{id}/reset":   "cancelling acts on a request in flight, and making one notifies the paid account",
	"POST /account/v1/access/{id}/auth":   "signing in opens a session on the granting account, which is always the paid one",

	"GET /core/v4/organizations/settings":    "Proton answers it only for an account in an organization, and the free accounts are in none",
	"POST /meet/v1/meetings/personal/rotate": "Proton lets a personal room's link change once every 24 hours, so a run would fail the next one that day",

	"POST /importer/v1/importers":                     "needs a mailbox on an IMAP server outside Proton, which no test account has",
	"GET /importer/v1/mail/importers/{id}":            "the same: only an importer that connected somewhere has folders to list",
	"POST /importer/v1/importers/start":               "the same: there is nothing to start without one",
	"PUT /importer/v1/importers/{id}":                 "the same: re-supplying credentials needs an importer holding some",
	"PUT /importer/v1/importers/cancel":               "the same: only a running import can be stopped",
	"PUT /importer/v1/importers/resume":               "the same: only a paused import can be picked up",
	"POST /importer/v1/reports/{id}/undo":             "the same: only a finished import can be taken back",
	"DELETE /importer/v1/mail/importers/reports/{id}": "the same: only a finished import leaves a record to forget",

	"GET /pass/v1/user/alias/mailbox/{n}/verify":  "sending the confirmation code again",
	"POST /pass/v1/user/alias/mailbox/{n}/verify": "handing the confirmation code back",

	"POST /pass/v1/breach/custom_email/{id}/resend_verification": "asking for a second verification email is what Proton's quota counts",
	"PUT /pass/v1/breach/custom_email/{id}/verify":               "handing back a code that arrives in a mailbox no run can read",
	"GET /pass/v1/breach/custom_email/{id}/breaches":             "needs a verified address added by hand, and no run can verify one",
	"PUT /pass/v1/breach/custom_email/{id}/monitor":              "the same: Proton is not watching an unverified address, so there is nothing to pause",
	"PUT /pass/v1/breach/custom_email/{id}/resolved":             "the same: only a verified address has breaches to resolve",

	"POST /pass/v1/breach/address/{id}/resolved":            "the only addresses with breaches are somebody's own, and Pass cannot reopen them",
	"POST /pass/v1/share/{id}/alias/{id}/breaches/resolved": "no test alias is in a breach, and none can be put in one",

	"PUT /account/v4/breaches/state": "Dark Web Monitoring has found no breach for any test account, so there is none to resolve or reopen",

	"PUT /mail/v4/settings/autoresponder": "writing an auto-reply cannot be undone, and only a real account has the plan for one",

	"DELETE /core/v4/logs/auth": "the events it removes are ones no run can cause Proton to record again",

	"POST /core/v4/reports/phishing": "a report is read by a person at Proton and cannot be withdrawn",

	"POST /drive/report/share":        "a report is read by a person at Proton and cannot be withdrawn",
	"POST /drive/unauth/report/share": "a report is read by a person at Proton and cannot be withdrawn",

	"POST /auth/v4/refresh": "only an expiring session causes one, and a run cannot make its session expire",

	"PUT /drive/devices/{id}":    "needs a computer, which only the Proton Drive desktop app makes, named before Proton moved the name onto the root",
	"DELETE /drive/devices/{id}": "needs a computer, which only the Proton Drive desktop app makes, and deleting an account's one computer cannot be undone by a run",

	"POST /mail/v4/messages/{id}/unsubscribe":                 "reaching it needs a message from a real mailing list carrying a List-Unsubscribe header, which no seeding can put on these accounts",
	"PUT /mail/v4/messages/mark/unsubscribed":                 "the same message: only one from a real list is left by sending mail or opening a link",
	"POST /mail/v4/newsletter-subscriptions/{id}":             "only a real list leaves a subscription record to mark as left",
	"POST /mail/v4/newsletter-subscriptions/{id}/unsubscribe": "the same record, which no run can cause Proton to create",
	"POST /mail/v4/newsletter-subscriptions/{id}/filter":      "the same record: a standing rule needs a list to put it on",
	"DELETE /mail/v4/newsletter-subscriptions/{id}":           "the same record, which has to exist before it can be forgotten",
	"GET /calendar/v1/{id}/events/{id}/attendees":             "reaching it needs an event with more attendees than a page holds, which would mean inviting a hundred addresses from these accounts",

	"GET /calendar/v1/{id}/busy-schedule": "only Duo, Family, Visionary and business plans may ask, and the paid test account is on Unlimited",

	"PUT /core/v4/addresses/{id}/delete":          "Proton allows one address deletion a year, and no test may spend it",
	"GET /core/v4/addresses/allowAddressDeletion": "only deleting an address asks whether the year's allowance is still there",

	"POST /core/v4/addresses": "the fixture address is minted once in an account's life, and every test account is past that moment",

	"PUT /core/v4/settings/email":       "Proton refuses a recovery address changed on every run as an attempt to take the account over",
	"PUT /core/v4/settings/email/reset": "the same guard, and allowing recovery needs an address in place to allow it through",
	"PUT /core/v4/settings/phone":       "the same guard, for the number",
	"PUT /core/v4/settings/phone/reset": "the same guard, and allowing recovery needs a number in place to allow it through",
	"POST /core/v4/verify/send":         "only an address in place is sent a verification, and the guard refuses putting one there",

	"POST /core/v4/users/code":   "Proton texts a code to a real phone, and no test account has one",
	"POST /core/v4/verify/phone": "handing back a code that arrives on a phone no run can read",

	"POST /pass/v1/share/{id}/invite/new_user/{id}/keys": "reaching it needs a mailbox to become a Proton account mid-run, which nobody can arrange",

	"POST /core/v4/addresses/setup": "the short domain is turned on once in an account's life, and every test account is past that moment",

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
	for _, req := range closed {
		t.Errorf("the live suite reaches %s, so the entry excusing it is stale;\n"+
			"\tdelete it from `unreachable` or `untested`", req)
	}
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
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			if s, ok := literal(v.Args[0]); ok {
				return s
			}
		}
	}
	return unresolved
}

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
