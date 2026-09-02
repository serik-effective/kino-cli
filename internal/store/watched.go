package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type WatchedFilm struct {
	TMDBID    int
	Title     string
	Year      int
	Rating    int // 0 when the viewer gave none
	WatchedAt string
}

// MarkWatched records a film as seen. Re-marking updates the rating rather than
// failing: changing your mind about a film is normal.
func (s *Store) MarkWatched(ctx context.Context, tmdbID, rating int) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO watched (tmdb_id, rating, watched_at) VALUES (?, ?, ?)
ON CONFLICT(tmdb_id) DO UPDATE SET rating = excluded.rating, watched_at = excluded.watched_at`,
		tmdbID, nullIfZeroInt(rating), nowRFC3339())
	return err
}

func (s *Store) ForgetWatched(ctx context.Context, tmdbID int) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watched WHERE tmdb_id = ?`, tmdbID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ListWatched(ctx context.Context) ([]WatchedFilm, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT w.tmdb_id, COALESCE(m.title_ru, m.title, ''), COALESCE(m.year, 0),
       COALESCE(w.rating, 0), w.watched_at
  FROM watched w LEFT JOIN movies m ON m.tmdb_id = w.tmdb_id
 ORDER BY w.watched_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []WatchedFilm
	for rows.Next() {
		var f WatchedFilm
		if err := rows.Scan(&f.TMDBID, &f.Title, &f.Year, &f.Rating, &f.WatchedAt); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FindMovies resolves a user's words to films. It matches a tmdb id outright,
// otherwise looks for the text in either title, so "коммерсант" finds the film
// without the user knowing any id.
//
// The case-insensitive comparison happens in Go, not in SQL: SQLite's LOWER()
// only folds ASCII, so a Cyrillic query matched nothing at all.
func (s *Store) FindMovies(ctx context.Context, query string, limit int) ([]WatchedFilm, error) {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil, fmt.Errorf("пустой запрос")
	}

	if id := parseIntOrZero(strings.TrimPrefix(q, "tmdb:")); id > 0 {
		row := s.db.QueryRowContext(ctx, `
SELECT tmdb_id, COALESCE(title_ru, title, ''), COALESCE(year, 0) FROM movies WHERE tmdb_id = ?`, id)
		var f WatchedFilm
		if err := row.Scan(&f.TMDBID, &f.Title, &f.Year); err != nil {
			if err == sql.ErrNoRows {
				return nil, nil
			}
			return nil, err
		}
		return []WatchedFilm{f}, nil
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT tmdb_id, COALESCE(title_ru,''), COALESCE(title,''), COALESCE(year,0)
  FROM movies
 ORDER BY COALESCE(kp_votes,0) + COALESCE(imdb_votes,0) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out, exact []WatchedFilm
	for rows.Next() {
		var id, year int
		var ru, orig string
		if err := rows.Scan(&id, &ru, &orig, &year); err != nil {
			return nil, err
		}
		lru, lorig := strings.ToLower(ru), strings.ToLower(orig)
		if !strings.Contains(lru, q) && !strings.Contains(lorig, q) {
			continue
		}
		title := ru
		if title == "" {
			title = orig
		}
		f := WatchedFilm{TMDBID: id, Title: title, Year: year}
		if lru == q || lorig == q {
			exact = append(exact, f)
		}
		if len(out) < limit {
			out = append(out, f)
		}
		// Keep reading past the limit only while an exact match is still
		// possible; otherwise a popular substring match would hide the film
		// actually named that.
		if len(out) >= limit && len(exact) > 0 {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// One film named exactly what was asked for wins over any number of films
	// merely containing those words: "майкл" is the film Майкл, not
	// "Дрю Майкл: Красный, синий, зеленый".
	if len(exact) == 1 {
		return exact, nil
	}
	return out, nil
}

func nullIfZeroInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func parseIntOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	if s == "" {
		return 0
	}
	return n
}
