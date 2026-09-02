package pipeline

import (
	"context"
	"sync"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
	"github.com/serik-effective/kino-cli/internal/source/kp"
	"github.com/serik-effective/kino-cli/internal/source/omdb"
	"github.com/serik-effective/kino-cli/internal/store"
)

type EnrichOpts struct {
	Limit       int
	StaleDays   int
	Concurrency int
	UseOMDB     bool
	UseKP       bool
	DryRun      bool
}

// Enrich fills IMDb/Metacritic/RT and Kinopoisk ratings for movies that have an
// IMDb id, staying inside each source's daily quota.
func (d *Deps) Enrich(ctx context.Context, o EnrichOpts) (store.RunResult, error) {
	res := store.RunResult{}

	budget := o.Limit
	if o.UseOMDB && d.OMDB != nil {
		used, err := d.Store.QuotaToday("omdb")
		if err != nil {
			return res, err
		}
		budget = min(budget, omdb.DailyLimit-used)
		d.logf("omdb quota: %d/%d used today", used, omdb.DailyLimit)
	}
	if o.UseKP && d.KP != nil {
		info, err := d.KP.TokenInfo(ctx)
		if err != nil {
			d.logf("kp token info: %v (falling back to local counter)", err)
			used, qerr := d.Store.QuotaToday("kp")
			if qerr != nil {
				return res, qerr
			}
			budget = min(budget, kp.DailyLimit-used)
		} else {
			d.logf("kp quota: %d/%d used, %d left (reset %s)", info.Used, info.Limit, info.Remaining, info.ResetAt)
			budget = min(budget, info.Remaining)
		}
	}
	if budget <= 0 {
		d.logf("daily quota exhausted, nothing to do")
		res.APICalls = d.Calls.Snapshot()
		return res, nil
	}

	pending, err := d.Store.PendingEnrich(ctx, budget, o.StaleDays)
	if err != nil {
		return res, err
	}
	res.Found = len(pending)

	var mu sync.Mutex
	each(ctx, pending, o.Concurrency, func(ctx context.Context, m *model.Movie) {
		touched := false

		if o.UseOMDB && d.OMDB != nil {
			r, err := d.OMDB.ByIMDb(ctx, m.IMDbID)
			if err != nil {
				d.logf("omdb %s: %v", m.IMDbID, err)
			} else {
				m.IMDbRating = omdb.Float(r.IMDbRating)
				m.IMDbVotes = omdb.Int(r.IMDbVotes)
				m.Metascore = omdb.Int(r.Metascore)
				m.RTScore = r.RottenTomatoes()
				if m.Runtime == nil {
					m.Runtime = omdb.Int(r.Runtime)
				}
				touched = true
			}
		}

		// A known kp_id means ratings come from the free XML feed and the
		// poiskkino quota stays untouched.
		if o.UseKP && m.KPID != nil && d.KPRating != nil {
			r, err := d.KPRating.Ratings(ctx, *m.KPID)
			if err != nil {
				d.logf("kp rating %d: %v", *m.KPID, err)
			} else {
				applyRatings(m, r, false)
				m.RatingsAt = time.Now().UTC().Format(time.RFC3339)
				touched = true
			}
		} else if o.UseKP && d.KP != nil {
			k, err := d.KP.ByIMDb(ctx, m.IMDbID)
			if err != nil {
				d.logf("kp %s: %v", m.IMDbID, err)
			} else {
				if k.ID > 0 {
					id := k.ID
					m.KPID = &id
				}
				if k.Rating.KP > 0 {
					v := k.Rating.KP
					m.KPRating = &v
				}
				if k.Votes.KP > 0 {
					v := k.Votes.KP
					m.KPVotes = &v
				}
				if m.TitleRU == "" {
					m.TitleRU = k.TitleRU()
				}
				if m.OverviewRU == "" {
					m.OverviewRU = k.Description
				}
				touched = true
			}
		}

		mu.Lock()
		defer mu.Unlock()
		if !touched {
			res.Skipped++
			return
		}
		if o.DryRun {
			res.Updated++
			return
		}
		m.EnrichedAt = time.Now().UTC().Format(time.RFC3339)
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
