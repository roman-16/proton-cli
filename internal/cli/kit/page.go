package kit

import (
	"sort"
	"strings"

	"github.com/roman-16/proton-cli/internal/ui"
	"github.com/spf13/cobra"
)

// Asking for part of a collection, and ordering one held whole.
//
// Every listing takes the same two flags, and they mean the same thing wherever
// they are typed: --limit is how many rows an answer holds and --page is which
// of those answers to give, counting from zero. A limit of zero is the whole
// collection in one answer. Whether that costs the server one request or twenty
// is the service's problem and never appears here - a page a user asks for is a
// page, not however much Proton happens to serve at once.
//
// A bulk verb asks the same question with the same word. "At most 150 of them"
// and "the first 150 of them" are one ask, so a cap and a page size are one
// field under one name, and only the sentence in the help differs.
//
// Ordering is the part that is not answered the same way twice. Proton orders
// mail itself, so the mailbox hands --sort to the server and reads back whatever
// comes. Nothing else it stores can be asked for in pieces: contacts arrive as
// one encrypted export, Pass items as one batch per vault, a Drive folder as its
// whole listing. Those are decrypted locally, so Slice cuts the page out here -
// and only they can also sort, because sorting one pre-cut page and calling it
// the answer would be a lie.

// Page is the position in a collection this invocation asked for.
//
// Default is how many rows a page holds when nothing was said, and every listing
// states its own: a screenful of mail is not a screenful of contacts. Zero is
// the whole collection, which is the right answer wherever the rows were fetched
// whole anyway - cutting those uninvited throws away work already paid for and
// under-reports what is there.
type Page struct {
	Number int
	Size   int

	Default int

	// capped records that the limit was asked for as a cap, which is all a
	// refusal needs to word itself the way the command was being used.
	capped bool
}

// Register adds --page and --limit.
//
// The pair validates together and locally: a negative page or limit, and a page
// of a listing that was asked for whole, are wrong whoever is signed in, so Run
// refuses them before the first request.
func (p *Page) Register(c *cobra.Command, noun string) {
	c.Flags().IntVar(&p.Number, "page", 0, "Which page of results, counting from zero")
	c.Flags().IntVar(&p.Size, "limit", p.Default, "How many "+noun+" per page; 0 for all of them")
	registerCheck(c, "page", nil, p)
}

// RegisterCap adds --limit alone, the most rows a bulk verb will act on. Zero
// lifts the cap for the same reason it lists everything.
func (p *Page) RegisterCap(c *cobra.Command, noun string) {
	p.capped = true
	c.Flags().IntVar(&p.Size, "limit", p.Default, "Most "+noun+" to affect; 0 for no cap")
	registerCheck(c, "limit", nil, p)
}

func (p *Page) validate() error {
	switch {
	case p.Number < 0:
		return Fail("--page counts from zero.")
	case p.Size < 0 && p.capped:
		return Fail("--limit is a count; 0 lifts the cap.")
	case p.Size < 0:
		return Fail("--limit is a count; 0 lists all of them.")
	case p.Number > 0 && p.Size == 0:
		return Fail("--page %d asks for a page of a listing --limit 0 does not cut into.", p.Number)
	}
	return nil
}

// Slice cuts rows down to the page that was asked for and reports how many there
// were in total, which is what lets the footer say "50 of 3000" rather than
// leaving a reader to wonder whether that was all of them.
//
// A page past the end is empty rather than an error: it is the honest answer, and
// it is what a script walking pages until they run out needs.
func Slice[T any](p Page, rows []T) ([]T, int) {
	total := len(rows)
	if p.Size <= 0 {
		return rows, total
	}
	start := p.Number * p.Size
	if start >= total {
		return nil, total
	}
	end := min(start+p.Size, total)
	return rows[start:end], total
}

// Order is the ordering this invocation asked for.
type Order struct {
	Desc bool
	key  *Enum
}

// Register adds --sort and --desc, offering only the keys this collection has.
//
// The key is an Enum, so a key that cannot be sorted by is refused from the
// command line before anything is fetched, its domain is printed when it is, and
// shell completion offers it - the three things a fixed set of values owes,
// discharged by the one declaration. The first key is the default.
func (o *Order) Register(c *cobra.Command, keys ...string) {
	o.key = &Enum{Name: "sort", Usage: "Order by", Values: keys, Default: keys[0]}
	o.key.Register(c)
	c.Flags().BoolVar(&o.Desc, "desc", false, "Reverse the order")
}

// Key is the ordering this invocation asked for, and "" for a command that
// offers none. It is what a collection ordered somewhere else - by Proton, on
// the far side of a request - reads instead of Sort.
func (o Order) Key() (string, error) {
	if o.key == nil {
		return "", nil
	}
	return o.key.Value()
}

// Comparators is how one collection may be ordered: a comparison per key it
// declared. Sorting reads from here, so a key offered by --sort and a key that
// can actually be applied are the same set.
type Comparators[T any] map[string]func(a, b T) int

// Sort orders rows in place.
//
// The key was already checked against the declared domain before the command
// body ran, so a comparator missing here is this CLI disagreeing with itself
// about what it offers - a bug rather than bad input, and it says so.
func Sort[T any](o Order, rows []T, by Comparators[T]) error {
	key, err := o.key.Value()
	if err != nil {
		return err
	}
	cmp, ok := by[key]
	if !ok {
		return Fail("--sort offers %q but this collection cannot order by it", key)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if o.Desc {
			return cmp(rows[i], rows[j]) > 0
		}
		return cmp(rows[i], rows[j]) < 0
	})
	return nil
}

// Key is one way a collection may be ordered: the word --sort takes, and the
// comparison it stands for.
type Key[T any] struct {
	Name string
	Less func(a, b T) int
}

// Held is a collection Proton hands over whole, which is most of them.
//
// It is ordered here and cut into pages here, so one declaration gives a listing
// --limit, --page, --sort and --desc, and one call answers with all four
// applied. Declaring the keys alongside their comparisons is what keeps the set
// --sort offers and the set that can actually be applied from drifting apart.
//
// It answers with the whole collection unless a page was asked for. The rows are
// already here by the time anything could be cut, so a default that cut them
// would discard what the request already fetched and report fewer aliases than
// the account has - which is a wrong answer, not a shorter one.
type Held[T any] struct {
	page  Page
	order Order
	by    Comparators[T]
}

// Register adds --limit and --page, and --sort and --desc for a collection that
// may be ordered. The first key is the order a listing comes back in when
// nothing asked for another.
//
// Naming no key at all is the collection whose order is itself the answer: the
// addresses of an account, where the first is the default, and the filters of a
// mailbox, where the first to match wins. Those page but never sort, because
// reordering the rows would be reporting something untrue about them.
func (h *Held[T]) Register(c *cobra.Command, noun string, keys ...Key[T]) {
	h.page.Register(c, noun)
	if len(keys) == 0 {
		return
	}
	h.by = make(Comparators[T], len(keys))
	names := make([]string, 0, len(keys))
	for _, k := range keys {
		h.by[k.Name] = k.Less
		names = append(names, k.Name)
	}
	h.order.Register(c, names...)
}

// Answer orders the rows, cuts out the page asked for, and reports how many
// there were in total.
func (h *Held[T]) Answer(c *Invocation, spec ui.TableSpec[T], rows []T) error {
	if h.by != nil {
		if err := Sort(h.order, rows, h.by); err != nil {
			return err
		}
	}
	page, total := Slice(h.page, rows)
	spec.Total, spec.Page, spec.PageSize = total, h.page.Number, h.page.Size
	return List(c, spec, page)
}

// Fold compares two strings the way a person reading a list would: without
// caring about case, and falling back to the exact bytes so the order never
// depends on which of two equal-looking names arrived first.
func Fold(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

// Ints compares two numbers, for a size or a timestamp.
func Ints(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
