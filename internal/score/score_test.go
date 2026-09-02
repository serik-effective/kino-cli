package score

import (
	"math"
	"testing"
	"time"

	"github.com/serik-effective/kino-cli/internal/config"
)

func tuning() config.Tuning { return config.DefaultTuning() }

var now = time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)

// base is a plainly mainstream, well-rated, freshly released film.
func base() Input {
	return Input{
		IMDbRating: 7.5, IMDbVotes: 50000,
		TMDBRating: 7.6, TMDBVotes: 3000,
		Popularity: 80,
		Runtime:    118,
		Genres:     []string{"Thriller"},
		Released:   now.AddDate(0, 0, -1),
		Now:        now,
		Window:     7 * 24 * time.Hour,
	}
}

func approx(t *testing.T, name string, got, want, tol float64) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %.4f, want %.4f ± %.4f", name, got, want, tol)
	}
}

func TestBayesianPullsThinRatingsTowardsTheMean(t *testing.T) {
	// The spec's own example: a big sample keeps its rating, a small one does not.
	approx(t, "50k votes", bayesian(7.5, 50000, 5000, 6.5), 7.409, 0.01)
	approx(t, "300 votes", bayesian(8.4, 300, 5000, 6.5), 6.607, 0.01)
	// No votes at all must not invent a rating above the mean.
	approx(t, "no votes", bayesian(9.9, 0, 5000, 6.5), 6.5, 0.0001)
}

// The core promise of the whole tool.
func TestDependableSevenBeatsThinEight(t *testing.T) {
	dependable := base()
	dependable.IMDbRating, dependable.IMDbVotes = 7.5, 50000

	thin := base()
	thin.IMDbRating, thin.IMDbVotes = 8.4, 400
	thin.TMDBVotes = 120
	thin.Popularity = 8

	d := Score(dependable, tuning(), General)
	th := Score(thin, tuning(), General)
	if d.Final <= th.Final {
		t.Errorf("7.5/50k scored %.2f, 8.4/400 scored %.2f — the dependable one must win",
			d.Final, th.Final)
	}
}

func TestEligibilityAcceptsAnySingleSource(t *testing.T) {
	tn := tuning()
	cases := []struct {
		name string
		in   Input
		want bool
	}{
		{"imdb only", Input{IMDbVotes: 300}, true},
		{"tmdb only", Input{TMDBVotes: 100}, true},
		// The reason min_kp_votes exists: Russian films are rated on Kinopoisk.
		{"kp only", Input{KPVotes: 3000}, true},
		{"all below", Input{IMDbVotes: 299, TMDBVotes: 99, KPVotes: 2999}, false},
	}
	for _, c := range cases {
		if got := Eligible(c.in, tn); got != c.want {
			t.Errorf("%s: Eligible = %v, want %v", c.name, got, c.want)
		}
	}
}

// A source with no votes must be dropped, not smoothed to the mean and mixed in
// at full weight.
func TestMissingSourceRedistributesItsWeight(t *testing.T) {
	in := base()
	in.TMDBVotes, in.TMDBRating = 0, 0

	b := Score(in, tuning(), General)
	if len(b.RatingSources) != 1 || b.RatingSources[0] != "imdb" {
		t.Fatalf("RatingSources = %v, want [imdb]", b.RatingSources)
	}
	if b.TMDBWeighted != 0 {
		t.Errorf("TMDBWeighted = %.2f, want 0 for a source with no votes", b.TMDBWeighted)
	}
	// IMDb should now carry the full 0.70 that imdb+tmdb share.
	both := Score(base(), tuning(), General)
	if b.Final <= 0 || math.Abs(b.Final-both.Final) > 1.0 {
		t.Errorf("dropping TMDB moved the score too far: %.2f vs %.2f", b.Final, both.Final)
	}
}

func TestConfidenceIsLogarithmic(t *testing.T) {
	tn := tuning()
	// Read the ends of the scale from the config rather than restating them:
	// a test that hardcodes a coefficient breaks every time it is tuned, which
	// says nothing about whether the behaviour is still right.
	floor := tn.Signals.ConfidenceFloor
	ceiling := tn.Signals.ConfidenceCeiling

	approx(t, "at the floor", confidence(floor, tn), 0, 0.001)
	approx(t, "at the ceiling", confidence(ceiling, tn), 1, 0.001)
	approx(t, "beyond the ceiling", confidence(ceiling*10, tn), 1, 0.001)

	// Equal ratios must give equal steps; that is what "logarithmic" buys us.
	c1 := confidence(floor*10, tn)
	c2 := confidence(floor*100, tn)
	approx(t, "one decade vs the next", c1-confidence(floor, tn), c2-c1, 0.001)

	if c2 <= c1 || c1 <= confidence(floor, tn) {
		t.Error("confidence must rise with votes")
	}
}

// The ceiling has to sit above the biggest real audiences, or films that differ
// by hundreds of thousands of votes all pin to the same value.
func TestConfidenceStillSeparatesLargeAudiences(t *testing.T) {
	tn := tuning()
	small := confidence(73000, tn)  // an animation on IMDb
	large := confidence(202000, tn) // a Russian release on Kinopoisk
	if large-small < 0.05 {
		t.Errorf("73k scored %.3f and 202k scored %.3f — the ceiling is too low to tell them apart",
			small, large)
	}
}

// Explicitly forbidden by the spec: Drama must not be treated as arthouse.
func TestPopularDramaIsNotArthouse(t *testing.T) {
	in := base()
	in.Genres = []string{"Drama"}
	in.IMDbVotes = 200000
	in.Popularity = 150

	b := Score(in, tuning(), General)
	if b.Arthouse > tuning().Arthouse.Low {
		t.Errorf("arthouse = %.2f for a drama 200k people rated; must stay below %.2f",
			b.Arthouse, tuning().Arthouse.Low)
	}
	if b.Penalty != 0 {
		t.Errorf("penalty = %.2f, want 0", b.Penalty)
	}
}

// A drama with the same genre but no audience is a different matter.
func TestUnwatchedDocumentaryScoresArthouse(t *testing.T) {
	in := base()
	in.Genres = []string{"Documentary", "History"}
	in.IMDbVotes, in.TMDBVotes = 400, 20
	in.Popularity = 3
	in.Runtime = 210

	b := Score(in, tuning(), General)
	if b.Arthouse < tuning().Arthouse.High {
		t.Errorf("arthouse = %.2f, want at least %.2f", b.Arthouse, tuning().Arthouse.High)
	}
	if b.Penalty != tuning().Arthouse.MaxPenalty {
		t.Errorf("penalty = %.2f, want the full %.2f", b.Penalty, tuning().Arthouse.MaxPenalty)
	}
}

// A documentary a million people rated must not be called niche.
func TestWidelyWatchedDocumentaryEscapesThePenalty(t *testing.T) {
	in := base()
	in.Genres = []string{"Documentary"}
	in.IMDbVotes = 1000000
	in.Popularity = 180

	b := Score(in, tuning(), General)
	if b.Penalty >= tuning().Arthouse.MaxPenalty {
		t.Errorf("penalty = %.2f for a documentary with a million votes", b.Penalty)
	}
}

func TestPenaltyRampsBetweenTheThresholds(t *testing.T) {
	a := tuning().Arthouse
	approx(t, "below low", penalty(0.10, a), 0, 0.0001)
	approx(t, "midpoint", penalty(0.45, a), a.MaxPenalty/2, 0.01)
	approx(t, "above high", penalty(0.90, a), a.MaxPenalty, 0.0001)
}

func TestFreshnessFadesAcrossTheWindow(t *testing.T) {
	in := base()
	in.Released = now
	approx(t, "released today", freshness(in), 1, 0.001)

	in.Released = now.AddDate(0, 0, -7)
	approx(t, "at the window edge", freshness(in), 0, 0.001)

	in.Released = now.AddDate(0, 0, -14)
	approx(t, "older than the window", freshness(in), 0, 0.001)
}

// The Russian mode exists so Kinopoisk-rated films can be ranked at all.
func TestRussianModeRanksOnKinopoisk(t *testing.T) {
	in := Input{
		KPRating: 7.8, KPVotes: 200000,
		Popularity: 20,
		Runtime:    104,
		Genres:     []string{"Comedy"},
		Released:   now.AddDate(0, 0, -2),
		Now:        now,
		Window:     30 * 24 * time.Hour,
	}
	general := Score(in, tuning(), General)
	russian := Score(in, tuning(), Russian)

	if len(general.RatingSources) != 0 {
		t.Errorf("general mode must not use Kinopoisk, got sources %v", general.RatingSources)
	}
	if len(russian.RatingSources) != 1 || russian.RatingSources[0] != "kp" {
		t.Fatalf("russian RatingSources = %v, want [kp]", russian.RatingSources)
	}
	if russian.Final <= general.Final {
		t.Errorf("russian %.2f should beat general %.2f when only KP has data",
			russian.Final, general.Final)
	}
}

func TestScoreStaysInRange(t *testing.T) {
	cases := []Input{
		{}, // nothing known at all
		base(),
		func() Input { in := base(); in.IMDbRating, in.IMDbVotes = 10, 5000000; return in }(),
		func() Input { in := base(); in.IMDbRating, in.IMDbVotes = 1, 5000000; in.Popularity = 0; return in }(),
	}
	for i, in := range cases {
		for _, m := range []Mode{General, Russian} {
			b := Score(in, tuning(), m)
			if b.Final < 0 || b.Final > 10 || math.IsNaN(b.Final) {
				t.Errorf("case %d mode %d: final = %v, must be within 0..10", i, m, b.Final)
			}
		}
	}
}

// A Kinopoisk rating the wider world does not share is a weaker prediction of
// enjoyment than one both audiences agree on.
func TestGapPenaltyDiscountsDomesticEnthusiasm(t *testing.T) {
	tn := tuning()
	base := Input{
		KPRating: 8.2, KPVotes: 202000,
		IMDbRating: 6.0, IMDbVotes: 5000,
		Genres: []string{"Drama"}, Runtime: 103,
		Released: now.AddDate(0, 0, -5), Now: now, Window: 30 * 24 * time.Hour,
	}
	b := Score(base, tn, Russian)
	if b.GapPenalty <= 0 {
		t.Errorf("a 2.2 point gap must cost something, got %.2f", b.GapPenalty)
	}

	// The same film, agreed on abroad, must score higher.
	agreed := base
	agreed.IMDbRating = 7.9
	a := Score(agreed, tn, Russian)
	if a.Final <= b.Final {
		t.Errorf("agreement %.2f should beat disagreement %.2f", a.Final, b.Final)
	}
	if a.GapPenalty != 0 {
		t.Errorf("a 0.3 gap is below the threshold, got penalty %.2f", a.GapPenalty)
	}
}

// The reverse direction says nothing bad about a film and must not be punished.
func TestHigherInternationalRatingIsNotPenalised(t *testing.T) {
	in := Input{
		KPRating: 6.5, KPVotes: 50000,
		IMDbRating: 8.4, IMDbVotes: 90000,
		Released: now, Now: now, Window: 30 * 24 * time.Hour,
	}
	b := Score(in, tuning(), Russian)
	if b.GapPenalty != 0 {
		t.Errorf("penalty %.2f for being liked more abroad", b.GapPenalty)
	}
}

// 49 international votes against 156 000 domestic is not a disagreement between
// audiences, and treating it as one would punish a film for being unknown.
func TestTinyInternationalSampleIsNotADisagreement(t *testing.T) {
	in := Input{
		KPRating: 8.3, KPVotes: 156000,
		IMDbRating: 2.9, IMDbVotes: 49,
		Released: now, Now: now, Window: 30 * 24 * time.Hour,
	}
	b := Score(in, tuning(), Russian)
	if b.GapPenalty != 0 {
		t.Errorf("penalty %.2f on a 49-vote sample", b.GapPenalty)
	}
}

// The general track never uses Kinopoisk, so it must never apply this penalty.
func TestGapPenaltyIsRussianTrackOnly(t *testing.T) {
	in := Input{
		KPRating: 8.9, KPVotes: 300000,
		IMDbRating: 5.0, IMDbVotes: 40000,
		TMDBRating: 6.0, TMDBVotes: 900,
		Released: now, Now: now, Window: 30 * 24 * time.Hour,
	}
	if b := Score(in, tuning(), General); b.GapPenalty != 0 {
		t.Errorf("general track applied a Kinopoisk penalty: %.2f", b.GapPenalty)
	}
}

// The catalogue stores genres in the configured language, so the signals have to
// recognise them there. Keying on English alone left every genre signal dead in
// production while the tests, written in English, kept passing.
func TestGenreSignalsWorkInBothLanguages(t *testing.T) {
	tn := tuning()
	pairs := []struct{ en, ru string }{
		{"Crime", "криминал"},
		{"Comedy", "комедия"},
		{"Horror", "ужасы"},
		{"Documentary", "документальный"},
		{"Drama", "драма"},
	}
	for _, p := range pairs {
		in := base()
		in.Genres = []string{p.en}
		en := Score(in, tn, General)

		in.Genres = []string{p.ru}
		ru := Score(in, tn, General)

		if math.Abs(en.Mainstream-ru.Mainstream) > 0.001 {
			t.Errorf("%s/%s: mainstream %.3f vs %.3f", p.en, p.ru, en.Mainstream, ru.Mainstream)
		}
		if math.Abs(en.Arthouse-ru.Arthouse) > 0.001 {
			t.Errorf("%s/%s: arthouse %.3f vs %.3f", p.en, p.ru, en.Arthouse, ru.Arthouse)
		}
	}
}

// A crime drama is popular cinema, not a festival entry.
func TestRussianCrimeGenreEarnsItsMainstreamCredit(t *testing.T) {
	tn := tuning()
	in := base()
	in.Genres = []string{"криминал", "драма"}
	in.IMDbVotes, in.TMDBVotes = 0, 0
	in.KPRating, in.KPVotes = 8.0, 4292
	in.Popularity = 3

	b := Score(in, tn, Russian)
	if b.Mainstream < 0.4 {
		t.Errorf("mainstream = %.2f, want the crime genre to count", b.Mainstream)
	}
	if b.Penalty > 0 {
		t.Errorf("penalty = %.2f — a crime drama must not be treated as arthouse", b.Penalty)
	}
}
