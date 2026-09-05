package kit

import "testing"

func TestSliceCutsThePageAndReportsTheWhole(t *testing.T) {
	rows := []int{0, 1, 2, 3, 4, 5, 6}
	for _, c := range []struct {
		name  string
		page  Page
		want  []int
		total int
	}{
		{"first page", Page{Number: 0, Size: 3}, []int{0, 1, 2}, 7},
		{"middle page", Page{Number: 1, Size: 3}, []int{3, 4, 5}, 7},
		// The last page is short rather than padded, and the total still says
		// how many there were.
		{"short last page", Page{Number: 2, Size: 3}, []int{6}, 7},
		// Walking off the end is the honest answer for a script that pages until
		// it runs out, not an error.
		{"past the end", Page{Number: 9, Size: 3}, nil, 7},
		// No page size means no paging, which is what a collection small enough
		// to print whole wants.
		{"unpaged", Page{}, rows, 7},
	} {
		got, total := Slice(c.page, rows)
		if total != c.total {
			t.Errorf("%s: total = %d, want %d", c.name, total, c.total)
		}
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v, want %v", c.name, got, c.want)
				break
			}
		}
	}
}

func TestPageRefusesWhatNoSessionCouldMakeRight(t *testing.T) {
	for _, c := range []struct {
		name string
		page Page
		want string
	}{
		{"a page before the first", Page{Number: -1, Size: 50}, "--page counts from zero."},
		{"a negative size", Page{Size: -5}, "--page-size is a count; 0 lists all of them."},
		// Zero is the whole collection, so there is no second page of it to
		// ask for and answering with an empty one would look like the end.
		{"a page of an unpaged listing", Page{Number: 2}, "--page 2 asks for a page of a listing --page-size 0 does not cut into."},
		{"the first page whole", Page{}, ""},
		{"an ordinary page", Page{Number: 3, Size: 25}, ""},
	} {
		err := c.page.validate()
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%s: %v, want no complaint", c.name, err)
		case c.want != "" && err == nil:
			t.Errorf("%s: allowed, want %q", c.name, c.want)
		case c.want != "" && err != nil && err.Error() != c.want:
			t.Errorf("%s: %q, want %q", c.name, err, c.want)
		}
	}
}

func TestSliceLeavesAnEmptyCollectionAlone(t *testing.T) {
	got, total := Slice(Page{Number: 0, Size: 10}, []int(nil))
	if len(got) != 0 || total != 0 {
		t.Errorf("got %v (total %d), want nothing", got, total)
	}
}

// Ordering is stable, so two rows a key cannot tell apart keep the order they
// arrived in rather than swapping between runs.
func TestSortIsStable(t *testing.T) {
	type row struct{ key, id string }
	rows := []row{{"b", "1"}, {"a", "2"}, {"a", "3"}, {"b", "4"}}
	by := Comparators[row]{"key": func(a, b row) int { return Fold(a.key, b.key) }}

	o := Order{key: &Enum{Values: []string{"key"}, Default: "key", target: "key"}}
	if err := Sort(o, rows, by); err != nil {
		t.Fatalf("sort: %v", err)
	}
	for i, want := range []string{"2", "3", "1", "4"} {
		if rows[i].id != want {
			t.Fatalf("order = %v, want ids 2 3 1 4", rows)
		}
	}

	o.Desc = true
	if err := Sort(o, rows, by); err != nil {
		t.Fatalf("sort desc: %v", err)
	}
	for i, want := range []string{"1", "4", "2", "3"} {
		if rows[i].id != want {
			t.Fatalf("reversed order = %v, want ids 1 4 2 3", rows)
		}
	}
}

func TestFoldIgnoresCaseButNeverTies(t *testing.T) {
	if Fold("apple", "Banana") >= 0 {
		t.Error("apple should sort before Banana whatever the case")
	}
	if Fold("a", "A") == 0 {
		t.Error("two spellings that differ must not compare equal, or the order is arbitrary")
	}
}
