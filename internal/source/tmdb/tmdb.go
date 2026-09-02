// Package tmdb wraps the TMDB v3 API: discovery plus movie details.
package tmdb

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/sbeysenov/kino-cli/internal/httpx"
)

const BaseURL = "https://api.themoviedb.org/3"

type Client struct {
	h     *httpx.Client
	token string
}

func New(token string, onCall func(string, int)) *Client {
	h := httpx.New("tmdb", 20, 20) // well under TMDB's ~40-50 rps soft ceiling
	h.OnCall = onCall
	return &Client{h: h, token: token}
}

// UseCache makes repeated questions free and lets the tool keep answering when
// TMDB is unreachable. onStale fires only when the network failed and a body
// older than ttl was served in its place.
func (c *Client) UseCache(cache httpx.Cache, ttl time.Duration, onStale func(string, time.Duration)) {
	c.h.Cache = cache
	c.h.TTL = ttl
	c.h.OnStale = onStale
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.token}
}

type DiscoverParams struct {
	Region   string
	From, To string // YYYY-MM-DD, inclusive
	// ReleaseTypes filters on release type (OR-ed). Empty means any type, in
	// which case the window applies to the primary release date instead.
	ReleaseTypes  []int
	OrigLang      string
	OriginCountry string
	MinRating     float64
	MinVotes      int
	SortBy        string
	Lang          string
	Page          int
}

type DiscoverMovie struct {
	ID               int     `json:"id"`
	Title            string  `json:"title"`
	OriginalTitle    string  `json:"original_title"`
	Overview         string  `json:"overview"`
	PosterPath       string  `json:"poster_path"`
	Popularity       float64 `json:"popularity"`
	VoteAverage      float64 `json:"vote_average"`
	VoteCount        int     `json:"vote_count"`
	ReleaseDate      string  `json:"release_date"`
	OriginalLanguage string  `json:"original_language"`
	GenreIDs         []int   `json:"genre_ids"`
}

type DiscoverResp struct {
	Page         int             `json:"page"`
	TotalPages   int             `json:"total_pages"`
	TotalResults int             `json:"total_results"`
	Results      []DiscoverMovie `json:"results"`
}

func (c *Client) Discover(ctx context.Context, p DiscoverParams) (*DiscoverResp, error) {
	q := url.Values{}
	q.Set("include_adult", "false")
	q.Set("include_video", "false")
	q.Set("language", orDefault(p.Lang, "en-US"))
	q.Set("page", strconv.Itoa(max(p.Page, 1)))
	q.Set("sort_by", orDefault(p.SortBy, "popularity.desc"))
	dateField := "primary_release_date"
	if len(p.ReleaseTypes) > 0 {
		parts := make([]string, len(p.ReleaseTypes))
		for i, t := range p.ReleaseTypes {
			parts[i] = strconv.Itoa(t)
		}
		q.Set("with_release_type", strings.Join(parts, "|")) // pipe = OR
		// release_date only makes sense together with a release type; without
		// one TMDB matches any country and returns noise.
		dateField = "release_date"
		if p.Region != "" {
			q.Set("region", p.Region)
		}
	}
	if p.From != "" {
		q.Set(dateField+".gte", p.From)
	}
	if p.To != "" {
		q.Set(dateField+".lte", p.To)
	}
	if p.OrigLang != "" {
		q.Set("with_original_language", p.OrigLang)
	}
	if p.OriginCountry != "" {
		q.Set("with_origin_country", p.OriginCountry)
	}
	if p.MinRating > 0 {
		q.Set("vote_average.gte", strconv.FormatFloat(p.MinRating, 'f', -1, 64))
	}
	if p.MinVotes > 0 {
		q.Set("vote_count.gte", strconv.Itoa(p.MinVotes))
	}

	var out DiscoverResp
	err := c.h.GetJSON(ctx, BaseURL+"/discover/movie?"+q.Encode(), c.headers(), &out)
	return &out, err
}

// SearchMovie looks a title up by name, optionally constrained to a year.
func (c *Client) SearchMovie(ctx context.Context, query string, year int, lang string) ([]DiscoverMovie, error) {
	q := url.Values{}
	q.Set("query", query)
	q.Set("include_adult", "false")
	q.Set("language", orDefault(lang, "en-US"))
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}

	var out DiscoverResp
	if err := c.h.GetJSON(ctx, BaseURL+"/search/movie?"+q.Encode(), c.headers(), &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

type ReleaseDate struct {
	Certification string `json:"certification"`
	Note          string `json:"note"`
	ReleaseDate   string `json:"release_date"` // 2026-08-20T00:00:00.000Z
	Type          int    `json:"type"`
}

type Details struct {
	ID            int     `json:"id"`
	IMDbID        string  `json:"imdb_id"`
	Title         string  `json:"title"`
	OriginalTitle string  `json:"original_title"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
	Runtime       *int    `json:"runtime"`
	Popularity    float64 `json:"popularity"`
	VoteAverage   float64 `json:"vote_average"`
	VoteCount     int     `json:"vote_count"`
	ReleaseDate   string  `json:"release_date"`
	OrigLang      string  `json:"original_language"`
	Genres        []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	ProductionCountries []struct {
		Code string `json:"iso_3166_1"`
		Name string `json:"name"`
	} `json:"production_countries"`
	// OriginCountry is often the only country TMDB has for a smaller or newer
	// film: production_countries comes back empty while this is filled in.
	OriginCountry []string `json:"origin_country"`
	ReleaseDates  struct {
		Results []struct {
			Country      string        `json:"iso_3166_1"`
			ReleaseDates []ReleaseDate `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// Details fetches one movie with release dates appended, which is what keeps
// the digital-date verification down to a single request per candidate.
func (c *Client) Details(ctx context.Context, id int, lang string) (*Details, error) {
	q := url.Values{}
	q.Set("append_to_response", "release_dates")
	q.Set("language", orDefault(lang, "en-US"))

	var out Details
	err := c.h.GetJSON(ctx, BaseURL+"/movie/"+strconv.Itoa(id)+"?"+q.Encode(), c.headers(), &out)
	return &out, err
}

func (d *Details) GenreNames() []string {
	out := make([]string, 0, len(d.Genres))
	for _, g := range d.Genres {
		out = append(out, g.Name)
	}
	return out
}

// CountryCodes reports where the film was made, falling back to origin_country
// when TMDB left production_countries empty. Without the fallback a Kazakh
// release rated by 82 000 people looks like a film from nowhere.
func (d *Details) CountryCodes() []string {
	if len(d.ProductionCountries) > 0 {
		out := make([]string, 0, len(d.ProductionCountries))
		for _, c := range d.ProductionCountries {
			out = append(out, c.Code)
		}
		return out
	}
	return d.OriginCountry
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
