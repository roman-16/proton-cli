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

func TestGroupsNeverAct(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		if c.Runnable() {
			t.Errorf("%s is a group but also runs; move the behaviour to a verb such as `get`", cmdPath(c))
		}
	}
}

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

func TestNoneIsTakenOnlyByAnArgumentACheckHolds(t *testing.T) {
	leaves, _ := partition(t)
	for _, c := range leaves {
		name, declared := c.Annotations[kit.TakesNone]
		if !declared {
			continue
		}
		if !slices.Contains(argTokens(c.Use), name) {
			t.Errorf("%s: takes none for %s, which %q does not show", cmdPath(c), name, c.Use)
		}
		if kit.Placeholders[name].Check == nil {
			t.Errorf("%s: takes none for %s, which no check holds", cmdPath(c), name)
		}
	}
}

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

func lists(root *cobra.Command, collection string) bool {
	return listing(root, collection) != nil
}

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

func argTokens(use string) []string {
	args := kit.Arguments(use)
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, a.Name)
	}
	return out
}

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
	global := map[string]bool{}
	newRoot().PersistentFlags().VisitAll(func(f *pflag.Flag) { global[f.Name] = true })
	for name := range kit.Flags {
		if len(users[name]) == 0 && !global[name] {
			t.Errorf("kit.Flags declares --%s and no command takes it", name)
		}
	}
}

var kitOwnedFlags = map[string]string{
	"after":                  "kit.DayRange",
	"all":                    "kit.All",
	"before":                 "kit.DayRange",
	"clear-link-password":    "kit.LinkPassword",
	"eo-password-file":       "kit.EOPassword",
	"extra-password-file":    "kit.ExtraPassword",
	"imap-password-file":     "kit.IMAPPassword",
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

var kitFlagUsage = map[string]string{
	"after":                  kit.AfterUsage,
	"all":                    kit.AllUsage,
	"before":                 kit.BeforeUsage,
	"clear-link-password":    kit.ClearLinkPasswordUsage,
	"eo-password-file":       kit.EOPasswordFileUsage,
	"extra-password-file":    kit.ExtraPasswordFileUsage,
	"imap-password-file":     kit.IMAPPasswordFileUsage,
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

var domainsByCollection = map[string]bool{"access": true, "format": true, "sort": true, "type": true}

func TestAFlagNameNamesOneSetOfValues(t *testing.T) {
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

func TestFlagsThatContradictAreDeclaredThroughKit(t *testing.T) {
	allowed := map[string]bool{"../cli/kit/exclusive.go": true}
	offenders := grepGo(t, []string{"../cli"}, func(src string) bool {
		return strings.Contains(src, ".MarkFlagsMutuallyExclusive\n")
	})
	for _, f := range offenders {
		if !allowed[f] {
			t.Errorf("%s marks flags exclusive itself; declare them with kit.Exclusive, which refuses them in one sentence", f)
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

func TestEverySettingSetTakesIsOneGetReports(t *testing.T) {
	newRoot()
	tables := kit.DeclaredSettings()
	if len(tables) == 0 {
		t.Fatal("found no settings tables; the registry is broken")
	}
	for path, table := range tables {
		reported := map[string]bool{}
		for i := range table.Reports.NumField() {
			name, _, _ := strings.Cut(table.Reports.Field(i).Tag.Get("json"), ",")
			reported[name] = true
		}
		for _, key := range table.Keys {
			if !reported[strings.ReplaceAll(key, "-", "_")] {
				t.Errorf("%s set takes %q, but %s get reports no %s",
					path, key, path, strings.ReplaceAll(key, "-", "_"))
			}
		}
	}
}

var filterParityExceptions = map[string]string{}

var notNarrowing = map[string]bool{
	"limit": true, "page": true, "scope": true, "help": true,
	"into": true, "dest": true, "dest-dir": true, "force": true,
	"format": true, "no-attachments": true, "label": true,
	"expires": true, "until": true,
}

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

func TestOnlyTheUIPackageTouchesTheProcessStreams(t *testing.T) {
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

func TestEverySettingsKeyIsAlsoAFlag(t *testing.T) {
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

func TestStandardInputHasOneOwner(t *testing.T) {
	allowed := map[string]bool{"../ui/ui.go": true}
	offenders := grepGo(t, []string{"../cli", "../app", "../service", "../account", "../proton", "../ui"},
		func(src string) bool { return strings.Contains(src, "os.Stdin") })
	for _, f := range offenders {
		if !allowed[f] {
			t.Errorf("%s reads os.Stdin directly; go through App.Stdin so stdin keeps one owner", f)
		}
	}
}

func TestReauthCommandsAreDeclared(t *testing.T) {
	want := []string{
		"proton account delete",
		"proton account keys create",
		"proton account keys delete",
		"proton account keys export",
		"proton account keys import",
		"proton account keys reactivate",
		"proton account keys update",
		"proton account login",
		"proton account security-log delete",
		"proton account security-log disable",
		"proton account security-log enable",
		"proton account sessions revoke",
		"proton account settings emergency-access add",
		"proton account settings emergency-access cancel",
		"proton account settings emergency-access grant",
		"proton account settings emergency-access remove",
		"proton account settings emergency-access update",
		"proton account settings password set",
		"proton account settings recovery-contacts add",
		"proton account settings recovery-contacts remove",
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
		"proton mail conversations update",
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
	spoken["completion"] = true
	for verb := range kit.Verbs {
		if !spoken[verb] {
			t.Errorf("kit.Verbs declares %q and no command ends in it", verb)
		}
	}
}

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

type logCall struct {
	file  string
	line  int
	names []string
}

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

func logReceiver(x ast.Expr) bool {
	switch v := x.(type) {
	case *ast.Ident:
		return v.Name == "slog" || v.Name == "log"
	case *ast.SelectorExpr:
		return v.Sel.Name == "log" || v.Sel.Name == "Log" || v.Sel.Name == "Trace"
	}
	return false
}

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

var accountNames = map[string]bool{"Name": true, "Subject": true, "Title": true}

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

func carriesAName(call *ast.CallExpr) bool {
	for _, arg := range call.Args[min(1, len(call.Args)):] {
		sel, ok := arg.(*ast.SelectorExpr)
		if ok && accountNames[sel.Sel.Name] {
			return true
		}
	}
	return false
}

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

var layers = map[string][]string{
	"ui":          {"units", "progress", "errs", "inflect", "ref", "redact"},
	"proton":      {"errs", "units", "crypto/aead", "hv", "hv/hvexit"},
	"errs":        {"inflect"},
	"inflect":     {},
	"units":       {},
	"progress":    {},
	"mailtext":    {"tidyurl"},
	"tidyurl":     {},
	"redact":      {"ref"},
	"search":      {"crypto/aead", "crypto/pgp", "localfile", "progress"},
	"localfile":   {"errs"},
	"export":      {"localfile", "progress", "proton"},
	"crypto/pgp":  {},
	"skip":        {},
	"runlog":      {},
	"idcache":     {"ref"},
	"ref":         {"errs"},
	"changelog":   {},
	"contentline": {},
	"accent":      {},
	"ical":        {"contentline"},
	"vcard":       {"contentline"},
	"profile":     {},
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
			if err := found.Args(found, found.Flags().Args()); err != nil {
				t.Errorf("%s: example %q is refused by %q: %v", cmdPath(c), line, found.Use, err)
			}
		}
	}
}

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

func TestNoGroupHasExamples(t *testing.T) {
	_, groups := partition(t)
	for _, c := range groups {
		if _, ok := examples[c.CommandPath()]; ok {
			t.Errorf("%s is a group, so it has nothing to show being used", cmdPath(c))
		}
	}
}

func exampleLines(example string) []string {
	var out []string
	for _, line := range strings.Split(example, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, seg := range strings.Split(line, "|") {
			seg = strings.TrimSpace(seg)
			if strings.HasPrefix(seg, kit.Program+" ") {
				out = append(out, seg)
			}
		}
	}
	return out
}

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
	seen["h"], seen["v"] = true, true
	for letter, name := range shorthands {
		if !seen[letter] {
			t.Errorf("-%s is declared as --%s but nothing defines it", letter, name)
		}
	}
}

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
