package kit

import (
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
)

// Declarative enum flags.
//
// A flag with a fixed set of values has three obligations: accept only those
// values, say which they are when it refuses, and offer them to shell
// completion. Declaring the domain once discharges all three, keeps every such
// refusal worded alike, and lets the conformance test assert that no enum flag
// exists without one.

// Enum is a flag whose value must come from a fixed set.
type Enum struct {
	// Name is the flag name, without dashes.
	Name string
	// Usage is the help text, without the value list: Register appends that.
	Usage string
	// Values are the accepted values, in the order they should be offered.
	Values []string
	// Default is the value used when the flag is absent. Empty means the flag is
	// optional and its absence is meaningful.
	Default string

	target string
}

// checked is anything Run validates before the first step: a flag whose accepted
// values are declared, and which is therefore wrong or right regardless of who is
// asking.
type checked interface {
	validate() error
}

// The registry records declared domains twice over: by command and flag name, so
// the conformance test can check that each constrained flag has one, and by
// command alone, so Run can validate them all before doing anything else.
//
// The command is kept rather than its path, because a flag is declared while the
// command is still on its own: two commands both called `export` have the same
// path until each is attached to its app, and recording that path would have one
// of them standing in for the other.
var (
	enumMu       sync.Mutex
	enumRegistry []declaredEnum
	checksByCmd  = map[*cobra.Command][]checked{}
)

type declaredEnum struct {
	cmd    *cobra.Command
	flag   string
	values []string
}

// registerCheck records a flag whose value can be validated locally.
func registerCheck(cmd *cobra.Command, flag string, values []string, c checked) {
	enumMu.Lock()
	defer enumMu.Unlock()
	if values != nil {
		enumRegistry = append(enumRegistry, declaredEnum{cmd: cmd, flag: flag, values: values})
	}
	checksByCmd[cmd] = append(checksByCmd[cmd], c)
}

// Register binds the flag to cmd, appends the accepted values to its help, and
// installs completion for them.
func (e *Enum) Register(cmd *cobra.Command) {
	e.target = e.Default
	usage := e.Usage + ": " + strings.Join(e.Values, ", ")
	cmd.Flags().StringVar(&e.target, e.Name, e.Default, usage)
	_ = cmd.RegisterFlagCompletionFunc(e.Name,
		func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return e.Values, cobra.ShellCompDirectiveNoFileComp
		})

	registerCheck(cmd, e.Name, e.Values, e)
}

func (e *Enum) validate() error {
	_, err := e.Value()
	return err
}

// validateFlags checks every constrained flag on cmd.
//
// Run calls this before the first step, which is what keeps an impossible value
// from costing a sign-in: `--format htm` is wrong whoever you are, so reporting a
// missing password instead would be answering a question nobody asked.
func validateFlags(cmd *cobra.Command) error {
	enumMu.Lock()
	checks := checksByCmd[cmd]
	enumMu.Unlock()
	for _, c := range checks {
		if err := c.validate(); err != nil {
			return err
		}
	}
	return nil
}

// Value returns the validated value.
//
// It is an error rather than a silent fallback when the value is not in the
// domain, and the message lists the whole domain: a user who guessed wrong needs
// to see every option, not be told they were wrong.
func (e *Enum) Value() (string, error) {
	v := strings.TrimSpace(e.target)
	if v == "" && e.Default == "" {
		return "", nil
	}
	for _, want := range e.Values {
		if strings.EqualFold(v, want) {
			return want, nil
		}
	}
	return "", Fail("--%s accepts: %s", e.Name, strings.Join(e.Values, ", "))
}

// Set reports whether a value was supplied at all.
func (e *Enum) Set() bool { return e.target != "" }

// Is reports whether the value equals want, without re-validating. Use it after
// Value has succeeded.
func (e *Enum) Is(want string) bool { return strings.EqualFold(e.target, want) }

// DeclaredEnums returns the registry keyed by command path and flag, for the
// conformance test. It is read once the tree is built, which is when a command
// knows where it sits.
func DeclaredEnums() map[string][]string {
	enumMu.Lock()
	defer enumMu.Unlock()
	out := make(map[string][]string, len(enumRegistry))
	for _, e := range enumRegistry {
		out[e.cmd.CommandPath()+" --"+e.flag] = e.values
	}
	return out
}

// ── choice sets shared across apps ──

// OnOff is the domain of a plain toggle. Proton stores these as 0 and 1; naming
// them means nobody has to remember which is which.
var OnOff = []string{"off", "on"}

// SortedKeys returns the keys of m in order, for help text that has to be stable.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
