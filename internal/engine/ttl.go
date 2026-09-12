package engine

import (
	"context"
	"time"
)

// Janitor context timeout bounds a single purge sweep; sweeps only take
// per-shard write locks briefly, so this is generous.
const janitorSweepTimeout = 5 * time.Second

// runJanitor periodically removes expired entries from every shard until
// Close is called. It holds at most one shard lock at a time and never
// blocks mutations for long, satisfying the project rule that TTL cleanup
// must not race with reads, writes, or snapshots.
func (s *Store) runJanitor() {
	defer close(s.janDone)

	ticker := time.NewTicker(s.janitorInt)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopJan:
			return
		case <-ticker.C:
			s.sweep()
		}
	}
}

// sweep removes expired entries from all shards once and logs the result
// at debug level. Individual shard errors are impossible here (purge never
// fails), but the sweep is guarded so a slow disk-free sweep can never hang.
func (s *Store) sweep() {
	ctx, cancel := context.WithTimeout(context.Background(), janitorSweepTimeout)
	defer cancel()

	removed := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		now := time.Now()
		for _, sh := range s.shards {
			removed += s.purge(sh, now)
		}
	}()

	select {
	case <-done:
		if removed > 0 {
			s.logger.Debug("janitor removed expired entries", "count", removed)
		}
	case <-ctx.Done():
		s.logger.Warn("janitor sweep timed out; it will retry on the next tick")
	}
}
