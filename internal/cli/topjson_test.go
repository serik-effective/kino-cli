package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sbeysenov/kino-cli/internal/config"
	"github.com/sbeysenov/kino-cli/internal/score"
	"github.com/sbeysenov/kino-cli/internal/store"
)

func decodeTop(t *testing.T, out []ranked, o topOpts) jsonTop {
	t.Helper()
	var buf strings.Builder
	now := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	if err := writeTopJSON(&buf, out, o, now.AddDate(0, 0, -o.days), now, len(out)); err != nil {
		t.Fatal(err)
	}
	var got jsonTop
	if err := json.Unmarshal([]byte(buf.String()), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	return got
}

// A source nobody voted on must come back as null. An agent reading 0.0 as a
// rating would tell someone that viewers hated a film nobody has rated.
func TestUnratedSourcesAreNullNotZero(t *testing.T) {
	row := ranked{
		c: store.Candidate{
			TMDBID: 1, Title: "Test", TitleRU: "Тест",
			IMDbRating: 7.4, IMDbVotes: 12000,
			// No TMDB and no Kinopoisk votes at all.
		},
		b:      score.Breakdown{Final: 7.0, RatingPoints: 7.0, IMDbWeighted: 7.1},
		tuning: config.DefaultTuning(),
	}
	got := decodeTop(t, []ranked{row}, topOpts{days: 7, limit: 3})
	f := got.Films[0]
	if f.Ratings.IMDb.Rating == nil || *f.Ratings.IMDb.Rating != 7.4 {
		t.Errorf("imdb rating = %v, want 7.4", f.Ratings.IMDb.Rating)
	}
	if f.Ratings.TMDB.Rating != nil {
		t.Errorf("tmdb rating = %v, want null for a film with no TMDB votes", *f.Ratings.TMDB.Rating)
	}
	if f.Ratings.KP.Rating != nil {
		t.Errorf("kinopoisk rating = %v, want null", *f.Ratings.KP.Rating)
	}
	// The key must be present and null, not absent: an agent checking for the
	// field should find it and see "unknown".
	var buf strings.Builder
	now := time.Now()
	if err := writeTopJSON(&buf, []ranked{row}, topOpts{days: 7}, now, now, 1); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"rating": null`) {
		t.Error("an unrated source must serialise as an explicit null")
	}
}

// Availability is three-valued. "We never checked" must not serialise as
// "not available".
func TestAvailabilityKeepsUnknownDistinctFromNo(t *testing.T) {
	rows := []ranked{
		{c: store.Candidate{TMDBID: 1, Title: "unknown"}, tuning: config.DefaultTuning()},
		{c: store.Candidate{TMDBID: 2, Title: "absent", WatchKnown: true}, tuning: config.DefaultTuning()},
		{c: store.Candidate{TMDBID: 3, Title: "present", WatchKnown: true, WatchOnline: true}, tuning: config.DefaultTuning()},
	}
	got := decodeTop(t, rows, topOpts{days: 7})

	if got.Films[0].Availability.Online != nil {
		t.Error("an unchecked film must report null, not false")
	}
	if v := got.Films[1].Availability.Online; v == nil || *v {
		t.Errorf("a checked, unavailable film must report false, got %v", v)
	}
	if v := got.Films[2].Availability.Online; v == nil || !*v {
		t.Errorf("an available film must report true, got %v", v)
	}
}

// The whole point of shipping the breakdown is that it explains the score.
func TestPointsAddUpToTheScore(t *testing.T) {
	b := score.Breakdown{
		RatingPoints: 5.52, ConfidencePoints: 1.43,
		MainstreamPoints: 0.98, FreshnessPoints: 0.32,
		Penalty: 0.5, GapPenalty: 0.25, KidsPenalty: 0.5,
	}
	b.Final = b.RatingPoints + b.ConfidencePoints + b.MainstreamPoints + b.FreshnessPoints -
		b.Penalty - b.GapPenalty - b.KidsPenalty

	got := decodeTop(t, []ranked{{
		c: store.Candidate{TMDBID: 1, Title: "Test"}, b: b, tuning: config.DefaultTuning(),
	}}, topOpts{days: 7})

	p := got.Films[0].Why.Points
	sum := p.Rating + p.Confidence + p.Mainstream + p.Freshness -
		p.ArthousePenalty - p.GapPenalty - p.KidsPenalty
	if diff := sum - got.Films[0].Score; diff > 0.02 || diff < -0.02 {
		t.Errorf("terms sum to %.2f but score is %.2f", sum, got.Films[0].Score)
	}
}

// The window and track are what make a list of films interpretable at all.
func TestEnvelopeCarriesTheQuestion(t *testing.T) {
	got := decodeTop(t, nil, topOpts{days: 30, ru: true, genre: "комедия"})
	if got.Track != "russian" {
		t.Errorf("track = %q, want russian", got.Track)
	}
	if got.Days != 30 || got.From == "" || got.To == "" {
		t.Errorf("window is incomplete: %+v", got)
	}
	if got.Genre != "комедия" {
		t.Errorf("genre = %q", got.Genre)
	}
	if got.Notice == "" {
		t.Error("the Russian track must state that it ranks on Kinopoisk")
	}
	// An empty result is an empty list, never null: agents iterate it.
	if got.Films == nil {
		t.Error("films must serialise as [] when there are none")
	}
}
