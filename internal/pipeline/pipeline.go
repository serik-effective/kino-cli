// Package pipeline orchestrates the sources and the store: discover, verify,
// upsert, enrich.
package pipeline

import (
	"context"
	"sync"

	"github.com/serik-effective/kino-cli/internal/source/kinozal"
	"github.com/serik-effective/kino-cli/internal/source/kp"
	"github.com/serik-effective/kino-cli/internal/source/omdb"
	"github.com/serik-effective/kino-cli/internal/source/tmdb"
	"github.com/serik-effective/kino-cli/internal/source/wikidata"
	"github.com/serik-effective/kino-cli/internal/store"
)

type Deps struct {
	Store *store.Store
	TMDB  *tmdb.Client
	OMDB  *omdb.Client
	KP    *kp.Client
	// KPRating hits rating.kinopoisk.ru, which needs no key and no quota.
	KPRating *kp.RatingClient
	Kinozal  *kinozal.Client
	Wikidata *wikidata.Client
	Lang     string
	Calls    *Counter
	Log      func(format string, args ...any)
}

func (d *Deps) logf(format string, args ...any) {
	if d.Log != nil {
		d.Log(format, args...)
	}
}

// Counter tallies outbound calls per source for the run summary.
type Counter struct {
	mu sync.Mutex
	n  map[string]int
	// Persist, when set, records the call against the daily quota table.
	Persist func(source string, n int)
}

func NewCounter() *Counter { return &Counter{n: map[string]int{}} }

func (c *Counter) Add(source string, n int) {
	c.mu.Lock()
	if c.n == nil {
		c.n = map[string]int{}
	}
	c.n[source] += n
	persist := c.Persist
	c.mu.Unlock()
	if persist != nil {
		persist(source, n)
	}
}

func (c *Counter) Snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]int, len(c.n))
	for k, v := range c.n {
		out[k] = v
	}
	return out
}

// each runs fn over items with at most n workers.
func each[T any](ctx context.Context, items []T, n int, fn func(context.Context, T)) {
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, it := range items {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(it T) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(ctx, it)
		}(it)
	}
	wg.Wait()
}
