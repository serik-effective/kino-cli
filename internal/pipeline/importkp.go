package pipeline

import (
	"context"
	"os"

	"github.com/serik-effective/kino-cli/internal/model"
	"github.com/serik-effective/kino-cli/internal/source/kp"
	"github.com/serik-effective/kino-cli/internal/store"
)

type ImportKPOpts struct {
	Files    []string
	AllTypes bool // keep series too, not just feature films
	// AddMissing looks unknown films up on TMDB and adds them, instead of
	// skipping everything that is not already in the database.
	AddMissing bool
	DryRun     bool
}

type ImportKPResult struct {
	Items     int
	Movies    int
	ByKPID    int // already linked through kp_id
	ByTitle   int // newly linked through title + year
	Added     int // pulled from TMDB because AddMissing was set
	Unmatched int
	Skipped   int // series and other non-movie entries
}

// ImportKP reads Kinopoisk player payloads saved to disk and folds their
// metadata into the local database: kinopoisk id, rating, view count and where
// the film can be watched. Signed stream URLs in the payload are ignored.
func (d *Deps) ImportKP(ctx context.Context, o ImportKPOpts) (ImportKPResult, error) {
	var res ImportKPResult

	for _, path := range o.Files {
		body, err := os.ReadFile(path)
		if err != nil {
			return res, err
		}
		items, err := kp.ParseDiscovery(body)
		if err != nil {
			return res, err
		}
		d.logf("%s: %d entries", path, len(items))
		res.Items += len(items)

		for _, it := range items {
			if !o.AllTypes && !it.IsMovie() {
				res.Skipped++
				continue
			}
			res.Movies++

			m, err := d.Store.FindByKPID(ctx, it.KPID)
			if err != nil {
				return res, err
			}
			matchedBy := "kp_id"
			if m == nil {
				matchedBy = "title"
				if m, err = d.Store.FindByTitleYear(ctx, it.Title, it.Year); err != nil {
					return res, err
				}
				if m == nil && it.OrigName != "" {
					if m, err = d.Store.FindByTitleYear(ctx, it.OrigName, it.Year); err != nil {
						return res, err
					}
				}
			}
			if m == nil && o.AddMissing && !o.DryRun {
				added, err := d.addFromTMDB(ctx, it)
				if err != nil {
					d.logf("tmdb %s: %v", it.Title, err)
				} else if added != nil {
					m, matchedBy = added, "added"
					res.Added++
				}
			}
			if m == nil {
				res.Unmatched++
				d.logf("не найден: %s (%d) kp:%d", it.Title, it.Year, it.KPID)
				continue
			}

			switch matchedBy {
			case "kp_id":
				res.ByKPID++
			case "title":
				res.ByTitle++
			}
			if o.DryRun {
				continue
			}
			if err := d.Store.ApplyKPInfo(ctx, m.TMDBID, store.KPInfo{
				KPID: it.KPID, Rating: it.Rating, Votes: it.Votes,
				Views: it.Views, Online: it.Online, Offer: it.Offer,
				OnlineKnown: it.OnlineKnown,
			}); err != nil {
				return res, err
			}
		}
	}
	return res, nil
}

// addFromTMDB resolves a Kinopoisk entry against TMDB and stores the movie, so
// the payload can seed films the database has never seen. A candidate counts
// only on an exact normalised title match, the same rule the tracker matcher
// uses.
func (d *Deps) addFromTMDB(ctx context.Context, it kp.DiscoveryItem) (*model.Movie, error) {
	if d.TMDB == nil {
		return nil, nil
	}
	queries := []struct{ q, lang string }{{it.Title, "ru-RU"}}
	if it.OrigName != "" {
		queries = append(queries, struct{ q, lang string }{it.OrigName, "en-US"})
	}

	for _, a := range queries {
		results, err := d.TMDB.SearchMovie(ctx, a.q, it.Year, a.lang)
		if err != nil {
			return nil, err
		}
		cand := pickMatch(results, a.q, it.Year)
		if cand == nil {
			continue
		}
		det, err := d.TMDB.Details(ctx, cand.ID, d.Lang)
		if err != nil {
			return nil, err
		}
		m := merge(*cand, det, d.Lang)
		if _, err := d.Store.UpsertMovie(ctx, m); err != nil {
			return nil, err
		}
		if err := d.Store.SaveReleases(ctx, releasesOf(det, "", nil)); err != nil {
			d.logf("releases %d: %v", m.TMDBID, err)
		}
		return m, nil
	}
	return nil, nil
}
