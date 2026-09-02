package kp

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/charmap"

	"github.com/sbeysenov/kino-cli/internal/httpx"
)

// RatingBaseURL serves per-movie ratings as XML. It needs no key and no quota,
// so once a kp_id is known, refreshing ratings is free.
const RatingBaseURL = "https://rating.kinopoisk.ru"

type RatingClient struct {
	h *httpx.Client
}

// NewRatingClient keeps the request rate deliberately low: the endpoint is
// undocumented and unauthenticated, so we stay a polite consumer.
func NewRatingClient(onCall func(string, int)) *RatingClient {
	h := httpx.New("kp-rating", 2, 2)
	h.OnCall = onCall
	return &RatingClient{h: h}
}

type Ratings struct {
	KP        *float64
	KPVotes   *int
	IMDb      *float64
	IMDbVotes *int
}

type ratingXML struct {
	KP struct {
		Votes string `xml:"num_vote,attr"`
		Value string `xml:",chardata"`
	} `xml:"kp_rating"`
	IMDb struct {
		Votes string `xml:"num_vote,attr"`
		Value string `xml:",chardata"`
	} `xml:"imdb_rating"`
}

// Ratings fetches KP and (when present) IMDb ratings for a Kinopoisk id.
func (c *RatingClient) Ratings(ctx context.Context, kpID int) (*Ratings, error) {
	body, err := c.h.Get(ctx, fmt.Sprintf("%s/%d.xml", RatingBaseURL, kpID), nil)
	if err != nil {
		return nil, err
	}
	return parseRatingXML(body)
}

func parseRatingXML(body []byte) (*Ratings, error) {
	var doc ratingXML
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	// The document declares Windows-1251; the payload is numeric, but decode it
	// properly rather than assuming ASCII.
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		switch strings.ToLower(charset) {
		case "windows-1251", "cp1251":
			return charmap.Windows1251.NewDecoder().Reader(input), nil
		case "", "utf-8", "us-ascii":
			return input, nil
		}
		return nil, fmt.Errorf("unsupported charset %q", charset)
	}
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("kp rating: %w", err)
	}

	r := &Ratings{
		KP:        parseRatingValue(doc.KP.Value),
		KPVotes:   parseVotes(doc.KP.Votes),
		IMDb:      parseRatingValue(doc.IMDb.Value),
		IMDbVotes: parseVotes(doc.IMDb.Votes),
	}
	if r.KP == nil && r.IMDb == nil {
		return nil, fmt.Errorf("kp rating: no ratings in response")
	}
	return r, nil
}

// parseRatingValue tolerates the "null" and "-1" placeholders KP uses for
// movies without a score yet.
func parseRatingValue(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "null") {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}

func parseVotes(s string) *int {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if s == "" {
		return nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v <= 0 {
		return nil
	}
	return &v
}
