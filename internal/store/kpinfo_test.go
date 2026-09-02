package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/serik-effective/kino-cli/internal/model"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func seed(t *testing.T, s *Store, m *model.Movie) {
	t.Helper()
	if _, err := s.UpsertMovie(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

func kpFields(t *testing.T, s *Store, tmdbID int) (rating float64, votes int) {
	t.Helper()
	var r, v any
	err := s.DB().QueryRow(`SELECT kp_rating, kp_votes FROM movies WHERE tmdb_id = ?`, tmdbID).Scan(&r, &v)
	if err != nil {
		t.Fatal(err)
	}
	if r != nil {
		rating = r.(float64)
	}
	if v != nil {
		votes = int(v.(int64))
	}
	return
}

// The browser helper reads a rating off a card and has no vote count for it.
// Such a number must never displace one that thousands of people produced: a
// misread digit would look exactly like a real change.
func TestVotelessRatingDoesNotOverwriteARatedFilm(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 1, Title: "Буратино"})

	if err := s.ApplyKPInfo(ctx, 1, KPInfo{KPID: 5354680, Rating: 7.0, Votes: 348894}); err != nil {
		t.Fatal(err)
	}
	// Now the same film arrives from the extension with a misread rating.
	if err := s.ApplyKPInfo(ctx, 1, KPInfo{KPID: 5354680, Rating: 2.0, Votes: 0}); err != nil {
		t.Fatal(err)
	}

	rating, votes := kpFields(t, s, 1)
	if rating != 7.0 {
		t.Errorf("kp_rating = %.1f, want the rated 7.0 to survive", rating)
	}
	if votes != 348894 {
		t.Errorf("kp_votes = %d, want 348894 untouched", votes)
	}
}

// When we know nothing yet, a voteless rating is better than none.
func TestVotelessRatingFillsAnEmptyField(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 2, Title: "Новый"})

	if err := s.ApplyKPInfo(ctx, 2, KPInfo{KPID: 111, Rating: 7.4, Votes: 0}); err != nil {
		t.Fatal(err)
	}
	rating, _ := kpFields(t, s, 2)
	if rating != 7.4 {
		t.Errorf("kp_rating = %.1f, want 7.4 to be recorded when nothing was known", rating)
	}
}

// A rating that does carry votes is authoritative and replaces what we had.
func TestRatedUpdateReplacesTheOldValue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 3, Title: "Растёт"})

	if err := s.ApplyKPInfo(ctx, 3, KPInfo{KPID: 222, Rating: 7.0, Votes: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyKPInfo(ctx, 3, KPInfo{KPID: 222, Rating: 7.6, Votes: 90000}); err != nil {
		t.Fatal(err)
	}
	rating, votes := kpFields(t, s, 3)
	if rating != 7.6 || votes != 90000 {
		t.Errorf("got %.1f / %d, want 7.6 / 90000", rating, votes)
	}
}

// An id-only harvest carries no ratings, so it must not mark the film as
// freshly rated: doing so hides it from the free refresh forever.
func TestIdOnlyImportLeavesTheFilmStale(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 4, Title: "Только id"})

	if err := s.ApplyKPInfo(ctx, 4, KPInfo{KPID: 333, Votes: 0}); err != nil {
		t.Fatal(err)
	}
	var ratingsAt any
	if err := s.DB().QueryRow(`SELECT ratings_at FROM movies WHERE tmdb_id = 4`).Scan(&ratingsAt); err != nil {
		t.Fatal(err)
	}
	if ratingsAt != nil {
		t.Errorf("ratings_at = %v, want NULL so the refresh still picks it up", ratingsAt)
	}

	// A real rating does update the timestamp.
	if err := s.ApplyKPInfo(ctx, 4, KPInfo{KPID: 333, Rating: 7.1, Votes: 5000}); err != nil {
		t.Fatal(err)
	}
	if err := s.DB().QueryRow(`SELECT ratings_at FROM movies WHERE tmdb_id = 4`).Scan(&ratingsAt); err != nil {
		t.Fatal(err)
	}
	if ratingsAt == nil {
		t.Error("ratings_at should be set once real ratings arrive")
	}
}

// The bug this guards: importing an id-only harvest wiped the availability flag
// for every film the Kinopoisk player payload had marked as streaming.
func TestIdOnlyImportDoesNotClearAvailability(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 5, Title: "Доступен"})

	if err := s.ApplyKPInfo(ctx, 5, KPInfo{
		KPID: 444, Votes: 1000, Online: true, OnlineKnown: true, Offer: "Плюс",
	}); err != nil {
		t.Fatal(err)
	}
	// Now the browser helper reports the same film knowing nothing about where
	// it can be watched.
	if err := s.ApplyKPInfo(ctx, 5, KPInfo{KPID: 444, OnlineKnown: false}); err != nil {
		t.Fatal(err)
	}

	var online int
	var offer string
	if err := s.DB().QueryRow(
		`SELECT watch_online, COALESCE(watch_offer,'') FROM movies WHERE tmdb_id = 5`).
		Scan(&online, &offer); err != nil {
		t.Fatal(err)
	}
	if online != 1 {
		t.Error("watch_online was cleared by a source that knows nothing about it")
	}
	if offer != "Плюс" {
		t.Errorf("watch_offer = %q, want it preserved", offer)
	}
}

// A source that does report availability may of course turn the flag off.
func TestReportedUnavailabilityDoesClearTheFlag(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seed(t, s, &model.Movie{TMDBID: 6, Title: "Ушёл из подписки"})

	if err := s.ApplyKPInfo(ctx, 6, KPInfo{KPID: 555, Online: true, OnlineKnown: true}); err != nil {
		t.Fatal(err)
	}
	if err := s.ApplyKPInfo(ctx, 6, KPInfo{KPID: 555, Online: false, OnlineKnown: true}); err != nil {
		t.Fatal(err)
	}
	var online int
	if err := s.DB().QueryRow(`SELECT watch_online FROM movies WHERE tmdb_id = 6`).Scan(&online); err != nil {
		t.Fatal(err)
	}
	if online != 0 {
		t.Error("a source that reports unavailability must be able to clear the flag")
	}
}
