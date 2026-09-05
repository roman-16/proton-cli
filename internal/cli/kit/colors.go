package kit

import (
	"fmt"

	"github.com/roman-16/proton-cli/internal/accent"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// AccentColor resolves a colour the user typed to the hex Proton stores, and
// refuses anything off Proton's palette by naming the whole of it.
//
// The refusal is local because the API's own says only that the colour was
// invalid, which leaves the user no better off than before.
//
// Empty resolves to empty, so a caller can supply its own default.
func AccentColor(color string) (string, error) {
	if color == "" {
		return "", nil
	}
	if hex := accent.Resolve(color); hex != "" {
		return hex, nil
	}
	lines := []string{"use a name or a hex value:"}
	for _, c := range accent.Palette {
		lines = append(lines, fmt.Sprintf("  %-11s %s", c.Name, c.Hex))
	}
	return "", Fail("%q is not a Proton accent color.", color).Hint(lines...)
}

// ValidateAccentColor rejects anything outside Proton's palette, naming the whole
// palette when it does.
func ValidateAccentColor(color string) error {
	_, err := AccentColor(color)
	return err
}

// ColorColumn is the COLOR column, wherever a collection has one.
//
// It shows the colour rather than describing it: a swatch painted in the colour
// itself, and beside it the name Proton uses. A hex code is what the API stores,
// not what a person reads, so it appears only for a value outside the palette -
// where there is no name to give.
//
// Machine output is untouched: the hex is the field, as it always was.
func ColorColumn[T any](hex func(T) string) ui.Column[T] {
	return ui.Column[T]{
		Header: "COLOR",
		Swatch: hex,
		Cell: func(row T) string {
			v := hex(row)
			if name := accent.Name(v); name != "" {
				return name
			}
			return v
		},
	}
}

// ColorField is ColorColumn's counterpart for a record.
func ColorField(hex string) ui.Field {
	value := hex
	if name := accent.Name(hex); name != "" {
		value = name
	}
	return ui.Field{Label: "Color", Value: value, Swatch: hex}
}

// Color is a flag holding one of Proton's accent colours.
//
// It is declared rather than checked by hand for the same reason an Enum is: the
// palette is fixed, so a wrong value is wrong before anyone signs in, and Run
// refuses it there. Twenty hex codes are too many to list in a flag's help, so
// the domain appears in the error instead - which is where a person who guessed
// wrong is looking.
type Color struct {
	// Name is the flag name, without dashes.
	Name string
	// Usage is the help text.
	Usage string
	// Default is the colour used when the flag is absent. Empty means the colour
	// is optional and its absence means "leave it alone".
	Default string

	target string
}

func (c *Color) Register(cmd *cobra.Command) {
	c.target = c.Default
	usage := c.Usage
	if usage == "" {
		usage = "Accent color, by name (purple) or hex (#8080FF)"
	}
	cmd.Flags().StringVar(&c.target, c.Name, c.Default, usage)
	_ = cmd.RegisterFlagCompletionFunc(c.Name,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			// Completing the names rather than the hexes offers the spelling a
			// person can recognise; the hex trails as the description.
			out := make([]string, 0, len(accent.Palette))
			for _, c := range accent.Palette {
				out = append(out, c.Name+"\t"+c.Hex)
			}
			return out, cobra.ShellCompDirectiveNoFileComp
		})
	registerCheck(cmd, c.Name, nil, c)
}

// Value returns the colour as Proton stores it, whichever spelling was given, or
// "" when none was.
func (c *Color) Value() string {
	hex, err := AccentColor(c.target)
	if err != nil {
		// Unreachable: validate has already refused anything unresolvable, before
		// the command body ran.
		return c.target
	}
	return hex
}

// Set reports whether a colour was supplied.
func (c *Color) Set() bool { return c.target != "" }

func (c *Color) validate() error { return ValidateAccentColor(c.target) }
