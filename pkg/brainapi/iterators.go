package brainapi

import "context"

// paginateAll drives the shared iterator loop behind the List*All methods:
// spawn a goroutine, walk the offset cursor, fan items out on a channel, and
// honor ctx cancellation. fetchPage fetches one page at the given offset and
// reports its items plus whether pagination is exhausted — endpoints signal
// "last page" differently (Next links vs a count cursor), so that decision
// stays with the caller. The errs channel is buffered (cap 1) so the producer
// never blocks delivering a terminal error.
func paginateAll[T any](ctx context.Context, offset int, fetchPage func(offset int) (items []T, done bool, err error)) (<-chan T, <-chan error) {
	out := make(chan T)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		for {
			items, done, err := fetchPage(offset)
			if err != nil {
				errs <- err
				return
			}
			for _, it := range items {
				select {
				case <-ctx.Done():
					errs <- ctx.Err()
					return
				case out <- it:
				}
			}
			if done || len(items) == 0 {
				return
			}
			offset += len(items)
		}
	}()
	return out, errs
}
