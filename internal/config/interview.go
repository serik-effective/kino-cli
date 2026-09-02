package config

import "math"

// Опрос вкуса: ответы человека -> коэффициенты ранжирования.
//
// Живёт в config, а не в cli, по той же причине, что и score: отображение
// «ответ -> число» проверяется таблицей, а терминальный ввод — нет. cli задаёт
// вопросы, здесь считаются последствия.

// Taste is how far from the multiplex the viewer is willing to go.
type Taste int

const (
	// TasteWide favours films a lot of people have already voted on.
	TasteWide Taste = iota
	// TasteBalanced is the built-in default.
	TasteBalanced
	// TasteNiche accepts thin vote counts and drops the arthouse penalty.
	TasteNiche
)

// KidsMode decides what happens to family films and animation.
type KidsMode int

const (
	// KidsNone is an adult-only list: family films and animation are pushed down.
	KidsNone KidsMode = iota
	// KidsWith turns both penalties off — the viewer picks films for children too.
	KidsWith
	// KidsAnime keeps the family penalty but stops punishing animation, which is
	// the only way an adult anime is not charged for being drawn.
	KidsAnime
)

// RuMode decides whether Kinopoisk is taken at face value in "kino ru".
type RuMode int

const (
	// RuCrossCheck penalises a film Kinopoisk loves and the world does not.
	RuCrossCheck RuMode = iota
	// RuTrust ranks Russian-language cinema on Kinopoisk alone.
	RuTrust
)

// FreshMode is how much "came out days ago" is worth on its own.
type FreshMode int

const (
	// FreshHigh is for someone who watches what just landed.
	FreshHigh FreshMode = iota
	// FreshNormal is the built-in default.
	FreshNormal
	// FreshAny ignores release date entirely and widens the default window.
	FreshAny
)

// Answers is one filled-in questionnaire.
type Answers struct {
	Taste Taste
	Kids  KidsMode
	Ru    RuMode
	Fresh FreshMode
	// Limit is how many films a bare "kino" prints. Zero keeps the default.
	Limit int
}

// Tuning turns answers into coefficients. It starts from the defaults and only
// moves what an answer actually speaks to: an unasked question must not silently
// change the model.
func (a Answers) Tuning() Tuning {
	t := DefaultTuning()

	switch a.Taste {
	case TasteWide:
		// Confidence is the vote-count term. Raising it, plus the admission
		// thresholds, is what "show me what everyone has seen" means in numbers.
		t.Weights = Weights{IMDb: 0.38, TMDB: 0.20, Confidence: 0.24, Mainstream: 0.13, Freshness: 0.05}
		t.Thresholds.MinIMDbVotes = 2000
		t.Thresholds.MinTMDBVotes = 300
		t.Thresholds.MinKPVotes = 10000
		t.Arthouse.MaxPenalty = 1.5
	case TasteNiche:
		// A film with few votes is the point here, not a defect, so the vote
		// term nearly vanishes, the gates drop, and Bayesian smoothing stops
		// dragging a thinly-voted film back to the mean.
		t.Weights = Weights{IMDb: 0.50, TMDB: 0.30, Confidence: 0.05, Mainstream: 0.02, Freshness: 0.13}
		t.Thresholds.MinIMDbVotes = 100
		t.Thresholds.MinTMDBVotes = 40
		t.Thresholds.MinKPVotes = 1000
		t.Bayes.IMDbM = 2000
		t.Bayes.TMDBM = 200
		t.Bayes.KPM = 2000
		t.Arthouse.MaxPenalty = 0
	}

	switch a.Kids {
	case KidsWith:
		t.Kids.Penalty = 0
		t.Kids.AnimationPenalty = 0
	case KidsAnime:
		t.Kids.AnimationPenalty = 0
	}

	if a.Ru == RuTrust {
		t.GapPenalty.MaxPenalty = 0
	}

	switch a.Fresh {
	case FreshHigh:
		t.Weights.Freshness = 0.12
	case FreshAny:
		t.Weights.Freshness = 0
		t.Defaults.Period = 30
		t.Defaults.RuPeriod = 90
	}

	if a.Limit > 0 {
		t.Defaults.Limit = a.Limit
	}

	// Freshness was set after the taste block chose the other four, so the sum
	// has drifted. Validate() rejects anything off 1.0, and rescaling keeps the
	// balance the taste answer asked for.
	t.Weights.normalize()
	return t
}

// normalize scales the terms so they sum to 1.0, preserving their ratios, and
// then snaps them to hundredths.
//
// The snapping is not cosmetic. config.toml prints weights with two decimals,
// so unrounded values would be written truncated and could sum to 0.99 on the
// way back in — the wizard would produce a file its own Validate() rejects. The
// rounding residual goes onto the largest term, where it is proportionally
// smallest.
func (w *Weights) normalize() {
	sum := w.Sum()
	if sum <= 0 {
		*w = DefaultTuning().Weights
		return
	}
	w.IMDb /= sum
	w.TMDB /= sum
	w.Confidence /= sum
	w.Mainstream /= sum
	w.Freshness /= sum

	terms := []*float64{&w.IMDb, &w.TMDB, &w.Confidence, &w.Mainstream, &w.Freshness}
	largest := terms[0]
	for _, t := range terms {
		*t = math.Round(*t*100) / 100
		if *t > *largest {
			largest = t
		}
	}
	*largest = math.Round((*largest+(1-w.Sum()))*100) / 100
}
