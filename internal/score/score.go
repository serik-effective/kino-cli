// Package score turns a film plus its ratings into a single 0..10 number.
//
// The question it answers is not "is this a good film" but "is this recently
// released film likely to please an ordinary viewer tonight". That is why a
// dependable 7.5 from fifty thousand people outranks an 8.4 from four hundred,
// and why niche cinema is discounted rather than celebrated.
//
// Nothing here holds a coefficient of its own: every number comes from
// config.Tuning, so the whole model can be read and changed in one file.
package score

import (
	"math"
	"strings"
	"time"

	"github.com/serik-effective/kino-cli/internal/config"
)

// Mode selects which audience the film is judged by.
type Mode int

const (
	// General ranks on IMDb and TMDB, the way the specification requires.
	General Mode = iota
	// Russian ranks on Kinopoisk. It exists because Russian cinema's mass
	// audience is on Kinopoisk: scored on IMDb, a Russian release with no
	// international footprint collapses to the global mean and can never place.
	// This mode is only ever used by an explicitly Russian query, never by the
	// general ranking, and the output says so.
	Russian
)

// Input is everything the scorer needs. Vote counts of zero mean "no data",
// which is different from "rated zero" and is handled by dropping the source.
type Input struct {
	IMDbRating float64
	IMDbVotes  int
	TMDBRating float64
	TMDBVotes  int
	KPRating   float64
	KPVotes    int

	Popularity float64
	Runtime    int
	Genres     []string

	// Released is the digital release date; Now and Window fix the freshness
	// scale, so a film at the edge of the window scores 0 and today's scores 1.
	Released time.Time
	Now      time.Time
	Window   time.Duration
}

// Breakdown is the score with its parts, as printed by "kino --why".
type Breakdown struct {
	IMDbWeighted float64
	TMDBWeighted float64
	KPWeighted   float64
	Confidence   float64
	Mainstream   float64
	Freshness    float64
	Arthouse     float64
	Penalty      float64
	// Gap is how far the Kinopoisk rating sits above the international one, and
	// GapPenalty what that cost. Both are zero unless the Russian track is in
	// use and both audiences left a real sample.
	Gap        float64
	GapPenalty float64
	// KidsPenalty is what being a children's film cost. It is a preference the
	// config expresses, not a judgement about the film.
	KidsPenalty float64
	Final       float64

	// The Points fields are what each term actually contributed to Final, in
	// score points. The fields above are the raw ingredients — a smoothed
	// rating on a 0..10 scale, a signal on 0..1 — and they cannot be added up
	// or compared with each other. Anything reporting "why this score" needs
	// the contributions, and recomputing them from the weights outside this
	// package is how the explanation drifts away from the arithmetic.
	RatingPoints     float64
	ConfidencePoints float64
	MainstreamPoints float64
	FreshnessPoints  float64

	// RatingSources names what the rating term was actually built from, so a
	// reader can tell a film judged on two sources from one judged on none.
	RatingSources []string
}

// Eligible reports whether the film has a large enough audience anywhere to be
// ranked at all. Any single source clearing its threshold is enough: the test
// is whether the sample is reliable, not which site it came from.
func Eligible(in Input, t config.Tuning) bool {
	th := t.Thresholds
	return in.IMDbVotes >= th.MinIMDbVotes ||
		in.TMDBVotes >= th.MinTMDBVotes ||
		in.KPVotes >= th.MinKPVotes
}

// Score computes the final 0..10 value and its breakdown.
func Score(in Input, t config.Tuning, mode Mode) Breakdown {
	var b Breakdown

	b.Confidence = confidence(effectiveVotes(in, mode), t)
	b.Mainstream = mainstream(in, t, b.Confidence)
	b.Arthouse = arthouse(in, t, b.Confidence)
	b.Freshness = freshness(in)

	rating := ratingTerm(in, t, mode, &b)

	w := t.Weights
	// The 0..1 signals are lifted to the 0..10 scale of a rating before being
	// mixed, otherwise their weights would be meaningless next to a 7.5.
	b.RatingPoints = rating
	b.ConfidencePoints = w.Confidence * b.Confidence * 10
	b.MainstreamPoints = w.Mainstream * b.Mainstream * 10
	b.FreshnessPoints = w.Freshness * b.Freshness * 10

	b.Final = b.RatingPoints + b.ConfidencePoints + b.MainstreamPoints + b.FreshnessPoints

	b.Penalty = penalty(b.Arthouse, t.Arthouse)
	if mode == Russian {
		b.Gap, b.GapPenalty = gapPenalty(in, t)
	}
	// The two are not added together: a family cartoon is one film, not two
	// reasons to discount it. The stronger signal wins.
	switch {
	case isForChildren(in.Genres):
		b.KidsPenalty = t.Kids.Penalty
	case isAnimation(in.Genres):
		b.KidsPenalty = t.Kids.AnimationPenalty
	}
	b.Final = clamp(b.Final-b.Penalty-b.GapPenalty-b.KidsPenalty, 0, 10)
	return b
}

// gapPenalty discounts a Kinopoisk rating the rest of the world does not share.
// It returns the gap in rating points and what it cost.
//
// Only an excess of Kinopoisk over the international rating counts. Both sides
// need a real sample first: 8.3 from 156 000 against 2.9 from 49 people is one
// audience and some noise, and reading a two-audience disagreement into it
// would punish a film for being unknown abroad.
func gapPenalty(in Input, t config.Tuning) (gap, cost float64) {
	min := t.Signals.GapMinVotes
	if in.KPVotes < min || in.IMDbVotes < min || in.KPRating == 0 || in.IMDbRating == 0 {
		return 0, 0
	}
	gap = in.KPRating - in.IMDbRating
	if gap <= 0 {
		return gap, 0
	}
	g := t.GapPenalty
	switch {
	case gap <= g.Low:
		return gap, 0
	case gap >= g.High:
		return gap, g.MaxPenalty
	default:
		return gap, g.MaxPenalty * (gap - g.Low) / (g.High - g.Low)
	}
}

// ratingTerm builds the audience part of the score and returns it already
// multiplied by its weights.
//
// A source with no votes is dropped rather than smoothed towards the mean: a
// film nobody rated on TMDB would otherwise be handed an invented 6.5 carrying
// a quarter of the final score. When one source is missing the other takes over
// its weight, so the terms still add up to what the config says.
func ratingTerm(in Input, t config.Tuning, mode Mode, b *Breakdown) float64 {
	w := t.Weights
	total := w.IMDb + w.TMDB

	if mode == Russian {
		if in.KPVotes > 0 {
			b.KPWeighted = bayesian(in.KPRating, float64(in.KPVotes), t.Bayes.KPM, t.Bayes.KPMean)
			b.RatingSources = []string{"kp"}
			return total * b.KPWeighted
		}
		// No Kinopoisk data: fall through to the general sources rather than
		// return nothing.
	}

	hasIMDb := in.IMDbVotes > 0
	hasTMDB := in.TMDBVotes > 0
	if hasIMDb {
		b.IMDbWeighted = bayesian(in.IMDbRating, float64(in.IMDbVotes), t.Bayes.IMDbM, t.Bayes.IMDbMean)
		b.RatingSources = append(b.RatingSources, "imdb")
	}
	if hasTMDB {
		b.TMDBWeighted = bayesian(in.TMDBRating, float64(in.TMDBVotes), t.Bayes.TMDBM, t.Bayes.TMDBMean)
		b.RatingSources = append(b.RatingSources, "tmdb")
	}

	switch {
	case hasIMDb && hasTMDB:
		return w.IMDb*b.IMDbWeighted + w.TMDB*b.TMDBWeighted
	case hasIMDb:
		return total * b.IMDbWeighted
	case hasTMDB:
		return total * b.TMDBWeighted
	default:
		// Nothing to judge by. The film scores only on its signals, which
		// without an audience are near zero anyway.
		return 0
	}
}

// bayesian pulls a rating towards the global mean in proportion to how thin the
// vote count is: m is the number of votes at which the film's own rating and
// the mean carry equal weight.
func bayesian(r, v, m, mean float64) float64 {
	if v <= 0 {
		return mean
	}
	return (v/(v+m))*r + (m/(v+m))*mean
}

// effectiveVotes is how many people rated the film anywhere. IMDb and TMDB
// audiences overlap only partly, so adding them is a fair proxy for reach.
func effectiveVotes(in Input, mode Mode) float64 {
	v := float64(in.IMDbVotes + in.TMDBVotes)
	if mode == Russian {
		v += float64(in.KPVotes)
	}
	return v
}

// confidence maps vote count onto 0..1 logarithmically, so each tenfold jump in
// audience is worth the same step. Linear growth would let one blockbuster
// flatten every other film to zero.
func confidence(votes float64, t config.Tuning) float64 {
	s := t.Signals
	if votes <= s.ConfidenceFloor {
		return 0
	}
	return clamp(math.Log(votes/s.ConfidenceFloor)/math.Log(s.ConfidenceCeiling/s.ConfidenceFloor), 0, 1)
}

// mainstreamGenres are the ones that usually signal a film made for a wide
// audience. Membership is a hint, never a verdict.
//
// Both spellings are listed because the catalogue stores genres in whatever
// language the config asks TMDB for, Russian by default. Keying on English
// alone silently disabled every genre signal: a crime drama looked like a film
// of no genre at all, losing its mainstream credit and keeping an arthouse
// penalty it had not earned.
var mainstreamGenres = map[string]bool{
	"action": true, "боевик": true,
	"adventure": true, "приключения": true,
	"comedy": true, "комедия": true,
	"thriller": true, "триллер": true,
	"science fiction": true, "sci-fi": true, "фантастика": true,
	"animation": true, "мультфильм": true,
	"crime": true, "криминал": true,
	"horror": true, "ужасы": true,
	"mystery": true, "детектив": true,
	"fantasy": true, "фэнтези": true,
	"family": true, "семейный": true,
	"western": true, "вестерн": true,
	"romance": true, "мелодрама": true,
}

// arthouseGenres carry a niche signal, weighted by how strong it is. Drama is
// deliberately weak: most popular films are dramas, and treating drama as
// arthouse is the single easiest way to get this wrong.
var arthouseGenres = map[string]float64{
	"documentary": 1.0, "документальный": 1.0,
	"history": 0.5, "история": 0.5,
	"war": 0.4, "военный": 0.4,
	"biography": 0.4, "биография": 0.4,
	"music": 0.3, "музыка": 0.3,
	"drama": 0.2, "драма": 0.2,
}

// kidsGenres mark a film aimed at children. Animation is deliberately absent:
// plenty of it is made for adults, and the family or children tag is what
// actually distinguishes the two.
var kidsGenres = map[string]bool{
	"family": true, "семейный": true,
	"kids": true, "детский": true,
}

var animationGenres = map[string]bool{"animation": true, "мультфильм": true}

func isAnimation(genres []string) bool {
	for _, g := range genres {
		if animationGenres[normalize(g)] {
			return true
		}
	}
	return false
}

func isForChildren(genres []string) bool {
	for _, g := range genres {
		if kidsGenres[normalize(g)] {
			return true
		}
	}
	return false
}

func mainstream(in Input, t config.Tuning, conf float64) float64 {
	s := t.Signals
	genre := 0.0
	for _, g := range in.Genres {
		if mainstreamGenres[normalize(g)] {
			genre = 1
			break
		}
	}
	pop := clamp(logScale(in.Popularity, s.PopularityCeiling), 0, 1)
	return clamp(s.MainstreamGenre*genre+s.MainstreamVotes*conf+s.MainstreamPopularity*pop, 0, 1)
}

// arthouse estimates the chance the film is niche, festival or critic-facing
// work. It is a guess about the audience, not a judgement of quality.
func arthouse(in Input, t config.Tuning, conf float64) float64 {
	s := t.Signals
	lowAudience := 1 - conf

	genre := 0.0
	mainstreamHit := false
	for _, g := range in.Genres {
		n := normalize(g)
		if v, ok := arthouseGenres[n]; ok && v > genre {
			genre = v
		}
		if mainstreamGenres[n] {
			mainstreamHit = true
		}
	}
	// A niche genre only counts against a film that also lacks an audience:
	// a documentary a million people rated is not arthouse.
	genre *= lowAudience

	long := 0.0
	if s.LongRuntimeMinutes > 0 && in.Runtime > s.LongRuntimeMinutes {
		long = clamp(float64(in.Runtime-s.LongRuntimeMinutes)/float64(s.LongRuntimeMinutes), 0, 1)
	}
	lowPop := 1 - clamp(logScale(in.Popularity, s.PopularityCeiling), 0, 1)

	v := s.ArthouseLowAudience*lowAudience +
		s.ArthouseGenre*genre +
		s.ArthouseLongRuntime*long +
		s.ArthouseLowPopularity*lowPop
	if mainstreamHit {
		v -= s.ArthouseMainstreamOffset
	}
	return clamp(v, 0, 1)
}

// penalty ramps linearly between the two thresholds: below low there is none,
// above high the full penalty applies. A genuinely outstanding niche film can
// still place, it just has to be better than the crowd-pleaser beside it.
func penalty(arthouseScore float64, a config.Arthouse) float64 {
	switch {
	case arthouseScore <= a.Low:
		return 0
	case arthouseScore >= a.High:
		return a.MaxPenalty
	default:
		return a.MaxPenalty * (arthouseScore - a.Low) / (a.High - a.Low)
	}
}

// freshness favours what came out today over what came out at the far edge of
// the window. Films outside the window score 0 rather than negative.
func freshness(in Input) float64 {
	if in.Window <= 0 || in.Released.IsZero() || in.Now.IsZero() {
		return 0
	}
	age := in.Now.Sub(in.Released)
	if age < 0 {
		age = 0
	}
	return clamp(1-age.Seconds()/in.Window.Seconds(), 0, 1)
}

// logScale maps 0..ceiling onto 0..1 logarithmically.
func logScale(v, ceiling float64) float64 {
	if v <= 0 || ceiling <= 1 {
		return 0
	}
	return math.Log1p(v) / math.Log1p(ceiling)
}

func normalize(g string) string { return strings.ToLower(strings.TrimSpace(g)) }

func clamp(v, lo, hi float64) float64 {
	return math.Min(math.Max(v, lo), hi)
}
