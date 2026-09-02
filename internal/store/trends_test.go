package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/sbeysenov/kino-cli/internal/model"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// seedTrend stores one movie plus two releases of it, each with an old and a
// new snapshot, so the delta must aggregate across both releases.
func seedTrend(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	id := 42
	if _, err := s.UpsertMovie(ctx, &model.Movie{TMDBID: id, Title: "Mutiny"}); err != nil {
		t.Fatalf("upsert movie: %v", err)
	}

	old := time.Now().UTC().AddDate(0, 0, -10).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)
	for i, ext := range []string{"111", "222"} {
		if err := s.UpsertTorrent(ctx, model.Torrent{
			Source: "kinozal", ExtID: ext, RawTitle: "Мятеж / Mutiny / 2026 / WEB-DLRip",
			TitleRU: "Мятеж", TitleOrig: "Mutiny", Year: 2026,
		}); err != nil {
			t.Fatalf("upsert torrent: %v", err)
		}
		if err := s.SetMatch(ctx, "kinozal", ext, &id); err != nil {
			t.Fatalf("set match: %v", err)
		}
		base := 1000 * (i + 1)
		if err := s.SaveTorrentStats(ctx, []model.TorrentStat{
			{Source: "kinozal", ExtID: ext, CapturedAt: old, Rank: i + 1, Seeds: 10, Downloads: base},
			{Source: "kinozal", ExtID: ext, CapturedAt: recent, Rank: i + 1, Seeds: 20, Downloads: base + 150},
		}); err != nil {
			t.Fatalf("save stats: %v", err)
		}
	}
}

func TestTrendingAggregatesReleasesAndDelta(t *testing.T) {
	s := testStore(t)
	seedTrend(t, s)

	rows, err := s.Trending(context.Background(), TrendOpts{
		Source: "kinozal",
		Since:  time.Now().UTC().AddDate(0, 0, -7).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (two releases of one movie must collapse)", len(rows))
	}
	r := rows[0]
	if r.Releases != 2 {
		t.Errorf("releases = %d, want 2", r.Releases)
	}
	if r.Downloads != 1150+2150 {
		t.Errorf("downloads = %d, want %d", r.Downloads, 1150+2150)
	}
	if r.DownloadsDelta != 300 {
		t.Errorf("delta = %d, want 300 (150 per release)", r.DownloadsDelta)
	}
	if r.Seeds != 40 {
		t.Errorf("seeds = %d, want 40 (newest snapshot only)", r.Seeds)
	}
	if r.Movie == nil || r.Movie.TMDBID != 42 {
		t.Errorf("movie not joined: %+v", r.Movie)
	}
}

func TestTrendingSingleSnapshotHasZeroDelta(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	id := 7
	if _, err := s.UpsertMovie(ctx, &model.Movie{TMDBID: id, Title: "Solo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertTorrent(ctx, model.Torrent{Source: "kinozal", ExtID: "9", RawTitle: "Solo"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMatch(ctx, "kinozal", "9", &id); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTorrentStats(ctx, []model.TorrentStat{
		{Source: "kinozal", ExtID: "9", CapturedAt: time.Now().UTC().Format(time.RFC3339), Downloads: 500},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Trending(ctx, TrendOpts{Source: "kinozal"})
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(rows) != 1 || rows[0].DownloadsDelta != 0 || rows[0].Downloads != 500 {
		t.Fatalf("got %+v, want one row with delta 0 and 500 downloads", rows)
	}
}

func TestTrendingUnmatchedListsRawTitles(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if err := s.UpsertTorrent(ctx, model.Torrent{
		Source: "kinozal", ExtID: "5", RawTitle: "Неизвестное кино / 2026 / WEB-DLRip",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveTorrentStats(ctx, []model.TorrentStat{
		{Source: "kinozal", ExtID: "5", CapturedAt: time.Now().UTC().Format(time.RFC3339), Downloads: 12, Seeds: 3},
	}); err != nil {
		t.Fatal(err)
	}

	rows, err := s.Trending(ctx, TrendOpts{Source: "kinozal", Unmatched: true, Metric: "downloads"})
	if err != nil {
		t.Fatalf("trending: %v", err)
	}
	if len(rows) != 1 || rows[0].RawTitle == "" || rows[0].Movie != nil {
		t.Fatalf("got %+v, want one unmatched raw row", rows)
	}
}
