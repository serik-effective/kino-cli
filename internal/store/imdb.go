package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// imdbBatch is how many rows go into one transaction. The datasets have
// millions of rows, so committing per row would take hours; committing once at
// the end would hold a single write lock for the whole import.
const imdbBatch = 20000

// IMDbWriter streams rows into one of the IMDb tables, committing in batches.
// Callers must call Close exactly once; on error the current batch is rolled
// back and previously committed batches stay, which is safe because every write
// is an upsert.
type IMDbWriter struct {
	db    *sql.DB
	query string
	tx    *sql.Tx
	stmt  *sql.Stmt
	n     int
	total int
}

func (s *Store) NewRatingsWriter() *IMDbWriter {
	return &IMDbWriter{db: s.db, query: `
INSERT INTO imdb_ratings (imdb_id, rating, votes) VALUES (?, ?, ?)
ON CONFLICT(imdb_id) DO UPDATE SET rating = excluded.rating, votes = excluded.votes`}
}

func (s *Store) NewTitlesWriter() *IMDbWriter {
	return &IMDbWriter{db: s.db, query: `
INSERT INTO imdb_titles (imdb_id, title, orig_title, year, runtime, genres) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(imdb_id) DO UPDATE SET
  title      = excluded.title,
  orig_title = excluded.orig_title,
  year       = excluded.year,
  runtime    = excluded.runtime,
  genres     = excluded.genres`}
}

func (w *IMDbWriter) Add(ctx context.Context, args ...any) error {
	if w.tx == nil {
		if err := w.begin(ctx); err != nil {
			return err
		}
	}
	if _, err := w.stmt.ExecContext(ctx, args...); err != nil {
		return err
	}
	w.n++
	w.total++
	if w.n >= imdbBatch {
		return w.commit()
	}
	return nil
}

// Close flushes the open batch. It is safe to call after an error.
func (w *IMDbWriter) Close() error {
	if w.tx == nil {
		return nil
	}
	return w.commit()
}

func (w *IMDbWriter) Total() int { return w.total }

func (w *IMDbWriter) begin(ctx context.Context) error {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, w.query)
	if err != nil {
		tx.Rollback()
		return err
	}
	w.tx, w.stmt, w.n = tx, stmt, 0
	return nil
}

func (w *IMDbWriter) commit() error {
	err := w.tx.Commit()
	w.tx, w.stmt, w.n = nil, nil, 0
	return err
}

// MarkIMDbDataset records when a dataset was last imported and how many rows it
// contributed, so the CLI can say how stale the local copy is.
func (s *Store) MarkIMDbDataset(ctx context.Context, name string, rows int) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO imdb_datasets (name, updated_at, rows) VALUES (?, ?, ?)
ON CONFLICT(name) DO UPDATE SET updated_at = excluded.updated_at, rows = excluded.rows`,
		name, nowRFC3339(), rows)
	return err
}

type IMDbDataset struct {
	Name      string
	UpdatedAt time.Time
	Rows      int
}

// IMDbDatasets reports the local copies, newest first.
func (s *Store) IMDbDatasets(ctx context.Context) ([]IMDbDataset, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name, updated_at, rows FROM imdb_datasets ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []IMDbDataset
	for rows.Next() {
		var d IMDbDataset
		var ts string
		if err := rows.Scan(&d.Name, &ts, &d.Rows); err != nil {
			return nil, err
		}
		d.UpdatedAt, _ = time.Parse(time.RFC3339, ts)
		out = append(out, d)
	}
	return out, rows.Err()
}

// ApplyIMDbRatings copies the bulk ratings onto movies we track. It only fills
// rows whose imdb_id we already know, and it overwrites: the dataset is the
// authoritative audience rating, fresher than anything OMDb gave us earlier.
func (s *Store) ApplyIMDbRatings(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `
UPDATE movies SET
  imdb_rating = (SELECT rating FROM imdb_ratings r WHERE r.imdb_id = movies.imdb_id),
  imdb_votes  = (SELECT votes  FROM imdb_ratings r WHERE r.imdb_id = movies.imdb_id)
WHERE imdb_id IS NOT NULL
  AND EXISTS (SELECT 1 FROM imdb_ratings r WHERE r.imdb_id = movies.imdb_id)`)
	if err != nil {
		return 0, fmt.Errorf("apply imdb ratings: %w", err)
	}
	n, err := res.RowsAffected()
	return int(n), err
}
