package pipeline

import (
	"context"
	"fmt"

	"github.com/sbeysenov/kino-cli/internal/source/kp"
	"github.com/sbeysenov/kino-cli/internal/store"
)

type UpdateKPOpts struct {
	FromYear, ToYear int
	MinVotes         int
	Countries        []string
	// MaxPages bounds the quota a single run may spend. Zero means every page
	// the filter matches.
	MaxPages int
	// AddMissing looks unknown films up on TMDB and stores them, instead of
	// skipping everything we do not already have.
	AddMissing bool
	DryRun     bool
}

type UpdateKPResult struct {
	Seen      int
	ByKPID    int
	ByIMDb    int
	ByTitle   int
	Added     int
	Unmatched int
}

// UpdateKP pulls a slice of the Kinopoisk catalogue and folds it into ours.
//
// This is the counterpart to the TMDB update for Russian cinema, which TMDB
// covers badly: it brings the Kinopoisk id, the rating with its vote count, and
// — the part nothing else gives us — which services actually carry the film.
//
// One filtered page returns up to 250 films, so a year of Russian releases
// costs one or two requests against a 200-a-day quota.
func (d *Deps) UpdateKP(ctx context.Context, o UpdateKPOpts) (UpdateKPResult, error) {
	var res UpdateKPResult
	if d.KP == nil {
		return res, fmt.Errorf("нужен ключ Кинопоиска (KINOPOISK_API_KEY)")
	}

	// Vote bands, not pages. The free tier refuses page 11, so a catalogue
	// slice wider than 2500 films can only be walked by narrowing the filter:
	// results come back sorted by votes descending, so dropping the ceiling to
	// the least-voted film of the previous band resumes where it stopped.
	spent := 0
	ceiling := 0 // unbounded
	for {
		lowest, pages, err := d.kpBand(ctx, o, ceiling, &spent, &res)
		if err != nil {
			return res, err
		}
		if o.MaxPages > 0 && spent >= o.MaxPages {
			return res, nil
		}
		next := nextCeiling(lowest, ceiling, o.MinVotes, pages)
		if next == 0 {
			return res, nil
		}
		d.logf("kp: тариф отдал %d страниц, продолжаю с голосов <= %d", pages, next)
		ceiling = next
	}
}

// nextCeiling reports the vote ceiling for the next band, or 0 when the walk is
// finished.
//
// The subtle case is a band whose films all share one vote count: the lowest
// seen equals the ceiling we asked for, so using it again would re-request the
// same page forever. Stepping one below is safe because the band was full — a
// film with exactly that many votes was already folded in.
func nextCeiling(lowest, ceiling, minVotes, pages int) int {
	if pages < kp.MaxTierPages {
		return 0 // the band fit; there is nothing below it
	}
	if lowest < 0 || lowest <= minVotes {
		return 0
	}
	if ceiling > 0 && lowest >= ceiling {
		lowest = ceiling - 1
	}
	if lowest <= 0 || lowest <= minVotes {
		return 0
	}
	return lowest
}

// kpBand walks one vote band and reports the lowest vote count it saw and how
// many pages it read.
func (d *Deps) kpBand(ctx context.Context, o UpdateKPOpts, ceiling int, spent *int, res *UpdateKPResult) (lowest, pages int, err error) {
	lowest = -1
	for page := 1; page <= kp.MaxTierPages; page++ {
		if o.MaxPages > 0 && *spent >= o.MaxPages {
			d.logf("kp: остановился на странице %d по --max-pages", o.MaxPages)
			return lowest, page - 1, nil
		}
		batch, err := d.KP.Discover(ctx, kp.DiscoverParams{
			FromYear: o.FromYear, ToYear: o.ToYear,
			MinVotes: o.MinVotes, MaxVotes: ceiling, Countries: o.Countries,
			Type: "movie", Page: page, Limit: kp.MaxPageSize,
		})
		*spent++
		if err != nil {
			return lowest, page - 1, err
		}
		if len(batch.Movies) == 0 {
			return lowest, page - 1, nil
		}
		d.logf("kp: страница %d из %d, фильмов %d", page, batch.Pages, len(batch.Movies))

		for i := range batch.Movies {
			if v := batch.Movies[i].Votes.KP; lowest < 0 || v < lowest {
				lowest = v
			}
			if err := d.foldKPMovie(ctx, &batch.Movies[i], o, res); err != nil {
				return lowest, page, err
			}
		}
		if page >= batch.Pages {
			return lowest, page, nil
		}
	}
	return lowest, kp.MaxTierPages, nil
}

func (d *Deps) foldKPMovie(ctx context.Context, m *kp.Movie, o UpdateKPOpts, res *UpdateKPResult) error {
	res.Seen++

	// Most reliable identifier first, guesswork last.
	found, err := d.Store.FindByKPID(ctx, m.ID)
	if err != nil {
		return err
	}
	if found != nil {
		res.ByKPID++
	}
	if found == nil && m.ExternalID.IMDb != "" {
		found, err = d.Store.FindByIMDbID(ctx, m.ExternalID.IMDb)
		if err != nil {
			return err
		}
		if found != nil {
			res.ByIMDb++
		}
	}
	if found == nil {
		for _, title := range []string{m.TitleRU(), m.EnName, m.AlternativeName} {
			if title == "" {
				continue
			}
			found, err = d.Store.FindByTitleYear(ctx, title, m.Year)
			if err != nil {
				return err
			}
			if found != nil {
				res.ByTitle++
				break
			}
		}
	}

	if found == nil {
		if !o.AddMissing {
			res.Unmatched++
			return nil
		}
		if o.DryRun {
			res.Unmatched++
			return nil
		}
		added, err := d.addFromTMDB(ctx, kp.DiscoveryItem{
			KPID: m.ID, Title: m.TitleRU(), OrigName: m.EnName, Year: m.Year, Type: "MOVIE",
		})
		if err != nil {
			return err
		}
		if added == nil {
			res.Unmatched++
			return nil
		}
		res.Added++
		found = added
	}

	if o.DryRun {
		return nil
	}
	services := m.Services()
	return d.Store.ApplyKPInfo(ctx, found.TMDBID, store.KPInfo{
		KPID:   m.ID,
		Rating: m.Rating.KP,
		Votes:  m.Votes.KP,
		// The catalogue always reports watchability, so its silence really does
		// mean "nowhere", unlike a harvest that never looked.
		OnlineKnown: true,
		Online:      len(services) > 0,
		Services:    services,
	})
}
