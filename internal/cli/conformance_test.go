package cli

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/redact"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// This file is the interface's specification, expressed as assertions rather
// than as prose.
//
// The CLI's inconsistencies did not arrive through carelessness; they arrived
// because nothing could tell that a new command had picked a different word for
// an existing idea. Every rule the design rests on is checked here, over the
// whole tree, with no network and no credentials, so a divergence fails a test
// instead of shipping.
//
// Rules that the tree does not satisfy yet are skipped with the step that turns
// them on. A skip is a debt with an address.

// ── walking the tree ──

// leaves returns every command that does work, and groups returns every command
// that only holds others.
func partition(t *testing.T) (leaves, groups []*cobra.Command) {
	t.Helper()
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Hidden || c.Name() == "completion" {
			return
		}
		if c.HasSubCommands() {
			groups = append(groups, c)
		}
		if c.Runnable() {
			leaves = append(leaves, c)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(newRoot())
	return leaves, groups
}

func cmdPath(c *cobra.Command) string { return c.CommandPath() }

// ── rule 1: every leaf introduces itself the same way ──

func TestEveryLeafHasAShortInTheHouseStyle(t *testing.T) {
	leaves, _ := partition(t)
	if len(leaves) == 0 {
		t.Fatal("no leaves found; the tree walk is broken")
	}
	for _, c := range leaves {
		short := c.Short
		switch {
		case short == "":
			t.Errorf("%s: no Short; every command has to say what it does", cmdPath(c))
		case strings.HasSuffix(short, "."):
			t.Errorf("%s: Short ends in a period: %q", cmdPath(c), short)
		case !unicode.IsUpper(rune(short[0])):
			t.Errorf("%s: Short should open with a capital: %q", cmdPath(c), short)
		}
	}
}

func TestEveryGroupHasAShort(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		if c.Short == "" && c.Name() != kit.Program {
			t.Errorf("%s: no Short", cmdPath(c))
		}
	}
}

// ── rule 2: groups never act ──

func TestGroupsNeverAct(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		if c.Runnable() {
			t.Errorf("%s is a group but also runs; move the behaviour to a verb such as `get`", cmdPath(c))
		}
	}
}

// A group holds commands, so a word it does not hold is a mistake worth
// reporting. Cobra makes that check only at the root; unknownSubcommand makes
// it everywhere, and this is what says so for every group in the tree.
func TestGroupsRejectAnUnknownSubcommand(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		path := strings.Fields(c.CommandPath())[1:]
		if err := unknownSubcommand(newRoot(), append(append([]string{}, path...), "nope")); err == nil {
			t.Errorf("%s: takes an unknown subcommand without complaint", cmdPath(c))
		}
		if err := unknownSubcommand(newRoot(), path); err != nil {
			t.Errorf("%s: rejects being called on its own: %v", cmdPath(c), err)
		}
	}
}

// ── rule 3: one placeholder set ──

// The only argument names the CLI uses are the ones kit.Placeholders declares.
// A new one means a new idea, which is exactly the thing worth noticing.
//
// The list is read from kit rather than repeated here: a second copy would make
// the declaration and the check two things that can disagree, and the one that
// loses is the file that calls itself the vocabulary.
func TestUsageUsesOnlyDeclaredPlaceholders(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		for _, tok := range argTokens(c.Use) {
			if _, ok := kit.Placeholders[tok]; !ok {
				t.Errorf("%s: undeclared placeholder %q in %q", cmdPath(c), tok, c.Use)
			}
		}
	}
}

// How many arguments a command takes is said once, on the usage line its help
// screen shows, and kit is what reads it. A command that declared an arity of
// its own would be a command that can disagree with the line above its own
// flags, and two of them did: `completion` showed no argument and demanded one,
// `filters reorder` showed one and demanded two.
//
// A value check is a different thing and stays with the command: what a setting
// key may be is the command's own knowledge. It runs after the count, so it can
// take the arguments it is handed as given.
func TestNoCommandCountsItsOwnArguments(t *testing.T) {
	arity := []string{
		"cobra.NoArgs", "cobra.ArbitraryArgs", "cobra.ExactArgs", "cobra.MinimumNArgs",
		"cobra.MaximumNArgs", "cobra.RangeArgs", "cobra.MatchAll", "cobra.OnlyValidArgs",
	}
	offenders := grepGo(t, []string{"."}, func(src string) bool {
		for _, v := range arity {
			if strings.Contains(src, v) {
				return true
			}
		}
		return false
	})
	for _, f := range offenders {
		t.Errorf("%s counts its own arguments; the usage line says how many it takes", f)
	}
}

// An argument that names one of the CLI's own things completes from a collection
// something lists, and a command's REF from one that can be listed by naming it
// alone.
//
// That is the whole promise behind a reference completing: what the shell offers
// back is what a listing put on the screen, so there has to be a listing that
// could have put it there. The stricter half is what a REF needs, because a REF
// is the first thing typed and nothing has been named yet - which is why
// `messages attachments download` has to reach past attachments to messages,
// whose listing needs nothing, while the attachment it takes second comes from a
// listing that had to be told which message.
func TestEveryReferenceCompletesFromSomethingListable(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		for i, arg := range kit.Arguments(c.Use) {
			picks := kit.Placeholders[arg.Name].Picks
			if picks == "" {
				continue
			}
			collection := kit.Picks(c, arg)
			if collection == "" {
				t.Errorf("%s: argument %d (%s) has no collection to complete from",
					cmdPath(c), i+1, arg.Name)
				continue
			}
			if !lists(c.Root(), collection) {
				t.Errorf("%s: argument %d (%s) completes from %q, which nothing lists",
					cmdPath(c), i+1, arg.Name, collection)
				continue
			}
			if picks == kit.PicksAddressed && !listable(c.Root(), collection) {
				t.Errorf("%s: argument %d (%s) completes from %q, which cannot be listed "+
					"without naming something first", cmdPath(c), i+1, arg.Name, collection)
			}
		}
	}
}

// A column or field that files its reference under another collection names one
// that exists, so a typo cannot quietly send what was shown somewhere nothing
// reads it. The declarations are inside closures, so the source is where they can
// be found.
func TestEveryCrossReferenceNamesACollection(t *testing.T) {
	root := newRoot()
	names := regexp.MustCompile(`Ref: +"([^"]+)"`)
	err := filepath.WalkDir(".", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		for _, m := range names.FindAllStringSubmatch(string(src), -1) {
			if !listable(root, m[1]) {
				t.Errorf("%s: Ref: %q names no collection that anything lists",
					filepath.ToSlash(p), m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// lists reports whether a command line names a collection with a list of its
// own, which is what puts anything into that collection's memory at all.
func lists(root *cobra.Command, collection string) bool {
	return listing(root, collection) != nil
}

// listable reports whether that list needs nothing named first.
func listable(root *cobra.Command, collection string) bool {
	l := listing(root, collection)
	return l != nil && len(kit.Arguments(l.Use)) == 0
}

func listing(root *cobra.Command, collection string) *cobra.Command {
	found, _, err := root.Find(strings.Fields(collection))
	if err != nil || found == root || found.CommandPath() != kit.Program+" "+collection {
		return nil
	}
	for _, sub := range found.Commands() {
		if sub.Name() == "list" {
			return sub
		}
	}
	return nil
}

// argTokens is the placeholder names in a Use string, read the way the CLI
// itself reads them.
func argTokens(use string) []string {
	args := kit.Arguments(use)
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, a.Name)
	}
	return out
}

// ── rule 8: a flag name means one thing ──

// The flag vocabulary lives in kit.Flags, beside the verbs and the argument
// names, because it is the same kind of declaration: a word this CLI may use and
// what it means. It is checked in both directions.
//
// This is the rule that would have caught --all meaning four different things
// and --html meaning three.
func TestSharedFlagNamesShareOneMeaning(t *testing.T) {
	leaves, _ := partition(t)
	users := map[string][]string{}
	for _, c := range leaves {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			users[f.Name] = append(users[f.Name], cmdPath(c))
		})
	}
	for name, cmds := range users {
		if len(cmds) < 2 {
			continue
		}
		if _, declared := kit.Flags[name]; !declared {
			t.Errorf("--%s is used by %d commands but has no declared meaning:\n  %s",
				name, len(cmds), strings.Join(cmds, "\n  "))
		}
	}
	// The other direction. A vocabulary that keeps a word after nothing says it
	// stops describing the CLI, and the next reader trusts it anyway: --totp-uri,
	// --cvv and five more outlived the flags they described by a whole release.
	// Global flags are declared too, and no leaf owns one, so they are exempt.
	global := map[string]bool{}
	newRoot().PersistentFlags().VisitAll(func(f *pflag.Flag) { global[f.Name] = true })
	for name := range kit.Flags {
		if len(users[name]) == 0 && !global[name] {
			t.Errorf("kit.Flags declares --%s and no command takes it", name)
		}
	}
}

// A flag whose meaning is shared is registered from one place.
//
// The rule above asks only that a name appear in the registry, which a flag
// satisfies by being written down once however many different things it goes on
// to mean. That is how --all came to say "everything in scope" on twenty-five
// commands, "reply to everyone" on two, "every scheduled send" on one and "every
// profile on this machine" on one, with the registry holding a single sentence
// true of the first group only.
//
// Comparing the help text would be the obvious repair and it is the wrong
// instrument: usage strings vary for good reasons - "Set the postal address" on
// a create and "Replace the postal address" on the update beside it are one
// meaning, not two - so a textual check either passes everything or fails on
// wording.
//
// So the enforcement is structural instead. The names below belong to kit, which
// registers each with the one usage string it has; a command that wants a
// different meaning has to pick a different name, because this fails on any
// command that declares one of them itself.
var kitOwnedFlags = map[string]string{
	"after":                  "kit.DayRange",
	"all":                    "kit.All",
	"before":                 "kit.DayRange",
	"clear-link-password":    "kit.LinkPassword",
	"eo-password-file":       "kit.EOPassword",
	"extra-password-file":    "kit.ExtraPassword",
	"link-password-file":     "kit.LinkPassword",
	"new-password-file":      "kit.NewPassword",
	"passphrase-file":        "kit.Passphrase",
	"password-file":          "kit.Reauth",
	"previous-password-file": "kit.Recovery",
	"recovery-file":          "kit.Recovery",
	"recovery-phrase":        "kit.Recovery",
	"recovery-phrase-file":   "kit.Recovery",
	"second-password-file":   "kit.SecondPassword",
	"totp":                   "kit.Reauth",
}

// kitFlagUsage is what kit itself registers each of those with, so the guard
// below can tell kit's own registration from a command redeclaring the name.
var kitFlagUsage = map[string]string{
	"after":                  kit.AfterUsage,
	"all":                    kit.AllUsage,
	"before":                 kit.BeforeUsage,
	"clear-link-password":    kit.ClearLinkPasswordUsage,
	"eo-password-file":       kit.EOPasswordFileUsage,
	"extra-password-file":    kit.ExtraPasswordFileUsage,
	"link-password-file":     kit.LinkPasswordFileUsage,
	"new-password-file":      kit.NewPasswordFileUsage,
	"passphrase-file":        kit.PassphraseFileUsage,
	"password-file":          kit.PasswordFileUsage,
	"previous-password-file": kit.PreviousPasswordFileUsage,
	"recovery-file":          kit.RecoveryFileUsage,
	"recovery-phrase":        kit.RecoveryPhraseUsage,
	"recovery-phrase-file":   kit.RecoveryPhraseFileUsage,
	"second-password-file":   kit.SecondPasswordFileUsage,
	"totp":                   kit.TOTPUsage,
}

// A flag name that names a fixed set of values names the same set everywhere.
//
// The rule above catches a shared flag whose meaning is a shared *behaviour*, by
// making kit own the registration. It cannot catch the other half: two commands
// that each declare their own Enum under one name, with different values. Those
// are two meanings wearing one word just as surely, and they are easier to write
// by accident, because each declaration reads fine on its own.
//
// `--status` was the case that proved it - whether an event is going ahead
// (confirmed, tentative, cancelled) against whether one participant is coming
// (accept, tentative, decline). Two subjects, one word, and nothing said so.
//
// The domains are compared rather than the help text, because a domain is what
// the flag actually means: two commands offering different values are asking
// different questions whatever their usage strings say.
//
// domainsByCollection are the three that ask one question whose answers belong
// to whatever is being acted on: which column to order by, which layout to
// write, and what somebody may do with a shared thing. A message can be laid
// down as eml or mbox and a vault cannot; a card sorts by name and a file by
// size; a Pass vault has a manager and a Drive folder has nothing above editor.
// The question is the same word in every one of them, and anything not named
// here that offers a second set is not.
var domainsByCollection = map[string]bool{"access": true, "format": true, "sort": true}

func TestAFlagNameNamesOneSetOfValues(t *testing.T) {
	// Building the tree is what populates the registry.
	newRoot()

	domains := map[string]map[string][]string{}
	for path, values := range kit.DeclaredEnums() {
		cmd, flag, ok := strings.Cut(path, " --")
		if !ok {
			t.Errorf("unreadable enum registration %q", path)
			continue
		}
		if domains[flag] == nil {
			domains[flag] = map[string][]string{}
		}
		domains[flag][strings.Join(values, ", ")] = append(domains[flag][strings.Join(values, ", ")], cmd)
	}

	flags := make([]string, 0, len(domains))
	for flag := range domains {
		flags = append(flags, flag)
	}
	sort.Strings(flags)

	for _, flag := range flags {
		if len(domains[flag]) < 2 || domainsByCollection[flag] {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "--%s accepts %d different sets of values; a flag name means one thing:",
			flag, len(domains[flag]))
		sets := make([]string, 0, len(domains[flag]))
		for set := range domains[flag] {
			sets = append(sets, set)
		}
		sort.Strings(sets)
		for _, set := range sets {
			cmds := domains[flag][set]
			sort.Strings(cmds)
			fmt.Fprintf(&b, "\n  %s\n    %s", set, strings.Join(cmds, "\n    "))
		}
		t.Error(b.String())
	}
}

// A flag that takes a value off names the flag that puts it on.
//
// There are three ways to say "none" in this CLI and each is the right one
// somewhere. A value that can carry the word says it: `--expires never`, which
// is what every screen prints for something that does not expire. A state
// somebody can also choose when making the thing is `--no-x`: `--no-remind`
// removes an event's reminders and gives a new event none, one word for one
// idea. What is left is a single-valued field of something that already exists
// whose flag cannot carry the word - a signature typed as text, a password read
// from a file - and that is `--clear-x`.
//
// So a `--clear-x` sits beside the `x` it clears, and this fails on one that
// does not: a `--clear-something` alone on a command is either a flag whose own
// value could have said "never", or a name that no longer matches what it takes
// off.
func TestAClearFlagSitsBesideWhatItClears(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		names := map[string]bool{}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) { names[f.Name] = true })
		for name := range names {
			cleared, isClear := strings.CutPrefix(name, "clear-")
			if !isClear {
				continue
			}
			sets := false
			for other := range names {
				if other != name && strings.HasPrefix(other, cleared) {
					sets = true
				}
			}
			if !sets {
				t.Errorf("%s takes --%s but nothing on it sets %q; "+
					"name it after the flag it clears, or let that flag's value say never",
					cmdPath(c), name, cleared)
			}
		}
	}
}

func TestSharedFlagsAreRegisteredFromOnePlace(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			owner, owned := kitOwnedFlags[f.Name]
			if !owned || f.Usage == kitFlagUsage[f.Name] {
				return
			}
			t.Errorf("%s declares its own --%s (%q); that name belongs to %s, "+
				"so a different meaning needs a different name", cmdPath(c), f.Name, f.Usage, owner)
		})
	}
}

// ── rule 4: a collection sits where Proton's own clients put it ──

// `settings` means one thing: what you set.
//
// It is the group most likely to become a drawer, because anything can be argued
// into it and nothing argues its way out. So every collection filed under one
// has to name the page of Proton's settings app it mirrors, and a collection
// that names a page has to be filed under one. Both halves matter: the first
// stops `settings` collecting things Proton puts in the product window, and the
// second stops a declaration going stale after a move.
//
// This is what decides where a new collection goes, and it decides it by
// research rather than by taste - which is the only way five apps end up
// agreeing.
func TestCollectionsUnderSettingsMirrorASettingsPage(t *testing.T) {
	_, groups := partition(t)
	found := map[string]bool{}
	for _, c := range groups {
		parent := c.Parent()
		if parent == nil || parent.Name() != "settings" {
			continue
		}
		path := strings.TrimPrefix(cmdPath(c), kit.Program+" ")
		found[path] = true
		if _, ok := kit.SettingsPages[path]; !ok {
			t.Errorf("%s sits under `settings` but names no page of Proton's settings app; "+
				"declare it in kit.SettingsPages, or move it beside what it contains", cmdPath(c))
		}
	}
	for path := range kit.SettingsPages {
		if !found[path] {
			t.Errorf("kit.SettingsPages declares %q, but no such group sits under a `settings`", path)
		}
	}
}

// ── rule 4b: a filter can be read before it is acted on ──

// Whatever narrows a bulk verb narrows the listing beside it.
//
// A filter decides what a removal touches, so the command to work one out on
// should be the one that only reads. Where `list` does not take what `trash`
// takes, the only way to see a selection is to point the destructive verb at it
// and add --dry-run, which is exactly the wrong place to be finding out.
//
// The comparison is one-way: a listing may offer more than a bulk verb - paging
// is its own business - but it may not offer less.
var filterParityExceptions = map[string]string{}

// notNarrowing are flags a bulk verb carries that say something other than which
// things: how many, whether everything was meant, where to look when there is no
// argument to say it with, and what to do with what it found.
var notNarrowing = map[string]bool{
	"limit": true, "page": true, "scope": true, "help": true,
	"into": true, "dest": true, "dest-dir": true, "force": true,
	"format": true, "no-attachments": true, "label": true,
	"expires": true, "until": true,
}

// narrows reports whether a flag on a bulk verb says which things it acts on.
// Nothing kit registers does: those say how the command is run, not what it is
// run over.
func narrows(name string) bool {
	_, fromKit := kitOwnedFlags[name]
	return !fromKit && !notNarrowing[name]
}

func TestListTakesWhateverNarrowsTheVerbsBesideIt(t *testing.T) {
	leaves, _ := partition(t)
	lists := map[string]*cobra.Command{}
	bulk := map[string][]*cobra.Command{}
	for _, c := range leaves {
		parent := c.Parent()
		if parent == nil {
			continue
		}
		collection := strings.TrimPrefix(cmdPath(parent), kit.Program+" ")
		switch {
		case c.Name() == "list":
			lists[collection] = c
		case c.Flags().Lookup("all") != nil:
			bulk[collection] = append(bulk[collection], c)
		}
	}

	for collection, verbs := range bulk {
		list, ok := lists[collection]
		if !ok {
			continue
		}
		if why, excused := filterParityExceptions[collection]; excused {
			t.Logf("%s: filter parity not required - %s", collection, why)
			continue
		}
		for _, verb := range verbs {
			verb.LocalFlags().VisitAll(func(f *pflag.Flag) {
				if !narrows(f.Name) || list.Flags().Lookup(f.Name) != nil {
					return
				}
				t.Errorf("%s takes --%s and `%s %s list` does not, so the only way to see "+
					"what it selects is to aim the verb at it",
					cmdPath(verb), f.Name, kit.Program, collection)
			})
		}
	}
	for collection := range filterParityExceptions {
		if _, ok := bulk[collection]; !ok {
			t.Errorf("filterParityExceptions names %q, which has no bulk verbs", collection)
		}
	}
}

// ── rule 5: flag help reads like the rest of the CLI ──

func TestFlagUsageIsInTheHouseStyle(t *testing.T) {
	leaves, groups := partition(t)
	seen := map[string]bool{}
	for _, c := range append(leaves, groups...) {
		c.Flags().VisitAll(func(f *pflag.Flag) {
			key := f.Name + "\x00" + f.Usage
			if seen[key] {
				return
			}
			seen[key] = true
			switch {
			case f.Usage == "":
				t.Errorf("%s --%s: no usage text", cmdPath(c), f.Name)
			case strings.HasSuffix(f.Usage, "."):
				t.Errorf("%s --%s: usage ends in a period: %q", cmdPath(c), f.Name, f.Usage)
			case !unicode.IsUpper(rune(f.Usage[0])) && !strings.HasPrefix(f.Usage, "-"):
				t.Errorf("%s --%s: usage should open with a capital: %q", cmdPath(c), f.Name, f.Usage)
			}
			if f.Name != strings.ToLower(f.Name) || strings.Contains(f.Name, "_") {
				t.Errorf("%s --%s: flag names are kebab-case", cmdPath(c), f.Name)
			}
		})
	}
}

// ── rule 7: only the ui package writes to the process streams ──

func TestOnlyTheUIPackageTouchesTheProcessStreams(t *testing.T) {
	// Execute is the one place with nowhere else to write: it reports the failures
	// that happen before an App - and therefore a renderer - exists, such as a
	// flag that could not be parsed.
	allowed := map[string]bool{"../cli/root.go": true}

	offenders := grepGo(t, []string{"../cli", "../service"}, func(src string) bool {
		return strings.Contains(src, "os.Stdout") || strings.Contains(src, "os.Stderr")
	})
	for _, f := range offenders {
		if !allowed[f] {
			t.Errorf("%s writes to a process stream; render through internal/ui instead", f)
		}
	}
}

// ── rule 10: only one place may ask a person for a credential ──

// A command that reads stdin behind the user's back is a command that can hang a
// cron job. Keeping the ability in one file is what makes that checkable.
func TestOnlyOnePlaceReadsCredentialsFromStdin(t *testing.T) {
	allowed := map[string]bool{
		"../ui/prompt.go": true,
	}
	offenders := grepGo(t, []string{"../cli", "../service", "../app", "../proton", "../account"}, func(src string) bool {
		return strings.Contains(src, "term.ReadPassword")
	})
	for _, f := range offenders {
		if !allowed[f] {
			t.Errorf("%s reads a secret from stdin; that belongs in internal/ui/prompt.go", f)
		}
	}
}

// ── rule 13: no environment variable may name an account ──

// An account is attached to a profile by `account login` and nowhere else. The
// moment a credential can arrive through the environment as well, a command can
// act as an account nobody named on the command line, which is how a profile
// ends up quietly meaning a different one.
func TestNoEnvironmentVariableCarriesACredential(t *testing.T) {
	forbidden := []string{"PROTON_USER", "PROTON_PASSWORD", "PROTON_TOTP"}
	offenders := grepGo(t, []string{"../cli", "../app", "../service", "../account", "../proton"},
		func(src string) bool {
			for _, v := range forbidden {
				if strings.Contains(src, v) {
					return true
				}
			}
			return false
		})
	for _, f := range offenders {
		t.Errorf("%s takes an account from the environment; sign in with `account login` instead", f)
	}
}

// ── rule 13b: the settings file and the flags say the same words ──

// A setting has one name wherever it is written.
//
// The file, the flags and the variables are three ways of saying the same
// thing, so a key that names no flag is a key nobody can discover from --help,
// and a rename that moves one and not the other leaves two spellings for one
// idea. The struct tags are the list; the flag set is the check.
func TestEverySettingsKeyIsAlsoAFlag(t *testing.T) {
	// Two are settable only in the file and by their variable: the release check
	// has never had a flag, and the policy's own scope words are its value rather
	// than a flag name.
	variableOnly := map[string]bool{"no-update-check": true}

	pf := newRoot().PersistentFlags()
	for _, key := range settingsKeys(t) {
		if variableOnly[key] {
			continue
		}
		if pf.Lookup(key) == nil {
			t.Errorf("config.yaml takes %q but there is no --%s", key, key)
		}
	}
}

// Three things are decided per run and never persisted, and one of them is the
// reason the other rules hold at all.
//
// A file that could say `yes: true` would answer every confirmation the same
// file went on to demand, including a deny - which would make the whole policy
// decorative. `dry-run` in a file turns every command into a preview that looks
// exactly like work getting done, `verified` is one token from one refusal, and
// `api-url` is a durable way to point a signed-in CLI at a host that is not
// Proton.
func TestTheSettingsFileHoldsNoPerRunDecision(t *testing.T) {
	keys := map[string]bool{}
	for _, k := range settingsKeys(t) {
		keys[k] = true
	}
	for _, forbidden := range []string{"yes", "dry-run", "verified", "api-url"} {
		if keys[forbidden] {
			t.Errorf("%q is settable in config.yaml; it is decided per run", forbidden)
		}
	}
}

// settingsKeys is every key the file accepts, read off the struct that decodes
// it so the list cannot fall behind the type.
func settingsKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for _, typ := range []reflect.Type{
		reflect.TypeOf(config.File{}), reflect.TypeOf(config.Settings{}),
	} {
		for i := range typ.NumField() {
			tag := typ.Field(i).Tag.Get("yaml")
			name, _, _ := strings.Cut(tag, ",")
			if name != "" && name != "per-profile" {
				keys = append(keys, name)
			}
		}
	}
	if len(keys) == 0 {
		t.Fatal("found no settings keys; the reflection is broken")
	}
	return keys
}

// ── rule 13c: a global flag's name is not available to a leaf ──

// A leaf that declares a flag the root already declares does not clash - it
// shadows, silently, and the global becomes unreachable on that command.
//
// This is how fourteen download commands came to have no way of asking for JSON:
// they each registered an --output meaning a file path, and cobra handed it the
// name before the root's format flag ever saw it. A name means one thing, and
// the root's names mean theirs everywhere.
func TestNoLeafShadowsAGlobalFlag(t *testing.T) {
	root := newRoot()
	global := map[string]bool{}
	root.PersistentFlags().VisitAll(func(f *pflag.Flag) { global[f.Name] = true })

	leaves, groups := partition(t)
	for _, c := range append(leaves, groups...) {
		c.LocalNonPersistentFlags().VisitAll(func(f *pflag.Flag) {
			if global[f.Name] {
				t.Errorf("%s declares --%s, which is the root's; it would shadow it",
					cmdPath(c), f.Name)
			}
		})
	}
}

// ── rule 14: standard input has one owner ──

// Everything that wants stdin asks with the same word: `--password-file -` for
// the account password, `--secret-file password=-` for a stored one, and a bare
// `-` for a body, a key, or a file to upload. Whichever read it second would find
// an empty stream and fail somewhere further along with a puzzle.
//
// So every reader goes through App.Stdin, which hands it out once and names both
// claimants when they collide - which is the only reason a command may declare
// one of these flags beside an argument that reads the same stream.
func TestStandardInputHasOneOwner(t *testing.T) {
	// internal/ui owns the process streams (rule 7) and supplies the reader that
	// App.Stdin hands out.
	allowed := map[string]bool{"../ui/ui.go": true}
	offenders := grepGo(t, []string{"../cli", "../app", "../service", "../account", "../proton", "../ui"},
		func(src string) bool { return strings.Contains(src, "os.Stdin") })
	for _, f := range offenders {
		if !allowed[f] {
			t.Errorf("%s reads os.Stdin directly; go through App.Stdin so stdin keeps one owner", f)
		}
	}
}

// ── rule 15: the commands that can be asked to re-authenticate are declared ──

// Proton guards a few endpoints behind an elevated session and grants that only
// for another SRP exchange, so the commands reaching one carry the credentials
// to answer with. Which endpoints those are is Proton's to decide and not
// discoverable from here, so the set is written down: adding to it is a decision
// rather than a reflex, and the integration harness keeps the same list.
func TestReauthCommandsAreDeclared(t *testing.T) {
	want := []string{
		"proton account keys reactivate",
		"proton account login",
		"proton account security-log delete",
		"proton account security-log disable",
		"proton account security-log enable",
		"proton account sessions revoke",
		"proton account settings password set",
		"proton account settings recovery-email disable",
		"proton account settings recovery-email enable",
		"proton account settings recovery-email set",
		"proton account settings recovery-phone disable",
		"proton account settings recovery-phone enable",
		"proton account settings recovery-phone set",
		"proton account settings recovery-phrase disable",
		"proton account settings recovery-phrase set",
		"proton account settings second-password disable",
		"proton account settings second-password enable",
		"proton account settings second-password set",
		"proton account settings security-keys create",
		"proton account settings security-keys delete",
		"proton account settings two-factor disable",
		"proton account settings two-factor enable",
		"proton calendar settings calendars delete",
		"proton mail messages update",
		"proton mail settings addresses create",
		"proton mail settings addresses delete",
		"proton mail settings addresses disable",
		"proton mail settings addresses enable",
		"proton mail settings autoreply disable",
		"proton mail settings autoreply enable",
		"proton mail settings autoreply set",
		"proton mail settings domains create",
		"proton mail settings domains delete",
		"proton mail settings domains update",
		"proton mail settings forwarding accept",
		"proton mail settings forwarding create",
		"proton mail settings forwarding delete",
		"proton mail settings smtp-tokens create",
		"proton mail settings smtp-tokens delete",
	}
	leaves, _ := partition(t)
	var got []string
	for _, c := range leaves {
		if c.Flags().Lookup("password-file") != nil {
			got = append(got, cmdPath(c))
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("commands carrying the credential flags are:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// The second password belongs to signing in and to nothing else.
//
// It is not what Proton asks for when it wants a session proved: that is the
// password, over SRP. It is what opens the keys, which every other command finds
// already sealed into its session - so a second command offering the flag would
// be offering one that almost never does anything.
func TestOnlySigningInTakesTheSecondPassword(t *testing.T) {
	want := []string{"proton account login"}
	leaves, _ := partition(t)
	var got []string
	for _, c := range leaves {
		if c.Flags().Lookup("second-password-file") != nil {
			got = append(got, cmdPath(c))
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("commands carrying the second-password flags are:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// The Pass extra password is taken by signing in and by the two commands whose
// subject it is.
//
// Any Pass command can be the one that finds the session without the scope the
// extra password buys, so the flags could be argued onto every one of them. They
// are on none: the scope lasts as long as the session, so a command that offered
// them would be offering a secret to hand over again for something already held -
// and the one run that cannot be asked for it is the one that signs in. Turning
// the extra password on or off is the other case, where the secret is not a toll
// on the way to something else but the thing being changed.
func TestExtraPasswordCommandsAreDeclared(t *testing.T) {
	want := []string{
		"proton account login",
		"proton pass settings extra-password disable",
		"proton pass settings extra-password enable",
	}
	leaves, _ := partition(t)
	var got []string
	for _, c := range leaves {
		if c.Flags().Lookup("extra-password-file") != nil {
			got = append(got, cmdPath(c))
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("commands carrying the extra-password flags are:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// ── rule 12b: what a preview has to be true about is declared ──

// A dry run asserts that an account exists, because it sends no request and
// would otherwise be the one path answering as though it had one. The exceptions
// are the commands that would have run with nobody signed in, and there are
// exactly four: the three that change this machine, and the one that makes the
// session the rest of them assume.
//
// The set is pinned rather than counted, because the failure this guards against
// is a fifth command quietly declaring itself exempt and skipping a check it
// needed.
func TestCommandsThatRunSignedOutAreDeclared(t *testing.T) {
	want := []string{
		"proton account login",
		"proton index delete",
		"proton uninstall",
		"proton update",
	}
	leaves, groups := partition(t)
	var got []string
	for _, c := range append(leaves, groups...) {
		if c.Annotations[kit.SignedOut] != "" {
			got = append(got, cmdPath(c))
		}
	}
	sort.Strings(got)
	if !slices.Equal(got, want) {
		t.Errorf("commands declaring they run signed out are:\n  %s\nwant:\n  %s",
			strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// ── the vocabulary is closed ──

// The vocabulary is read from kit rather than restated here. A test that keeps
// its own copy of the list it is checking will eventually be checking the copy:
// this one had drifted to hold a verb the CLI no longer has.

func TestEveryVerbIsInTheVocabulary(t *testing.T) {
	leaves, _ := partition(t)
	spoken := map[string]bool{}
	for _, c := range leaves {
		if c.Name() == kit.Program {
			continue
		}
		spoken[c.Name()] = true
		if _, ok := kit.Verbs[c.Name()]; !ok {
			t.Errorf("%s: %q is not in the declared verb vocabulary", cmdPath(c), c.Name())
		}
	}
	// `completion` is a command and is not one of the leaves: partition drops it,
	// because cobra writes it and it answers to none of the rules the rest are
	// held to. It is still a word this CLI says.
	spoken["completion"] = true
	// The other direction. `expire`, `forget`, `link`, `unlink` and `options`
	// outlived the commands that said them by a whole release, and a vocabulary
	// holding words nothing speaks describes a CLI that is not there.
	for verb := range kit.Verbs {
		if !spoken[verb] {
			t.Errorf("kit.Verbs declares %q and no command ends in it", verb)
		}
	}
}

// ── rule 12: what cannot be taken back is declared, not remembered ──

// The CLI stops for a yes for one reason: something is about to be removed. The
// verbs that can never be taken back and the actions that say so have to name
// the same set, because the verb is what a user reads in the help and the action
// is what actually decides at run time. Two lists that can disagree are one bug
// away from a `delete` that never asks.
//
// An opaque verb is the one that answers through neither: it has nothing to
// resolve, so it is put to the person at the gate and reports no action of its
// own to carry the cost.
func TestIrreversibleVerbsAndActionsAgree(t *testing.T) {
	fromActions := map[string]bool{}
	for _, a := range ui.Actions {
		if a.Cost == ui.Forever {
			fromActions[a.Verb] = true
		}
	}
	for verb := range kit.Irreversible {
		if !fromActions[verb] && !kit.Opaque[verb] {
			t.Errorf("%q is declared irreversible but no action reports it as Forever", verb)
		}
	}
	for verb := range kit.Opaque {
		if !kit.Irreversible[verb] {
			t.Errorf("%q is declared opaque but not irreversible; nothing about it is known", verb)
		}
	}
	for verb := range fromActions {
		if !kit.Irreversible[verb] {
			t.Errorf("an action reports %q as Forever, but the verb is not declared irreversible", verb)
		}
	}
}

// Every leaf named by an irreversible verb has to be reachable as one, so a
// command cannot quietly sit outside the guard by being spelled differently.
func TestIrreversibleVerbsAreMutatingVerbs(t *testing.T) {
	for verb := range kit.Irreversible {
		if _, ok := kit.Verbs[verb]; !ok {
			t.Errorf("%q is declared irreversible but is not a verb", verb)
		}
		if !kit.Mutating[verb] {
			t.Errorf("%q is declared irreversible but is not declared mutating", verb)
		}
	}
}

// Whether a change happens at all is settled globally, so no command may
// redefine the two flags that settle it.
//
// A local --yes is not a naming clash to be tidied up; it is a command deciding
// for itself what consent means, which is the one thing the guard cannot
// survive. `uninstall` used to spell "actually do it" that way, leaving --yes
// meaning "proceed without asking" everywhere except the command with the most
// to lose.
func TestNoCommandRedefinesConsent(t *testing.T) {
	leaves, groups := partition(t)
	for _, c := range append(leaves, groups...) {
		for _, name := range []string{"yes", "dry-run"} {
			if f := c.Flags().Lookup(name); f != nil && c.InheritedFlags().Lookup(name) == nil {
				t.Errorf("%s declares its own --%s; that flag is the root's alone", cmdPath(c), name)
			}
		}
	}
}

// ── rule 14: nothing is logged under a name nobody declared ──

// The diagnostic log is written to be handed to a stranger, and every attribute
// in it is redacted according to a policy declared for its name. So a name with
// no policy is a value nobody decided about, which is how an address ends up in
// a file somebody attaches to a public issue.
//
// This reads every log call in the tree and checks its attribute names against
// redact.Fields. The handler refuses an undeclared name at run time as well -
// this is the half that says so before anybody ships it.
func TestNothingIsLoggedUnderAnUndeclaredName(t *testing.T) {
	for _, call := range logCalls(t, []string{".."}) {
		for _, name := range call.names {
			if !redact.Declared(name) {
				t.Errorf("%s:%d logs under %q, which redact.Fields declares no policy for",
					call.file, call.line, name)
			}
		}
	}
}

// And the other direction: the vocabulary holds the names in use and no others.
//
// A policy for a name nothing writes is a decision nobody has had to make,
// carrying the authority of one that somebody did. Left alone it accumulates -
// this caught five entries describing records that were never written - and a
// reader can no longer tell the declarations that hold from the ones that are
// aspirations.
func TestTheLogVocabularyHoldsOnlyWhatIsWritten(t *testing.T) {
	written := map[string]bool{}
	for _, call := range logCalls(t, []string{".."}) {
		for _, name := range call.names {
			written[name] = true
		}
	}
	for name := range redact.Fields {
		if !written[name] {
			t.Errorf("redact.Fields declares %q and nothing logs under it; "+
				"declare a name when you write one", name)
		}
	}
}

// logCall is one log statement and the attribute names it writes.
type logCall struct {
	file  string
	line  int
	names []string
}

// logCalls finds every attribute name a record can be written under: the pairs
// passed to a Debug/Info/Warn/Error call, and the attributes attached to a
// handler, which name one in every record that handler writes.
//
// Only the literal names are read: slog takes alternating key/value pairs, so
// the keys are the even arguments, and a key that is not a literal string is a
// key nobody can check. Those are reported too, under a name that is declared
// nowhere on purpose.
func logCalls(t *testing.T, dirs []string) []logCall {
	t.Helper()
	var out []logCall
	levels := map[string]bool{"Debug": true, "Info": true, "Warn": true, "Error": true,
		"DebugContext": true, "InfoContext": true, "WarnContext": true, "ErrorContext": true}
	attrs := map[string]bool{"Any": true, "Bool": true, "Duration": true, "Float64": true,
		"Group": true, "Int": true, "Int64": true, "String": true, "Time": true, "Uint64": true}
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
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if pkg, named := sel.X.(*ast.Ident); named && pkg.Name == "slog" && attrs[sel.Sel.Name] {
					if len(call.Args) > 0 {
						if lit, isString := call.Args[0].(*ast.BasicLit); isString && lit.Kind == token.STRING {
							if name, uerr := strconv.Unquote(lit.Value); uerr == nil {
								out = append(out, logCall{file: filepath.ToSlash(p),
									line: fset.Position(call.Pos()).Line, names: []string{name}})
							}
						}
					}
					return true
				}
				if !levels[sel.Sel.Name] || !logReceiver(sel.X) {
					return true
				}
				// The message, and a context before it for the Context variants,
				// come first; the attributes are pairs after that.
				start := 1
				if strings.HasSuffix(sel.Sel.Name, "Context") {
					start = 2
				}
				entry := logCall{file: filepath.ToSlash(p), line: fset.Position(call.Pos()).Line}
				for i := start; i < len(call.Args); i += 2 {
					lit, ok := call.Args[i].(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						entry.names = append(entry.names, "<not a literal name>")
						continue
					}
					name, uerr := strconv.Unquote(lit.Value)
					if uerr == nil {
						entry.names = append(entry.names, name)
					}
				}
				if len(entry.names) > 0 {
					out = append(out, entry)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return out
}

// logReceiver reports whether what is being called looks like a logger: the
// slog package, or a field named for one.
func logReceiver(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name == "slog" || v.Name == "log"
	case *ast.SelectorExpr:
		return v.Sel.Name == "log" || v.Sel.Name == "Log" || v.Sel.Name == "Trace"
	}
	return false
}

// ── rule 15: an error is never dropped on the floor ──

// A loop that decrypts cannot stop at the first item it fails to open: one
// damaged item is no reason to refuse the other forty-one. But carrying on
// silently is how a listing comes to under-report and exit zero, which is a
// wrong answer presented as a right one - and it took thirty instances of this
// exact three-line shape before anybody noticed.
//
// So a skipped item is recorded: counted on the invocation where a listing can
// warn about it, or at minimum written to the log where a report can find it.
// This finds the shape and checks that something was said.
func TestNoErrorIsSkippedInSilence(t *testing.T) {
	srcs := []string{"../service", "../account"}
	swallow := regexp.MustCompile(`(?m)err\s*!=\s*nil\s*\{\n\s*continue\n`)
	for _, dir := range srcs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			src, rerr := os.ReadFile(p)
			if rerr != nil {
				return rerr
			}
			for _, loc := range swallow.FindAllStringIndex(string(src), -1) {
				line := 1 + strings.Count(string(src[:loc[0]]), "\n")
				t.Errorf("%s:%d skips an item without recording why; "+
					"call skip.Record so a listing can say it is short, or log it",
					filepath.ToSlash(p), line)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// ── rule 16: a name out of the account travels beside the error ──

// Every other value a message picks up has a shape redact can find again - an
// address, an ID, an absolute path. What somebody called a vault item, an event
// or an attachment is words, and no reader of the finished sentence can tell it
// from the sentence, so a log carrying that message carries the name with it.
//
// errs.Naming is what separates the two readers: the person is told which of
// their things failed, and the file gets <name> in its place. Formatting one in
// with %s tells the person the same thing and hands the file the name, which is
// why this fails on it.
//
// It reads the arguments rather than the sentence, so what it catches is exactly
// the shape that leaks: a field of something fetched from the account going into
// a message. A name held in a local first is invisible here - the check is a net
// under the rule, not the rule itself, which is written in AGENTS.md.
//
// kit is outside it. It phrases what the command line got wrong, so the Name it
// formats in is a flag's own - `--%s accepts:` - and never the account's.
var accountNames = map[string]bool{"Name": true, "Subject": true, "Title": true}

// phrasing is what turns a failure into a sentence for a person.
var phrasing = map[string]bool{"Fail": true, "Problemf": true, "Unsupportedf": true}

func TestTheAccountsOwnNamesReachErrorsThroughNaming(t *testing.T) {
	fset := token.NewFileSet()
	for _, dir := range []string{"../cli", "../service"} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			switch {
			case err != nil || d.IsDir():
				return err
			case !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go"):
				return nil
			case strings.HasPrefix(filepath.ToSlash(p), "../cli/kit/"):
				return nil
			}
			file, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if perr != nil {
				t.Fatalf("parse %s: %v", p, perr)
			}
			for _, call := range namedOutsideNaming(file) {
				t.Errorf("%s:%d: a name out of the account goes into a failure with %%s; "+
					"wrap it in errs.Naming so the log can stand it aside",
					filepath.ToSlash(p), fset.Position(call.Pos()).Line)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// namedOutsideNaming are the messages built from a thing's own name with nothing
// carrying that name beside them. The wrapper encloses the call it is given, so
// the question is whether one of them is inside one.
func namedOutsideNaming(file *ast.File) []*ast.CallExpr {
	var named, phrased []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch {
		case sel.Sel.Name == "Naming":
			named = append(named, call)
		case phrasing[sel.Sel.Name] && carriesAName(call):
			phrased = append(phrased, call)
		}
		return true
	})
	var out []*ast.CallExpr
	for _, call := range phrased {
		if slices.ContainsFunc(named, func(n *ast.CallExpr) bool {
			return n.Pos() < call.Pos() && call.End() <= n.End()
		}) {
			continue
		}
		out = append(out, call)
	}
	return out
}

// The checker earns its own test: reading no name out of any message would pass
// forever while every one of them carried one.
func TestTheNamingCheckerFindsAMessageCarryingAName(t *testing.T) {
	const src = `package p

func bare() error {
	return kit.Fail("%s is a %s, not a login.", it.Name, it.Type)
}

func wrapped() error {
	return errs.Naming(it.Name, kit.Fail("%s carries no two-factor secret.", it.Name).
		Hint("store one first"))
}

func unnamed() error {
	return kit.Fail("A vault needs a name.").Hint("--name Work")
}
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "p.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	found := namedOutsideNaming(file)
	if len(found) != 1 {
		t.Fatalf("found %d messages carrying a name, want 1 (the bare one)", len(found))
	}
	if line := fset.Position(found[0].Pos()).Line; line != 4 {
		t.Errorf("reported line %d, want 4", line)
	}
}

func TestTheNamingCheckerKnowsANameFromAWord(t *testing.T) {
	for _, c := range []struct {
		src  string
		want bool
	}{
		{`kit.Fail("%s carries no two-factor secret.", it.Name)`, true},
		{`errs.Problemf("%s could not be read: %v.", f.File.Name, err)`, true},
		{`kit.Fail("A vault needs a name.")`, false},
		{`errs.Problemf("%q is not a time zone this machine knows.", name)`, false},
		{`kit.Fail("could not read PATH from stdin: %v", err)`, false},
		// What kit says about a flag of its own, which is why kit is not walked.
		{`Fail("--%s accepts: %s", e.Name, strings.Join(e.Values, ", "))`, true},
	} {
		expr, err := parser.ParseExpr(c.src)
		if err != nil {
			t.Fatalf("parse %s: %v", c.src, err)
		}
		if got := carriesAName(expr.(*ast.CallExpr)); got != c.want {
			t.Errorf("%s: carries a name = %v, want %v", c.src, got, c.want)
		}
	}
}

// carriesAName reports whether one of the values a message is built from is a
// thing's own name, read off whatever it was fetched from.
func carriesAName(call *ast.CallExpr) bool {
	for _, arg := range call.Args[min(1, len(call.Args)):] {
		sel, ok := arg.(*ast.SelectorExpr)
		if ok && accountNames[sel.Sel.Name] {
			return true
		}
	}
	return false
}

// ── helpers ──

// grepGo returns the non-test Go files under the given directories whose source
// satisfies match, as paths relative to this package.
func grepGo(t *testing.T, dirs []string, match func(string) bool) []string {
	t.Helper()
	var hits []string
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			// Parse so a mention inside a comment does not count as a use.
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
			if perr == nil {
				var b strings.Builder
				ast.Inspect(file, func(n ast.Node) bool {
					if sel, ok := n.(*ast.SelectorExpr); ok {
						if id, ok := sel.X.(*ast.Ident); ok {
							b.WriteString(id.Name + "." + sel.Sel.Name + "\n")
						}
					}
					return true
				})
				if match(b.String()) {
					hits = append(hits, filepath.ToSlash(p))
				}
				return nil
			}
			if match(string(src)) {
				hits = append(hits, filepath.ToSlash(p))
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
	return hits
}

// ── rule 11: layers import downward only ──

// layers records, per package, which of our own packages it may import.
//
// The direction is what keeps the design honest. An inversion - a domain service
// reaching for the progress bar, the presentation package holding mail body
// transforms - stays invisible until stated as a rule, and shows up as the same
// symptom either way: neither half can be tested without the other.
var layers = map[string][]string{
	// ui may reach ref for one thing: the notation a reference is written in.
	// It prints references, so it has to shorten them without taking them apart
	// wrongly, and the alternative to borrowing that is restating it - which is
	// precisely what produced listings whose rows the next command rejected.
	// ref is a leaf over errs, so this is a downward import and not an inversion.
	// ui reaches redact for the reason it reaches ref: it writes the diagnostic
	// log, and what may be written in one is declared in redact rather than
	// restated by each handler. Putting the policy above every destination is
	// what makes "the log holds nothing sensitive" one rule instead of two.
	"ui": {"units", "progress", "errs", "ref", "redact"},
	// proton reaches units for the reason ui does: it announces a wait to the
	// person sitting through it, and how long a span is put to a reader is
	// declared once rather than spelled a second way here.
	"proton":   {"errs", "units", "crypto/aead", "hv", "hv/hvexit"},
	"errs":     {},
	"units":    {},
	"progress": {},
	"mailtext": {},
	// redact reaches ref for the reason ui does: it decides which part of an API
	// path names a thing rather than an endpoint, and what a Proton ID looks like
	// is declared where references are read and written. A rule of thumb here
	// instead would disagree with that one, and did.
	"redact": {"ref"},
	// search is the index on disk and the rules for matching against it. It seals
	// records with the symmetric primitive Pass items are sealed with, and reports
	// a build through the same sink a transfer reports through; the keys it seals
	// to are handed in, so it reaches for nothing that knows about an account.
	"search": {"crypto/aead", "progress"},
	// skip logs through the package-level logger and counts on the context, so it
	// needs nothing of ours. That is what lets every service reach it.
	"skip":   {},
	"runlog": {},
	// idcache reaches ref for the reason ui does: it stores whole IDs and answers
	// short ones, and how the two relate belongs where the notation is declared
	// rather than restated here.
	"idcache":     {"ref"},
	"ref":         {"errs"},
	"changelog":   {},
	"contentline": {},
	// accent is Proton's palette, which a flag and an import both need: one to
	// refuse what a person typed, one to snap what a file said. It is a table and
	// the arithmetic over it, so it reaches for nothing.
	"accent":  {},
	"ical":    {"contentline"},
	"vcard":   {"contentline"},
	"profile": {},
}

func TestPackagesImportDownwardOnly(t *testing.T) {
	const mod = "github.com/roman-16/proton-cli/internal/"
	for pkg, allowed := range layers {
		permitted := map[string]bool{}
		for _, a := range allowed {
			permitted[a] = true
		}
		for _, imported := range ourImports(t, filepath.Join("..", pkg), mod) {
			if imported == pkg || strings.HasPrefix(imported, pkg+"/") {
				continue
			}
			if !permitted[imported] {
				t.Errorf("%s imports %s, which is not below it", pkg, imported)
			}
		}
	}
}

// ourImports lists the internal packages dir's non-test sources import.
func ourImports(t *testing.T, dir, prefix string) []string {
	t.Helper()
	seen := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, e.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", e.Name(), err)
		}
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if after, ok := strings.CutPrefix(path, prefix); ok {
				seen[after] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	return out
}

// ── rule 9: every command shows how it is used ──

// The grammar is the whole premise of this CLI: one shape, learned once, and
// then guessed correctly everywhere else. --help is where that learning happens,
// so a command that shows its flags but never shows itself being used has left
// the reader to assemble the sentence from parts.
//
// The examples are checked rather than decorative. Each one is parsed against
// the real tree, so an example cannot name a command that does not exist, a flag
// that was renamed, or a different command from the one it illustrates - the
// failure modes that turn documentation into a liability as a tree moves.
func TestEveryLeafShowsHowItIsUsed(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		if strings.TrimSpace(c.Example) == "" {
			t.Errorf("%s: no Example; every command has to show itself being used", cmdPath(c))
		}
	}
}

func TestEveryExampleIsTheCommandItIllustrates(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		for _, line := range exampleLines(c.Example) {
			args, err := splitExample(line)
			if err != nil {
				t.Errorf("%s: example %q: %v", cmdPath(c), line, err)
				continue
			}
			if len(args) == 0 || args[0] != kit.Program {
				t.Errorf("%s: example %q should start with `%s`", cmdPath(c), line, kit.Program)
				continue
			}
			found, rest, err := newRoot().Find(args[1:])
			if err != nil || found.CommandPath() != c.CommandPath() {
				t.Errorf("%s: example %q resolves to %q", cmdPath(c), line, found.CommandPath())
				continue
			}
			if err := found.ParseFlags(rest); err != nil {
				t.Errorf("%s: example %q: %v", cmdPath(c), line, err)
				continue
			}
			// The usage line is what counts a command's arguments, so an example
			// is where the two meet: a line the reader is invited to copy has to
			// survive the check the command makes of it.
			if err := found.Args(found, found.Flags().Args()); err != nil {
				t.Errorf("%s: example %q is refused by %q: %v", cmdPath(c), line, found.Use, err)
			}
		}
	}
}

// An entry filed under a path that names nothing is an example nobody will ever
// read, which is how a renamed command leaves its documentation behind.
func TestNoExampleIsFiledUnderACommandThatDoesNotExist(t *testing.T) {
	leaves, groups := partition(t)
	paths := map[string]bool{}
	for _, c := range append(leaves, groups...) {
		paths[c.CommandPath()] = true
	}
	for path := range examples {
		if !paths[path] {
			t.Errorf("examples has an entry for %q, which is not a command", path)
		}
	}
}

// A group never acts, so an example filed under one would show a command that
// cannot be run.
func TestNoGroupHasExamples(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		if _, ok := examples[c.CommandPath()]; ok {
			t.Errorf("%s is a group, so it has nothing to show being used", cmdPath(c))
		}
	}
}

// exampleLines returns the runnable lines of an Example block, dropping blank
// lines and the comments that head a group of them.
func exampleLines(example string) []string {
	var out []string
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Anything piped or redirected is a shell construct illustrating the
		// command's place in a pipeline; only the proton side is ours to
		// check, and it is whichever segment names the binary.
		for _, seg := range strings.Split(line, "|") {
			seg = strings.TrimSpace(seg)
			if strings.HasPrefix(seg, kit.Program+" ") {
				out = append(out, seg)
			}
		}
	}
	return out
}

// splitExample splits a command line into arguments, honouring the single and
// double quotes an example uses to hold a subject or a name together, and
// stopping at a redirection.
func splitExample(line string) ([]string, error) {
	var (
		args  []string
		cur   strings.Builder
		quote rune
		open  bool
	)
	flush := func() {
		if open || cur.Len() > 0 {
			args = append(args, cur.String())
			cur.Reset()
			open = false
		}
	}
	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote, open = r, true
		case r == '>' || r == '<':
			flush()
			return args, nil
		case unicode.IsSpace(r):
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unbalanced %c quote", quote)
	}
	flush()
	return args, nil
}

// ── rule 12: shorthands are the root's alone ──

// shorthands is every single-letter flag the CLI answers to, and what it means.
//
// A shorthand is a second name for a flag, which is exactly what the rest of
// this file exists to prevent. They are allowed only where the letter is the one
// every other tool already uses for the idea, so it is guessed rather than
// learned, and only on flags typed often enough to be worth a letter.
//
// The namespace is small and global on purpose. `-p` is the profile everywhere,
// which is only safe because no leaf may claim `-p` for `--page`; a shorthand
// that meant one thing on `messages list` and another on `messages send` would
// be the worst version of a flag name meaning two things, because the reader
// cannot even see which one they got.
var shorthands = map[string]string{
	"h": "help",
	"v": "version",
	"o": "output",
	"p": "profile",
	"q": "quiet",
	"n": "dry-run",
	"y": "yes",
}

func TestOnlyTheRootDefinesShorthands(t *testing.T) {
	leaves, groups := partition(t)
	for _, c := range append(leaves, groups...) {
		if c.Name() == kit.Program {
			continue
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Shorthand != "" && f.Name != "help" {
				t.Errorf("%s: --%s takes the shorthand -%s; the letters belong to the root alone",
					cmdPath(c), f.Name, f.Shorthand)
			}
		})
	}
}

// Every letter the root hands out is declared above, and every letter declared
// above is handed out. A shorthand that arrives without being written down is a
// second name nobody agreed to.
func TestShorthandsAreDeclared(t *testing.T) {
	root := newRoot()
	seen := map[string]bool{}
	check := func(f *pflag.Flag) {
		if f.Shorthand == "" {
			return
		}
		seen[f.Shorthand] = true
		if want, ok := shorthands[f.Shorthand]; !ok {
			t.Errorf("-%s is not declared", f.Shorthand)
		} else if want != f.Name {
			t.Errorf("-%s is --%s, but is declared as --%s", f.Shorthand, f.Name, want)
		}
	}
	root.PersistentFlags().VisitAll(check)
	root.Flags().VisitAll(check)
	// cobra supplies these two itself, so they are declared but never visited.
	seen["h"], seen["v"] = true, true
	for letter, name := range shorthands {
		if !seen[letter] {
			t.Errorf("-%s is declared as --%s but nothing defines it", letter, name)
		}
	}
}

// A creation reports the identity it made.
//
// `ID=$(proton ... create ...)` is the whole reason stdout and stderr are split,
// and it works because kit.Create puts the new ID on stdout. A create routed
// through kit.Mutate instead reports the same sentence to a human and gives a
// script nothing, which is a contract broken in the one place nobody looks:
// the exit code is zero and the output is empty.
//
// So the action names the seam. Anything reporting ui.Created goes through
// kit.Create, and this reads the source rather than trusting it.
//
// AnswerFollows is the one exception, and it is the same contract by another
// route: the command writes its own record to stdout straight after, so what a
// script captures is that - the URL of a link it just made, which is the thing
// worth capturing and is not the ID.
func TestACreationIsReportedByTheSeamThatKnowsItsID(t *testing.T) {
	for _, dir := range []string{"."} {
		err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return err
			}
			fset := token.NewFileSet()
			file, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
			if perr != nil {
				return nil
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				seam := selectorName(call.Fun)
				if !strings.HasPrefix(seam, "kit.") {
					return true
				}
				for _, arg := range call.Args {
					if !reportsCreated(arg) || writesItsOwnAnswer(arg) {
						continue
					}
					if seam != "kit.Create" {
						t.Errorf("%s:%d: %s reports ui.Created; a creation goes through "+
							"kit.Create, which is what puts the new ID on stdout",
							p, fset.Position(call.Pos()).Line, seam)
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// writesItsOwnAnswer reports whether a result spec says a record follows it on
// stdout.
func writesItsOwnAnswer(arg ast.Expr) bool {
	lit, ok := arg.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "AnswerFollows" {
			return true
		}
	}
	return false
}

// reportsCreated reports whether an argument is a result spec whose action is
// ui.Created.
func reportsCreated(arg ast.Expr) bool {
	lit, ok := arg.(*ast.CompositeLit)
	if !ok || selectorName(lit.Type) != "ui.ResultSpec" {
		return false
	}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if name, _ := kv.Key.(*ast.Ident); name != nil && name.Name == "Action" &&
			selectorName(kv.Value) == "ui.Created" {
			return true
		}
	}
	return false
}

// selectorName renders a package-qualified name, or "" for anything else.
func selectorName(e ast.Expr) string {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return pkg.Name + "." + sel.Sel.Name
}
