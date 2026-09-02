package cli

import (
	"os"
	"path/filepath"
	"regexp"
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
	what: "the commands Proton makes you prove yourself for again",
	pages: []string{
		doc("docs/account/README.md"),
		doc("docs/using/scripting.md"),
		doc("docs/help/troubleshooting.md"),
	},
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
	if c.Hidden || c.Name() == "help" {
		return
	}
	if c.Runnable() {
		visit(c)
	}
	for _, sub := range c.Commands() {
		walkTree(sub, visit)
	}
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
		{src: "```bash\nproton mail messages list --page-size=3\n```"},
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
