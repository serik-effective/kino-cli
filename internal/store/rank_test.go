package store

import (
	"context"
	"testing"
	"time"

	"github.com/serik-effective/kino-cli/internal/model"
)

func ptrInt(v int) *int { return &v }

// A catalogue title arriving on a new streaming service is not a new film.
//
// The general track already takes the EARLIEST type-4 release to guard against
// this, and that is not enough: discovery only walks recent windows, so a 2015
// film whose 2026 release is the only row we ever stored has a "minimum" of
// 2026. The gap to the production year is the only signal that survives an
// incomplete release history.
func TestMaxYearGapDropsCatalogueReleases(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	films := []struct {
		id      int
		title   string
		year    int
		digital string
	}{
		{1, "новый", 2026, "2026-08-20"},
		{2, "прошлогодний", 2025, "2026-08-20"}, // gap 1: normal
		{3, "задержанный", 2024, "2026-08-20"},  // gap 2: delayed indie, kept
		{4, "переиздание", 2015, "2026-08-20"},  // gap 11: catalogue
		{5, "классика", 1962, "2026-08-20"},     // gap 64: catalogue
		{6, "без года", 0, "2026-08-20"},        // unknown year: kept
	}
	for _, f := range films {
		m := &model.Movie{
			TMDBID: f.id, Title: f.title,
			Runtime: ptrInt(100), IMDbVotes: ptrInt(5000),
		}
		if f.year > 0 {
			m.Year = ptrInt(f.year)
		}
		seed(t, s, m)
		if err := s.SaveReleases(ctx, []model.Release{
			{TMDBID: f.id, Region: "US", Type: 4, Date: f.digital},
		}); err != nil {
			t.Fatal(err)
		}
	}

	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)

	got := titlesOf(t, s, CandidateQuery{From: from, To: to, MaxYearGap: 2})
	want := map[string]bool{"новый": true, "прошлогодний": true, "задержанный": true, "без года": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want exactly %v", got, keysOf(want))
	}
	for _, title := range got {
		if !want[title] {
			t.Errorf("%q should have been dropped as a re-release", title)
		}
	}

	// Zero disables the check — that is what --reissues asks for.
	if all := titlesOf(t, s, CandidateQuery{From: from, To: to}); len(all) != len(films) {
		t.Errorf("without a cutoff all %d films must be returned, got %d: %v",
			len(films), len(all), all)
	}
}

func titlesOf(t *testing.T, s *Store, q CandidateQuery) []string {
	t.Helper()
	cands, err := s.Candidates(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.Title)
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
