package proton

import "context"

// Reading a collection Proton hands over in pieces.
//
// Almost every listing Proton serves is capped: a folder of mail, a volume's
// trash, a vault's item history, a calendar's events. Reading one whole means
// asking again with the next page number or the cursor the last answer carried,
// until the answers run out. Every collection does that the same way, so it is
// written once here, at the transport layer, where the fact that an answer
// arrived in pieces belongs and stops.
//
// What differs per endpoint is only how it says there is more: a flag in the
// response, a cursor to send back, or nothing at all - in which case a full page
// is the sign, and Full is that judgement.

// pageLimit is how many pages a single walk will ask for.
//
// A collection is finite, so reaching this means an endpoint kept claiming there
// was more: a cursor that never advances, a filter the server ignored. Stopping
// is the honest end of a request nobody can wait out, and the caller records the
// short answer.
const pageLimit = 1000

// Pages calls fetch with ascending page numbers, starting at zero, until it
// reports there is nothing more.
//
// It is for a walk that keeps no rows - a search that stops once it has found
// what it came for, a sweep acting on each page as it arrives.
func Pages(ctx context.Context, fetch func(ctx context.Context, page int) (bool, error)) error {
	for page := range pageLimit {
		if err := ctx.Err(); err != nil {
			return err
		}
		more, err := fetch(ctx, page)
		if err != nil {
			return err
		}
		if !more {
			return nil
		}
	}
	return nil
}

// All walks a collection from its first page and returns every row.
//
// The rows come back in the order the pages arrived, which is the order the
// server put them in.
func All[T any](ctx context.Context, fetch func(ctx context.Context, page int) ([]T, bool, error)) ([]T, error) {
	var all []T
	err := Pages(ctx, func(ctx context.Context, page int) (bool, error) {
		rows, more, err := fetch(ctx, page)
		if err != nil {
			return false, err
		}
		all = append(all, rows...)
		return more, nil
	})
	if err != nil {
		return nil, err
	}
	return all, nil
}

// Full reports whether a page was filled, which is how an endpoint that says
// nothing else says there may be more.
//
// A short page is the end. A full one asks again and may come back empty, which
// costs one request and is the only way to be sure when the count is a multiple
// of the page size.
func Full[T any](rows []T, size int) bool { return size > 0 && len(rows) >= size }

// Window reads the page a caller asked for out of however many of Proton's it
// spans, and reports how many rows the whole result has.
//
// max is the widest page the endpoint serves, which each one declares for
// itself. A page no wider than that is one request, which is every ordinary
// listing. A wider one, and the whole result asked for with a size of zero, are
// read at the endpoint's width and cut down to what was asked for. That is what
// keeps --limit the reader's number: how many requests it costs is this layer's
// business, and 150 never reaches a screen.
func Window[T any](ctx context.Context, page, size, max int, fetch func(ctx context.Context, page, size int) ([]T, int, error)) ([]T, int, error) {
	if size > 0 && size <= max {
		return fetch(ctx, page, size)
	}

	from := page * size
	start := from - from%max
	var rows []T
	total := 0
	err := Pages(ctx, func(ctx context.Context, i int) (bool, error) {
		got, count, err := fetch(ctx, start/max+i, max)
		if err != nil {
			return false, err
		}
		rows = append(rows, got...)
		total = count
		if !Full(got, max) {
			return false, nil
		}
		return size == 0 || start+len(rows) < from+size, nil
	})
	if err != nil {
		return nil, 0, err
	}

	rows = rows[min(from-start, len(rows)):]
	if size > 0 {
		rows = rows[:min(size, len(rows))]
	}
	return rows, total, nil
}
