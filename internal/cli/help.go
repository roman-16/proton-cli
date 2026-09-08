package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The help screen is the documentation most people read, and there are 164 of
// them. What every screen repeats is what every screen is judged by, so the nine
// flags that work everywhere are listed once, on the root, and every other
// screen spends the line it saves on a link to that command's own entry in the
// published reference.
//
// The reference URL is not written here. It comes from kit, which the generator
// asks the same question, so a heading and a help screen cannot disagree about
// where a command is documented.

// What the tail is labelled, and so how wide its first column is.
const (
	globalFlagsLabel = "Global flags:"
	perCommandLabel  = "Per-command help:"
	referenceLabel   = "Full reference:"
)

// installHelp gives the whole tree one voice. Cobra resolves both functions by
// walking to the parent, so setting them on the root is setting them everywhere.
//
// The tree is given its `help` command here too, and given it before anything
// walks the tree: cobra would otherwise add its own inside Execute, which is
// after the probe that judges an unknown word has already looked - and a word
// nothing has heard of is exactly what that probe complains about.
func installHelp(root *cobra.Command) {
	root.SetHelpFunc(func(c *cobra.Command, _ []string) {
		writeHelp(c.OutOrStdout(), c)
	})
	root.SetUsageFunc(func(c *cobra.Command) error {
		var b strings.Builder
		usage(&b, c)
		_, err := io.WriteString(c.ErrOrStderr(), b.String())
		return err
	})
	root.SetHelpCommand(helpCmd())
	root.InitDefaultHelpCmd()
}

// helpName is the word, which several places have to recognise without
// spelling it again.
const helpName = "help"

// helpCmd is the other way to ask, for the hand that types `git help log`.
//
// It is the same screen --help shows, from the same renderer, and it fails the
// same way: a word the tree does not hold is the one complaint this CLI makes
// about an unknown word, wherever it was typed. It stays out of the listings
// because every screen already teaches --help, and a second entry for the same
// thing would be the only command in the map that documents nothing.
func helpCmd() *cobra.Command {
	return &cobra.Command{
		Use:    helpName + " [COMMAND...]",
		Short:  "Show the help for a command",
		Hidden: true,
		ValidArgsFunction: func(_ *cobra.Command, typed []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return helpTopics(typed, toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			// Probed on a tree of its own, for the reason Execute probes on one:
			// Find and ParseFlags leave state behind, and the tree that is running
			// this command is that tree.
			probe := newRoot()
			if err := unknownSubcommand(probe, args); err != nil {
				return err
			}
			target, _, _ := probe.Find(args)
			writeHelp(cmd.OutOrStdout(), target)
			return nil
		},
	}
}

// helpTopics is what may follow what has been typed so far: the subcommands of
// wherever those words have reached.
func helpTopics(typed []string, toComplete string) []string {
	found, _, err := newRoot().Find(typed)
	if err != nil {
		return nil
	}
	var out []string
	for _, sub := range visibleSubcommands(found) {
		if strings.HasPrefix(sub.Name(), toComplete) {
			out = append(out, sub.Name()+"\t"+sub.Short)
		}
	}
	return out
}

// writeHelp renders a command's whole screen: what it is, then how to use it.
//
// The screen is assembled before any of it is written, so the only thing that
// can fail is the one write at the end.
func writeHelp(w io.Writer, c *cobra.Command) {
	var b strings.Builder
	if described := strings.TrimSpace(describe(c)); described != "" {
		fmt.Fprintf(&b, "%s\n\n", described)
	}
	usage(&b, c)
	// Nowhere to report a failed write to: cobra's help hook returns nothing, and
	// a screen that cannot be printed has nothing left to print it with.
	_, _ = io.WriteString(w, b.String())
}

// usage renders everything below the description.
func usage(b *strings.Builder, c *cobra.Command) {
	c.InitDefaultHelpFlag()

	fmt.Fprintf(b, "Usage:\n  %s\n", usageLine(c))

	if len(c.Aliases) > 0 {
		fmt.Fprintf(b, "\nAliases:\n  %s\n", strings.Join(append([]string{c.Name()}, c.Aliases...), ", "))
	}

	if example := strings.TrimSpace(c.Example); example != "" {
		fmt.Fprintf(b, "\nExamples:\n%s\n", indent(example))
	}

	writeCommands(b, c)

	// A group's only flag is the one that got you here, and a block holding
	// nothing else says nothing.
	if local := c.LocalFlags(); hasFlagsOfItsOwn(local) {
		fmt.Fprintf(b, "\nFlags:\n%s", local.FlagUsages())
	}

	writeTail(b, c)
}

// writeCommands lists what a group holds.
//
// The root lists them under its own headings, because that is the map of the
// product; everywhere else the flat list is the map.
func writeCommands(b *strings.Builder, c *cobra.Command) {
	subs := visibleSubcommands(c)
	if len(subs) == 0 {
		return
	}
	pad := 0
	for _, sub := range subs {
		pad = max(pad, len(sub.Name()))
	}
	write := func(in []*cobra.Command) {
		for _, sub := range in {
			fmt.Fprintf(b, "  %-*s  %s\n", pad, sub.Name(), sub.Short)
		}
	}
	if !c.HasParent() && len(c.Groups()) > 0 {
		for _, group := range c.Groups() {
			var members []*cobra.Command
			for _, sub := range subs {
				if sub.GroupID == group.ID {
					members = append(members, sub)
				}
			}
			if len(members) == 0 {
				continue
			}
			fmt.Fprintf(b, "\n%s\n", group.Title)
			write(members)
		}
		return
	}
	b.WriteString("\nCommands:\n")
	write(subs)
}

// writeTail is where every screen says where to read more.
func writeTail(b *strings.Builder, c *cobra.Command) {
	// The nine global flags are listed on the root and nowhere else. Naming them
	// on each screen instead is the ten lines this exists to save.
	lead, pointer := globalFlagsLabel, kit.Program+" --help"
	if !c.HasParent() {
		lead, pointer = perCommandLabel, kit.Program+" <command> --help"
	}
	reference := kit.Reference(c)
	if reference == "" {
		fmt.Fprintf(b, "\n%-*s%s\n", len(lead)+2, lead, pointer)
		return
	}
	width := max(len(lead), len(referenceLabel)) + 2
	fmt.Fprintf(b, "\n%-*s%s\n%-*s%s\n", width, lead, pointer, width, referenceLabel, reference)
}

// usageLine is the shape of the command.
//
// The root teaches the whole grammar, because the grammar is what makes the rest
// of the tree guessable and the root is where somebody arrives to learn it. A
// group below it has already had that taught and only needs to say what comes
// next; a leaf says exactly what it takes.
func usageLine(c *cobra.Command) string {
	switch {
	case !c.HasParent():
		return kit.Program + " <app> <collection> <verb> [TARGET...] [--flags]"
	case len(visibleSubcommands(c)) > 0:
		return c.CommandPath() + " <command>"
	default:
		return c.UseLine()
	}
}

func visibleSubcommands(c *cobra.Command) []*cobra.Command {
	var out []*cobra.Command
	for _, sub := range c.Commands() {
		if sub.Hidden {
			continue
		}
		out = append(out, sub)
	}
	return out
}

func indent(block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			lines[i] = "  " + line
		}
	}
	return strings.Join(lines, "\n")
}

func hasFlagsOfItsOwn(set *pflag.FlagSet) bool {
	found := false
	set.VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Name != "help" {
			found = true
		}
	})
	return found
}

// describe prefers the long form, which is where a command explains itself
// rather than merely names itself.
func describe(c *cobra.Command) string {
	if strings.TrimSpace(c.Long) != "" {
		return c.Long
	}
	return c.Short
}
