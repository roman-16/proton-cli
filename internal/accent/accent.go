// Package accent is Proton's accent palette: the twenty colours it offers for a
// label, a folder, a calendar, a contact group or an event, and the ways a
// colour reaches one of them.
//
// The palette lives on its own because two very different callers need it. A
// flag resolves what a person typed and refuses anything else, since the API's
// own refusal says only that the colour was invalid. An import resolves what a
// file said, where refusing is not an option: a file may name any colour CSS
// has, and the event still has to land.
package accent

import (
	"strconv"
	"strings"
)

// Color is one entry of the palette: the name Proton shows and the hex it
// stores.
type Color struct {
	Name string
	Hex  string
}

// Palette is Proton's fixed accent palette, in the order its own clients offer
// it, mirroring ACCENT_COLORS in WebClients (packages/shared/lib/colors.ts).
var Palette = []Color{
	{"purple", "#8080FF"}, {"pink", "#DB60D6"}, {"strawberry", "#EC3E7C"},
	{"carrot", "#F78400"}, {"sahara", "#936D58"}, {"enzian", "#5252CC"},
	{"plum", "#A839A4"}, {"cerise", "#BA1E55"}, {"copper", "#C44800"},
	{"soil", "#54473F"}, {"slateblue", "#415DF0"}, {"pacific", "#179FD9"},
	{"reef", "#1DA583"}, {"fern", "#3CBB3A"}, {"olive", "#B4A40E"},
	{"cobalt", "#273EB2"}, {"ocean", "#0A77A6"}, {"pine", "#0F735A"},
	{"forest", "#258723"}, {"pickle", "#807304"},
}

// Default is the purple Proton offers first, used wherever a colour is optional.
const Default = "#8080FF"

// Resolve turns a colour a person typed into the hex Proton stores, taking
// either spelling, and answers "" for anything off the palette.
//
// Both spellings are accepted because both are spellings of the same thing: the
// palette has twenty entries and every one of them has a name. A list that shows
// "purple" and a flag that only takes "#8080FF" would be the CLI printing
// something it will not read back.
func Resolve(color string) string {
	for _, c := range Palette {
		if strings.EqualFold(c.Name, color) || strings.EqualFold(c.Hex, color) {
			return c.Hex
		}
	}
	return ""
}

// Name is Proton's own name for a colour, or "" for a hex outside the palette.
// It mirrors getColorName in WebClients (packages/shared/lib/colors.ts).
func Name(hex string) string {
	for _, c := range Palette {
		if strings.EqualFold(c.Hex, hex) {
			return c.Name
		}
	}
	return ""
}

// Nearest is the palette entry closest to any colour CSS can name - a name, a
// three-digit hex or a six-digit one - and "" for a value that is not a colour
// at all.
//
// A file may say COLOR:tomato, and there is no tomato here: Proton takes its
// twenty and nothing else. Snapping is what its own import does, and it beats
// the alternatives - refusing the event over a decoration, or dropping the
// colour and drawing an event the file described as red in the calendar's blue.
//
// Distance is measured in RGB, which is the same arithmetic Proton's clients
// use to answer the same question.
func Nearest(color string) string {
	r, g, b, ok := parse(color)
	if !ok {
		return ""
	}
	best, closest := "", 0
	for i, c := range Palette {
		cr, cg, cb, _ := parse(c.Hex)
		d := square(r-cr) + square(g-cg) + square(b-cb)
		if i == 0 || d < closest {
			best, closest = c.Hex, d
		}
	}
	return best
}

func square(n int) int { return n * n }

// parse reads a colour as its three channels.
func parse(color string) (r, g, b int, ok bool) {
	hex := strings.TrimSpace(color)
	if named, found := cssColors[strings.ToLower(hex)]; found {
		hex = named
	}
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 {
		// The short form names each channel with one digit, which stands for that
		// digit twice: #F00 is #FF0000.
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	if len(hex) != 6 {
		return 0, 0, 0, false
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return 0, 0, 0, false
	}
	return int(n>>16) & 0xFF, int(n>>8) & 0xFF, int(n) & 0xFF, true
}
