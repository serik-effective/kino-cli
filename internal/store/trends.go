package store

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
)

// UpsertTorrent records a release, preserving first_seen across runs.
func (s *Store) UpsertTorrent(ctx context.Context, t model.Torrent) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO torrents (source, ext_id, raw_title, title_ru, title_orig, year, tags, quality, match_state, first_seen, last_seen)
VALUES (?,?,?,?,?,?,?,?,COALESCE(?,'pending'),?,?)
ON CONFLICT(source, ext_id) DO UPDATE SET
  raw_title  = excluded.raw_title,
  title_ru   = excluded.title_ru,
  title_orig = excluded.title_orig,
  year       = COALESCE(excluded.year, torrents.year),
  tags       = excluded.tags,
  quality    = excluded.quality,
  last_seen  = excluded.last_seen`,
		t.Source, t.ExtID, t.RawTitle, t.TitleRU, t.TitleOrig, nullInt(t.Year), t.Tags, t.Quality,
		nullStr(t.MatchState), now, now)
	return err
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

func (s *Store) SaveTorrentStats(ctx context.Context, stats []model.TorrentStat) error {
	if len(stats) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO torrent_stats (source, ext_id, captured_at, period, rank, seeds, leechers, downloads, comments)
VALUES (?,?,?,?,?,?,?,?,?)
ON CONFLICT(source, ext_id, captured_at) DO UPDATE SET
  rank = excluded.rank, seeds = excluded.seeds, leechers = excluded.leechers,
  downloads = excluded.downloads, comments = excluded.comments`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, st := range stats {
		if _, err := stmt.ExecContext(ctx, st.Source, st.ExtID, st.CapturedAt, st.Period,
			st.Rank, st.Seeds, st.Leechers, st.Downloads, st.Comments); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PendingMatch returns releases not yet resolved to a TMDB movie.
func (s *Store) PendingMatch(ctx context.Context, limit int) ([]model.Torrent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT source, ext_id, raw_title, COALESCE(title_ru,''), COALESCE(title_orig,''), COALESCE(year,0)
FROM torrents WHERE tmdb_id IS NULL AND match_state = 'pending' LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Torrent
	for rows.Next() {
		var t model.Torrent
		if err := rows.Scan(&t.Source, &t.ExtID, &t.RawTitle, &t.TitleRU, &t.TitleOrig, &t.Year); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetMatch links a release to a movie, or marks it unmatched when tmdbID is nil.
func (s *Store) SetMatch(ctx context.Context, source, extID string, tmdbID *int) error {
	state := "unmatched"
	if tmdbID != nil {
		state = "matched"
	}
	_, err := s.db.ExecContext(ctx,
		"UPDATE torrents SET tmdb_id = ?, match_state = ? WHERE source = ? AND ext_id = ?",
		tmdbID, state, source, extID)
	return err
}

type TrendOpts struct {
	Source string
	Since  string // RFC3339 baseline for the download delta
	Metric string // delta | downloads | seeds | rank
	Limit  int
	// Unmatched lists releases that could not be resolved to a movie instead.
	Unmatched bool
}

// Trending aggregates the newest snapshot of every release, grouped by movie:
// several rips of one film (1080p, 4K, different dubs) become one row.
func (s *Store) Trending(ctx context.Context, o TrendOpts) ([]*model.TrendRow, error) {
	since := o.Since
	if since == "" {
		since = time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339)
	}

	order := "delta DESC, downloads DESC"
	switch o.Metric {
	case "", "delta":
	case "downloads":
		order = "downloads DESC"
	case "seeds":
		order = "seeds DESC"
	case "rank":
		order = "rank ASC"
	default:
		return nil, fmt.Errorf("unknown metric %q, want delta|downloads|seeds|rank", o.Metric)
	}
	limit := ""
	if o.Limit > 0 {
		limit = " LIMIT " + strconv.Itoa(o.Limit)
	}

	srcFilter, srcArgs := "", []any{}
	if o.Source != "" {
		srcFilter = " AND t.source = ?"
		srcArgs = append(srcArgs, o.Source)
	}

	// latest: newest snapshot per release. base: the newest snapshot at or
	// before the cutoff; when tracking started later than that, the earliest
	// snapshot we do have, so the delta covers the observed window instead of
	// silently reading zero.
	const cte = `
WITH latest AS (
  SELECT source, ext_id, rank, seeds, leechers, downloads,
         ROW_NUMBER() OVER (PARTITION BY source, ext_id ORDER BY captured_at DESC) rn
  FROM torrent_stats
),
base AS (
  SELECT source, ext_id, downloads,
         ROW_NUMBER() OVER (
           PARTITION BY source, ext_id
           ORDER BY (captured_at <= ?) DESC,
                    CASE WHEN captured_at <= ? THEN captured_at END DESC,
                    captured_at ASC
         ) rn
  FROM torrent_stats
)`

	if o.Unmatched {
		q := cte + `
SELECT t.raw_title,
       COALESCE(MIN(l.rank),0) AS rank,
       COUNT(*) AS releases,
       COALESCE(SUM(l.seeds),0) AS seeds,
       COALESCE(SUM(l.leechers),0) AS leechers,
       COALESCE(SUM(l.downloads),0) AS downloads,
       COALESCE(SUM(l.downloads - COALESCE(b.downloads, l.downloads)),0) AS delta
FROM torrents t
JOIN latest l ON l.source = t.source AND l.ext_id = t.ext_id AND l.rn = 1
LEFT JOIN base b ON b.source = t.source AND b.ext_id = t.ext_id AND b.rn = 1
WHERE t.tmdb_id IS NULL` + srcFilter + `
GROUP BY t.source, t.ext_id ORDER BY ` + order + limit
		rows, err := s.db.QueryContext(ctx, q, append([]any{since, since}, srcArgs...)...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []*model.TrendRow
		for rows.Next() {
			var r model.TrendRow
			if err := rows.Scan(&r.RawTitle, &r.Rank, &r.Releases, &r.Seeds, &r.Leechers, &r.Downloads, &r.DownloadsDelta); err != nil {
				return nil, err
			}
			out = append(out, &r)
		}
		return out, rows.Err()
	}

	q := cte + `
SELECT ` + movieCols + `, COALESCE(MIN(l.rank),0) AS rank, COUNT(*) AS releases,
       COALESCE(SUM(l.seeds),0) AS seeds, COALESCE(SUM(l.leechers),0) AS leechers,
       COALESCE(SUM(l.downloads),0) AS downloads,
       COALESCE(SUM(l.downloads - COALESCE(b.downloads, l.downloads)),0) AS delta
FROM torrents t
JOIN latest l ON l.source = t.source AND l.ext_id = t.ext_id AND l.rn = 1
LEFT JOIN base b ON b.source = t.source AND b.ext_id = t.ext_id AND b.rn = 1
JOIN movies m ON m.tmdb_id = t.tmdb_id
WHERE t.tmdb_id IS NOT NULL` + srcFilter + `
GROUP BY m.tmdb_id ORDER BY ` + order + limit
	rows, err := s.db.QueryContext(ctx, q, append([]any{since, since}, srcArgs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.TrendRow
	for rows.Next() {
		var r model.TrendRow
		m, err := scanMovieInto(rows, &r.Rank, &r.Releases, &r.Seeds, &r.Leechers, &r.Downloads, &r.DownloadsDelta)
		if err != nil {
			return nil, err
		}
		r.Movie = m
		out = append(out, &r)
	}
	return out, rows.Err()
}
