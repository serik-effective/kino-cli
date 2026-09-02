package pipeline

import (
	"context"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/serik-effective/kino-cli/internal/model"
	"github.com/serik-effective/kino-cli/internal/source/kinozal"
	"github.com/serik-effective/kino-cli/internal/source/tmdb"
	"github.com/serik-effective/kino-cli/internal/store"
)

const SourceKinozal = "kinozal"

type TrendsOpts struct {
	Period kinozal.Period
	// Pages > 0 reads the movie category listing instead of the ranked chart.
	Pages       int
	Details     bool // fetch per-release download counters (one request each)
	Limit       int  // cap releases per run
	Match       bool // resolve releases to TMDB movies afterwards
	Concurrency int
	DryRun      bool
}

// FetchTrends captures a snapshot of what is popular on the tracker right now.
// Only metadata is read: titles, ranks and counters.
func (d *Deps) FetchTrends(ctx context.Context, o TrendsOpts) (store.RunResult, error) {
	res := store.RunResult{}
	if d.Kinozal == nil {
		return res, nil
	}

	var items []kinozal.Item
	var err error
	if o.Pages > 0 {
		for page := 0; page < o.Pages; page++ {
			batch, perr := d.Kinozal.Browse(ctx, page)
			if perr != nil {
				err = perr
				break
			}
			items = append(items, batch...)
			d.logf("browse page %d: %d movie releases", page, len(batch))
		}
	} else {
		items, err = d.Kinozal.Top(ctx, o.Period)
		d.logf("top %s: %d movie releases", o.Period, len(items))
	}
	if err != nil {
		res.APICalls = d.Calls.Snapshot()
		return res, err
	}
	if o.Limit > 0 && len(items) > o.Limit {
		d.logf("capping %d releases at --limit %d", len(items), o.Limit)
		items = items[:o.Limit]
	}
	res.Found = len(items)

	captured := time.Now().UTC().Format(time.RFC3339)
	stats := make([]model.TorrentStat, 0, len(items))
	var mu sync.Mutex

	each(ctx, items, o.Concurrency, func(ctx context.Context, it kinozal.Item) {
		st := model.TorrentStat{
			Source: SourceKinozal, ExtID: it.ExtID, CapturedAt: captured,
			Period: string(o.Period), Rank: it.Rank,
			Seeds: it.Seeds, Leechers: it.Leechers, Comments: it.Comments,
		}
		if o.Details {
			det, err := d.Kinozal.Details(ctx, it.ExtID)
			if err != nil {
				d.logf("details %s: %v", it.ExtID, err)
			} else {
				st.Seeds, st.Leechers = det.Seeds, det.Leechers
				st.Downloads, st.Comments = det.Downloads, det.Comments
			}
		}

		mu.Lock()
		stats = append(stats, st)
		mu.Unlock()

		if o.DryRun {
			return
		}
		t := model.Torrent{
			Source: SourceKinozal, ExtID: it.ExtID, RawTitle: it.RawTitle,
			TitleRU: it.TitleRU, TitleOrig: it.TitleOrig, Year: it.Year,
			Tags: it.Tags, Quality: it.Quality,
		}
		if err := d.Store.UpsertTorrent(ctx, t); err != nil {
			d.logf("upsert torrent %s: %v", it.ExtID, err)
			mu.Lock()
			res.Skipped++
			mu.Unlock()
			return
		}
		mu.Lock()
		res.Inserted++
		mu.Unlock()
	})

	if !o.DryRun {
		if err := d.Store.SaveTorrentStats(ctx, stats); err != nil {
			return res, err
		}
	}
	res.APICalls = d.Calls.Snapshot()
	return res, ctx.Err()
}

// MatchTorrents resolves pending releases to TMDB movies and pulls those movies
// into the local database so the ratings pipeline can pick them up.
func (d *Deps) MatchTorrents(ctx context.Context, limit, concurrency int) (matched, unmatched int, err error) {
	pending, err := d.Store.PendingMatch(ctx, limit)
	if err != nil {
		return 0, 0, err
	}
	if d.TMDB == nil || len(pending) == 0 {
		return 0, 0, nil
	}

	var mu sync.Mutex
	each(ctx, pending, concurrency, func(ctx context.Context, t model.Torrent) {
		cand := d.findMovie(ctx, t)
		if cand == nil {
			if err := d.Store.SetMatch(ctx, t.Source, t.ExtID, nil); err != nil {
				d.logf("set match %s: %v", t.ExtID, err)
			}
			mu.Lock()
			unmatched++
			mu.Unlock()
			d.logf("no TMDB match: %s", t.RawTitle)
			return
		}

		// Pull full details so the row is as complete as an `update` would make it.
		det, err := d.TMDB.Details(ctx, cand.ID, d.Lang)
		if err != nil {
			d.logf("details %d: %v", cand.ID, err)
			return
		}
		m := merge(*cand, det, d.Lang)
		if _, err := d.Store.UpsertMovie(ctx, m); err != nil {
			d.logf("upsert %d: %v", m.TMDBID, err)
			return
		}
		if err := d.Store.SaveReleases(ctx, releasesOf(det, "", nil)); err != nil {
			d.logf("releases %d: %v", m.TMDBID, err)
		}
		id := cand.ID
		if err := d.Store.SetMatch(ctx, t.Source, t.ExtID, &id); err != nil {
			d.logf("set match %s: %v", t.ExtID, err)
			return
		}
		mu.Lock()
		matched++
		mu.Unlock()
	})
	return matched, unmatched, ctx.Err()
}

// findMovie tries the original title first, then the Russian one. A candidate
// counts only when a title matches after normalisation, so "Мятеж" cannot
// silently become an unrelated film with a similar name.
func (d *Deps) findMovie(ctx context.Context, t model.Torrent) *tmdb.DiscoverMovie {
	type attempt struct {
		query string
		lang  string
	}
	attempts := []attempt{}
	if t.TitleOrig != "" {
		attempts = append(attempts, attempt{t.TitleOrig, "en-US"})
	}
	if t.TitleRU != "" {
		attempts = append(attempts, attempt{t.TitleRU, "ru-RU"})
	}

	for _, a := range attempts {
		for _, year := range []int{t.Year, 0} {
			results, err := d.TMDB.SearchMovie(ctx, a.query, year, a.lang)
			if err != nil {
				d.logf("search %q: %v", a.query, err)
				continue
			}
			if best := pickMatch(results, a.query, t.Year); best != nil {
				return best
			}
			if year == 0 {
				break
			}
		}
	}
	return nil
}

func pickMatch(results []tmdb.DiscoverMovie, query string, year int) *tmdb.DiscoverMovie {
	want := normalizeTitle(query)
	for i := range results {
		r := &results[i]
		if normalizeTitle(r.Title) != want && normalizeTitle(r.OriginalTitle) != want {
			continue
		}
		if year > 0 && r.ReleaseDate != "" {
			if ry := yearOf(r.ReleaseDate); ry > 0 && abs(ry-year) > 1 {
				continue // same name, wrong decade
			}
		}
		return r
	}
	return nil
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for _, ch := range date[:4] {
		if ch < '0' || ch > '9' {
			return 0
		}
		y = y*10 + int(ch-'0')
	}
	return y
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// normalizeTitle lowercases, folds ё to е and drops punctuation so that
// "Мятеж." and "Мятеж" compare equal.
func normalizeTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r == 'ё':
			b.WriteRune('е')
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
