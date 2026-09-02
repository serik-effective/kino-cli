package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
)

type ListOpts struct {
	Region string
	// ReleaseTypes filters on the releases table; empty means "any release",
	// which falls back to the movie's primary release date.
	ReleaseTypes []int
	Since        string // YYYY-MM-DD, inclusive
	Until        string // YYYY-MM-DD, inclusive
	OrigLang     string
	Country      string // ISO 3166-1 production country
	RatingSource string // tmdb | imdb | kp
	MinRating    float64
	MinVotes     int
	Sort         string // date | rating | imdb | kp | popularity | votes | title
	Desc         bool
	Limit        int
}

var ratingCols = map[string][2]string{
	"tmdb": {"m.tmdb_rating", "m.tmdb_votes"},
	"imdb": {"m.imdb_rating", "m.imdb_votes"},
	"kp":   {"m.kp_rating", "m.kp_votes"},
}

// List returns movies released inside the window, matching the release types.
func (s *Store) List(ctx context.Context, o ListOpts) ([]*model.Movie, error) {
	cols, ok := ratingCols[o.RatingSource]
	if !ok {
		cols = ratingCols["tmdb"]
	}
	byRelease := len(o.ReleaseTypes) > 0

	var where []string
	var args []any
	dateCol := "m.release_date"
	if byRelease {
		dateCol = "MIN(r.date)"
		ph := make([]string, len(o.ReleaseTypes))
		for i, t := range o.ReleaseTypes {
			ph[i] = "?"
			args = append(args, t)
		}
		where = append(where, "r.type IN ("+strings.Join(ph, ",")+")")
		if o.Region != "" && !strings.EqualFold(o.Region, "any") {
			where = append(where, "r.region = ?")
			args = append(args, strings.ToUpper(o.Region))
		}
		if o.Since != "" {
			where = append(where, "r.date >= ?")
			args = append(args, o.Since)
		}
		if o.Until != "" {
			where = append(where, "r.date <= ?")
			args = append(args, o.Until)
		}
	} else {
		if o.Since != "" {
			where = append(where, "m.release_date >= ?")
			args = append(args, o.Since)
		}
		if o.Until != "" {
			where = append(where, "m.release_date <= ?")
			args = append(args, o.Until)
		}
	}
	if o.OrigLang != "" {
		where = append(where, "m.orig_lang = ?")
		args = append(args, strings.ToLower(o.OrigLang))
	}
	if o.Country != "" {
		// countries is a JSON array of ISO codes; quoting keeps "US" from
		// matching a substring of another value.
		where = append(where, "m.countries LIKE ?")
		args = append(args, `%"`+strings.ToUpper(o.Country)+`"%`)
	}
	if o.MinRating > 0 {
		where = append(where, cols[0]+" >= ?")
		args = append(args, o.MinRating)
	}
	if o.MinVotes > 0 {
		where = append(where, cols[1]+" >= ?")
		args = append(args, o.MinVotes)
	}
	if len(where) == 0 {
		where = append(where, "1 = 1")
	}

	order := "match_date"
	switch o.Sort {
	case "", "date":
	case "rating":
		order = cols[0]
	case "tmdb":
		order = "m.tmdb_rating"
	case "imdb":
		order = "m.imdb_rating"
	case "kp":
		order = "m.kp_rating"
	case "popularity":
		order = "m.popularity"
	case "votes":
		order = cols[1]
	case "title":
		order = "m.title"
	default:
		return nil, fmt.Errorf("unknown sort %q", o.Sort)
	}
	dir := "ASC"
	if o.Desc {
		dir = "DESC"
	}
	limit := ""
	if o.Limit > 0 {
		limit = " LIMIT " + strconv.Itoa(o.Limit)
	}

	join, group := "", ""
	if byRelease {
		join = " JOIN releases r ON r.tmdb_id = m.tmdb_id"
		group = " GROUP BY m.tmdb_id"
	}
	q := fmt.Sprintf("SELECT %s, %s AS match_date\nFROM movies m%s\nWHERE %s%s ORDER BY %s %s NULLS LAST%s",
		movieCols, dateCol, join, strings.Join(where, " AND "), group, order, dir, limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Movie
	for rows.Next() {
		m, err := scanMovie(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Get finds one movie by tmdb id or imdb id.
func (s *Store) Get(ctx context.Context, tmdbID int, imdbID string) (*model.Movie, error) {
	q := "SELECT " + movieCols + " FROM movies m WHERE "
	var args []any
	if tmdbID > 0 {
		q += "m.tmdb_id = ?"
		args = append(args, tmdbID)
	} else {
		q += "m.imdb_id = ?"
		args = append(args, imdbID)
	}
	row := s.db.QueryRowContext(ctx, q, args...)
	m, err := scanMovie(row, false)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (s *Store) Releases(ctx context.Context, tmdbID int) ([]model.Release, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT tmdb_id, region, type, date, COALESCE(note,'') FROM releases WHERE tmdb_id = ? ORDER BY date, type", tmdbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Release
	for rows.Next() {
		var r model.Release
		if err := rows.Scan(&r.TMDBID, &r.Region, &r.Type, &r.Date, &r.Note); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingEnrich returns movies that have an imdb id and were never enriched,
// or whose enrichment is older than staleDays.
func (s *Store) PendingEnrich(ctx context.Context, limit, staleDays int) ([]*model.Movie, error) {
	cutoff := time.Now().UTC().AddDate(0, 0, -staleDays).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, "SELECT "+movieCols+`
FROM movies m WHERE m.imdb_id IS NOT NULL AND (m.enriched_at IS NULL OR m.enriched_at < ?)
ORDER BY m.enriched_at IS NOT NULL, m.popularity DESC NULLS LAST LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Movie
	for rows.Next() {
		m, err := scanMovie(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// PendingRatings returns movies with a known Kinopoisk id whose ratings were
// never fetched or are older than staleHours. Refreshing those is quota-free.
func (s *Store) PendingRatings(ctx context.Context, limit, staleHours int) ([]*model.Movie, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(staleHours) * time.Hour).Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, "SELECT "+movieCols+`
FROM movies m WHERE m.kp_id IS NOT NULL AND (m.ratings_at IS NULL OR m.ratings_at < ?)
ORDER BY m.ratings_at IS NOT NULL, m.ratings_at, m.popularity DESC NULLS LAST LIMIT ?`, cutoff, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Movie
	for rows.Next() {
		m, err := scanMovie(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MissingKPID lists IMDb ids of movies whose Kinopoisk id is still unknown.
func (s *Store) MissingKPID(ctx context.Context, limit int) ([]string, error) {
	q := `SELECT imdb_id FROM movies WHERE imdb_id IS NOT NULL AND kp_id IS NULL
	      ORDER BY popularity DESC NULLS LAST`
	if limit > 0 {
		q += " LIMIT " + strconv.Itoa(limit)
	}
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// SetKPIDs writes resolved Kinopoisk ids, skipping any already taken by another
// movie so a bad mapping cannot overwrite a good one.
func (s *Store) SetKPIDs(ctx context.Context, byIMDb map[string]int) (int, error) {
	if len(byIMDb) == 0 {
		return 0, nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	stmt, err := tx.PrepareContext(ctx, `UPDATE movies SET kp_id = ?
		WHERE imdb_id = ? AND kp_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM movies m2 WHERE m2.kp_id = ?)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	n := 0
	for imdb, kp := range byIMDb {
		res, err := stmt.ExecContext(ctx, kp, imdb, kp)
		if err != nil {
			return n, err
		}
		if affected, _ := res.RowsAffected(); affected > 0 {
			n++
		}
	}
	return n, tx.Commit()
}

func (s *Store) Count(ctx context.Context, table string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n)
	return n, err
}

// --- quota ---

func today() string { return time.Now().UTC().Format("2006-01-02") }

func (s *Store) AddQuota(source string, n int) error {
	_, err := s.db.Exec(`INSERT INTO quota_usage (source, day, calls) VALUES (?,?,?)
		ON CONFLICT(source, day) DO UPDATE SET calls = calls + excluded.calls`, source, today(), n)
	return err
}

func (s *Store) QuotaToday(source string) (int, error) {
	var n int
	err := s.db.QueryRow("SELECT calls FROM quota_usage WHERE source = ? AND day = ?", source, today()).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// --- sync runs ---

func (s *Store) StartRun(ctx context.Context, command string, params any) (int64, error) {
	b, _ := json.Marshal(params)
	res, err := s.db.ExecContext(ctx, "INSERT INTO sync_runs (started_at, command, params) VALUES (?,?,?)",
		time.Now().UTC().Format(time.RFC3339), command, string(b))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

type RunResult struct {
	Found    int            `json:"found"`
	Inserted int            `json:"inserted"`
	Updated  int            `json:"updated"`
	Skipped  int            `json:"skipped"`
	APICalls map[string]int `json:"api_calls"`
	Err      string         `json:"error,omitempty"`
}

func (s *Store) FinishRun(ctx context.Context, id int64, r RunResult) error {
	calls, _ := json.Marshal(r.APICalls)
	_, err := s.db.ExecContext(ctx, `UPDATE sync_runs SET finished_at=?, found=?, inserted=?, updated=?,
		skipped=?, api_calls=?, error=? WHERE id=?`,
		time.Now().UTC().Format(time.RFC3339), r.Found, r.Inserted, r.Updated, r.Skipped,
		string(calls), nullStr(r.Err), id)
	return err
}
