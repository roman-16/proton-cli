package proton

import (
	"context"
	"errors"
	"testing"
)

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
