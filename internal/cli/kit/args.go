package kit

import (
	"fmt"
	"slices"
	"strings"

	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/spf13/cobra"
)

// How many arguments a command takes is not a second thing to declare. The
// usage line on its help screen already says it - `SRC [DEST]` takes one or two,
// `REF...` takes at least one - so that line is what counts them, and the
// complaint about the wrong number is written from the same names the screen
// showed.

// unbounded is the ceiling of a usage line ending in a variadic, which has none.
const unbounded = -1

// arity is how many positionals a usage line allows.
func arity(args []Argument) (least, most int) {
	for _, a := range args {
		if !a.Optional {
			least++
		}
		if a.Variadic {
			return least, unbounded
		}
		most++
	}
	return least, most
}

// counting refuses the wrong number of arguments before anything else looks at
// them, then hands what is left to whatever the command judges for itself.
func counting(declared []Argument, own cobra.PositionalArgs) cobra.PositionalArgs {
	least, most := arity(declared)
	return func(c *cobra.Command, args []string) error {
		switch {
		case len(args) < least:
			return missing(c, At(declared, len(args)))
		case most != unbounded && len(args) > most:
			return surplus(c, declared, args)
		}
		if err := offered(c, args); err != nil {
			return err
		}
		if own == nil {
			return nil
		}
		return own(c, args)
	}
}

// offered holds a command whose argument comes from a fixed list to that list.
//
// The list is the one the shell completes from, so what is offered and what is
// accepted are the same declaration and cannot drift apart. Cobra lets an entry
// carry a description after a tab, which is for the shell to show and not part
// of the value.
func offered(c *cobra.Command, args []string) error {
	if len(c.ValidArgs) == 0 {
		return nil
	}
	values := make([]string, len(c.ValidArgs))
	for i, v := range c.ValidArgs {
		values[i], _, _ = strings.Cut(v, "\t")
	}
	for _, a := range args {
		if !slices.Contains(values, a) {
			return Fail("`%s` accepts: %s.", c.CommandPath(), strings.Join(values, ", ")).
				Hint(c.CommandPath() + " --help")
		}
	}
	return nil
}

// missing names the first argument that was not given, and what it stands for.
//
// The example goes with it because somebody who has not supplied an argument is
// usually somebody who does not yet know what one looks like, and a working
// command line answers that faster than a description of one.
func missing(c *cobra.Command, arg Argument) error {
	problem := Fail("`%s` needs %s: %s.", c.CommandPath(), arg.Name, Placeholders[arg.Name].Means)
	if line := firstExample(c); line != "" {
		problem = problem.Hint(line)
	}
	return problem.Hint(c.CommandPath() + " --help")
}

// surplus reports more arguments than the command has places for.
//
// One extra argument is often not an extra argument at all. About one Proton ID
// in sixty-four begins with a dash, and everything written after such an ID
// arrives as a positional, so a command line whose flags came last presents
// itself here rather than as a flag that went missing.
func surplus(c *cobra.Command, declared []Argument, args []string) error {
	problem := Fail("`%s` takes %s, but %s.", c.CommandPath(), places(declared), counted(len(args)))
	for _, a := range args {
		if ref.Unambiguous(a) {
			problem = problem.Hint(
				"an ID beginning with '-' takes everything after it as an argument; put the flags first")
			break
		}
	}
	return problem.Hint(c.CommandPath() + " --help")
}

// places is the argument names a usage line offers, as a reader would say them.
func places(declared []Argument) string {
	names := make([]string, len(declared))
	for i, a := range declared {
		names[i] = a.Name
	}
	switch len(names) {
	case 0:
		return "no arguments"
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

func counted(n int) string {
	if n == 1 {
		return "1 argument was given"
	}
	return fmt.Sprintf("%d arguments were given", n)
}

// firstExample is the command's own first line of documentation, which is a
// command line that works.
func firstExample(c *cobra.Command) string {
	for _, line := range strings.Split(c.Example, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, Program+" ") {
			return line
		}
	}
	return ""
}
