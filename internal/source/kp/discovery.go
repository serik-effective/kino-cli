package kp

import (
	"encoding/json"
	"strconv"
	"strings"
)

// DiscoveryItem is one entry of a Kinopoisk player payload. Only metadata is
// read: the payload also carries signed, session-bound stream URLs, which are
// deliberately ignored and never stored.
type DiscoveryItem struct {
	KPID     int
	Title    string
	OrigName string
	Year     int
	Type     string // MOVIE | SHOW
	Country  string
	Genres   []string
	Rating   float64
	Votes    int
	Views    int
	Online   bool
	Offer    string
	// OnlineKnown says whether the source spoke about availability at all. A
	// harvest that only knows a film's id must not clear a flag the player
	// payload set.
	OnlineKnown bool
}

// rawDiscovery mirrors the parts of the payload we care about.
type rawDiscovery struct {
	Film struct {
		ID            int      `json:"id"`
		Title         string   `json:"title"`
		OriginalTitle string   `json:"originalTitle"`
		Year          string   `json:"year"` // "2026" or "с 2026"
		Type          string   `json:"type"`
		Genres        []string `json:"genres"`
		Country       struct {
			Name string `json:"name"`
		} `json:"country"`
		Rating struct {
			Value string `json:"value"`
			Count int    `json:"count"`
		} `json:"kinopoiskRating"`
		// A pointer so an absent field is distinguishable from an empty one.
		// The player payload always carries this key, so nil means the record
		// came from somewhere that simply does not know about availability —
		// and must not be allowed to answer the question.
		Online *struct {
			Text      string `json:"text"`
			Available bool   `json:"isAvailableOnline"`
		} `json:"onlineViewOption"`
	} `json:"film"`
	Views int `json:"views"`
}

// ParseDiscovery reads one or more concatenated JSON objects, which is how the
// payload arrives when several player responses are captured together.
func ParseDiscovery(body []byte) ([]DiscoveryItem, error) {
	dec := json.NewDecoder(strings.NewReader(string(body)))
	seen := map[int]bool{}
	var out []DiscoveryItem

	for {
		var doc map[string]rawDiscovery
		if err := dec.Decode(&doc); err != nil {
			if len(out) > 0 || isEOF(err) {
				break
			}
			return nil, err
		}
		for _, raw := range doc {
			it := convert(raw)
			if it.KPID == 0 || seen[it.KPID] {
				continue
			}
			seen[it.KPID] = true
			out = append(out, it)
		}
	}
	return out, nil
}

func isEOF(err error) bool {
	return err != nil && err.Error() == "EOF"
}

func convert(raw rawDiscovery) DiscoveryItem {
	f := raw.Film
	it := DiscoveryItem{
		KPID:     f.ID,
		Title:    strings.TrimSpace(f.Title),
		OrigName: strings.TrimSpace(f.OriginalTitle),
		Type:     f.Type,
		Country:  f.Country.Name,
		Genres:   f.Genres,
		Votes:    f.Rating.Count,
		Views:    raw.Views,
	}
	if f.Online != nil {
		it.OnlineKnown = true
		it.Online = f.Online.Available
		it.Offer = strings.TrimSpace(f.Online.Text)
	}
	if v, err := strconv.ParseFloat(f.Rating.Value, 64); err == nil && v > 0 {
		it.Rating = v
	}
	it.Year = parseYear(f.Year)
	return it
}

// parseYear handles both "2026" and the ongoing-series form "с 2026".
func parseYear(s string) int {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "с"))
	if len(s) < 4 {
		return 0
	}
	y, err := strconv.Atoi(s[:4])
	if err != nil {
		return 0
	}
	return y
}

// IsMovie reports whether the entry is a feature film rather than a series.
func (d DiscoveryItem) IsMovie() bool { return d.Type == "MOVIE" }
