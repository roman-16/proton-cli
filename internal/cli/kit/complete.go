package kit

import (
	"strings"
	"unicode"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/config"
	"github.com/roman-16/proton-cli/internal/profile"
	"github.com/spf13/cobra"
)

// Completing a reference.
//
// A shell completes by running the binary again, so the only answers it can give
// are the ones already on this disk: asking Proton what a subject is would put a
// request between a key press and the cursor moving. What is on the disk is what
// the last listings showed, which is also what the person at the keyboard is
// reaching for - they read the short ID off the screen and are typing it back.
//
// So the whole feature is: remember what was shown, and offer it again. A
// reference answers to its short form and to its whole self, and a thing answers
// to its handle as well, because the shell decides between them by the prefix
// already typed - "ket" keeps the ID and "Invo" keeps the subject.

// Argument is one positional a command takes.
type Argument struct {
	// Name is the placeholder, as kit.Placeholders declares it.
	Name string
	// Variadic marks the argument that swallows the rest, so the position after
	// it is still the same kind of thing.
	Variadic bool
}

// Arguments reads the positionals out of a Use string, dropping the command name
// and the optional brackets. It is the one parser for the notation, so a usage
// line, the conformance test and a completion all read a command's arguments the
// same way.
func Arguments(use string) []Argument {
	fields := strings.Fields(use)
	out := make([]Argument, 0, len(fields))
	for _, f := range fields[min(1, len(fields)):] {
		f = strings.Trim(f, "[]{}()")
		variadic := strings.HasSuffix(f, "...")
		f = strings.TrimSuffix(f, "...")
		if f == "" || f == "|" {
			continue
		}
		// Only shouted tokens are placeholders; a literal subcommand word is not.
		if f == strings.ToUpper(f) && strings.ContainsFunc(f, unicode.IsLetter) {
			out = append(out, Argument{Name: f, Variadic: variadic})
		}
	}
	return out
}

// At is the argument standing at position i, which is the last one when that one
// swallows the rest.
func At(args []Argument, i int) Argument {
	if i < len(args) {
		return args[i]
	}
	if n := len(args); n > 0 && args[n-1].Variadic {
		return args[n-1]
	}
	return Argument{}
}

// Picks is the collection a command's argument names, or empty when it names
// nothing this CLI holds.
func Picks(c *cobra.Command, arg Argument) string {
	switch picks := Placeholders[arg.Name].Picks; picks {
	case "":
		return ""
	case PicksHolding:
		return Holding(c)
	case PicksAddressed:
		return Addressed(c)
	default:
		return picks
	}
}

// Holding is the collection a command is part of, which is the collection its
// own responses are about.
func Holding(c *cobra.Command) string {
	if c == nil || c.Parent() == nil {
		return ""
	}
	return commandPath(c.Parent())
}

// Addressed is the collection a command's REF names.
//
// It is the nearest collection above the command that can be listed without
// naming something first. That walk is what puts `messages mark read` and
// `messages attachments download` on the same footing: `mark` holds no things of
// its own, and an attachment listing has to be told which message, so neither is
// somewhere a reference could have been read off. A command whose REF names
// something else entirely says so with Addresses.
func Addressed(c *cobra.Command) string {
	if c == nil {
		return ""
	}
	if declared := c.Annotations[Addresses]; declared != "" {
		return declared
	}
	for n := c.Parent(); n != nil && n.Parent() != nil; n = n.Parent() {
		if listsWholly(n) {
			return commandPath(n)
		}
	}
	return ""
}

// listsWholly reports whether a collection can be enumerated by naming it alone.
func listsWholly(c *cobra.Command) bool {
	for _, sub := range c.Commands() {
		if sub.Name() == "list" && len(Arguments(sub.Use)) == 0 {
			return true
		}
	}
	return false
}

// CompleteReferences teaches every command that takes a reference to offer back
// what has already been shown.
//
// Installed over the whole tree rather than command by command, because a
// command that forgot to ask for it would be indistinguishable from one whose
// collection has nothing in it yet. A command that completes its own arguments -
// a settings key, a shell name - keeps doing so.
func CompleteReferences(root *cobra.Command) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			walk(sub)
		}
		if c.HasSubCommands() || c.ValidArgsFunction != nil || len(c.ValidArgs) > 0 {
			return
		}
		args := Arguments(c.Use)
		if !namesAnything(c, args) {
			return
		}
		c.ValidArgsFunction = func(cmd *cobra.Command, typed []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return complete(cmd, At(args, len(typed)), toComplete)
		}
	}
	walk(root)
}

func namesAnything(c *cobra.Command, args []Argument) bool {
	for _, a := range args {
		if Picks(c, a) != "" {
			return true
		}
	}
	return false
}

// complete answers one press of the tab key.
//
// An argument that names nothing this CLI holds is handed back to the shell
// untouched, so a local path still completes as a local path on a command line
// that also takes a reference.
func complete(cmd *cobra.Command, arg Argument, toComplete string) ([]string, cobra.ShellCompDirective) {
	collection := Picks(cmd, arg)
	if collection == "" {
		return nil, cobra.ShellCompDirectiveDefault
	}
	cache := app.Seen(profileFor(cmd))
	found := cache.Candidates(collection, toComplete)
	if len(found) == 0 {
		if toComplete != "" {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Silence here reads as a broken completion rather than as an empty one,
		// and the way out is a command the person has not thought to run.
		return cobra.AppendActiveHelp(nil,
				"Nothing seen yet - run `"+Program+" "+collection+" list` first"),
			cobra.ShellCompDirectiveNoFileComp
	}
	out := make([]string, 0, len(found))
	for _, c := range found {
		if c.About == "" {
			out = append(out, c.Value)
			continue
		}
		out = append(out, c.Value+"\t"+c.About)
	}
	// Newest first is the order they were read off the screen in, which is the
	// order they are wanted in; alphabetical would bury the listing just run.
	return out, cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveKeepOrder
}

// profileFor settles which profile a completion is about.
//
// A completion request carries the command line being typed, flags and all, and
// cobra has parsed it by the time this runs - so `--profile work` decides here
// exactly as it would decide the command it is being typed into. A configuration
// that cannot be read leaves the default profile answering, because a shell has
// nowhere to put a complaint.
func profileFor(c *cobra.Command) profile.Name {
	flag := ""
	if f := c.Flags().Lookup("profile"); f != nil {
		flag = f.Value.String()
	}
	path, named, err := config.Path(configFlag(c))
	if err != nil {
		return profile.Name{}
	}
	file, err := config.Load(path, named)
	if err != nil {
		file = nil
	}
	name, err := config.Profile(file, flag)
	if err != nil {
		return profile.Name{}
	}
	return name
}

func configFlag(c *cobra.Command) string {
	if f := c.Flags().Lookup("config"); f != nil {
		return f.Value.String()
	}
	return ""
}
