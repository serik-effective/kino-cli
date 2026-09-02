package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// CacheKey hashes a request URL. The raw URL is never stored: for some sources
// it carries the API key.
func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}

// Lookup returns a cached body and when it was fetched.
func (s *Store) Lookup(source, key string) ([]byte, time.Time, bool) {
	var body []byte
	var ts string
	err := s.db.QueryRow(
		`SELECT body, fetched_at FROM api_cache WHERE source = ? AND key = ?`,
		source, key).Scan(&body, &ts)
	if err != nil {
		return nil, time.Time{}, false
	}
	at, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return nil, time.Time{}, false
	}
	return body, at, true
}

func (s *Store) StoreResponse(source, key string, body []byte) error {
	_, err := s.db.Exec(`
INSERT INTO api_cache (source, key, body, fetched_at) VALUES (?, ?, ?, ?)
ON CONFLICT(source, key) DO UPDATE SET body = excluded.body, fetched_at = excluded.fetched_at`,
		source, key, body, nowRFC3339())
	return err
}

// PruneCache drops entries older than the given age and returns how many went.
func (s *Store) PruneCache(ctx context.Context, olderThan time.Duration) (int, error) {
	cutoff := time.Now().Add(-olderThan).UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_cache WHERE fetched_at < ?`, cutoff)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// CacheStats reports how much is cached and how old the oldest entry is.
func (s *Store) CacheStats(ctx context.Context) (rows int, oldest time.Time, err error) {
	var ts *string
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), MIN(fetched_at) FROM api_cache`).Scan(&rows, &ts)
	if err != nil || ts == nil {
		return rows, time.Time{}, err
	}
	oldest, _ = time.Parse(time.RFC3339, *ts)
	return rows, oldest, nil
}
