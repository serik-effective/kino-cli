package pipeline

import (
	"testing"

	"github.com/sbeysenov/kino-cli/internal/source/tmdb"
)

func details() *tmdb.Details {
	d := &tmdb.Details{ID: 1}
	d.ReleaseDates.Results = []struct {
		Country      string             `json:"iso_3166_1"`
		ReleaseDates []tmdb.ReleaseDate `json:"release_dates"`
	}{
		{Country: "US", ReleaseDates: []tmdb.ReleaseDate{
			{Type: 3, ReleaseDate: "2026-07-01T00:00:00.000Z"},
			{Type: 4, ReleaseDate: "2026-08-25T00:00:00.000Z", Note: "Netflix"},
			{Type: 4, ReleaseDate: "2026-08-20T00:00:00.000Z"},
		}},
		{Country: "FR", ReleaseDates: []tmdb.ReleaseDate{
			{Type: 4, ReleaseDate: "2026-08-21T00:00:00.000Z"},
			{Type: 3, ReleaseDate: "2026-06-01T00:00:00.000Z"},
		}},
	}
	return d
}

func TestMatchDatePicksEarliestForRegion(t *testing.T) {
	got, ok := matchDate(details(), "US", []int{4})
	if !ok || got != "2026-08-20" {
		t.Fatalf("matchDate(US) = %q, %v; want 2026-08-20, true", got, ok)
	}
	if _, ok := matchDate(details(), "JP", []int{4}); ok {
		t.Fatal("matchDate(JP) = true; want false, no JP dates present")
	}
	got, ok = matchDate(details(), "US", []int{3})
	if !ok || got != "2026-07-01" {
		t.Fatalf("matchDate(US, theatrical) = %q, %v; want 2026-07-01, true", got, ok)
	}
}

func TestMatchDateAnyTypeUsesPrimaryDate(t *testing.T) {
	d := details()
	d.ReleaseDate = "2026-05-05"
	got, ok := matchDate(d, "US", nil)
	if !ok || got != "2026-05-05" {
		t.Fatalf("matchDate(any) = %q, %v; want 2026-05-05, true", got, ok)
	}
}

func TestReleasesOfKeepsRegionAndWorldwideDigital(t *testing.T) {
	got := releasesOf(details(), "US", []int{4})
	if len(got) != 4 {
		t.Fatalf("got %d releases, want 4 (3 US of any type + 1 FR digital): %+v", len(got), got)
	}
	for _, r := range got {
		if r.Region == "FR" && r.Type != 4 {
			t.Fatalf("kept non-digital foreign release %+v", r)
		}
		if len(r.Date) != 10 {
			t.Fatalf("date %q not trimmed to YYYY-MM-DD", r.Date)
		}
	}
}

func TestMergePrefersPrimaryReleaseDate(t *testing.T) {
	// TMDB localises discover's release_date to --region, so a re-release must
	// not overwrite the production year.
	cand := tmdb.DiscoverMovie{ID: 550, Title: "Fight Club", ReleaseDate: "2026-05-12"}
	det := details()
	det.ReleaseDate = "1999-10-15"

	m := merge(cand, det, "en-US")
	if m.ReleaseDate != "1999-10-15" {
		t.Errorf("release_date = %q, want the primary 1999-10-15", m.ReleaseDate)
	}
	if m.Year == nil || *m.Year != 1999 {
		t.Errorf("year = %v, want 1999", m.Year)
	}
}
