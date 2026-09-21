package proton

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

// width stands for the widest page an endpoint serves, which is what every
// listing Proton caps is capped at.
const width = 150

func TestAllReadsEveryPageInOrder(t *testing.T) {
	pages := [][]int{{0, 1, 2}, {3, 4, 5}, {6}}
	var asked []int

	got, err := All(context.Background(), func(_ context.Context, page int) ([]int, bool, error) {
		asked = append(asked, page)
		rows := pages[page]
		return rows, Full(rows, 3), nil
	})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if want := []int{0, 1, 2, 3, 4, 5, 6}; !equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	if want := []int{0, 1, 2}; !equal(asked, want) {
		t.Errorf("asked for pages %v, want %v", asked, want)
	}
}

// A short page is the end, so a collection that fits in one is one request.
func TestAllStopsOnAShortPage(t *testing.T) {
	calls := 0
	got, err := All(context.Background(), func(_ context.Context, _ int) ([]int, bool, error) {
		calls++
		rows := []int{1, 2}
		return rows, Full(rows, 50), nil
	})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if calls != 1 {
		t.Errorf("asked %d times, want 1", calls)
	}
	if want := []int{1, 2}; !equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// A page that fills exactly asks once more, because a full page is the only
// thing an endpoint without a "more" flag says, and it may have been the last.
func TestAllAsksAgainAfterAFullPage(t *testing.T) {
	pages := [][]int{{0, 1}, {}}
	calls := 0
	got, err := All(context.Background(), func(_ context.Context, page int) ([]int, bool, error) {
		calls++
		rows := pages[page]
		return rows, Full(rows, 2), nil
	})
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	if calls != 2 {
		t.Errorf("asked %d times, want 2", calls)
	}
	if want := []int{0, 1}; !equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

// Half a collection is not an answer, so a failed page fails the walk rather
// than returning what was read before it.
func TestAllFailsWholeOnAFailedPage(t *testing.T) {
	boom := errors.New("boom")
	got, err := All(context.Background(), func(_ context.Context, page int) ([]int, bool, error) {
		if page == 1 {
			return nil, false, boom
		}
		return []int{page}, true, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got != nil {
		t.Errorf("rows = %v, want none", got)
	}
}

func TestPagesStopsWhenTheCallerIsDone(t *testing.T) {
	var asked []int
	err := Pages(context.Background(), func(_ context.Context, page int) (bool, error) {
		asked = append(asked, page)
		return page < 2, nil
	})
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if want := []int{0, 1, 2}; !equal(asked, want) {
		t.Errorf("asked for pages %v, want %v", asked, want)
	}
}

// A cancelled walk stops rather than working through the pages it was told to
// abandon.
func TestPagesHonoursCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := Pages(ctx, func(_ context.Context, _ int) (bool, error) {
		calls++
		cancel()
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want canceled", err)
	}
	if calls != 1 {
		t.Errorf("asked %d times, want 1", calls)
	}
}

// An endpoint that never stops claiming more still ends the request.
func TestPagesGivesUpOnAnEndlessCollection(t *testing.T) {
	calls := 0
	err := Pages(context.Background(), func(_ context.Context, _ int) (bool, error) {
		calls++
		return true, nil
	})
	if err != nil {
		t.Fatalf("pages: %v", err)
	}
	if calls != pageLimit {
		t.Errorf("asked %d times, want %d", calls, pageLimit)
	}
}

func TestFullReadsAFilledPageAsMore(t *testing.T) {
	if Full([]int{1, 2}, 2) != true {
		t.Error("a page filled to the size may have more behind it")
	}
	if Full([]int{1}, 2) != false {
		t.Error("a short page is the end")
	}
	if Full([]int{1}, 0) != false {
		t.Error("no page size means nothing was paged")
	}
}

// server stands in for a collection of that many rows, answering a page the way
// Proton does: never more than width rows, and the total whatever the page
// holds.
type server struct {
	rows  int
	asked [][2]int
}

func (s *server) fetch(_ context.Context, page, size int) ([]int, int, error) {
	s.asked = append(s.asked, [2]int{page, size})
	size = min(size, width)
	start := min(page*size, s.rows)
	out := make([]int, 0, min(size, s.rows-start))
	for i := start; i < min(start+size, s.rows); i++ {
		out = append(out, i)
	}
	return out, s.rows, nil
}

func TestWindowReadsTheReadersPageNotProtons(t *testing.T) {
	for _, c := range []struct {
		name       string
		rows       int
		page, size int
		want       []int
		requests   int
	}{
		// An ordinary page is one page Proton can cut itself, so it stays one
		// request however many rows are behind it.
		{"a page Proton serves", 4812, 0, 25, seq(0, 25), 1},
		{"a later page Proton serves", 4812, 3, 25, seq(75, 100), 1},
		{"exactly Proton's width", 4812, 1, width, seq(150, 300), 1},
		// Wider than Proton serves: composed from its pages and cut down, so
		// the reader gets the number they asked for.
		{"wider than Proton serves", 4812, 0, 500, seq(0, 500), 4},
		{"a later wide page", 4812, 1, 200, seq(200, 400), 2},
		// The whole collection, which is what a size of zero means.
		{"everything", 380, 0, 0, seq(0, 380), 3},
		{"everything, exactly filling Proton's pages", 300, 0, 0, seq(0, 300), 3},
		{"everything of nothing", 0, 0, 0, nil, 1},
		// A page past the end is empty rather than short of what came before.
		{"past the end", 380, 2, 500, nil, 1},
		{"a short last page", 380, 1, 200, seq(200, 380), 2},
	} {
		srv := &server{rows: c.rows}
		got, total, err := Window(context.Background(), c.page, c.size, width, srv.fetch)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if total != c.rows {
			t.Errorf("%s: total = %d, want %d", c.name, total, c.rows)
		}
		if !equal(got, c.want) {
			t.Errorf("%s: rows = %v, want %v", c.name, brief(got), brief(c.want))
		}
		if len(srv.asked) != c.requests {
			t.Errorf("%s: %d requests %v, want %d", c.name, len(srv.asked), srv.asked, c.requests)
		}
	}
}

// An endpoint narrower than another's is read at its own width, so a listing
// capped at ten costs ten requests for a hundred rows rather than one that the
// server would refuse.
func TestWindowAsksAtTheEndpointsOwnWidth(t *testing.T) {
	srv := &server{rows: 25}
	got, _, err := Window(context.Background(), 0, 0, 10, srv.fetch)
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	if !equal(got, seq(0, 25)) {
		t.Errorf("rows = %v, want all 25", brief(got))
	}
	for _, asked := range srv.asked {
		if asked[1] != 10 {
			t.Errorf("asked for a page of %d, want 10", asked[1])
		}
	}
}

// Half a listing presented as a whole one is a wrong answer, so a page that
// fails fails the call.
func TestWindowFailsWholeWhenAPageFails(t *testing.T) {
	boom := errors.New("boom")
	_, _, err := Window(context.Background(), 0, 0, width, func(_ context.Context, page, _ int) ([]int, int, error) {
		if page == 1 {
			return nil, 0, boom
		}
		return seq(0, width), 400, nil
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
}

func seq(from, to int) []int {
	out := make([]int, 0, to-from)
	for i := from; i < to; i++ {
		out = append(out, i)
	}
	return out
}

func brief(rows []int) string {
	if len(rows) == 0 {
		return "nothing"
	}
	return fmt.Sprintf("%d rows %d..%d", len(rows), rows[0], rows[len(rows)-1])
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
