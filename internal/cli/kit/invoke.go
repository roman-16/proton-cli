package kit

import (
	"context"
	"errors"
	"strings"

	"github.com/roman-16/proton-cli/internal/app"
	"github.com/roman-16/proton-cli/internal/confirm"
	"github.com/roman-16/proton-cli/internal/errs"
	"github.com/roman-16/proton-cli/internal/idcache"
	"github.com/roman-16/proton-cli/internal/ref"
	"github.com/roman-16/proton-cli/internal/skip"
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

	// tally counts what the services could not decrypt. List reads it, so an
	// incomplete listing says so without any command having to check.
	tally *skip.Tally
}

// Incomplete is what this invocation could not show, phrased for whatever is
// about to render the answer.
//
// The remedy is added here rather than in the ui package, which never names the
// program it belongs to.
func (c *Invocation) Incomplete() ui.IncompleteSpec {
	if c.tally.Count() == 0 {
		return ui.IncompleteSpec{}
	}
	return ui.IncompleteSpec{
		Count:  c.tally.Count(),
		Kind:   string(c.tally.Kind()),
		Hides:  c.tally.Hides(),
		Remedy: "This is a bug or damaged data - `" + Program + " report` has the details.",
	}
}

// incomplete attaches the tally to a reference that matched nothing.
//
// "No item matching that" is the wrong answer when the item was there and could
// not be read, and the two are indistinguishable to the person who typed it.
// Whatever could not be decrypted was also not searched, so the search says so.
func (c *Invocation) incomplete(err error) error {
	var missing *errs.NotFound
	if !errors.As(err, &missing) || c.tally.Count() == 0 {
		return err
	}
	spec := c.Incomplete()
	missing.Try = append(missing.Try, spec.Sentence(), Program+" report")
	return err
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
		// Everything from here on is the command's own work, so a failure with no
		// words on it is this CLI's rather than the caller's - which is the one
		// distinction the exit code cannot make for itself, since cobra's
		// complaints about a command line are just as bare and are nobody's bug.
		ctx, tally := skip.With(cmd.Context())
		c := &Invocation{Ctx: ctx, App: app.From(ctx), Args: args, Cmd: cmd, tally: tally}
		if err := gate(c); err != nil {
			return errs.Bug(err)
		}
		for _, s := range steps {
			if err := s(c); err != nil {
				return errs.Bug(c.incomplete(err))
			}
		}
		return errs.Bug(c.incomplete(h(c)))
	}
}

// ── responses ──

// List renders a collection and remembers the references it showed, so a short
// ID read off this output resolves on the next command and a shell can offer it
// back.
//
// What was shown is read from the columns rather than declared a second time
// beside them: the table already says which cell is the row's reference, which
// points into another collection, and which is the name a person would use. Two
// declarations of the same thing would only give the CLI a way to disagree with
// its own screen.
func List[T any](c *Invocation, spec ui.TableSpec[T], items []T) error {
	spec.Skipped = c.Incomplete()
	var seen []idcache.Entry
	for _, it := range items {
		row := showing{in: Holding(c.Cmd)}
		for _, col := range spec.Columns {
			if col.Cell == nil {
				continue
			}
			row.cell(col.ID, col.Ref, col.Handle, col.Cell(it))
		}
		seen = append(seen, row.entries()...)
	}
	c.remember(seen)
	return ui.Table(c.UI(), spec, items)
}

// Watch renders a response with no end: one thing per line, as it happens.
//
// It takes the loop rather than being driven by one, because a stream is open
// for as long as the work lasts and there is no other moment at which the
// response is finished. run is handed the way to report a thing, and returns
// when the reader stops watching.
//
// References are remembered as they go past, exactly as a listing remembers
// them, so a short ID read off a stream resolves in the next command.
func Watch[T any](c *Invocation, spec ui.StreamSpec[T], run func(emit func(T) error) error) error {
	mine := Holding(c.Cmd)
	stream := ui.Open(c.UI(), spec)
	return run(func(item T) error {
		row := showing{in: mine}
		for _, col := range spec.Columns {
			if col.Cell == nil {
				continue
			}
			row.cell(col.ID, col.Ref, col.Handle, col.Cell(item))
		}
		c.remember(row.entries())
		return stream.Emit(item)
	})
}

// Show renders one object, remembering the references it showed.
//
// A record is how a thing reached by its handle first tells you its ID, and how
// one thing points at another - the item a link opens, the calendar an account
// writes to by default. Both are references that were on the screen, so both are
// references the next command line can start typing.
func Show(c *Invocation, spec ui.RecordSpec) error {
	c.remember(shown(c, spec.Fields))
	return ui.Record(c.UI(), spec)
}

// Read renders decrypted content meant to be read, remembering the references in
// its header for the same reason Show does.
func Read(c *Invocation, spec ui.DocumentSpec) error {
	fields := spec.Header
	for _, p := range spec.Parts {
		fields = append(fields, p.Header...)
	}
	c.remember(shown(c, fields))
	return ui.Document(c.UI(), spec)
}

// shown is the references in a block of fields, filed where each belongs.
func shown(c *Invocation, fields []ui.Field) []idcache.Entry {
	row := showing{in: Holding(c.Cmd)}
	for _, f := range fields {
		row.cell(f.ID, f.Ref, f.Handle, f.Value)
	}
	return row.entries()
}

// showing gathers what one thing's row or record put on the screen.
//
// The handles go to the reference the response is about, which is the first one
// drawn: every table opens with the row's own ID and every record with the
// thing's own, so anything further along points at something else, whose name
// this response never showed.
type showing struct {
	in      string
	refs    []idcache.Entry
	handles []string
}

func (s *showing) cell(id bool, elsewhere string, handle bool, value string) {
	switch {
	case id || elsewhere != "":
		in := s.in
		if elsewhere != "" {
			in = elsewhere
		}
		s.refs = append(s.refs, idcache.Entry{Collection: in, Ref: value})
	case handle && value != "" && !strings.HasPrefix(value, "("):
		// A parenthesised stand-in is what a listing prints where a name could
		// not be decrypted, and offering it back would complete to nothing.
		s.handles = append(s.handles, value)
	}
}

func (s *showing) entries() []idcache.Entry {
	if len(s.refs) > 0 {
		s.refs[0].Handles = s.handles
	}
	return s.refs
}

// remember files what a response showed, so far as there is anywhere to file it.
// A cache that cannot be written is not worth failing a command over: the
// listing on the screen is the answer, and the memory of it is a convenience.
func (c *Invocation) remember(entries []idcache.Entry) {
	if len(entries) == 0 || c.App == nil || c.App.IDCache == nil {
		return
	}
	_ = c.App.IDCache.Save(entries...)
}

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
	if err := consent(c, spec); err != nil {
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

// confirm stops for a yes before a change that cannot be taken back, that would
// remove things the user never named, or that the configured policy asks about.
//
// The first two are the built-in floor and hold with no configuration at all.
// The third sits on top of them and can only ever add: no policy makes a
// deletion stop being worth a question.
//
// --yes is the answer given in advance, which is also the only way through in a
// script: a prompt nobody can see is a hang, so an unattended run is told what
// to add rather than left waiting. A change that affects nothing is not worth a
// question, so an empty selection passes.
func consent(c *Invocation, spec ui.ResultSpec) error {
	if c.App.Yes || spec.Count == 0 {
		return nil
	}
	asked := c.App.Confirm.Require(classify(c.Cmd))
	byPolicy := asked.Outcome == confirm.Ask
	if !spec.Action.Asks(c.computed) && !byPolicy {
		return nil
	}
	if !c.UI().CanPrompt() {
		refusal := spec.Refusal()
		if byPolicy && !spec.Action.Asks(c.computed) {
			refusal += " " + policyAsks(asked.Class)
		}
		return Fail("%s", refusal).
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
	id, err := apply()
	if err != nil {
		return err
	}
	spec.IDs = []string{id}
	spec.EmitID = true
	return ui.Result(c.UI(), spec)
}

// ── references ──

// Expand turns a short ID into the full one using the local cache.
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
		candidates := make([]errs.Candidate, 0, len(amb.Candidates))
		for _, id := range amb.Candidates {
			candidates = append(candidates, errs.Candidate{ID: id})
		}
		return "", &errs.Ambiguous{Kind: "cached ID", Ref: reference, Candidates: candidates}
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

// ── attempting ──

// Attempt makes a change to many things and reports what it managed.
//
// It is separate from Mutate because how many of them land is not known until
// the work is done: a file offers cards or events, a bulk verb offers a list of
// items, Proton takes the ones it can and the rest are named with the reason.
// Counting the offer would report a number that is right only when nothing went
// wrong, which is the one case nobody needs told about.
//
// Under --dry-run there is nothing to report but the offer, since no server has
// yet had the chance to refuse anything.
func Attempt[T any](c *Invocation, spec ui.ResultSpec, apply func() ([]T, error)) error {
	if c.App.DryRun {
		if err := c.preview(&spec); err != nil {
			return err
		}
		return ui.Result(c.UI(), spec)
	}
	if err := consent(c, spec); err != nil {
		return err
	}
	if spec.Count == 0 {
		return ui.Result(c.UI(), spec)
	}
	skipped, err := apply()
	if err != nil {
		return err
	}
	spec.Count -= len(skipped)
	if err := ui.Result(c.UI(), spec); err != nil {
		return err
	}
	// Landing part of what was asked for is the common failure, so what did not
	// land is named rather than left to a count that does not add up.
	for _, s := range skipped {
		c.Warn("%v", s)
	}
	return nil
}
