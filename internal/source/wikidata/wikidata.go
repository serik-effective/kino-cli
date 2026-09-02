// Package wikidata resolves external movie ids through the Wikidata Query
// Service. It needs no key and has no daily quota, which makes it the cheapest
// way to learn a film's Kinopoisk id — the only piece the free rating feed
// cannot supply on its own.
package wikidata

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/sbeysenov/kino-cli/internal/httpx"
)

const Endpoint = "https://query.wikidata.org/sparql"

// ChunkSize is how many ids go into one VALUES clause. 250 keeps the query well
// inside the service's URL and timeout limits.
const ChunkSize = 250

type Client struct {
	h *httpx.Client
}

// New builds a client. WDQS asks for one query per second and a descriptive
// User-Agent; both are honoured here.
func New(onCall func(string, int)) *Client {
	h := httpx.New("wikidata", 1, 1)
	h.OnCall = onCall
	return &Client{h: h}
}

type sparqlResp struct {
	Results struct {
		Bindings []map[string]struct {
			Value string `json:"value"`
		} `json:"bindings"`
	} `json:"results"`
}

// KinopoiskIDs maps IMDb ids (tt…) to Kinopoisk ids via P345 → P2603.
// Films Wikidata does not know are simply absent from the result.
func (c *Client) KinopoiskIDs(ctx context.Context, imdbIDs []string) (map[string]int, error) {
	out := make(map[string]int, len(imdbIDs))
	for start := 0; start < len(imdbIDs); start += ChunkSize {
		end := min(start+ChunkSize, len(imdbIDs))
		part, err := c.chunk(ctx, imdbIDs[start:end])
		if err != nil {
			return out, err
		}
		for k, v := range part {
			out[k] = v
		}
	}
	return out, nil
}

func (c *Client) chunk(ctx context.Context, ids []string) (map[string]int, error) {
	var values strings.Builder
	for _, id := range ids {
		if !validIMDb(id) {
			continue
		}
		values.WriteString(`"`)
		values.WriteString(id)
		values.WriteString(`" `)
	}
	if values.Len() == 0 {
		return nil, nil
	}

	query := fmt.Sprintf(
		`SELECT ?imdb ?kp WHERE { VALUES ?imdb { %s } ?f wdt:P345 ?imdb; wdt:P2603 ?kp . }`,
		values.String())
	u := Endpoint + "?" + url.Values{"query": {query}}.Encode()

	var resp sparqlResp
	err := c.h.GetJSON(ctx, u, map[string]string{
		"Accept":     "application/sparql-results+json",
		"User-Agent": "kino-cli/0.1 (personal movie database; https://github.com/sbeysenov/kino-cli)",
	}, &resp)
	if err != nil {
		return nil, err
	}

	out := make(map[string]int, len(resp.Results.Bindings))
	for _, b := range resp.Results.Bindings {
		imdb, okI := b["imdb"]
		kp, okK := b["kp"]
		if !okI || !okK {
			continue
		}
		// Kinopoisk ids are numeric; anything else is a malformed statement.
		id, err := strconv.Atoi(strings.TrimSpace(kp.Value))
		if err != nil || id <= 0 {
			continue
		}
		out[imdb.Value] = id
	}
	return out, nil
}

// validIMDb guards the SPARQL literal: only tt-ids reach the query string.
func validIMDb(id string) bool {
	if !strings.HasPrefix(id, "tt") || len(id) < 3 || len(id) > 15 {
		return false
	}
	for _, r := range id[2:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
