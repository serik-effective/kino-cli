// Package kp wraps poiskkino.dev (the service the @kinopoiskdev_bot token now
// belongs to). Only /v1.5 is used: /v1.4 is deprecated upstream.
package kp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/sbeysenov/kino-cli/internal/httpx"
)

const BaseURL = "https://api.poiskkino.dev"

// DailyLimit is the free-tier quota; the authoritative number comes from TokenInfo.
const DailyLimit = 200

type Client struct {
	h   *httpx.Client
	key string
}

func New(key string, onCall func(string, int)) *Client {
	h := httpx.New("kp", 3, 3)
	h.OnCall = onCall
	return &Client{h: h, key: key}
}

func (c *Client) headers() map[string]string {
	return map[string]string{"X-API-KEY": c.key}
}

type TokenInfo struct {
	Limit     int    `json:"requestsLimit"`
	Used      int    `json:"requestsUsed"`
	Remaining int    `json:"requestsRemaining"`
	ResetAt   string `json:"resetAt"`
}

// TokenInfo reports the daily quota. Upstream does not charge for this call.
func (c *Client) TokenInfo(ctx context.Context) (*TokenInfo, error) {
	var out TokenInfo
	err := c.h.GetJSON(ctx, BaseURL+"/v1.5/token", c.headers(), &out)
	return &out, err
}

type Movie struct {
	ID              int    `json:"id"`
	Name            string `json:"name"`
	AlternativeName string `json:"alternativeName"`
	EnName          string `json:"enName"`
	Description     string `json:"description"`
	Year            int    `json:"year"`
	MovieLength     int    `json:"movieLength"`
	Type            string `json:"type"`
	Rating          struct {
		KP          float64 `json:"kp"`
		IMDb        float64 `json:"imdb"`
		TMDB        float64 `json:"tmdb"`
		FilmCritics float64 `json:"filmCritics"`
	} `json:"rating"`
	Votes struct {
		KP   int `json:"kp"`
		IMDb int `json:"imdb"`
		TMDB int `json:"tmdb"`
	} `json:"votes"`
	ExternalID struct {
		IMDb string `json:"imdb"`
		TMDB int    `json:"tmdb"`
	} `json:"externalId"`
	Premiere struct {
		Digital string `json:"digital"`
		Russia  string `json:"russia"`
		World   string `json:"world"`
	} `json:"premiere"`
	Watchability struct {
		Items []struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"items"`
	} `json:"watchability"`
}

type searchResp struct {
	Docs    []Movie `json:"docs"`
	Limit   int     `json:"limit"`
	Next    string  `json:"next"`
	HasNext bool    `json:"hasNext"`
}

var defaultFields = []string{
	"id", "name", "alternativeName", "enName", "description", "year", "movieLength", "type",
	"rating", "votes", "externalId", "premiere", "watchability",
}

// ByIMDb resolves a movie through its IMDb id, the only mapping we trust.
func (c *Client) ByIMDb(ctx context.Context, imdbID string) (*Movie, error) {
	q := url.Values{}
	q.Set("externalId.imdb", imdbID)
	q.Set("limit", "1")
	for _, f := range defaultFields {
		q.Add("selectFields", f)
	}

	var out searchResp
	if err := c.h.GetJSON(ctx, BaseURL+"/v1.5/movie?"+q.Encode(), c.headers(), &out); err != nil {
		return nil, err
	}
	if len(out.Docs) == 0 {
		return nil, fmt.Errorf("kp: %s not found", imdbID)
	}
	return &out.Docs[0], nil
}

// TitleRU picks the Russian title, falling back to the alternative name.
func (m *Movie) TitleRU() string {
	if strings.TrimSpace(m.Name) != "" {
		return m.Name
	}
	return m.AlternativeName
}

// Platforms lists the RU streaming services carrying the movie.
func (m *Movie) Platforms() []string {
	out := make([]string, 0, len(m.Watchability.Items))
	for _, it := range m.Watchability.Items {
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out
}
