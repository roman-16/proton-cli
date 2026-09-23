package kit

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/roman-16/proton-cli/internal/proton"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Table-driven settings.
//
// A scope declares its writable settings once; parsing, validation, the key
// listing, the help text, the error wording and shell completion are all derived
// from that table. A setting therefore cannot accept a value its own help does
// not document, which is the failure mode hand-written validation always
// eventually produces.
//
// The trio is deliberately three verbs rather than a group that acts:
//
//	get   the values now in effect       (a record)
//	list  the keys that can be written   (a collection)
//	set   change one                     (a mutation)
//
// Keeping `list` out of `set` matters: a bare `set` would be a mutation
// returning a collection, and `settings` itself would be doing work that its own
// subcommands should name.

type Choice struct {
	Name   string
	Value  any
	Bodies []map[string]any
}

// IntRange bounds an integer setting. Unit annotates the rendered domain, as in
// "0-20 (seconds)".
type IntRange struct {
	Min, Max int
	Unit     string
}

// Setting describes one writable setting: where it is stored, which body field
// carries it, which page of Proton's settings it belongs to, and what it accepts.
//
// At most one value domain is set. Neither means free text, which is right for
// an opaque value such as a locale or an IANA time zone.
type Setting struct {
	Path  string
	Field string
	Page  string
	Desc  string

	Enum  []Choice
	Range *IntRange
}

// Parse finds the choice a user-supplied value names, rejecting anything the
// domain does not permit.
func (s Setting) Parse(key, raw string) (Choice, error) {
	switch {
	case len(s.Enum) > 0:
		for _, c := range s.Enum {
			if strings.EqualFold(raw, c.Name) {
				return c, nil
			}
		}
		if n, err := strconv.Atoi(raw); err == nil {
			for _, c := range s.Enum {
				if v, ok := c.Value.(int); ok && v == n {
					return c, nil
				}
			}
		}
		return Choice{}, Fail("%s accepts: %s", key, s.Domain())
	case s.Range != nil:
		n, err := strconv.Atoi(raw)
		if err != nil || n < s.Range.Min || n > s.Range.Max {
			return Choice{}, Fail("%s accepts %s", key, s.Domain())
		}
		return Choice{Name: strconv.Itoa(n), Value: n}, nil
	}
	if raw == "" {
		return Choice{}, Fail("%s needs a value.", key)
	}
	return Choice{Name: raw, Value: raw}, nil
}

// Domain renders what the setting accepts, for help text and error messages.
func (s Setting) Domain() string {
	switch {
	case len(s.Enum) > 0:
		return strings.Join(s.Completions(), ", ")
	case s.Range != nil:
		if s.Range.Unit != "" {
			return fmt.Sprintf("%d-%d (%s)", s.Range.Min, s.Range.Max, s.Range.Unit)
		}
		return fmt.Sprintf("%d-%d", s.Range.Min, s.Range.Max)
	}
	return "any text"
}

// Completions lists the concrete values shell completion should offer. A range or
// free text has no finite set, so it offers nothing rather than guessing.
func (s Setting) Completions() []string {
	out := make([]string, 0, len(s.Enum))
	for _, v := range s.Enum {
		out = append(out, v.Name)
	}
	return out
}

// Name maps a raw API value back to its declared name, so reads and writes speak
// the same vocabulary. An unrecognised value renders as itself, which is more use
// than hiding it.
func (s Setting) Name(v any) string {
	for _, c := range s.Enum {
		if stored(c.Value, v) {
			return c.Name
		}
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	}
	return strconv.Itoa(IntOf(v))
}

func stored(want, got any) bool {
	switch w := want.(type) {
	case nil:
		return got == nil
	case int:
		switch g := got.(type) {
		case float64:
			return g == float64(w)
		case int:
			return g == w
		}
	case bool:
		g, ok := got.(bool)
		return ok && g == w
	case string:
		g, ok := got.(string)
		return ok && g == w
	}
	return false
}

// Ordered names the values 0, 1, 2 and so on, which is how nearly all of
// Proton's enumerated settings are numbered. Writing them positionally keeps the
// declaration as short as the idea.
func Ordered(names ...string) []Choice {
	out := make([]Choice, len(names))
	for i, n := range names {
		out[i] = Choice{Name: n, Value: i}
	}
	return out
}

// OnOffNumbers is the domain of a plain 0/1 toggle.
func OnOffNumbers() []Choice { return Ordered("off", "on") }

func OnOffBooleans() []Choice {
	return []Choice{{Name: "off", Value: false}, {Name: "on", Value: true}}
}

type SettingsTable struct {
	Keys    []string
	Reports reflect.Type
}

type settingsTable struct {
	cmd     *cobra.Command
	specs   map[string]Setting
	reports reflect.Type
}

var (
	settingsMu     sync.Mutex
	settingsTables []settingsTable
)

func DeclaredSettings() map[string]SettingsTable {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	out := make(map[string]SettingsTable, len(settingsTables))
	for _, t := range settingsTables {
		out[t.cmd.CommandPath()] = SettingsTable{Keys: SortedKeys(t.specs), Reports: t.reports}
	}
	return out
}

// Settings builds the get/list/set trio for one scope. scope names the tree in
// help and error text ("account", "mail"); reports is what show renders the
// current values as.
func Settings(scope, short string, specs map[string]Setting, reports any, show Handler) *cobra.Command {
	c := &cobra.Command{Use: "settings", Short: short}
	c.AddCommand(
		&cobra.Command{
			Use:   "get",
			Short: "Show the " + scope + " settings now in effect",
			RunE:  Run(nil, show),
		},
		settingsListCmd(scope, specs),
		settingsSetCmd(scope, specs),
	)
	settingsMu.Lock()
	settingsTables = append(settingsTables, settingsTable{cmd: c, specs: specs, reports: reflect.TypeOf(reports)})
	settingsMu.Unlock()
	return c
}

// settingRow is one line of the writable-keys listing.
type settingRow struct {
	Key    string `json:"key"`
	Values string `json:"values"`
	Page   string `json:"page"`
	Desc   string `json:"description"`
}

func settingsListCmd(scope string, specs map[string]Setting) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the " + scope + " settings that can be changed",
		// No authentication: this is the schema, not the state.
		RunE: Run(nil, func(c *Invocation) error {
			keys := SortedKeys(specs)
			// Group by the page Proton puts each setting on, so the listing maps
			// onto what a user already knows from the web client.
			sort.SliceStable(keys, func(i, j int) bool {
				return specs[keys[i]].Page < specs[keys[j]].Page
			})
			rows := make([]settingRow, 0, len(keys))
			for _, k := range keys {
				s := specs[k]
				rows = append(rows, settingRow{Key: k, Values: s.Domain(), Page: s.Page, Desc: s.Desc})
			}
			return List(c, ui.TableSpec[settingRow]{
				Noun:  "settings",
				Total: ui.Unknown, Page: ui.Unpaged,
				Columns: []ui.Column[settingRow]{
					{Header: "KEY", Cell: func(r settingRow) string { return r.Key }},
					{Header: "VALUES", Flex: true, Cell: func(r settingRow) string { return r.Values }},
					{Header: "PAGE", Cell: func(r settingRow) string { return r.Page }},
					{Header: "DESCRIPTION", Flex: true, Cell: func(r settingRow) string { return r.Desc }},
				},
			}, rows)
		}),
	}
}

func settingsSetCmd(scope string, specs map[string]Setting) *cobra.Command {
	return &cobra.Command{
		Use:   "set KEY VALUE",
		Short: "Change one " + scope + " setting",
		// Validating here rather than in the body means a mistyped key or an
		// impossible value is refused before anything touches the network. Cobra
		// runs Args before the persistent setup, so no session is established to
		// reject a value that could never have been sent.
		Args: func(_ *cobra.Command, args []string) error {
			spec, ok := specs[args[0]]
			if !ok {
				return Fail("There is no %s setting called %q.", scope, args[0]).
					Hint(fmt.Sprintf("%s %s settings list", Program, scope)).Exit(3)
			}
			_, err := spec.Parse(args[0], args[1])
			return err
		},
		ValidArgsFunction: func(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
			switch len(args) {
			case 0:
				return SortedKeys(specs), cobra.ShellCompDirectiveNoFileComp
			case 1:
				return specs[args[0]].Completions(), cobra.ShellCompDirectiveNoFileComp
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: Run(nil, func(c *Invocation) error {
			key, raw := c.Args[0], c.Args[1]
			// Already validated by Args; re-parsing is how the checked value gets
			// here without a package-level variable to carry it.
			spec := specs[key]
			choice, err := spec.Parse(key, raw)
			if err != nil {
				return err
			}
			bodies := choice.Bodies
			if bodies == nil {
				bodies = []map[string]any{{spec.Field: choice.Value}}
			}
			return Mutate(c, ui.ResultSpec{
				Action: ui.Set, Count: 1, Name: key,
				Detail: "to " + choice.Name,
				Extra:  map[string]any{"key": key, "value": choice.Name},
			}, func() error {
				for _, body := range bodies {
					if err := c.App.API.Decode(c.Ctx, proton.Request{
						Method: "PUT", Path: spec.Path, Body: body,
					}, nil); err != nil {
						return err
					}
				}
				return nil
			})
		}),
	}
}

// IntOf coerces a JSON-decoded value to an int, which is what Proton's numeric
// settings arrive as.
func IntOf(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// StrOf coerces a JSON-decoded value to a string.
func StrOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// OnOffText renders a 0/1 toggle in the same vocabulary `set` accepts.
func OnOffText(i int) string {
	if i == 1 {
		return "on"
	}
	return "off"
}
