package kit

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Asking for part of a collection, and ordering one held whole.
//
// Every listing takes the same two flags, and they mean the same thing wherever
// they are typed: --page-size is how many rows an answer holds and --page is
// which of those answers to give, counting from zero. A size of zero is the
// whole collection in one answer. Whether that costs the server one request or
// twenty is the service's problem and never appears here - a page a user asks
// for is a page, not however much Proton happens to serve at once.
//
// Ordering is the part that cannot be asked of every collection. Proton orders
// mail itself, so `mail messages list` hands --sort to the server. Nothing else
// it stores can be asked for in pieces: contacts arrive as one encrypted export,
// Pass items as one batch per vault, a Drive folder as its whole listing. Those
// are decrypted locally, so Slice cuts the page out here - and only they can also
// sort, because sorting one pre-cut page and calling it the answer would be a
// lie.

// defaultSize is how wide a page is when the collection does not say, which is
// most of them: a listing of things a person reads down.
const defaultSize = 50

// Page is the position in a collection this invocation asked for.
//
// Default is how many rows a page holds when nothing was said, which differs per
// collection: a screenful of mail is not a screenful of contacts.
type Page struct {
	Number int
	Size   int

	Default int

	// capped records that the size was asked for as a cap, which is the only
	// thing that differs between the two spellings and all a refusal needs to
	// name the flag the user actually typed.
	capped bool
}

// Register adds --page and --page-size.
//
// The pair validates together and locally: a negative page or size, and a page
// of a listing that was asked for whole, are wrong whoever is signed in, so Run
// refuses them before the first request.
func (p *Page) Register(c *cobra.Command, noun string) {
	if p.Default == 0 {
		p.Default = defaultSize
	}
	c.Flags().IntVar(&p.Number, "page", 0, "Which page of results, counting from zero")
	c.Flags().IntVar(&p.Size, "page-size", p.Default, "How many "+noun+" per page; 0 for all of them")
	registerCheck(c, "page", nil, p)
}

// RegisterCap adds --limit, the most rows a bulk verb will act on.
//
// A cap is a page: "at most 150 of them" and "the first 150 of them" are the
// same ask, so they are the same field, and a command offers one spelling or the
// other rather than both. Zero lifts the cap for the same reason it lists
// everything.
func (p *Page) RegisterCap(c *cobra.Command, noun string) {
	if p.Default == 0 {
		p.Default = defaultSize
	}
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
		return Fail("--page-size is a count; 0 lists all of them.")
	case p.Number > 0 && p.Size == 0:
		return Fail("--page %d asks for a page of a listing --page-size 0 does not cut into.", p.Number)
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
