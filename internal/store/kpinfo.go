package store

import (
	"context"
	"strings"
	"unicode"

	"github.com/sbeysenov/kino-cli/internal/model"
)

// KPInfo is what one Kinopoisk player entry can teach the local database.
type KPInfo struct {
	KPID   int
	Rating float64
	Votes  int
	Views  int
	Online bool
	Offer  string
	// Services names every platform carrying the film. Empty means the source
	// said nothing, which OnlineKnown distinguishes from "nowhere".
	Services []string
	// OnlineKnown says whether the source reported availability at all. False
	// means "no opinion", which is not the same as "not available".
	OnlineKnown bool
}

// ApplyKPInfo updates a movie's Kinopoisk fields. Ratings and counters are
// overwritten (they are fresher than ours); the kp_id is only ever set, never
// changed, so a bad match cannot silently repoint an existing link.
func (s *Store) ApplyKPInfo(ctx context.Context, tmdbID int, info KPInfo) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE movies SET
  kp_id         = COALESCE(kp_id, ?),
  -- A rating with no vote count behind it (a number scraped off a card by the
  -- browser helper) must not overwrite one that has votes: it is less reliable,
  -- and a misread would be indistinguishable from a real change.
  kp_rating     = CASE
                    WHEN ? > 0 OR kp_votes IS NULL OR kp_votes = 0
                      THEN COALESCE(NULLIF(?, 0), kp_rating)
                    ELSE kp_rating
                  END,
  kp_votes      = COALESCE(NULLIF(?, 0), kp_votes),
  kp_views      = COALESCE(NULLIF(?, 0), kp_views),
  -- Only a source that actually reports availability may change the flag;
  -- an id-only harvest knows nothing about it and must leave it alone.
  watch_online  = CASE WHEN ? = 1 THEN ? ELSE watch_online END,
  watch_offer   = COALESCE(NULLIF(?, ''), watch_offer),
  watch_services = CASE WHEN ? != '' THEN ? ELSE watch_services END,
  -- First seen only: this is the closest thing we have to a Russian digital
  -- release date, so a later re-import must not move it forward.
  watch_seen_at = CASE WHEN ? = 1 THEN COALESCE(watch_seen_at, ?) ELSE watch_seen_at END,
  -- Only claim the ratings are fresh when this import actually carried some.
  -- An id-only harvest (the browser helper knows the film but not its votes)
  -- must leave the film looking stale, or the free refresh through
  -- rating.kinopoisk.ru will skip it forever as already up to date.
  ratings_at    = CASE WHEN ? > 0 THEN ? ELSE ratings_at END
WHERE tmdb_id = ?`,
		info.KPID, info.Votes, info.Rating, info.Votes, info.Views,
		boolInt(info.OnlineKnown), boolInt(info.Online), info.Offer,
		jsonList(info.Services), jsonList(info.Services),
		boolInt(info.Online), nowRFC3339(),
		info.Votes, nowRFC3339(), tmdbID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// FindByKPID returns the movie already linked to this Kinopoisk id.
func (s *Store) FindByKPID(ctx context.Context, kpID int) (*model.Movie, error) {
	row := s.db.QueryRowContext(ctx, "SELECT "+movieCols+" FROM movies m WHERE m.kp_id = ?", kpID)
	m, err := scanMovie(row, false)
	if err != nil && err.Error() == "sql: no rows in result set" {
		return nil, nil
	}
	return m, err
}

// FindByIMDbID is the one cross-source link we never have to guess at.
func (s *Store) FindByIMDbID(ctx context.Context, imdbID string) (*model.Movie, error) {
	if imdbID == "" {
		return nil, nil
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+movieCols+" FROM movies m WHERE m.imdb_id = ?", imdbID)
	m, err := scanMovie(row, false)
	if err != nil && err.Error() == "sql: no rows in result set" {
		return nil, nil
	}
	return m, err
}

// FindByTitleYear matches on the Russian or English title within a year of the
// given one. Comparison is done in Go so it can normalise case, ё and
// punctuation the way SQLite cannot.
func (s *Store) FindByTitleYear(ctx context.Context, title string, year int) (*model.Movie, error) {
	if title == "" || year == 0 {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+movieCols+`
FROM movies m WHERE m.year BETWEEN ? AND ?`, year-1, year+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	want := NormalizeTitle(title)
	for rows.Next() {
		m, err := scanMovie(rows, false)
		if err != nil {
			return nil, err
		}
		if NormalizeTitle(m.TitleRU) == want || NormalizeTitle(m.Title) == want ||
			NormalizeTitle(m.OrigTitle) == want {
			return m, rows.Err()
		}
	}
	return nil, rows.Err()
}

// NormalizeTitle lowercases, folds ё to е and drops punctuation.
func NormalizeTitle(s string) string {
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
