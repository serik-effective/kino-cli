package kp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// MaxPageSize is what the API allows in one request. It matters more than the
// daily quota does: a filtered page returns up to 250 films for a single call,
// so a whole year of Russian cinema costs one or two requests rather than one
// per film.
const MaxPageSize = 250

// MaxTierPages is where the free tier stops: page 11 comes back as HTTP 403,
// however many pages the response claims exist. It caps one query at 2500
// films, which is why UpdateKP walks vote bands instead of pages.
const MaxTierPages = 10

// maxVotes is an open upper bound the API accepts for the votes filter.
const maxVotes = 99999999

// The API rejects a year range outside these bounds with a 400.
const (
	yearMin = 1874
	yearMax = 2050
)

// DiscoverParams selects a slice of the Kinopoisk catalogue.
type DiscoverParams struct {
	// FromYear and ToYear bound the release year. Zero means unbounded.
	FromYear, ToYear int
	// MinVotes drops films too thinly rated to rank. It is the same idea as the
	// admission threshold, applied at the source so the quota is not spent on
	// films we would discard anyway.
	MinVotes int
	// MaxVotes bounds the band from above. Zero means unbounded. It exists to
	// walk a catalogue larger than the tier's page limit: results are sorted by
	// vote count, so lowering the ceiling to the last film seen resumes exactly
	// where the previous band ran out of pages.
	MaxVotes int
	// Countries filters by production country name, e.g. "Россия".
	Countries []string
	// Type is the catalogue type: movie, tv-series, cartoon, anime.
	Type string
	Page int
	// Limit is the page size, capped at MaxPageSize.
	Limit int
}

type DiscoverResult struct {
	Movies []Movie
	Total  int
	Pages  int
}

// Discover lists films matching the filters, newest and most-voted first.
//
// Sorting by vote count rather than date is deliberate: when a filter matches
// more than one page, the films worth ranking should arrive first, so stopping
// early costs nothing that matters.
func (c *Client) Discover(ctx context.Context, p DiscoverParams) (*DiscoverResult, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(max(p.Page, 1)))

	limit := p.Limit
	if limit <= 0 || limit > MaxPageSize {
		limit = MaxPageSize
	}
	q.Set("limit", strconv.Itoa(limit))

	if p.Type != "" {
		q.Set("type", p.Type)
	}
	switch {
	case p.FromYear > 0 && p.ToYear > 0:
		q.Set("year", fmt.Sprintf("%d-%d", p.FromYear, p.ToYear))
	case p.FromYear > 0:
		q.Set("year", fmt.Sprintf("%d-%d", p.FromYear, yearMax))
	case p.ToYear > 0:
		q.Set("year", fmt.Sprintf("%d-%d", yearMin, p.ToYear))
	}
	if p.MinVotes > 0 || p.MaxVotes > 0 {
		hi := p.MaxVotes
		if hi <= 0 {
			hi = maxVotes
		}
		q.Set("votes.kp", fmt.Sprintf("%d-%d", max(p.MinVotes, 0), hi))
	}
	for _, c := range p.Countries {
		q.Add("countries.name", c)
	}
	q.Set("sortField", "votes.kp")
	q.Set("sortType", "-1")
	for _, f := range defaultFields {
		q.Add("selectFields", f)
	}

	var out discoverResp
	if err := c.h.GetJSON(ctx, BaseURL+"/v1.4/movie?"+q.Encode(), c.headers(), &out); err != nil {
		return nil, err
	}
	return &DiscoverResult{Movies: out.Docs, Total: out.Total, Pages: out.Pages}, nil
}

type discoverResp struct {
	Docs  []Movie `json:"docs"`
	Total int     `json:"total"`
	Pages int     `json:"pages"`
}

// Services lists where the film can be watched, by name.
func (m *Movie) Services() []string {
	out := make([]string, 0, len(m.Watchability.Items))
	for _, it := range m.Watchability.Items {
		if it.Name != "" {
			out = append(out, it.Name)
		}
	}
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
