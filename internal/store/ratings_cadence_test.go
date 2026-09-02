package store

import (
	"context"
	"testing"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
)

// A rating moves fastest right after release and barely at all years later.
// Refreshing everything on one schedule spends the run's budget re-reading
// numbers that did not change, while last week's release waits behind a film
// from 1962.
func TestRatingStalenessScalesWithAge(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	ago := func(days int) string { return now.AddDate(0, 0, -days).Format("2006-01-02") }
	refreshed := func(hoursAgo int) string {
		return now.Add(-time.Duration(hoursAgo) * time.Hour).Format(time.RFC3339)
	}

	films := []struct {
		id       int
		title    string
		released string
		// hours since the rating was last read
		checked int
	}{
		{1, "свежий", ago(5), 30},                // 30 h > base 24 h  -> due
		{2, "недавний", ago(60), 30},             // needs 72 h        -> not due
		{3, "недавний-старый", ago(60), 100},     // 100 h > 72 h -> due
		{4, "годовалый", ago(200), 100},          // needs 336 h  -> not due
		{5, "классика", ago(9000), 1000},         // needs 1440 h -> not due
		{6, "классика-забытая", ago(9000), 2000}, // > 1440 h -> due
	}
	for _, f := range films {
		seed(t, s, &model.Movie{
			TMDBID: f.id, Title: f.title, KPID: ptrInt(f.id * 100),
			ReleaseDate: f.released,
		})
		if _, err := s.db.ExecContext(ctx,
			"UPDATE movies SET ratings_at = ? WHERE tmdb_id = ?", refreshed(f.checked), f.id); err != nil {
			t.Fatal(err)
		}
	}

	due, err := s.PendingRatings(ctx, 100, 24)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, m := range due {
		got[m.Title] = true
	}

	want := map[string]bool{"свежий": true, "недавний-старый": true, "классика-забытая": true}
	for title := range want {
		if !got[title] {
			t.Errorf("%q should be due for a refresh", title)
		}
	}
	for _, title := range []string{"недавний", "годовалый", "классика"} {
		if got[title] {
			t.Errorf("%q was refreshed too eagerly — its rating barely moves", title)
		}
	}
}

// Within one run the freshest films come first: they are what a recommendation
// about the last week is made of.
func TestFreshFilmsAreRefreshedFirst(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, f := range []struct {
		id    int
		title string
		days  int
	}{
		{1, "классика", 9000},
		{2, "свежий", 3},
		{3, "годовалый", 300},
	} {
		seed(t, s, &model.Movie{
			TMDBID: f.id, Title: f.title, KPID: ptrInt(f.id * 100),
			ReleaseDate: now.AddDate(0, 0, -f.days).Format("2006-01-02"),
		})
	}

	due, err := s.PendingRatings(ctx, 3, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(due) != 3 {
		t.Fatalf("got %d films, want 3 — none has ever been read", len(due))
	}
	if due[0].Title != "свежий" {
		t.Errorf("first in line is %q, want the freshest film", due[0].Title)
	}
	if due[2].Title != "классика" {
		t.Errorf("last in line is %q, want the oldest film", due[2].Title)
	}
}
