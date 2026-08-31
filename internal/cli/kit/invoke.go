package kit

import (
	"context"
	"errors"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/idcache"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Invocation is the prepared context handed to a command body. The steps a
// command declares populate it - expand short IDs, check a selection - so no
// handler repeats that preamble.
type Invocation struct {
	Ctx  context.Context
	App  *app.App
	Args []string
	// Cmd is the running command, so a handler can tell a flag left alone from
	// one explicitly set to its zero value.
	Cmd *cobra.Command

	// computed records that a filter, rather than the user, chose something to
	// act on. Select sets it and Mutate reads it, so no command has to pass the
	// fact along - the same reason --dry-run needs nothing from a handler.
	computed bool
}

// UI is the renderer for this invocation. It is the only way a command produces
// output.
func (c *Invocation) UI() *ui.UI { return c.App.UI }

// Changed reports whether the user passed the named flag, however they set it.
func (c *Invocation) Changed(flag string) bool {
	return c.Cmd != nil && c.Cmd.Flags().Changed(flag)
}

// preview makes sure a dry run is as true as the change would be.
//
// A preview claims what the command would do, and for a mutation that reaches
// Proton the honest claim includes needing an account: it sends no request, so
// nothing else would ever discover there is nobody signed in. A command that
// declares OnThisMachine changes the disk and not the account, and would have
// succeeded signed out, so its preview says so too.
func (c *Invocation) preview(spec *ui.ResultSpec) error {
	spec.DryRun = true
	if c.Cmd != nil && c.Cmd.Annotations[OnThisMachine] != "" {
		return nil
	}
	return c.App.Authenticate(c.Ctx)
}

// Step is a requirement satisfied before the body runs. The first failure aborts.
type Step func(*Invocation) error

// Handler is a command body.
type Handler func(*Invocation) error

// StepExpand turns short IDs in the positional arguments into full ones. It
// leaves anything that is not short-ID-shaped alone, so applying it everywhere is
// safe.
func StepExpand(c *Invocation) error {
	out := make([]string, len(c.Args))
	for i, a := range c.Args {
		full, err := Expand(c.App, a)
		if err != nil {
			return err
		}
		out[i] = full
	}
	c.Args = out
	return nil
}

// Run wires a command body to cobra, running the declared steps first.
//
// Before any step, every enum flag the command declared is checked. Local
// validation preceding the network is a rule rather than a habit: a value that
// could never have been sent should not first cost a sign-in to discover.
//
// Which is why no step asserts that an account exists, and why none of them
// unlocks the account's keys: both requirements belong to the request, and the
// client holds them there, so a command body judges what it can judge first and
// only then finds out whether anyone is signed in.
func Run(steps []Step, h Handler) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		if err := validateFlags(cmd); err != nil {
			return err
		}
		c := &Invocation{Ctx: cmd.Context(), App: app.From(cmd.Context()), Args: args, Cmd: cmd}
		for _, s := range steps {
			if err := s(c); err != nil {
				return err
			}
		}
		return h(c)
	}
}

// ── responses ──

// List renders a collection and remembers the IDs it showed, so a short ID read
// off this output resolves on the next command.
//
// Caching here rather than in the ui package is deliberate: remembering an ID is
// something this CLI does about Proton, not something a table does.
func List[T any](c *Invocation, spec ui.TableSpec[T], items []T, ids func(T) []string) error {
	if ids != nil && c.App.IDCache != nil && len(items) > 0 {
		seen := make([]string, 0, len(items))
		for _, it := range items {
			seen = append(seen, ids(it)...)
		}
		_ = c.App.IDCache.Save(seen...)
	}
	return ui.Table(c.UI(), spec, items)
}

// Watch renders a response with no end: one thing per line, as it happens.
//
// It takes the loop rather than being driven by one, because a stream is open
// for as long as the work lasts and there is no other moment at which the
// response is finished. run is handed the way to report a thing, and returns
// when the reader stops watching.
//
// IDs are remembered as they go past, exactly as a listing remembers them, so a
// short ID read off a stream resolves in the next command.
func Watch[T any](c *Invocation, spec ui.StreamSpec[T], ids func(T) []string, run func(emit func(T) error) error) error {
	stream := ui.Open(c.UI(), spec)
	return run(func(item T) error {
		if ids != nil && c.App.IDCache != nil {
			_ = c.App.IDCache.Save(ids(item)...)
		}
		return stream.Emit(item)
	})
}

// Show renders one object.
func Show(c *Invocation, spec ui.RecordSpec) error { return ui.Record(c.UI(), spec) }

// Read renders decrypted content meant to be read.
func Read(c *Invocation, spec ui.DocumentSpec) error { return ui.Document(c.UI(), spec) }

// Mutate performs a change and reports it - or, under --dry-run, reports what it
// would have done and changes nothing.
//
// Routing every mutation through here is what makes both --dry-run and the
// confirmations properties of the CLI rather than flags each command has to
// remember to check. What is worth asking about is read off the action the
// command reports, so a command cannot describe a deletion and then perform it
// unannounced.
func Mutate(c *Invocation, spec ui.ResultSpec, apply func() error) error {
	if c.App.DryRun {
		if err := c.preview(&spec); err != nil {
			return err
		}
		return ui.Result(c.UI(), spec)
	}
	if err := confirm(c, spec); err != nil {
		return err
	}
	// A change that affects nothing is not made. A selection that matched nothing
	// would otherwise reach Proton as a request to act on no IDs, which it answers
	// by complaining about the request rather than saying the one true thing:
	// there was nothing to do.
	if spec.Count == 0 {
		return ui.Result(c.UI(), spec)
	}
	if err := apply(); err != nil {
		return err
	}
	return ui.Result(c.UI(), spec)
}

// confirm stops for a yes before a change that cannot be taken back, crosses an
// external/network/security boundary, or acts on targets a filter selected.
//
// --yes is the answer given in advance, which is also the only way through in a
// script: a prompt nobody can see is a hang, so an unattended run is told what
// to add rather than left waiting. A change that affects nothing is not worth a
// question, so an empty selection passes.
func confirm(c *Invocation, spec ui.ResultSpec) error {
	if c.App.Yes || spec.Count == 0 || !spec.Action.Asks(c.computed) {
		return nil
	}
	if !c.UI().CanPrompt() {
		return Fail("%s", spec.Refusal()).
			Hint("--yes to confirm, or --dry-run to see what it would touch.")
	}
	ok, err := ui.Confirm(c.UI(), spec)
	if err != nil {
		return err
	}
	if !ok {
		return Fail("Cancelled.")
	}
	return nil
}

// Create makes one thing and reports its new ID.
//
// It is separate from Mutate because a creation's identity does not exist until
// the work is done, and because that ID goes to stdout: `ID=$(proton ...
// create ...)` is the whole reason the split between the streams matters.
func Create(c *Invocation, spec ui.ResultSpec, apply func() (string, error)) error {
	spec.Count = 1
	if c.App.DryRun {
		if err := c.preview(&spec); err != nil {
			return err
		}
		return ui.Result(c.UI(), spec)
	}
	if err := confirm(c, spec); err != nil {
		return err
	}
	id, err := apply()
	if err != nil {
		return err
	}
	spec.IDs = []string{id}
	spec.EmitID = true
	return ui.Result(c.UI(), spec)
}

// ── references ──

// Expand turns an eight-character short ID into the full one using the local
// cache.
//
// Anything that is not short-ID-shaped, and anything the cache has not seen,
// passes through untouched: it may well be a name or a subject that the service
// layer will resolve. An ambiguous prefix is an error, never a guess.
func Expand(a *app.App, reference string) (string, error) {
	if reference == "" || ref.Full(reference) || !ref.Short(reference) {
		return reference, nil
	}
	if a == nil || a.IDCache == nil {
		return reference, nil
	}
	full, err := a.IDCache.Resolve(reference)
	if err == nil {
		return full, nil
	}
	var amb *idcache.AmbiguousError
	if errors.As(err, &amb) {
		lines := []string{"use one of:"}
		for _, cand := range amb.Candidates {
			lines = append(lines, "  "+cand)
		}
		return "", errs.Problemf("%q matches %d cached IDs.", reference, len(amb.Candidates)).
			Hint(lines...).Exit(4)
	}
	if errors.Is(err, idcache.ErrNotFound) {
		return reference, nil
	}
	return "", err
}

// Pair splits a compound reference into its two halves.
//
// Proton addresses some things with two IDs - a Pass item lives in a share, an
// event lives in a calendar - and this CLI writes the two as one token so that
// every command still takes a single REF. The notation itself is internal/ref's,
// which is what keeps the reference this splits identical to the one the
// listings print.
//
// A reference with no separator is returned as the second half with an empty
// first, since that is the shape a human handle takes.
func Pair(reference string) (first, second string) {
	parts, occurrence := ref.Split(reference)
	if len(parts) < 2 {
		return "", reference
	}
	second = ref.Join(parts[1:]...)
	if occurrence != "" {
		second += ref.Occurrence + occurrence
	}
	return parts[0], second
}

// ExpandPair splits a compound reference and expands each half, so a short ID
// works on either side of the slash.
//
// StepExpand cannot do this. A slash is not part of an ID, so a compound
// reference never looks short to it - and a Drive path is full of slashes too,
// so a step that applies to every argument would take paths apart as well. Only
// a command that knows it is holding two IDs can safely separate them.
func ExpandPair(a *app.App, reference string) (first, second string, err error) {
	first, second = Pair(reference)
	if first == "" {
		return "", second, nil
	}
	if first, err = Expand(a, first); err != nil {
		return "", "", err
	}
	if second, err = Expand(a, second); err != nil {
		return "", "", err
	}
	return first, second, nil
}

// JoinPair renders a compound reference as the single token a user pastes back.
func JoinPair(first, second string) string {
	if first == "" {
		return second
	}
	return ref.Join(first, second)
}

// Dedupe removes repeated strings, preserving order. Selections union explicit
// references with filter matches, so an overlap is expected rather than an error.
func Dedupe(ss []string) []string {
	seen := make(map[string]struct{}, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// Note reports something incidental on stderr.
func (c *Invocation) Note(format string, a ...any) { c.UI().Notef(format, a...) }

// Warn reports a caveat on stderr: something true and worth noticing that did
// not stop the command. It is not an error and does not change the exit code.
func (c *Invocation) Warn(format string, a ...any) { c.UI().Warnf(format, a...) }

// Fail starts a user error. It is the same as errs.Problemf, re-exported so a
// command package needs one import for the whole vocabulary.
func Fail(format string, a ...any) *errs.Problem { return errs.Problemf(format, a...) }

// Object writes v in the machine format. It is for the handful of results that
// have no table, record or mutation shape of their own - the self-management
// commands, which report on the binary rather than on an account.
func Object(c *Invocation, v any) error { return ui.Record(c.UI(), ui.RecordSpec{Object: v}) }

// ── ingesting ──

// Ingest reads things out of a file and reports what landed.
//
// It is separate from Mutate because how many of them land is not known until
// the work is done: a file offers cards or events, Proton takes the ones it can
// identify, and the rest are named with the reason. Counting the offer would
// report a number that is right only when nothing went wrong, which is the one
// case nobody needs told about.
//
// Under --dry-run there is nothing to report but the offer, since no server has
// yet had the chance to refuse anything.
func Ingest[T any](c *Invocation, spec ui.ResultSpec, read func() ([]T, error)) error {
	if c.App.DryRun {
		if err := c.preview(&spec); err != nil {
			return err
		}
		return ui.Result(c.UI(), spec)
	}
	if err := confirm(c, spec); err != nil {
		return err
	}
	if spec.Count == 0 {
		return ui.Result(c.UI(), spec)
	}
	skipped, err := read()
	if err != nil {
		return err
	}
	spec.Count -= len(skipped)
	if err := ui.Result(c.UI(), spec); err != nil {
		return err
	}
	// A partial import is the common failure, so what did not land is named
	// rather than left to a count that does not add up.
	for _, s := range skipped {
		c.Warn("%v", s)
	}
	return nil
}
