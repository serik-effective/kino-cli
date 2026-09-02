package config

import "fmt"

// Tuning holds every number the ranking algorithm uses. Nothing in the scoring
// code may hardcode a coefficient: a reader must be able to see, and change,
// the whole model from one file.
type Tuning struct {
	Defaults   Defaults   `toml:"defaults"`
	Thresholds Thresholds `toml:"thresholds"`
	Weights    Weights    `toml:"weights"`
	Bayes      Bayes      `toml:"bayes"`
	Arthouse   Arthouse   `toml:"arthouse"`
	Signals    Signals    `toml:"signals"`
	GapPenalty GapPenalty `toml:"gap_penalty"`
	Kids       Kids       `toml:"kids"`
}

// Kids discounts films made for children.
//
// This is a taste setting, not a correction: family cinema is genuinely
// mainstream and its high ratings are real, they are simply given by the people
// it is made for. Left alone it dominates the Russian list, because family
// comedies are most of what reaches Russian streaming. Set penalty to 0 to rank
// them like anything else.
type Kids struct {
	Penalty float64 `toml:"penalty"`
	// Animation is discounted separately and more gently, because the tag says
	// less: a Russian cartoon is almost always for children, while an anime
	// feature usually is not. Genre tagging is uneven too — one film in a
	// franchise carries "семейный" and the next only "мультфильм" — so this
	// catches what the family tag misses.
	AnimationPenalty float64 `toml:"animation_penalty"`
}

type Defaults struct {
	// Period is the default window in days for a bare "kino".
	Period int `toml:"period"`
	// Limit is how many films the TOP list shows.
	Limit int `toml:"limit"`
	// RuPeriod is the window for "kino ru": Russian digital releases are sparse
	// enough that a week usually holds nothing worth ranking.
	RuPeriod int `toml:"ru_period"`
	// IMDbYearFloor drops older films when importing title.basics.
	IMDbYearFloor int `toml:"imdb_year_floor"`
}

// Thresholds gate which films may be ranked at all. A film qualifies if ANY of
// them is met: the point is a reliable sample, and where that sample lives
// differs by market. Russian cinema is rated on Kinopoisk, not IMDb — without
// MinKPVotes only 8 Russian films a year would clear the bar, against 116 with it.
type Thresholds struct {
	MinIMDbVotes int `toml:"min_imdb_votes"`
	MinTMDBVotes int `toml:"min_tmdb_votes"`
	MinKPVotes   int `toml:"min_kp_votes"`
	// MinRuntime drops shorts. Concerts and stand-up are excluded by IMDb
	// titleType at import time, not here.
	MinRuntime int `toml:"min_runtime"`
}

// Weights are the terms of the final score and must sum to 1.
type Weights struct {
	IMDb       float64 `toml:"imdb"`
	TMDB       float64 `toml:"tmdb"`
	Confidence float64 `toml:"confidence"`
	Mainstream float64 `toml:"mainstream"`
	Freshness  float64 `toml:"freshness"`
}

func (w Weights) Sum() float64 {
	return w.IMDb + w.TMDB + w.Confidence + w.Mainstream + w.Freshness
}

// Bayes smooths a rating towards the global mean in proportion to how few
// votes back it. M is the vote count at which a film's own rating carries half
// the weight; Mean is what it is pulled towards.
type Bayes struct {
	IMDbM    float64 `toml:"imdb_m"`
	IMDbMean float64 `toml:"imdb_mean"`
	TMDBM    float64 `toml:"tmdb_m"`
	TMDBMean float64 `toml:"tmdb_mean"`
	// KP is used only by "kino ru", never by the general ranking.
	KPM    float64 `toml:"kp_m"`
	KPMean float64 `toml:"kp_mean"`
}

// Arthouse turns an arthouse probability into a score penalty. Below Low there
// is none; above High the full penalty applies; in between it ramps linearly.
type Arthouse struct {
	Low        float64 `toml:"low"`
	High       float64 `toml:"high"`
	MaxPenalty float64 `toml:"max_penalty"`
}

// GapPenalty discounts a Kinopoisk rating that the wider world does not share.
//
// It applies to the Russian track only, where Kinopoisk is the ranking source.
// A film rated 8.2 at home and 6.0 abroad may be genuinely local in appeal, or
// its domestic number may be inflated; either way it is a weaker prediction of
// "you will enjoy this" than a rating both audiences agree on. Only a Kinopoisk
// rating ABOVE the international one is penalised: the reverse says nothing bad
// about a film.
//
// Nothing is penalised without a real sample on both sides, so the many Russian
// films with no international footprint at all are simply left alone. That is a
// known limit: we can only discount a disagreement we can see.
type GapPenalty struct {
	// Low is the gap, in rating points, below which nothing is taken off.
	Low float64 `toml:"low"`
	// High is where the full penalty applies; between the two it ramps.
	High       float64 `toml:"high"`
	MaxPenalty float64 `toml:"max_penalty"`
}

// Signals holds the sub-weights of the composite 0..1 scores. They live here
// for the same reason the headline weights do: the whole model must be
// readable and adjustable without touching Go.
type Signals struct {
	// ConfidenceFloor is the vote count mapped to 0 confidence, Ceiling the one
	// mapped to 1. The curve between them is logarithmic, so the step from 300
	// to 3000 votes counts for as much as 3000 to 30000.
	//
	// The ceiling sits above the spec's "100 000+ is very high" because at
	// 100 000 it stopped separating anything real: a Russian release with
	// 202 000 Kinopoisk votes and an animation with 73 000 on IMDb both pinned
	// to 1.00.
	ConfidenceFloor   float64 `toml:"confidence_floor"`
	ConfidenceCeiling float64 `toml:"confidence_ceiling"`
	// PopularityCeiling is the TMDB popularity treated as "as popular as it gets".
	PopularityCeiling float64 `toml:"popularity_ceiling"`

	// Mainstream mixes three signals; genre alone must never decide the result,
	// so a drama with a large audience can still score high.
	MainstreamGenre      float64 `toml:"mainstream_genre"`
	MainstreamVotes      float64 `toml:"mainstream_votes"`
	MainstreamPopularity float64 `toml:"mainstream_popularity"`

	// Arthouse signals. LowAudience dominates: a niche film is first of all a
	// film almost nobody rated.
	ArthouseLowAudience   float64 `toml:"arthouse_low_audience"`
	ArthouseGenre         float64 `toml:"arthouse_genre"`
	ArthouseLongRuntime   float64 `toml:"arthouse_long_runtime"`
	ArthouseLowPopularity float64 `toml:"arthouse_low_popularity"`
	// ArthouseMainstreamOffset is subtracted when the film carries a clearly
	// popular genre, so a well-attended thriller is never called arthouse.
	ArthouseMainstreamOffset float64 `toml:"arthouse_mainstream_offset"`
	// LongRuntimeMinutes is where "unusually long" begins.
	LongRuntimeMinutes int `toml:"long_runtime_minutes"`

	// GapPoints is how far two audiences must diverge, in rating points, before
	// the card says so. GapMinVotes is the sample each side needs before the
	// difference is worth reporting at all: 49 votes against 156 000 is noise,
	// not a disagreement.
	GapPoints   float64 `toml:"gap_points"`
	GapMinVotes int     `toml:"gap_min_votes"`
}

// DefaultTuning is the model as specified. Every value here is also the value a
// generated config.toml starts with.
func DefaultTuning() Tuning {
	return Tuning{
		Defaults: Defaults{
			Period:        7,
			Limit:         3,
			RuPeriod:      30,
			IMDbYearFloor: 2015,
		},
		Thresholds: Thresholds{
			MinIMDbVotes: 300,
			MinTMDBVotes: 100,
			MinKPVotes:   3000,
			MinRuntime:   70,
		},
		Weights: Weights{
			IMDb:       0.45,
			TMDB:       0.25,
			Confidence: 0.15,
			Mainstream: 0.10,
			Freshness:  0.05,
		},
		Bayes: Bayes{
			IMDbM:    5000,
			IMDbMean: 6.5,
			// TMDB has far fewer votes per film than IMDb, so the same M would
			// flatten every film to the mean.
			TMDBM:    500,
			TMDBMean: 6.5,
			// Kinopoisk vote counts run an order of magnitude above IMDb for
			// Russian releases, and its mean sits higher.
			//
			// Held at the same value as IMDb rather than higher: vote counts
			// scale with the size of the market, and a stricter m punished
			// smaller ones. A Kazakh release with 4 000 votes is as well
			// established at home as a Russian one with thirty times that, and
			// at 10 000 its own rating carried less than a third of the weight.
			KPM:    5000,
			KPMean: 6.7,
		},
		Arthouse: Arthouse{
			Low:        0.30,
			High:       0.60,
			MaxPenalty: 1.0,
		},
		GapPenalty: GapPenalty{
			Low:  1.0,
			High: 3.0,
			// Smaller than the arthouse penalty: a disagreement between
			// audiences is a caution, not a disqualification.
			MaxPenalty: 1.0,
		},
		Kids: Kids{Penalty: 1.0, AnimationPenalty: 0.5},
		Signals: Signals{
			ConfidenceFloor:   300,
			ConfidenceCeiling: 500000,
			PopularityCeiling: 200,

			MainstreamGenre:      0.40,
			MainstreamVotes:      0.35,
			MainstreamPopularity: 0.25,

			ArthouseLowAudience:      0.45,
			ArthouseGenre:            0.20,
			ArthouseLongRuntime:      0.15,
			ArthouseLowPopularity:    0.20,
			ArthouseMainstreamOffset: 0.30,
			LongRuntimeMinutes:       150,
			GapPoints:                1.5,
			GapMinVotes:              100,
		},
	}
}

// Validate catches a config that would silently skew every ranking.
func (t Tuning) Validate() error {
	if s := t.Weights.Sum(); s < 0.999 || s > 1.001 {
		return fmt.Errorf("weights must sum to 1.0, got %.3f "+
			"(imdb %.2f + tmdb %.2f + confidence %.2f + mainstream %.2f + freshness %.2f)",
			s, t.Weights.IMDb, t.Weights.TMDB, t.Weights.Confidence,
			t.Weights.Mainstream, t.Weights.Freshness)
	}
	if t.Bayes.IMDbM <= 0 || t.Bayes.TMDBM <= 0 || t.Bayes.KPM <= 0 {
		return fmt.Errorf("bayes m values must be positive")
	}
	if t.Arthouse.Low > t.Arthouse.High {
		return fmt.Errorf("arthouse.low (%.2f) must not exceed arthouse.high (%.2f)",
			t.Arthouse.Low, t.Arthouse.High)
	}
	if t.Defaults.Limit < 1 || t.Defaults.Period < 1 {
		return fmt.Errorf("defaults.limit and defaults.period must be at least 1")
	}
	if t.Signals.ConfidenceFloor <= 0 || t.Signals.ConfidenceCeiling <= t.Signals.ConfidenceFloor {
		return fmt.Errorf("signals.confidence_ceiling (%.0f) must exceed a positive confidence_floor (%.0f)",
			t.Signals.ConfidenceCeiling, t.Signals.ConfidenceFloor)
	}
	if t.GapPenalty.Low > t.GapPenalty.High {
		return fmt.Errorf("gap_penalty.low (%.2f) must not exceed gap_penalty.high (%.2f)",
			t.GapPenalty.Low, t.GapPenalty.High)
	}
	if t.Signals.PopularityCeiling <= 0 {
		return fmt.Errorf("signals.popularity_ceiling must be positive")
	}
	return nil
}
