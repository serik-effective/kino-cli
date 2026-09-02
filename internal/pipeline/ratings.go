package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
	"github.com/serik-effective/kino-cli/internal/source/kp"
	"github.com/serik-effective/kino-cli/internal/store"
)

type kpRatings = kp.Ratings

type RefreshOpts struct {
	Limit         int
	StaleHours    int
	Concurrency   int
	OverwriteIMDb bool
	DryRun        bool
}

// RefreshRatings updates KP (and, when missing, IMDb) scores through
// rating.kinopoisk.ru. No key, no daily quota — safe to run often.
func (d *Deps) RefreshRatings(ctx context.Context, o RefreshOpts) (store.RunResult, error) {
	res := store.RunResult{}
	if d.KPRating == nil {
		return res, nil
	}

	pending, err := d.Store.PendingRatings(ctx, o.Limit, o.StaleHours)
	if err != nil {
		return res, err
	}
	res.Found = len(pending)

	var mu sync.Mutex
	each(ctx, pending, o.Concurrency, func(ctx context.Context, m *model.Movie) {
		r, err := d.KPRating.Ratings(ctx, *m.KPID)
		if err != nil {
			d.logf("kp rating %d: %v", *m.KPID, err)
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			return
		}
		applyRatings(m, r, o.OverwriteIMDb)

		mu.Lock()
		defer mu.Unlock()
		if o.DryRun {
			res.Updated++
			return
		}
		m.RatingsAt = time.Now().UTC().Format(time.RFC3339)
		if _, err := d.Store.UpsertMovie(ctx, m); err != nil {
			d.logf("upsert %d: %v", m.TMDBID, err)
			res.Skipped++
			return
		}
		res.Updated++
	})

	res.APICalls = d.Calls.Snapshot()
	return res, ctx.Err()
}

// applyRatings trusts the XML feed for Kinopoisk scores, but leaves an existing
// IMDb rating alone: OMDb reports it more precisely than KP's mirror.
func applyRatings(m *model.Movie, r *kpRatings, overwriteIMDb bool) {
	if r.KP != nil {
		m.KPRating = r.KP
	}
	if r.KPVotes != nil {
		m.KPVotes = r.KPVotes
	}
	if r.IMDb != nil && (overwriteIMDb || m.IMDbRating == nil) {
		m.IMDbRating = r.IMDb
		if r.IMDbVotes != nil {
			m.IMDbVotes = r.IMDbVotes
		}
	}
}
