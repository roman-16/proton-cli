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
