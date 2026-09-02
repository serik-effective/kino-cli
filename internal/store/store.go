// Package store owns the local SQLite database: migrations and all queries.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/sbeysenov/kino-cli/internal/model"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) migrate() error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	for i, name := range names {
		if i < version {
			continue
		}
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// UpsertMovie writes a movie, never overwriting an existing value with NULL.
// It reports whether the row was newly inserted.
func (s *Store) UpsertMovie(ctx context.Context, m *model.Movie) (bool, error) {
	var exists int
	err := s.db.QueryRowContext(ctx, "SELECT 1 FROM movies WHERE tmdb_id = ?", m.TMDBID).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	inserted := err == sql.ErrNoRows

	genres := jsonList(m.Genres)
	countries := jsonList(m.Countries)
	if m.UpdatedAt == "" {
		m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	const q = `
INSERT INTO movies (tmdb_id, imdb_id, kp_id, title, original_title, title_ru, year, runtime,
  overview, overview_ru, poster_path, genres, countries, orig_lang, popularity,
  tmdb_rating, tmdb_votes, imdb_rating, imdb_votes, metascore, rt_score, kp_rating, kp_votes,
  release_date, updated_at, enriched_at, ratings_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(tmdb_id) DO UPDATE SET
  imdb_id        = COALESCE(excluded.imdb_id, movies.imdb_id),
  kp_id          = COALESCE(excluded.kp_id, movies.kp_id),
  title          = COALESCE(NULLIF(excluded.title,''), movies.title),
  original_title = COALESCE(NULLIF(excluded.original_title,''), movies.original_title),
  title_ru       = COALESCE(NULLIF(excluded.title_ru,''), movies.title_ru),
  year           = COALESCE(excluded.year, movies.year),
  runtime        = COALESCE(excluded.runtime, movies.runtime),
  overview       = COALESCE(NULLIF(excluded.overview,''), movies.overview),
  overview_ru    = COALESCE(NULLIF(excluded.overview_ru,''), movies.overview_ru),
  poster_path    = COALESCE(NULLIF(excluded.poster_path,''), movies.poster_path),
  genres         = COALESCE(NULLIF(excluded.genres,''), movies.genres),
  countries      = COALESCE(NULLIF(excluded.countries,''), movies.countries),
  orig_lang      = COALESCE(NULLIF(excluded.orig_lang,''), movies.orig_lang),
  popularity     = COALESCE(excluded.popularity, movies.popularity),
  tmdb_rating    = COALESCE(excluded.tmdb_rating, movies.tmdb_rating),
  tmdb_votes     = COALESCE(excluded.tmdb_votes, movies.tmdb_votes),
  imdb_rating    = COALESCE(excluded.imdb_rating, movies.imdb_rating),
  imdb_votes     = COALESCE(excluded.imdb_votes, movies.imdb_votes),
  metascore      = COALESCE(excluded.metascore, movies.metascore),
  rt_score       = COALESCE(excluded.rt_score, movies.rt_score),
  kp_rating      = COALESCE(excluded.kp_rating, movies.kp_rating),
  kp_votes       = COALESCE(excluded.kp_votes, movies.kp_votes),
  updated_at     = excluded.updated_at,
  enriched_at    = COALESCE(excluded.enriched_at, movies.enriched_at),
  ratings_at     = COALESCE(excluded.ratings_at, movies.ratings_at),
  release_date   = COALESCE(NULLIF(excluded.release_date,''), movies.release_date)`

	_, err = s.db.ExecContext(ctx, q,
		m.TMDBID, nullStr(m.IMDbID), m.KPID, m.Title, m.OrigTitle, m.TitleRU, m.Year, m.Runtime,
		m.Overview, m.OverviewRU, m.PosterPath, genres, countries, m.OrigLang, m.Popularity,
		m.TMDBRating, m.TMDBVotes, m.IMDbRating, m.IMDbVotes, m.Metascore, m.RTScore, m.KPRating, m.KPVotes,
		m.ReleaseDate, m.UpdatedAt, nullStr(m.EnrichedAt), nullStr(m.RatingsAt))
	return inserted, err
}

func (s *Store) SaveReleases(ctx context.Context, rels []model.Release) error {
	if len(rels) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO releases (tmdb_id, region, type, date, note) VALUES (?,?,?,?,?)
		 ON CONFLICT(tmdb_id, region, type, date) DO UPDATE SET note = excluded.note`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rels {
		if _, err := stmt.ExecContext(ctx, r.TMDBID, r.Region, r.Type, r.Date, r.Note); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func jsonList(v []string) string {
	if len(v) == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func nowRFC3339() string { return time.Now().UTC().Format(time.RFC3339) }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

var movieCols = strings.Join([]string{
	"m.tmdb_id", "m.imdb_id", "m.kp_id", "m.title", "m.original_title", "m.title_ru", "m.year", "m.runtime",
	"m.overview", "m.overview_ru", "m.poster_path", "m.genres", "m.countries", "m.orig_lang", "m.popularity",
	"m.tmdb_rating", "m.tmdb_votes", "m.imdb_rating", "m.imdb_votes", "m.metascore", "m.rt_score",
	"m.kp_rating", "m.kp_votes", "m.kp_views", "m.watch_online", "m.watch_offer", "m.release_date", "m.updated_at", "m.enriched_at", "m.ratings_at",
}, ", ")

func scanMovie(rows interface{ Scan(...any) error }, withDate bool) (*model.Movie, error) {
	if withDate {
		var date sql.NullString
		m, err := scanMovieInto(rows, &date)
		if err != nil {
			return nil, err
		}
		m.MatchDate = date.String
		return m, nil
	}
	return scanMovieInto(rows)
}

// scanMovieInto scans the standard movie columns followed by extra destinations
// for whatever the query appended.
func scanMovieInto(rows interface{ Scan(...any) error }, extra ...any) (*model.Movie, error) {
	var m model.Movie
	var imdb, origTitle, titleRU, overview, overviewRU, poster, genres, countries, lang sql.NullString
	var releaseDate, enriched, ratingsAt, watchOffer sql.NullString
	var watchOnline sql.NullBool
	dst := []any{
		&m.TMDBID, &imdb, &m.KPID, &m.Title, &origTitle, &titleRU, &m.Year, &m.Runtime,
		&overview, &overviewRU, &poster, &genres, &countries, &lang, &m.Popularity,
		&m.TMDBRating, &m.TMDBVotes, &m.IMDbRating, &m.IMDbVotes, &m.Metascore, &m.RTScore,
		&m.KPRating, &m.KPVotes, &m.KPViews, &watchOnline, &watchOffer,
		&releaseDate, &m.UpdatedAt, &enriched, &ratingsAt,
	}
	dst = append(dst, extra...)
	if err := rows.Scan(dst...); err != nil {
		return nil, err
	}
	m.IMDbID, m.OrigTitle, m.TitleRU = imdb.String, origTitle.String, titleRU.String
	m.Overview, m.OverviewRU, m.PosterPath = overview.String, overviewRU.String, poster.String
	m.OrigLang, m.EnrichedAt = lang.String, enriched.String
	m.ReleaseDate, m.RatingsAt, m.WatchOffer = releaseDate.String, ratingsAt.String, watchOffer.String
	if watchOnline.Valid {
		v := watchOnline.Bool
		m.WatchOnline = &v
	}
	if genres.String != "" {
		_ = json.Unmarshal([]byte(genres.String), &m.Genres)
	}
	if countries.String != "" {
		_ = json.Unmarshal([]byte(countries.String), &m.Countries)
	}
	return &m, nil
}
