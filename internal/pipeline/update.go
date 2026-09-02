package pipeline

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sbeysenov/kino-cli/internal/model"
	"github.com/sbeysenov/kino-cli/internal/source/tmdb"
	"github.com/sbeysenov/kino-cli/internal/store"
)

type UpdateOpts struct {
	From, To string // YYYY-MM-DD, inclusive
	Region   string
	// ReleaseTypes to match; empty means any type, matched on the primary
	// release date and without per-region verification.
	ReleaseTypes  []int
	OrigLang      string
	OriginCountry string
	MinRating     float64
	MinVotes      int
	MaxPages      int
	Concurrency   int
	DryRun        bool
}

// Update discovers movies released inside the window, verifies each date
// against /movie/{id}/release_dates, and upserts them.
func (d *Deps) Update(ctx context.Context, o UpdateOpts) (store.RunResult, []*model.Movie, error) {
	res := store.RunResult{}
	region := strings.ToUpper(o.Region)

	var candidates []tmdb.DiscoverMovie
	page := 1
	for {
		resp, err := d.TMDB.Discover(ctx, tmdb.DiscoverParams{
			Region:        region,
			From:          o.From,
			To:            o.To,
			ReleaseTypes:  o.ReleaseTypes,
			OrigLang:      o.OrigLang,
			OriginCountry: o.OriginCountry,
			MinRating:     o.MinRating,
			MinVotes:      o.MinVotes,
			SortBy:        "popularity.desc",
			Lang:          "en-US", // English title/overview; the RU pair comes from Details
			Page:          page,
		})
		if err != nil {
			res.APICalls = d.Calls.Snapshot()
			return res, nil, err
		}
		candidates = append(candidates, resp.Results...)
		d.logf("discover page %d/%d: %d candidates (total %d)", resp.Page, resp.TotalPages, len(resp.Results), resp.TotalResults)
		if page >= resp.TotalPages || (o.MaxPages > 0 && page >= o.MaxPages) || len(resp.Results) == 0 {
			break
		}
		page++
	}
	res.Found = len(candidates)

	var (
		mu       sync.Mutex
		verified []*model.Movie
	)
	each(ctx, candidates, o.Concurrency, func(ctx context.Context, cand tmdb.DiscoverMovie) {
		det, err := d.TMDB.Details(ctx, cand.ID, d.Lang)
		if err != nil {
			d.logf("details %d: %v", cand.ID, err)
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			return
		}
		date, ok := matchDate(det, region, o.ReleaseTypes)
		if !ok || date < o.From || date > o.To {
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			return
		}

		m := merge(cand, det, d.Lang)
		m.MatchDate = date
		rels := releasesOf(det, region, o.ReleaseTypes)

		mu.Lock()
		verified = append(verified, m)
		mu.Unlock()

		if o.DryRun {
			return
		}
		inserted, err := d.Store.UpsertMovie(ctx, m)
		if err != nil {
			d.logf("upsert %d: %v", m.TMDBID, err)
			return
		}
		if err := d.Store.SaveReleases(ctx, rels); err != nil {
			d.logf("releases %d: %v", m.TMDBID, err)
		}
		mu.Lock()
		if inserted {
			res.Inserted++
		} else {
			res.Updated++
		}
		mu.Unlock()
	})

	res.APICalls = d.Calls.Snapshot()
	return res, verified, ctx.Err()
}

// matchDate returns the earliest date in the region matching one of the wanted
// release types. With no types it falls back to the primary release date, which
// is what the discover query filtered on.
func matchDate(d *tmdb.Details, region string, types []int) (string, bool) {
	if len(types) == 0 {
		day := day(d.ReleaseDate)
		return day, day != ""
	}
	best := ""
	for _, c := range d.ReleaseDates.Results {
		if !strings.EqualFold(c.Country, region) {
			continue
		}
		for _, r := range c.ReleaseDates {
			if !containsType(types, r.Type) {
				continue
			}
			day := day(r.ReleaseDate)
			if day != "" && (best == "" || day < best) {
				best = day
			}
		}
	}
	return best, best != ""
}

func containsType(types []int, t int) bool {
	for _, x := range types {
		if x == t {
			return true
		}
	}
	return false
}

// releasesOf keeps every release for the target region, plus the wanted types
// worldwide. Querying any type keeps everything.
func releasesOf(d *tmdb.Details, region string, types []int) []model.Release {
	var out []model.Release
	for _, c := range d.ReleaseDates.Results {
		sameRegion := strings.EqualFold(c.Country, region)
		for _, r := range c.ReleaseDates {
			if !sameRegion && len(types) > 0 && !containsType(types, r.Type) {
				continue
			}
			day := day(r.ReleaseDate)
			if day == "" {
				continue
			}
			out = append(out, model.Release{
				TMDBID: d.ID, Region: strings.ToUpper(c.Country), Type: r.Type, Date: day, Note: r.Note,
			})
		}
	}
	return out
}

// day trims TMDB's "2026-08-20T00:00:00.000Z" down to a plain date.
func day(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}

func merge(c tmdb.DiscoverMovie, d *tmdb.Details, lang string) *model.Movie {
	m := &model.Movie{
		TMDBID:     c.ID,
		IMDbID:     d.IMDbID,
		Title:      c.Title,
		OrigTitle:  c.OriginalTitle,
		Overview:   c.Overview,
		PosterPath: strings.TrimPrefix(c.PosterPath, "/"),
		OrigLang:   c.OriginalLanguage,
		Genres:     d.GenreNames(),
		Countries:  d.CountryCodes(),
		Runtime:    d.Runtime,
		// Details carries the primary (production) release date. The discover
		// result is localised by --region, so a 2026 re-release of a 1999 film
		// would otherwise be recorded as a 2026 film.
		ReleaseDate: day(firstNonEmpty(d.ReleaseDate, c.ReleaseDate)),
		UpdatedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if strings.HasPrefix(strings.ToLower(lang), "ru") {
		m.TitleRU = d.Title
		m.OverviewRU = d.Overview
	}
	if c.Popularity > 0 {
		p := c.Popularity
		m.Popularity = &p
	}
	if c.VoteAverage > 0 {
		v := c.VoteAverage
		m.TMDBRating = &v
	}
	if c.VoteCount > 0 {
		v := c.VoteCount
		m.TMDBVotes = &v
	}
	if y := year(d.ReleaseDate, c.ReleaseDate); y > 0 {
		m.Year = &y
	}
	return m
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func year(dates ...string) int {
	for _, s := range dates {
		if len(s) >= 4 {
			y := 0
			for _, ch := range s[:4] {
				if ch < '0' || ch > '9' {
					return 0
				}
				y = y*10 + int(ch-'0')
			}
			return y
		}
	}
	return 0
}
