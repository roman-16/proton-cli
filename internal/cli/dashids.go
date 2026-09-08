package cli

import (
	"strings"

	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/spf13/pflag"
)

// About one Proton ID in sixty-four begins with a dash, because the IDs are
// base64 and '-' is one of its sixty-four characters. argv hands a leading dash
// to the flag parser first, so such an ID has to be marked as data before cobra
// sees it.
//
// What counts as a reference is internal/ref's to say, and it says so by shape
// alone: a short ID never begins with a dash, so the only dash-leading token
// that can be a reference is a complete ID - and no flag, nor any run of
// shorthand flags, is 22, 32 or 88 characters of base64.

// preprocessArgs walks argv and protects the first leading-dash reference by
// inserting "--" before it, so cobra treats the rest as positional. A literal
// "--" already present leaves argv untouched.
//
// A reference that is a flag's value is left alone, because it was never in
// danger: pflag reads the token after `--album` as its value whatever it starts
// with. Inserting there is what would do the damage, handing "--" to the flag and
// stranding the reference as a positional argument.
func preprocessArgs(args []string) []string {
	for i := 1; i < len(args); i++ {
		if args[i] == "--" {
			return args
		}
		if !ref.Unambiguous(args[i]) || isFlagValue(args[1:i]) {
			continue
		}
		out := make([]string, 0, len(args)+1)
		out = append(out, args[:i]...)
		out = append(out, "--")
		out = append(out, args[i:]...)
		return out
	}
	return args
}

// isFlagValue reports whether the token ending path is a flag that will take
// what follows it as its value.
//
// The answer differs per flag: `--album X` takes a value and `--unread X` does
// not, so after `--unread` a reference really is positional and really does need
// protecting. Only the command knows which, so the tree is asked; a path that
// resolves to nothing answers no, since a reference certain by shape is worth
// protecting either way.
func isFlagValue(path []string) bool {
	if len(path) == 0 {
		return false
	}
	prev := path[len(path)-1]
	if !strings.HasPrefix(prev, "-") || prev == "-" || strings.Contains(prev, "=") {
		return false
	}
	// Probed on a tree of its own: Find leaves state behind, and the tree that
	// answers the command has to start clean.
	cmd, _, err := newRoot().Find(path)
	if err != nil || cmd == nil {
		return false
	}
	var flag *pflag.Flag
	if name := strings.TrimPrefix(prev, "--"); name != prev {
		flag = cmd.Flags().Lookup(name)
	} else if short := prev[1:]; len(short) == 1 {
		flag = cmd.Flags().ShorthandLookup(short)
	}
	// A boolean flag never consumes the next argument, so what follows one is a
	// positional and is exactly what the protection exists for.
	return flag != nil && flag.NoOptDefVal == ""
}
