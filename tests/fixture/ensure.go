package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/roman-16/proton-cli/internal/cli/kit"
)

// Bringing a fixture about, for whoever asks.
//
// A fixture is made when something needs it and not before. The suite asks as it
// runs, so a run that touches no aliases makes no alias and a run of one mail
// test costs one lookup; `just seed` asks for everything at once, to put an
// account in shape by hand. Both go through here, so the two cannot mean
// different things by the same declaration.

// Runner runs the CLI as one account and hands back its stdout.
type Runner func(profile string, args ...string) (string, error)

// A Pin is one row a collection has to hold.
//
// It is judged on the fields named here and on nothing else: an ID and a
// timestamp are the server's to choose, so demanding those match would mean
// rebuilding the account on every run.
type Pin struct {
	ID     string            // the value identifying it, under the collection's key
	Fields map[string]string // what else has to match
	Create []string          // the command that makes it
	// Secrets are the parts of it that argv may not carry, by the field they
	// belong to. Each is written to a file of its own for the one command that
	// reads it, which is what a person does and so what the fixture does.
	Secrets map[string]string
	// ByHand marks a fixture a person makes once and the suite only ever finds.
	//
	// An alias address cannot be un-minted, so making one is a thing to be sure
	// about - and a listing that comes back short for any reason at all looks
	// exactly like an account that has not got one. A run that guesses wrong
	// spends an address for good, which is why this one guesses in the direction
	// that costs nothing: it reports what to run, and Create is the command it
	// prints rather than the command it runs.
	ByHand bool
}

// shellWords renders a command as somebody would type it, so a word holding a
// space arrives as one word rather than two.
func shellWords(args []string) string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = strconv.Quote(a)
		}
		out = append(out, a)
	}
	return strings.Join(out, " ")
}

// withSecrets writes what a pin keeps out of argv and answers with the command
// that reads it back, and the way to take the files away again.
func withSecrets(p Pin) ([]string, func(), error) {
	if len(p.Secrets) == 0 {
		return p.Create, func() {}, nil
	}
	dir, err := os.MkdirTemp("", "proton-cli-fixture-*")
	if err != nil {
		return nil, nil, err
	}
	done := func() { _ = os.RemoveAll(dir) }
	fields := make([]string, 0, len(p.Secrets))
	for field := range p.Secrets {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	create := slices.Clone(p.Create)
	for _, field := range fields {
		file := filepath.Join(dir, strings.ReplaceAll(field, "/", "-"))
		if err := os.WriteFile(file, []byte(p.Secrets[field]), 0o600); err != nil {
			done()
			return nil, nil, err
		}
		create = append(create, "--secret-file", field+"="+file)
	}
	return create, done, nil
}

// A Collection is one list the fixture pins rows in.
type Collection struct {
	What   string   // a noun for whoever reports on it
	List   []string // the command that lists it
	Key    string   // the field holding a row's identity
	IDKeys []string // the fields holding a row's ID, joined into the reference a removal takes
	Remove []string // the command that removes one, with the target appended
	// Parent is set on a collection the CLI addresses by path rather than by ID:
	// Drive names a thing by where it sits in the tree.
	Parent string
	Pins   []Pin
}

// Target renders the reference Remove takes for one row. An event needs two IDs
// to address it and is written the way the CLI writes it, which is why this is a
// join rather than a lookup.
func (c Collection) Target(row map[string]any, name string) string {
	if c.Parent != "" {
		return path.Join(c.Parent, name)
	}
	parts := make([]string, 0, len(c.IDKeys))
	for _, k := range c.IDKeys {
		parts = append(parts, Str(row[k]))
	}
	return strings.Join(parts, "/")
}

// Pin finds one of a collection's pins by the name it goes by.
func (c Collection) Pin(id string) (Pin, bool) {
	for _, p := range c.Pins {
		if p.ID == id {
			return p, true
		}
	}
	return Pin{}, false
}

// Ensure returns the row for a pin, making it if the account lacks it and
// replacing it if what is there disagrees with the fixture.
//
// A wrong row is worse than a missing one - it passes a presence check and then
// fails an assertion somewhere far away - so a row that disagrees is removed and
// made again rather than accepted.
//
// It judges the listing it is handed rather than making one, because a caller
// that swept the collection already has it. Only a create costs a second read.
func Ensure(run Runner, profile string, c Collection, p Pin, list []map[string]any) (map[string]any, error) {
	rows, err := theOnlyOne(run, profile, c, p, FindAll(list, c.Key, p.ID))
	if err != nil {
		return nil, err
	}
	row, found := map[string]any(nil), len(rows) == 1
	if found {
		row = rows[0]
	}
	if found && agrees(row, p.Fields) {
		return row, nil
	}
	if found {
		// A collection with no way to remove a row holds something too costly to
		// replace - the paid account's alias, whose address cannot be re-minted.
		// Deleting it to make a better one is not a trade to make quietly.
		if len(c.Remove) == 0 {
			return nil, fmt.Errorf("the %s account's %s called %q is not what the fixture declares,"+
				" and replacing it would mean deleting it: %v", profile, c.What, p.ID, row)
		}
		target := append(append([]string{"--yes"}, c.Remove...), c.Target(row, p.ID))
		if _, err := run(profile, target...); err != nil {
			return nil, fmt.Errorf("%s: %s: %w", c.What, p.ID, err)
		}
	}
	if p.ByHand {
		return nil, fmt.Errorf("the %s account has no %s called %q, and the suite never makes one."+
			" Create it once:\n\n    %s --profile %s %s",
			profile, c.What, p.ID, kit.Program, profile, shellWords(p.Create))
	}
	create, done, err := withSecrets(p)
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", c.What, p.ID, err)
	}
	_, err = run(profile, append([]string{"--yes"}, create...)...)
	done()
	if err != nil {
		return nil, fmt.Errorf("%s: %s: %w", c.What, p.ID, err)
	}
	made, err := Rows(run, profile, c.List...)
	if err != nil {
		return nil, err
	}
	if row, found := Find(made, c.Key, p.ID); found {
		return row, nil
	}
	return nil, fmt.Errorf("%s: %s was made and is not in the listing", c.What, p.ID)
}

// Sweep removes what an interrupted run left behind, from a listing already in
// hand.
//
// It costs nothing where a fixture was being looked up anyway, which is why it
// takes the listing rather than making one. Only the free accounts are ever
// swept: on the paid one there is nothing of the suite's to clear, and a filter
// over somebody's real data is the mistake the paid rules exist to prevent.
//
// A recurring event is listed once per occurrence and removed as a series, so a
// reference already swept is not swept again.
func Sweep(run Runner, profile string, c Collection, list []map[string]any) []string {
	var removed []string
	swept := map[string]bool{}
	for _, row := range list {
		name := Str(row[c.Key])
		if !strings.HasPrefix(name, TestPrefix) || len(c.Remove) == 0 {
			continue
		}
		target := c.Target(row, name)
		if swept[target] {
			continue
		}
		swept[target] = true
		if _, err := run(profile, append(append([]string{"--yes"}, c.Remove...), target)...); err != nil {
			continue
		}
		removed = append(removed, name)
	}
	return removed
}

// Rows lists a collection and hands back what it holds.
func Rows(run Runner, profile string, args ...string) ([]map[string]any, error) {
	out, err := run(profile, append([]string{"--output", "json"}, args...)...)
	if err != nil {
		return nil, err
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		return nil, fmt.Errorf("%s: not an envelope: %w", strings.Join(args, " "), err)
	}
	for _, raw := range envelope {
		var list []map[string]any
		if json.Unmarshal(raw, &list) == nil {
			return list, nil
		}
	}
	return nil, nil
}

// Find returns the row whose key holds want, and whether there was one.
func Find(list []map[string]any, key, want string) (map[string]any, bool) {
	if rows := FindAll(list, key, want); len(rows) > 0 {
		return rows[0], true
	}
	return nil, false
}

// FindAll returns every row whose key holds want.
//
// More than one is the state a fixture cannot survive, which is why finding them
// all is a thing the harness can do rather than something Find hides.
func FindAll(list []map[string]any, key, want string) []map[string]any {
	var out []map[string]any
	for _, r := range list {
		if Str(r[key]) == want {
			out = append(out, r)
		}
	}
	return out
}

// theOnlyOne reduces the rows answering to a pin's name to at most one.
//
// A fixture is addressed by its name, so two rows carrying it address neither:
// `pass items get "Travel card"` exits 4 and every test that reads the fixture
// fails somewhere far from the account state that caused it. Nothing else in the
// harness sees this - Find reconciles the first match and Sweep only removes
// what carries the test prefix - so a duplicate sits there until somebody reads
// a listing by eye, which is how one sat for a month.
//
// One is kept and the rest are removed, which is what reconciling a pin means. A
// collection with no way to remove one says so instead, for the same reason it
// refuses to replace a row that disagrees.
func theOnlyOne(run Runner, profile string, c Collection, p Pin, rows []map[string]any) ([]map[string]any, error) {
	if len(rows) < 2 {
		return rows, nil
	}
	if len(c.Remove) == 0 {
		return nil, fmt.Errorf("the %s account has %d %ss called %q, so that name addresses none of them,"+
			" and this collection has no way to remove one. Delete all but one by hand",
			profile, len(rows), c.What, p.ID)
	}
	keep := 0
	for i, r := range rows {
		if agrees(r, p.Fields) {
			keep = i
			break
		}
	}
	for i, r := range rows {
		if i == keep {
			continue
		}
		target := append(append([]string{"--yes"}, c.Remove...), c.Target(r, p.ID))
		if _, err := run(profile, target...); err != nil {
			return nil, fmt.Errorf("%s: %s: a second one could not be removed: %w", c.What, p.ID, err)
		}
	}
	return rows[keep : keep+1], nil
}

// Str renders a JSON value for comparison. Numbers arrive as float64, and a
// whole one should read as "1" rather than "1e+00".
func Str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case bool:
		return fmt.Sprintf("%t", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// agrees reports whether a row matches every field the pin names.
func agrees(row map[string]any, fields map[string]string) bool {
	for k, want := range fields {
		if Str(row[k]) != want {
			return false
		}
	}
	return true
}
