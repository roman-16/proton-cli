package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// Every command the documentation shows is a command that exists.
//
// The command reference is generated, so it cannot drift. Everything else - the
// guides, the per-app pages, the README - is prose somebody wrote by hand, and
// prose drifts silently: a renamed command leaves a page telling people to run
// something that is gone, and nothing fails. This reads every invocation the
// documentation shows and resolves it against the tree the binary actually
// builds, so a rename that misses a page fails here instead of confusing a
// reader.
//
// It lives beside the conformance test rather than in tests/offline because it
// needs the tree and nothing else: no binary, no session, no network.

// docRoots are the files a reader is expected to follow instructions from.
//
// The site's front page is among them. What each app can do is written there as
// markdown for exactly this reason: the page most people meet the tool through
// is the last place a renamed command should survive.
var docRoots = []string{
	"../../CONTRIBUTING.md",
	"../../README.md",
	"../../docs",
	"../../web/src/content/landing",
}

// doc is a page this test names, from the root of the repository.
func doc(path string) string { return filepath.Join("..", "..", path) }

// ── the facts a page states as a whole list ──

// enumeration is a fact a page states as a complete list, and the tree question
// that settles it.
//
// A page promising "these are the ones" makes a claim the tree can answer, and
// the way such a claim rots is silent: a fifth command joins the set and the
// sentence listing four still reads perfectly. Three pages named the commands
// Proton asks for a password again on; conformance pinned three of them and two
// of the pages said two.
type enumeration struct {
	// what the list is, for the failure message.
	what string
	// pages that promise it in full. A page mentioning one member in passing is
	// not making the claim; these are the ones that enumerate.
	pages []string
	// members, from the tree.
	members func(*cobra.Command) []string
}

var enumerations = []enumeration{{
	what:  "the commands Proton makes you prove yourself for again",
	pages: []string{doc("docs/account/README.md")},
	members: func(root *cobra.Command) []string {
		var out []string
		walkTree(root, func(c *cobra.Command) {
			// Signing in is where a password is expected; these pages are about
			// the commands that ask for it when you are already signed in.
			if c.Name() == "login" || c.Flags().Lookup("password-file") == nil {
				return
			}
			out = append(out, strings.TrimPrefix(c.CommandPath(), kit.Program+" "))
		})
		return out
	},
}, {
	what:  "the flags that work on every command",
	pages: []string{doc("docs/using/settings.md")},
	members: func(root *cobra.Command) []string {
		var out []string
		root.PersistentFlags().VisitAll(func(f *pflag.Flag) {
			if !f.Hidden {
				out = append(out, "--"+f.Name)
			}
		})
		return out
	},
}}

func TestPagesThatPromiseAWholeListHaveTheWholeList(t *testing.T) {
	root := newRoot()
	for _, e := range enumerations {
		members := e.members(root)
		if len(members) == 0 {
			t.Fatalf("%s: the tree answers with nothing; the question is broken", e.what)
		}
		for _, page := range e.pages {
			src, err := os.ReadFile(page)
			if err != nil {
				t.Fatalf("read %s: %v", page, err)
			}
			for _, member := range members {
				if !strings.Contains(string(src), member) {
					t.Errorf("%s lists %s but never names `%s`",
						filepath.Base(page), e.what, member)
				}
			}
		}
	}
}

func walkTree(c *cobra.Command, visit func(*cobra.Command)) {
	if c.Hidden {
		return
	}
	if c.Runnable() {
		visit(c)
	}
	for _, sub := range c.Commands() {
		walkTree(sub, visit)
	}
}

// ── the flags a screen names ──

// A flag a command's own help names is a flag that command takes.
//
// The check above reads pages, and a page's sentence has no command to be held
// against: `update REF --future` on the Calendar page names no screen it belongs
// to, so its flags are not attributed to one. A Short and a Long are the other
// case entirely - there is exactly one command whose help they are - which is
// why this asks the tree rather than the pages built from it.
//
// Nothing weaker than "on this command" would do. `events list` went a release
// telling people to narrow it with --start and --end after the pair became
// --after and --before, and every existence check passes that: --start is still
// live on `events create`, two commands away.
//
// A flag belonging to another command is written with that command - `items list
// --risk` - which is where it is read against. Where the sentence cannot name
// one, because it points at a collection or at the CLI at large, the pointer is
// declared below.

// flagsNamedElsewhere are the screens whose help names a flag that lives
// somewhere else, and where it lives. An owner that is a group means any command
// under it: --computer works on every `items` command, which is what that
// sentence says and what no single command could stand for.
var flagsNamedElsewhere = map[string]map[string]string{
	kit.Program + " completion": {
		"folder": "mail messages list",
		"into":   "drive items move",
		"vault":  "pass items list",
	},
	kit.Program + " drive computers list":            {"computer": "drive items"},
	kit.Program + " drive items list":                {"scope": "drive items move"},
	kit.Program + " drive items revisions":           {"if-exists": "drive items upload"},
	kit.Program + " drive shared add":                {"shared": "drive items"},
	kit.Program + " drive shared list":               {"shared": "drive items"},
	kit.Program + " mail messages unschedule":        {"send-at": "mail messages send"},
	kit.Program + " mail settings addresses reorder": {"from": "mail messages send"},
}

func TestEveryFlagTheHelpNamesIsOnThatCommand(t *testing.T) {
	root := newRoot()
	spoken := map[string]map[string]bool{}
	var problems []string
	for _, c := range everyCommand(root) {
		path := c.CommandPath()
		elsewhere := flagsNamedElsewhere[path]
		own, named := flagsPutOn(root, c, c.Short+" "+c.Long)
		for _, cl := range named {
			if !takesUnder(cl.on, cl.flag) {
				problems = append(problems, fmt.Sprintf(
					"%s: its help names `%s`, and %s takes no --%s",
					path, cl.quoted, cl.on.CommandPath(), cl.flag))
			}
		}
		for _, flag := range own {
			owner, pointed := elsewhere[flag]
			if !pointed {
				if !takes(c, flag) {
					problems = append(problems, fmt.Sprintf(
						"%s: its help names --%s, which it does not take; "+
							"a flag that lives on another command goes in flagsNamedElsewhere",
						path, flag))
				}
				continue
			}
			if spoken[path] == nil {
				spoken[path] = map[string]bool{}
			}
			spoken[path][flag] = true
			at, _, err := root.Find(strings.Fields(owner))
			if err != nil || !takesUnder(at, flag) {
				problems = append(problems, fmt.Sprintf(
					"%s: its help names --%s, declared as living on `%s`, which does not take it",
					path, flag, owner))
			}
		}
	}
	// The other direction. A pointer left behind after the sentence that needed it
	// stops describing the screen, and the next rename is let through under it.
	for path, flags := range flagsNamedElsewhere {
		for flag := range flags {
			if !spoken[path][flag] {
				problems = append(problems, fmt.Sprintf(
					"flagsNamedElsewhere says %s names --%s, and its help does not", path, flag))
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// Every flag the CLI prints at run time is one it has.
//
// A hint is worse than a help screen when it rots, because it is an instruction
// somebody follows straight away: `pass items totp` spent a release telling
// people to store a secret with a flag that had been gone since the release
// before. Nothing reaches these strings from the tree, so they are read where
// they are written.
//
// A command line inside backticks is held to the command it names, where the
// line says unmistakably which one that is. A string has no screen to be read
// from, so a word that several commands answer to names none of them here, and
// what is left is held to the loosest thing still worth checking: the flag
// exists somewhere in this CLI.

// otherPrograms open a command line the CLI prints for a tool that is not it -
// the package managers that installed it. Their flags are theirs.
var otherPrograms = []string{"brew ", "npm ", "winget "}

func TestEveryFlagTheCLIPrintsExists(t *testing.T) {
	root := newRoot()
	known := map[string]bool{}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { known[f.Name] = true })
	for _, c := range everyCommand(root) {
		// cobra adds --help when a command runs rather than when it is built, and
		// the CLI prints it in its own help footers.
		c.InitDefaultHelpFlag()
		c.Flags().VisitAll(func(f *pflag.Flag) { known[f.Name] = true })
	}

	var problems []string
	for _, lit := range printedStrings(t, []string{".."}) {
		text := lit.text
		if slices.ContainsFunc(otherPrograms, func(p string) bool {
			return strings.HasPrefix(strings.TrimSpace(text), p)
		}) {
			continue
		}
		for _, span := range inlineSpan.FindAllStringSubmatch(text, -1) {
			named := namedCommandAnywhere(root, span[1])
			if named == nil {
				continue
			}
			for _, flag := range flagTokens(span[1]) {
				if !takesUnder(named, flag) {
					problems = append(problems, fmt.Sprintf("%s: `%s` - no --%s on %s",
						lit.where, span[1], flag, named.CommandPath()))
				}
			}
			text = strings.Replace(text, span[0], " ", 1)
		}
		for _, flag := range flagTokens(text) {
			if !known[flag] {
				problems = append(problems, fmt.Sprintf("%s: --%s is on no command", lit.where, flag))
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// printed is one string the source carries, and where it is written.
type printed struct {
	text  string
	where string
}

func printedStrings(t *testing.T, dirs []string) []printed {
	t.Helper()
	var out []printed
	fset := token.NewFileSet()
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			file, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if perr != nil {
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				expr, ok := n.(ast.Expr)
				if !ok {
					return true
				}
				text, ok := literal(expr)
				if !ok || !strings.Contains(text, "--") {
					return true
				}
				out = append(out, printed{text: text, where: fmt.Sprintf("%s:%d",
					filepath.ToSlash(p), fset.Position(expr.Pos()).Line)})
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	if len(out) == 0 {
		t.Fatal("read no printable strings out of the source; the walk is broken")
	}
	return out
}

// The checker earns its own test, twice over: one that reads no flag out of a
// screen would pass while every screen rotted, and one that read a flag off the
// wrong command would fail on prose that is right.
func TestTheHelpCheckerReadsWhatAScreenNames(t *testing.T) {
	root := newRoot()
	for _, c := range []struct {
		name string
		at   []string
		text string
		own  []string
		on   map[string]string
	}{{
		name: "a flag written bare is the screen's own",
		at:   []string{"calendar", "events", "list"},
		text: "Covers every calendar unless --calendar narrows it to one.",
		own:  []string{"calendar"},
	}, {
		name: "a renamed flag is still read, which is what catches it",
		at:   []string{"calendar", "events", "list"},
		text: "--start and --end are whole days in your own zone, both included.",
		own:  []string{"start", "end"},
	}, {
		name: "a flag quoted with its value is the screen's own",
		at:   []string{"drive", "computers", "list"},
		text: "Pass `--computer REF` to any `items` command to work inside it.",
		own:  []string{"computer"},
	}, {
		name: "a flag written with a command belongs to that command",
		at:   []string{"pass", "breaches", "disable"},
		text: "Pausing an alias also leaves it out of `items list --risk`.",
		on:   map[string]string{"risk": "proton pass items list"},
	}, {
		name: "a command written whole is read whole",
		at:   []string{"pass", "settings", "mailboxes", "list"},
		text: "To point an alias at one, run `proton pass items update REF --mailbox`.",
		on:   map[string]string{"mailbox": "proton pass items update"},
	}, {
		name: "a sibling is read from where the reader stands",
		at:   []string{"drive", "items", "update"},
		text: "Renaming is `update --name`; there is no `rename` verb.",
		on:   map[string]string{"name": "proton drive items update"},
	}, {
		name: "a negation names the flag it negates",
		at:   []string{"calendar", "events", "update"},
		text: "--all-day=false is the other direction.",
		own:  []string{"all-day"},
	}, {
		name: "a placeholder and a format verb are not flags",
		at:   []string{"calendar", "events", "list"},
		text: kit.Program + " <app> <collection> <verb> [TARGET...] [--flags], --%s",
	}} {
		t.Run(c.name, func(t *testing.T) {
			at, _, err := root.Find(c.at)
			if err != nil {
				t.Fatalf("find %v: %v", c.at, err)
			}
			own, named := flagsPutOn(root, at, c.text)
			if !slices.Equal(own, c.own) {
				t.Errorf("read %v as the screen's own, want %v", own, c.own)
			}
			got := map[string]string{}
			for _, cl := range named {
				got[cl.flag] = cl.on.CommandPath()
			}
			if len(got) != len(c.on) {
				t.Fatalf("read %v as another command's, want %v", got, c.on)
			}
			for flag, want := range c.on {
				if got[flag] != want {
					t.Errorf("--%s was read as %s's, want %s's", flag, got[flag], want)
				}
			}
		})
	}
}

// A string is read as a command line only where it names one command and no
// other. Guessing is worse than the loose check it would replace: an instruction
// read off the wrong command fails on prose that is right.
func TestThePrintedCheckerReadsACommandLineOnlyWhenItIsOne(t *testing.T) {
	root := newRoot()
	for _, c := range []struct{ name, span, want string }{
		{"a line naming one command", "pass items update REF --secret-file totp-uri=FILE",
			"proton pass items update"},
		{"a line written with the program", "proton mail messages list --unread",
			"proton mail messages list"},
		{"a verb eight collections answer to", "update --name", ""},
		{"a flag with its value", "--computer REF", ""},
		{"another program's line", "brew upgrade --cask proton-cli", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ""
			if found := namedCommandAnywhere(root, c.span); found != nil {
				got = found.CommandPath()
			}
			if got != c.want {
				t.Errorf("read %q as %q, want %q", c.span, got, c.want)
			}
		})
	}
}

// claim is one flag a screen names, and the command the sentence put it on.
type claim struct {
	flag string
	on   *cobra.Command
	// quoted is the span it was read out of, so a failure quotes the sentence.
	quoted string
}

// flagsPutOn separates the flags a screen claims for itself from the ones it
// hands to a command it names. A flag written bare is this screen's: somebody
// reading `--send-at` on an unschedule screen will type it there.
func flagsPutOn(root, at *cobra.Command, text string) (own []string, named []claim) {
	text = unwrapped(text)
	for _, span := range inlineSpan.FindAllStringSubmatch(text, -1) {
		on := namedCommand(root, at, span[1])
		if on == nil {
			continue
		}
		for _, flag := range flagTokens(span[1]) {
			named = append(named, claim{flag: flag, on: on, quoted: span[1]})
		}
		text = strings.Replace(text, span[0], " ", 1)
	}
	return flagTokens(text), named
}

// everyCommand is the whole tree a reader can reach, groups included: a group
// has a Long of its own, and `items revisions` names a flag in it.
func everyCommand(root *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden {
			return
		}
		out = append(out, c)
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(root)
	return out
}

// unwrapped is help text as a reader meets it: the lines a Long is written in
// are where the screen breaks, not where a sentence does, and a command
// reference spans one of them.
func unwrapped(s string) string { return strings.Join(strings.Fields(s), " ") }

// flagTokens reads the long flags a piece of prose names. A flag opens the span
// it is quoted in and closes the clause it ends, and neither the backtick nor
// the full stop is part of its name.
func flagTokens(text string) []string {
	var out []string
	for _, tok := range strings.Fields(text) {
		if name, ok := longFlag(strings.Trim(tok, "`.,:;!?()\"'")); ok {
			out = append(out, name)
		}
	}
	return out
}

// namedCommand reads the command a span names, the way the reader of that screen
// does: whole, or relative to where they are standing. `items list` on a Pass
// screen is `pass items list`, and `attachments list` under `pass items` is the
// one beside it.
func namedCommand(root, at *cobra.Command, span string) *cobra.Command {
	var words []string
	for _, tok := range strings.Fields(span) {
		if !word.MatchString(tok) {
			break
		}
		words = append(words, tok)
	}
	if len(words) == 0 {
		return nil
	}
	if words[0] == kit.Program || words[0] == kit.Alias {
		return descend(root, words[1:])
	}
	for from := at; from != nil; from = from.Parent() {
		if found := descend(from, words); found != nil {
			return found
		}
	}
	return nil
}

// namedCommandAnywhere reads the command a span names with nowhere to read it
// from: the whole line, or a tail that only one command in the tree answers to.
// `pass items update REF` is one command; `update --name` is eight, and eight is
// none.
func namedCommandAnywhere(root *cobra.Command, span string) *cobra.Command {
	var words []string
	for _, tok := range strings.Fields(span) {
		if !word.MatchString(tok) {
			break
		}
		words = append(words, tok)
	}
	if len(words) == 0 {
		return nil
	}
	if words[0] == kit.Program || words[0] == kit.Alias {
		return descend(root, words[1:])
	}
	var found *cobra.Command
	for _, c := range everyCommand(root) {
		path := strings.Fields(c.CommandPath())
		if len(path) <= len(words) || !slices.Equal(path[len(path)-len(words):], words) {
			continue
		}
		if found != nil {
			return nil
		}
		found = c
	}
	return found
}

// descend walks words down from cur while they name subcommands, and answers
// only when the first of them did.
func descend(cur *cobra.Command, words []string) *cobra.Command {
	var found *cobra.Command
	for _, w := range words {
		next := child(cur, w)
		if next == nil {
			break
		}
		cur, found = next, next
	}
	return found
}

// takes reports whether this command accepts the flag, its own or inherited.
func takes(c *cobra.Command, name string) bool {
	c.InitDefaultHelpFlag()
	return c.Flags().Lookup(name) != nil || c.InheritedFlags().Lookup(name) != nil
}

// takesUnder is the same question of a command that may be a group, where the
// answer is any command it holds: "any `items` command" is a real thing to say
// about a flag and names no single screen.
func takesUnder(c *cobra.Command, name string) bool {
	if c == nil {
		return false
	}
	if takes(c, name) {
		return true
	}
	for _, sub := range c.Commands() {
		if takesUnder(sub, name) {
			return true
		}
	}
	return false
}

func TestEveryCommandTheDocsShowExists(t *testing.T) {
	files := markdownFiles(t)
	if len(files) == 0 {
		t.Fatal("found no documentation to check; the walk is broken")
	}
	root := newRoot()

	var problems []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		for _, inv := range invocations(root, string(src)) {
			if err := resolves(root, inv.words); err != nil {
				problems = append(problems,
					filepath.Base(path)+": `"+inv.text+"` - "+err.Error())
				continue
			}
			for _, flag := range unknownFlags(root, inv) {
				problems = append(problems, filepath.Base(path)+": `"+inv.text+
					"` - no --"+flag+" on that command")
			}
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}

// A flag a page shows is a flag that command has.
//
// This is the half that catches a rename, which is how a page rots without the
// command itself ever ceasing to exist. It is deliberately narrow: only long
// flags, only lines that named the program, and nothing after a pipe.
func TestTheDocsCheckerFindsAFlagThatIsNotThere(t *testing.T) {
	root := newRoot()
	for _, c := range []struct {
		src  string
		want []string
	}{
		{src: "```bash\nproton mail messages list --unread\n```"},
		{src: "```bash\nproton mail messages list --nope\n```", want: []string{"nope"}},
		{src: "```bash\nproton mail messages list --limit=3\n```"},
		{src: "```bash\nproton mail messages send --help\n```"},
		{src: "```bash\nproton mail messages list --output json\n```"},
		// Another program's flags, and a page-relative reference, are not this
		// command's to answer for.
		{src: "```bash\nproton drive items download /a --output - | gpg --encrypt\n```"},
		{src: "```bash\nproton mail messages list --unread > out.txt\n```"},
		{src: "`update REF --future` widens it", want: nil},
	} {
		var got []string
		for _, inv := range invocations(root, c.src) {
			got = append(got, unknownFlags(root, inv)...)
		}
		if len(got) != len(c.want) {
			t.Errorf("%q reported %v, want %v", c.src, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q reported %v, want %v", c.src, got, c.want)
			}
		}
	}
}

// The checker earns its own test: a documentation check that silently matched
// nothing would pass forever while the pages rotted.
func TestTheDocsCheckerReadsWhatThePagesSay(t *testing.T) {
	root := newRoot()
	for _, c := range []struct {
		src   string
		want  string // the invocation it should find, or "" for none
		valid bool
	}{
		{src: "```bash\nproton mail messages list --unread\n```", want: "proton mail messages list", valid: true},
		{src: "```console\n$ proton drive items get /a/b.pdf\n```", want: "proton drive items get", valid: true},
		{src: "```bash\npg_dump db | proton drive items upload - /B\n```", want: "proton drive items upload", valid: true},
		{src: "```bash\nPROTON_NO_INPUT=1 proton account login\n```", want: "proton account login", valid: true},
		{src: "run `proton mail settings` to see them", want: "proton mail settings", valid: true},
		{src: "the `mail labels list` command", want: "proton mail labels list", valid: false},
		{src: "the `mail settings labels list` command", want: "proton mail settings labels list", valid: true},
		{src: "```bash\nproton mail messages nope\n```", want: "proton mail messages nope", valid: false},
		{src: "`proton drive items create`", want: "proton drive items create", valid: true},
		// Prose, another tool, and a reference relative to a page's own app are
		// all left alone.
		{src: "`just test-fast` runs in a second", want: ""},
		{src: "proton is unaudited, and the model is written down", want: ""},
		{src: "`trash empty` clears it out", want: ""},
		{src: "`--dry-run` shows the rows", want: ""},
	} {
		found := invocations(root, c.src)
		if c.want == "" {
			if len(found) != 0 {
				t.Errorf("%q: read %q as a command; it is not one", c.src, found[0].text)
			}
			continue
		}
		if len(found) != 1 {
			t.Errorf("%q: found %d invocations, want 1", c.src, len(found))
			continue
		}
		if found[0].text != c.want {
			t.Errorf("%q: read %q, want %q", c.src, found[0].text, c.want)
		}
		if err := resolves(root, found[0].words); (err == nil) != c.valid {
			t.Errorf("%q: resolved=%v, want %v (%v)", c.src, err == nil, c.valid, err)
		}
	}
}

func markdownFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, root := range docRoots {
		info, err := os.Stat(root)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			out = append(out, root)
			continue
		}
		err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(path, ".md") {
				out = append(out, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
	return out
}

// invocation is one command line the documentation shows.
type invocation struct {
	// text is what the page actually printed, for the failure message.
	text string
	// words are the command path tokens, up to the first thing that cannot be
	// one: a flag, an argument, a quote, a pipe.
	words []string
	// flags are the long flag names the line goes on to use, up to the first
	// shell operator. A renamed flag is the way a page rots most often, because
	// a rename leaves the command itself resolving perfectly.
	//
	// They are collected only when the line named the program: a reference
	// relative to a page's own app - `update REF --future` on the Calendar page -
	// has no way of saying which command it belongs to, so its flags cannot be
	// attributed to one either.
	flags []string
}

// A page names a command in one of two ways, and both are read: with the
// program in front of it, which is what a fenced block always has, or bare,
// which is what a sentence uses once the page has established which app it is
// about.
//
// A bare name is only recognised when its first word names one of the tree's
// top-level commands, because that is what separates `mail labels list` from
// another tool's command line or from an ordinary phrase in backticks. A
// reference relative to the page's own app - `trash empty` inside the Drive
// page - is therefore not checked; nothing in the span says which app it
// belongs to, and guessing would report the wrong thing.

var (
	// A fence names its language first and may go on to say how it should be
	// drawn, which is the renderer's business and not this checker's.
	fencedBlock = regexp.MustCompile("(?s)```(?:bash|console|sh|shell)[^\n]*\n(.*?)```")
	inlineSpan  = regexp.MustCompile("`([^`\n]+)`")
	// headingLine is a title, not an instruction. The reference names a command
	// on its own page by the part that page has not already said, so `contacts
	// allow` heads an entry under `proton pass aliases` and means nothing on its
	// own. The synopsis below it carries the whole command line.
	headingLine = regexp.MustCompile(`(?m)^#+ .*$`)
	// commandStart finds the program name wherever a line puts it: at the start,
	// after a prompt, after a pipe, or after environment assignments.
	commandStart = regexp.MustCompile(`(?:^|[|(]\s*|\$\s+|\s)(` + kit.Program + `|` + kit.Alias + `)\s+(.*)$`)
	// word is a command-path token. Anything else - a flag, a path, a quote, a
	// shell variable - ends the path.
	word = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	// shellOperator ends the command line: what comes after belongs to another
	// program, or to the shell.
	shellOperator = map[string]bool{"|": true, "||": true, "&&": true, ";": true,
		">": true, ">>": true, "<": true, "2>": true, "&": true}
)

// invocations finds every command line a page shows, in both fenced blocks and
// inline spans. Both matter: a fenced block is what someone copies, and an
// inline span is what a sentence tells them to run.
func invocations(root *cobra.Command, src string) []invocation {
	var out []invocation
	collect := func(line string) {
		if inv, ok := parseInvocation(root, line); ok {
			out = append(out, inv)
		}
	}
	for _, block := range fencedBlock.FindAllStringSubmatch(src, -1) {
		for _, line := range strings.Split(block[1], "\n") {
			collect(line)
		}
	}
	// Fenced blocks and headings are stripped before the inline pass, so a line
	// inside one is not also read as a span and a title is not read as a command.
	prose := headingLine.ReplaceAllString(fencedBlock.ReplaceAllString(src, ""), "")
	for _, span := range inlineSpan.FindAllStringSubmatch(prose, -1) {
		collect(span[1])
	}
	return out
}

func parseInvocation(root *cobra.Command, line string) (invocation, bool) {
	line = strings.TrimSpace(line)
	program, rest, named := kit.Program, "", false
	if m := commandStart.FindStringSubmatch(line); m != nil {
		program, rest, named = m[1], m[2], true
	} else if first, _, _ := strings.Cut(line, " "); child(root, first) != nil {
		rest = line
	} else {
		return invocation{}, false
	}
	var words, flags []string
	path := true
	for _, tok := range strings.Fields(rest) {
		// What follows a pipe or a redirect is another program's command line,
		// and its flags are none of this one's business.
		if shellOperator[tok] {
			break
		}
		if path && word.MatchString(tok) {
			words = append(words, tok)
			continue
		}
		path = false
		if name, ok := longFlag(tok); ok && named {
			flags = append(flags, name)
		}
	}
	if len(words) == 0 {
		return invocation{}, false
	}
	return invocation{
		text: program + " " + strings.Join(words, " "), words: words, flags: flags,
	}, true
}

// longFlag reads the name out of a --flag token, however the line spells its
// value. A short flag is left alone: one letter says too little to check.
func longFlag(tok string) (string, bool) {
	if !strings.HasPrefix(tok, "--") || tok == "--" {
		return "", false
	}
	name, _, _ := strings.Cut(strings.TrimPrefix(tok, "--"), "=")
	if !word.MatchString(name) {
		return "", false
	}
	return name, true
}

// resolves walks the tree the way cobra would.
//
// Tokens are consumed while they name subcommands. What is left over is the
// command's arguments, which is fine for a leaf and a mistake for a group: a
// group holds commands, so a word it does not hold is a word that does not
// exist.
func resolves(root *cobra.Command, words []string) error {
	cur := root
	i := 0
	for ; i < len(words); i++ {
		next := child(cur, words[i])
		if next == nil {
			break
		}
		cur = next
	}
	switch {
	case cur == root:
		return errNoSuchCommand(words[0], root)
	case i == len(words):
		return nil
	case cur.Runnable():
		// The rest is this command's own arguments.
		return nil
	}
	return errNoSuchCommand(words[i], cur)
}

// unknownFlags are the flags an invocation uses that its command does not have.
func unknownFlags(root *cobra.Command, inv invocation) []string {
	cur := root
	for _, w := range inv.words {
		next := child(cur, w)
		if next == nil {
			break
		}
		cur = next
	}
	// cobra adds --help when a command runs, not when it is built, so a page
	// showing it would otherwise look wrong.
	cur.InitDefaultHelpFlag()
	var out []string
	for _, name := range inv.flags {
		if cur.Flags().Lookup(name) == nil && cur.InheritedFlags().Lookup(name) == nil {
			out = append(out, name)
		}
	}
	return out
}

func errNoSuchCommand(word string, parent *cobra.Command) error {
	var have []string
	for _, sub := range parent.Commands() {
		if !sub.Hidden {
			have = append(have, sub.Name())
		}
	}
	sort.Strings(have)
	where := parent.CommandPath()
	if parent.Parent() == nil {
		where = kit.Program
	}
	return &docError{word: word, where: where, have: have}
}

type docError struct {
	word  string
	where string
	have  []string
}

func (e *docError) Error() string {
	return "no `" + e.word + "` under `" + e.where + "` (has: " + strings.Join(e.have, ", ") + ")"
}

func child(c *cobra.Command, name string) *cobra.Command {
	for _, sub := range c.Commands() {
		if sub.Name() == name {
			return sub
		}
		for _, alias := range sub.Aliases {
			if alias == name {
				return sub
			}
		}
	}
	return nil
}
