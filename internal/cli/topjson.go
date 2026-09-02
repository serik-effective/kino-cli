package cli

import (
	"io"
	"math"
	"time"

	"github.com/sbeysenov/kino-cli/internal/store"
)

// The JSON shape of a recommendation.
//
// This is a contract, not a dump of internal structs: agents read it, and a
// field that appears and disappears with refactoring is worse than no field.
// Two rules hold it together.
//
// Ratings are pointers. A film nobody rated on Kinopoisk must come back as
// null, never as 0.0 — an agent that reads a zero as a rating will report that
// viewers hated a film nobody has voted on. The same distinction the CLI keeps
// between "no" and "we never looked" applies here.
//
// The score breakdown ships with every row. The one thing an agent must be able
// to do is explain why a film is on the list, and it cannot do that from a
// single number.
type jsonRec struct {
	Rank  int     `json:"rank"`
	Score float64 `json:"score"`

	Title         string   `json:"title"`
	TitleOriginal string   `json:"title_original,omitempty"`
	Year          int      `json:"year,omitempty"`
	Runtime       int      `json:"runtime_minutes,omitempty"`
	Genres        []string `json:"genres,omitempty"`
	Countries     []string `json:"countries,omitempty"`
	Language      string   `json:"original_language,omitempty"`

	TMDBID int    `json:"tmdb_id"`
	IMDbID string `json:"imdb_id,omitempty"`

	Ratings jsonRatings `json:"ratings"`

	// ReleaseDate is a digital release in the general track and the film's own
	// release date in the Russian one — the track says which.
	ReleaseDate string `json:"release_date,omitempty"`

	Availability jsonAvail `json:"availability"`

	Why    jsonWhy  `json:"why"`
	Badges []string `json:"badges,omitempty"`
}

type jsonRating struct {
	Rating *float64 `json:"rating"`
	Votes  int      `json:"votes"`
}

type jsonRatings struct {
	IMDb jsonRating `json:"imdb"`
	TMDB jsonRating `json:"tmdb"`
	KP   jsonRating `json:"kinopoisk"`
}

type jsonAvail struct {
	// Online is null when nobody ever told us, true or false when they did.
	// An agent must be able to say "unknown" rather than "not available".
	Online *bool `json:"online_ru"`
	// TheatricalOnly is true only when availability was checked and the film is
	// in cinemas but not at home.
	TheatricalOnly bool   `json:"theatrical_only"`
	Offer          string `json:"offer,omitempty"`
}

// jsonWhy is the score taken apart.
//
// Every field under "points" is in score points and they add up: the four
// contributions minus the three penalties equal the score, up to two-decimal
// rounding and the clamp into 0..10 (a film whose penalties take it below zero
// reports a score of 0, and the terms then sum to less than that).
//
// "smoothed" holds the ingredients on their own 0..10 rating scale — useful to
// show, never to add together.
type jsonWhy struct {
	RatingSources []string     `json:"rating_sources"`
	Points        jsonPoints   `json:"points"`
	Smoothed      jsonSmoothed `json:"smoothed_ratings"`
	// Gap is how far Kinopoisk sits above the international audience, in rating
	// points. Zero outside the Russian track.
	Gap float64 `json:"kp_vs_world_gap"`
}

type jsonPoints struct {
	Rating     float64 `json:"rating"`
	Confidence float64 `json:"confidence"`
	Mainstream float64 `json:"mainstream"`
	Freshness  float64 `json:"freshness"`

	ArthousePenalty float64 `json:"arthouse_penalty"`
	GapPenalty      float64 `json:"gap_penalty"`
	KidsPenalty     float64 `json:"kids_penalty"`
}

// jsonSmoothed are the Bayesian-smoothed ratings the score was built from. A
// source with no votes is absent rather than zero.
type jsonSmoothed struct {
	IMDb *float64 `json:"imdb,omitempty"`
	TMDB *float64 `json:"tmdb,omitempty"`
	KP   *float64 `json:"kinopoisk,omitempty"`
}

// jsonTop is the whole answer, window included: a bare list of films cannot be
// interpreted without knowing what was asked.
type jsonTop struct {
	Track  string    `json:"track"` // "general" | "russian"
	From   string    `json:"window_from"`
	To     string    `json:"window_to"`
	Days   int       `json:"window_days"`
	Genre  string    `json:"genre,omitempty"`
	Pool   int       `json:"candidates_in_window"`
	Films  []jsonRec `json:"films"`
	Notice string    `json:"notice,omitempty"`
}

func writeTopJSON(w io.Writer, out []ranked, o topOpts, from, to time.Time, pool int) error {
	res := jsonTop{
		Track: "general",
		From:  from.Format(time.DateOnly),
		To:    to.Format(time.DateOnly),
		Days:  o.days,
		Genre: o.genre,
		Pool:  pool,
		Films: make([]jsonRec, 0, len(out)),
	}
	if o.ru {
		res.Track = "russian"
		res.Notice = "ранжировано по оценкам Кинопоиска; часть фильмов пока только в прокате"
	}
	if o.empty {
		res.Notice = "база пуста — запустите kino setup"
	}

	for i, r := range out {
		res.Films = append(res.Films, jsonOf(i+1, r))
	}
	return writeJSON(w, res)
}

func jsonOf(rank int, r ranked) jsonRec {
	c, b := r.c, r.b

	rec := jsonRec{
		Rank:          rank,
		Score:         round2(b.Final),
		Title:         firstNonEmpty(c.TitleRU, c.Title),
		TitleOriginal: c.Title,
		Year:          c.Year,
		Runtime:       c.Runtime,
		Genres:        c.Genres,
		Countries:     c.Countries,
		Language:      c.OrigLang,
		TMDBID:        c.TMDBID,
		IMDbID:        c.IMDbID,
		Ratings: jsonRatings{
			IMDb: jsonRating{Rating: ratingPtr(c.IMDbRating, c.IMDbVotes), Votes: c.IMDbVotes},
			TMDB: jsonRating{Rating: ratingPtr(c.TMDBRating, c.TMDBVotes), Votes: c.TMDBVotes},
			KP:   jsonRating{Rating: ratingPtr(c.KPRating, c.KPVotes), Votes: c.KPVotes},
		},
		Availability: jsonAvail{
			Online:         onlinePtr(c),
			TheatricalOnly: c.WatchKnown && !c.WatchOnline && c.HasTheatrical && !c.HasDigital,
			Offer:          c.WatchOffer,
		},
		Why: jsonWhy{
			RatingSources: b.RatingSources,
			Points: jsonPoints{
				Rating:          round2(b.RatingPoints),
				Confidence:      round2(b.ConfidencePoints),
				Mainstream:      round2(b.MainstreamPoints),
				Freshness:       round2(b.FreshnessPoints),
				ArthousePenalty: round2(b.Penalty),
				GapPenalty:      round2(b.GapPenalty),
				KidsPenalty:     round2(b.KidsPenalty),
			},
			Smoothed: jsonSmoothed{
				IMDb: positivePtr(b.IMDbWeighted),
				TMDB: positivePtr(b.TMDBWeighted),
				KP:   positivePtr(b.KPWeighted),
			},
			Gap: round2(b.Gap),
		},
		// The badges are the same strings the cards print, deliberately. A
		// second, machine-only badge taxonomy would be one more thing to keep
		// in agreement with the first, and the numeric terms above already
		// carry everything an agent needs to reason with.
		Badges: notes(c, b, r.tuning),
	}
	if !c.Digital.IsZero() {
		rec.ReleaseDate = c.Digital.Format(time.DateOnly)
	}
	if rec.TitleOriginal == rec.Title {
		rec.TitleOriginal = ""
	}
	return rec
}

// ratingPtr returns nil for a rating nobody gave. A film with no votes has no
// rating, and 0.0 would read as the worst possible one.
func ratingPtr(v float64, votes int) *float64 {
	if votes <= 0 || v <= 0 {
		return nil
	}
	r := round2(v)
	return &r
}

// onlinePtr keeps the three-valued answer three-valued.
func onlinePtr(c store.Candidate) *bool {
	if !c.WatchKnown {
		return nil
	}
	online := c.WatchOnline
	return &online
}

// positivePtr omits a source that contributed nothing, rather than reporting a
// smoothed rating of zero for a film it never saw.
func positivePtr(v float64) *float64 {
	if v <= 0 {
		return nil
	}
	r := round2(v)
	return &r
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
