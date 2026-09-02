// Package omdb wraps the OMDb API, used only to enrich ratings.
package omdb

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/serik-effective/kino-cli/internal/httpx"
)

const BaseURL = "https://www.omdbapi.com/"

// DailyLimit is the documented free-tier quota.
const DailyLimit = 1000

type Client struct {
	h   *httpx.Client
	key string
}

func New(key string, onCall func(string, int)) *Client {
	h := httpx.New("omdb", 5, 5)
	h.OnCall = onCall
	return &Client{h: h, key: key}
}

type rating struct {
	Source string `json:"Source"`
	Value  string `json:"Value"`
}

type Response struct {
	Response   string   `json:"Response"`
	Error      string   `json:"Error"`
	Title      string   `json:"Title"`
	Year       string   `json:"Year"`
	Runtime    string   `json:"Runtime"`
	IMDbRating string   `json:"imdbRating"`
	IMDbVotes  string   `json:"imdbVotes"`
	Metascore  string   `json:"Metascore"`
	BoxOffice  string   `json:"BoxOffice"`
	Ratings    []rating `json:"Ratings"`
}

// ByIMDb looks a movie up by its tt… id.
func (c *Client) ByIMDb(ctx context.Context, imdbID string) (*Response, error) {
	q := url.Values{}
	q.Set("apikey", c.key)
	q.Set("i", imdbID)
	q.Set("tomatoes", "true")

	var out Response
	if err := c.h.GetJSON(ctx, BaseURL+"?"+q.Encode(), nil, &out); err != nil {
		return nil, err
	}
	// OMDb reports failures with HTTP 200 and Response:"False".
	if !strings.EqualFold(out.Response, "True") {
		return nil, fmt.Errorf("omdb %s: %s", imdbID, orUnknown(out.Error))
	}
	return &out, nil
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown error"
	}
	return s
}

// Float parses OMDb's stringly-typed numbers, tolerating "N/A" and thousands separators.
func Float(s string) *float64 {
	s = clean(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &v
}

func Int(s string) *int {
	s = clean(s)
	if s == "" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return nil
	}
	return &v
}

func clean(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "N/A") {
		return ""
	}
	s = strings.ReplaceAll(s, ",", "")
	s = strings.TrimSuffix(s, "%")
	s = strings.TrimSuffix(s, " min")
	return s
}

// RottenTomatoes returns the RT percentage as an int, if present.
func (r *Response) RottenTomatoes() *int {
	for _, x := range r.Ratings {
		if strings.EqualFold(x.Source, "Rotten Tomatoes") {
			return Int(x.Value)
		}
	}
	return nil
}
