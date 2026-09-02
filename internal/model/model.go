// Package model holds the domain types shared by sources, store and CLI.
package model

// ReleaseType mirrors TMDB release type ids.
// TypeNames maps CLI-friendly release type names to TMDB ids.
var TypeNames = map[string]int{
	"premiere":   ReleasePremiere,
	"limited":    ReleaseLimited,
	"theatrical": ReleaseTheater,
	"digital":    ReleaseDigital,
	"physical":   ReleasePhysical,
	"tv":         ReleaseTV,
}

const (
	ReleasePremiere = 1
	ReleaseLimited  = 2
	ReleaseTheater  = 3
	ReleaseDigital  = 4
	ReleasePhysical = 5
	ReleaseTV       = 6
)

type Movie struct {
	TMDBID      int      `json:"tmdb_id"`
	IMDbID      string   `json:"imdb_id,omitempty"`
	KPID        *int     `json:"kp_id,omitempty"`
	Title       string   `json:"title"`
	OrigTitle   string   `json:"original_title,omitempty"`
	TitleRU     string   `json:"title_ru,omitempty"`
	Year        *int     `json:"year,omitempty"`
	Runtime     *int     `json:"runtime,omitempty"`
	Overview    string   `json:"overview,omitempty"`
	OverviewRU  string   `json:"overview_ru,omitempty"`
	PosterPath  string   `json:"poster_path,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Countries   []string `json:"countries,omitempty"` // ISO 3166-1 production countries
	OrigLang    string   `json:"original_language,omitempty"`
	Popularity  *float64 `json:"popularity,omitempty"`
	TMDBRating  *float64 `json:"tmdb_rating,omitempty"`
	TMDBVotes   *int     `json:"tmdb_votes,omitempty"`
	IMDbRating  *float64 `json:"imdb_rating,omitempty"`
	IMDbVotes   *int     `json:"imdb_votes,omitempty"`
	Metascore   *int     `json:"metascore,omitempty"`
	RTScore     *int     `json:"rt_score,omitempty"`
	KPRating    *float64 `json:"kp_rating,omitempty"`
	KPVotes     *int     `json:"kp_votes,omitempty"`
	KPViews     *int     `json:"kp_views,omitempty"`
	WatchOnline *bool    `json:"watch_online,omitempty"`
	WatchOffer  string   `json:"watch_offer,omitempty"`
	ReleaseDate string   `json:"release_date,omitempty"` // primary TMDB release date
	UpdatedAt   string   `json:"updated_at,omitempty"`
	EnrichedAt  string   `json:"enriched_at,omitempty"`
	RatingsAt   string   `json:"ratings_at,omitempty"`

	// MatchDate is the release date that satisfied the query (digital by
	// default). Populated by list/update, not stored on the movies row.
	MatchDate string `json:"match_date,omitempty"`
}

type Release struct {
	TMDBID int    `json:"tmdb_id"`
	Region string `json:"region"`
	Type   int    `json:"type"`
	Date   string `json:"date"`
	Note   string `json:"note,omitempty"`
}

// Torrent is one release tracked on a public tracker. Only metadata is kept:
// titles, counters and the movie it maps to.
type Torrent struct {
	Source     string `json:"source"`
	ExtID      string `json:"ext_id"`
	RawTitle   string `json:"raw_title"`
	TitleRU    string `json:"title_ru,omitempty"`
	TitleOrig  string `json:"title_orig,omitempty"`
	Year       int    `json:"year,omitempty"`
	Tags       string `json:"tags,omitempty"`
	Quality    string `json:"quality,omitempty"`
	TMDBID     *int   `json:"tmdb_id,omitempty"`
	MatchState string `json:"match_state"`
	FirstSeen  string `json:"first_seen,omitempty"`
	LastSeen   string `json:"last_seen,omitempty"`
}

// TorrentStat is one snapshot of a release's counters.
type TorrentStat struct {
	Source     string `json:"source"`
	ExtID      string `json:"ext_id"`
	CapturedAt string `json:"captured_at"`
	Period     string `json:"period,omitempty"`
	Rank       int    `json:"rank,omitempty"`
	Seeds      int    `json:"seeds"`
	Leechers   int    `json:"leechers"`
	Downloads  int    `json:"downloads"`
	Comments   int    `json:"comments"`
}

// TrendRow aggregates every release of one movie into a single trend entry.
type TrendRow struct {
	Movie          *Movie `json:"movie,omitempty"`
	RawTitle       string `json:"raw_title,omitempty"`
	Rank           int    `json:"rank,omitempty"`
	Releases       int    `json:"releases"`
	Seeds          int    `json:"seeds"`
	Leechers       int    `json:"leechers"`
	Downloads      int    `json:"downloads"`
	DownloadsDelta int    `json:"downloads_delta"`
}
