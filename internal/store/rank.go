package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

// Candidate is one film with everything the scorer needs, plus what the card
// prints. It deliberately carries raw values: the ranking model lives in
// internal/score, not in SQL.
type Candidate struct {
	TMDBID    int
	IMDbID    string
	Title     string
	TitleRU   string
	Year      int
	Runtime   int
	Genres    []string
	Countries []string
	OrigLang  string

	IMDbRating float64
	IMDbVotes  int
	TMDBRating float64
	TMDBVotes  int
	KPRating   float64
	KPVotes    int
	Metascore  int
	RTScore    int
	Popularity float64

	// Digital is the date the film became watchable at home, by whichever rule
	// the query used.
	Digital time.Time

	WatchOnline bool
	// WatchKnown says whether anyone ever told us about this film's online
	// availability. Without it, "no" and "we never looked" are the same value.
	WatchKnown bool
	WatchOffer string
	// HasDigital and HasTheatrical say which kinds of release TMDB records.
	// Together with WatchOnline they are what lets a card admit that a film
	// cannot yet be watched at home.
	HasDigital    bool
	HasTheatrical bool
}

// CandidateQuery selects the pool the ranking runs over.
type CandidateQuery struct {
	From, To time.Time
	// RussianTrack switches both the language filter and the meaning of
	// "released digitally": see Candidates.
	RussianTrack bool
	// Genres matches any of the given genre names, case-insensitively. The list
	// exists because the catalogue stores genres in the configured language, so
	// one shorthand has to match several spellings. Empty means any genre.
	Genres []string
	// MinRuntime drops shorts. Films with no known runtime are kept: an unknown
	// runtime is not evidence of a short, and IMDb fills most of them in.
	MinRuntime int
	// MaxYearGap drops catalogue re-releases: a film whose digital date is this
	// many years after it was made is not new, it is an old film that just
	// landed on another service. Zero disables the check.
	//
	// The general track already takes the EARLIEST type-4 release for exactly
	// this reason, and it is not enough: discovery only ever walks recent
	// windows, so a 2015 film whose 2026 Starz release is the only row we ever
	// stored has a "minimum" of 2026. The gap to the production year is the
	// only signal that survives incomplete release history.
	MaxYearGap int
	// IncludeWatched keeps films the viewer has already seen. They are hidden by
	// default: a recommendation you have acted on is no longer a recommendation.
	IncludeWatched bool
}

// Candidates returns the films whose digital release falls inside the window.
//
// Two different rules produce that date, because two different markets record
// it in two different places:
//
//   - general: the EARLIEST TMDB release of type 4. Earliest, not any, or a
//     re-release drags decades-old films into a "what's new" list.
//   - russian: the film's own release date. TMDB carries no Digital release for
//     most Russian cinema, and gating on "we saw it in the player" made the
//     list a report on one payload capture rather than on Russian film: a
//     release rated 8.28 by 335 000 people was invisible because it happened
//     not to be in the snapshot. Availability is still shown when known, it
//     just no longer decides who is eligible.
func (s *Store) Candidates(ctx context.Context, q CandidateQuery) ([]Candidate, error) {
	var sb strings.Builder
	args := []any{}

	sb.WriteString(`
SELECT m.tmdb_id, COALESCE(m.imdb_id,''), m.title, COALESCE(m.title_ru,''),
       COALESCE(m.year,0), COALESCE(m.runtime,0), COALESCE(m.genres,''),
       COALESCE(m.countries,''), COALESCE(m.orig_lang,''),
       COALESCE(m.imdb_rating,0), COALESCE(m.imdb_votes,0),
       COALESCE(m.tmdb_rating,0), COALESCE(m.tmdb_votes,0),
       COALESCE(m.kp_rating,0), COALESCE(m.kp_votes,0),
       COALESCE(m.metascore,0), COALESCE(m.rt_score,0), COALESCE(m.popularity,0),
       COALESCE(dg.yes,0), COALESCE(th.yes,0), `)

	// The same expression is used in SELECT and WHERE rather than a column
	// alias: SQLite tolerates aliases in WHERE, but that is an extension and
	// silently changes meaning on any other engine.
	digitalExpr := "f.d"
	if q.RussianTrack {
		digitalExpr = "m.release_date"
	}

	if q.RussianTrack {
		sb.WriteString(`m.release_date AS digital,
       COALESCE(m.watch_online,0), m.watch_online IS NOT NULL, COALESCE(m.watch_offer,'')
  FROM movies m
  LEFT JOIN (SELECT tmdb_id, 1 yes FROM releases WHERE type = 4 GROUP BY tmdb_id) dg
    ON dg.tmdb_id = m.tmdb_id
  LEFT JOIN (SELECT tmdb_id, 1 yes FROM releases WHERE type = 3 GROUP BY tmdb_id) th
    ON th.tmdb_id = m.tmdb_id
 WHERE m.release_date IS NOT NULL AND m.orig_lang = 'ru'`)
	} else {
		sb.WriteString(`f.d AS digital,
       COALESCE(m.watch_online,0), m.watch_online IS NOT NULL, COALESCE(m.watch_offer,'')
  FROM movies m
  JOIN (SELECT tmdb_id, MIN(date) d FROM releases WHERE type = 4 GROUP BY tmdb_id) f
    ON f.tmdb_id = m.tmdb_id
  -- Every film on this track has a digital release by construction; the join
  -- is here only so both branches select the same columns.
  LEFT JOIN (SELECT tmdb_id, 1 yes FROM releases WHERE type = 4 GROUP BY tmdb_id) dg
    ON dg.tmdb_id = m.tmdb_id
  LEFT JOIN (SELECT tmdb_id, 1 yes FROM releases WHERE type = 3 GROUP BY tmdb_id) th
    ON th.tmdb_id = m.tmdb_id
 WHERE 1 = 1`)
	}

	sb.WriteString(" AND " + digitalExpr + " >= ? AND " + digitalExpr + " <= ?")
	args = append(args, q.From.Format("2006-01-02"), q.To.Format("2006-01-02"))

	if q.MaxYearGap > 0 {
		// A film with no known year is kept: an unknown year is not evidence of
		// a re-release, and dropping it would hide genuinely new films whose
		// metadata is thin.
		sb.WriteString(" AND (m.year IS NULL OR m.year = 0 OR CAST(strftime('%Y', " +
			digitalExpr + ") AS INTEGER) - m.year <= ?)")
		args = append(args, q.MaxYearGap)
	}
	if q.MinRuntime > 0 {
		// runtime 0 means "unknown", which must not be read as "a short".
		sb.WriteString(" AND (m.runtime IS NULL OR m.runtime = 0 OR m.runtime >= ?)")
		args = append(args, q.MinRuntime)
	}
	if !q.IncludeWatched {
		sb.WriteString(" AND m.tmdb_id NOT IN (SELECT tmdb_id FROM watched)")
	}
	if len(q.Genres) > 0 {
		var ors []string
		for _, g := range q.Genres {
			ors = append(ors, "LOWER(COALESCE(m.genres,'')) LIKE ?")
			args = append(args, "%\""+strings.ToLower(g)+"\"%")
		}
		sb.WriteString(" AND (" + strings.Join(ors, " OR ") + ")")
	}

	rows, err := s.db.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Candidate
	for rows.Next() {
		var c Candidate
		var genres, countries string
		var digital sql.NullString
		var online, watchKnown, hasDigital, hasTheatrical int
		if err := rows.Scan(&c.TMDBID, &c.IMDbID, &c.Title, &c.TitleRU, &c.Year,
			&c.Runtime, &genres, &countries, &c.OrigLang,
			&c.IMDbRating, &c.IMDbVotes, &c.TMDBRating, &c.TMDBVotes,
			&c.KPRating, &c.KPVotes, &c.Metascore, &c.RTScore, &c.Popularity,
			&hasDigital, &hasTheatrical, &digital, &online, &watchKnown, &c.WatchOffer); err != nil {
			return nil, err
		}
		c.Genres = decodeList(genres)
		c.Countries = decodeList(countries)
		c.WatchOnline = online == 1
		c.WatchKnown = watchKnown == 1
		c.HasDigital = hasDigital == 1
		c.HasTheatrical = hasTheatrical == 1
		if digital.Valid {
			c.Digital, _ = time.Parse("2006-01-02", digital.String)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// decodeList reads the JSON arrays genres and countries are stored as.
func decodeList(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
