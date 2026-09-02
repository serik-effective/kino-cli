// Package imdb reads the official IMDb Non-Commercial Datasets.
//
// The datasets are the only way to learn a film's audience rating in bulk: the
// per-title APIs we have are quota-bound (OMDb allows 1000 calls a day), so a
// catalogue of thousands of films could never be scored through them. The files
// are plain gzipped TSV served over HTTPS with no key.
//
// Everything here streams. title.basics is 11+ million rows and roughly 900 MB
// once expanded, so it is filtered row by row and never held in memory or
// written to disk in full.
package imdb

import (
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	RatingsURL = "https://datasets.imdbws.com/title.ratings.tsv.gz"
	BasicsURL  = "https://datasets.imdbws.com/title.basics.tsv.gz"
)

// nullMarker is IMDb's stand-in for a missing value.
const nullMarker = `\N`

type Client struct {
	HTTP *http.Client
}

// New builds a client. The timeout is generous because title.basics is a
// couple of hundred megabytes and the transfer, not the server, is the slow part.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Minute}}
}

type Rating struct {
	ID     string
	Rating float64
	Votes  int
}

type Title struct {
	ID       string
	Title    string
	OrigName string
	Year     int
	Runtime  int
	Genres   string
}

// TitleFilter decides which rows of title.basics are worth keeping.
type TitleFilter struct {
	// YearFloor drops anything released before it. Zero keeps every year.
	YearFloor int
}

func (f TitleFilter) keep(titleType string, isAdult bool, year int) bool {
	// "movie" is IMDb's feature-film type. Excluding the rest is what removes
	// shorts, episodes, concerts and stand-up specials in one step — the same
	// content the spec asks us to drop, but decided by IMDb rather than guessed
	// from a runtime.
	if titleType != "movie" || isAdult {
		return false
	}
	// A year of zero means IMDb does not know it; those are unreleased or
	// mis-catalogued entries and are of no use for a "what came out" question.
	return year != 0 && (f.YearFloor == 0 || year >= f.YearFloor)
}

// Ratings streams title.ratings and calls fn for every row. The whole file is
// kept: it is about 7 MB compressed and every row is a rating we may need.
// The count is of rows handed to fn, so malformed lines do not inflate it.
func (c *Client) Ratings(ctx context.Context, fn func(Rating) error) (int, error) {
	return c.streamRatings(ctx, RatingsURL, fn)
}

// streamRatings takes the URL so tests can point it at a local server.
func (c *Client) streamRatings(ctx context.Context, url string, fn func(Rating) error) (int, error) {
	kept := 0
	_, err := c.stream(ctx, url, func(rec []string) error {
		if len(rec) < 3 {
			return nil
		}
		rating, err := strconv.ParseFloat(rec[1], 64)
		if err != nil {
			return nil // a malformed row is skipped, not fatal
		}
		votes, err := strconv.Atoi(rec[2])
		if err != nil {
			return nil
		}
		kept++
		return fn(Rating{ID: rec[0], Rating: rating, Votes: votes})
	})
	return kept, err
}

// Titles streams title.basics and calls fn only for rows passing the filter.
// The return value counts kept rows, not rows read.
func (c *Client) Titles(ctx context.Context, f TitleFilter, fn func(Title) error) (int, error) {
	return c.streamTitles(ctx, BasicsURL, f, fn)
}

func (c *Client) streamTitles(ctx context.Context, url string, f TitleFilter, fn func(Title) error) (int, error) {
	kept := 0
	_, err := c.stream(ctx, url, func(rec []string) error {
		// tconst titleType primaryTitle originalTitle isAdult startYear endYear runtimeMinutes genres
		if len(rec) < 9 {
			return nil
		}
		year := atoiOrZero(rec[5])
		if !f.keep(rec[1], rec[4] == "1", year) {
			return nil
		}
		kept++
		return fn(Title{
			ID:       rec[0],
			Title:    rec[2],
			OrigName: nullable(rec[3]),
			Year:     year,
			Runtime:  atoiOrZero(rec[7]),
			Genres:   nullable(rec[8]),
		})
	})
	return kept, err
}

// stream downloads a gzipped TSV and hands each data row to fn. The header line
// is dropped. It returns the number of rows read from the file; callers that
// filter count the rows they keep themselves.
func (c *Client) stream(ctx context.Context, url string, fn func([]string) error) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", url, err)
	}
	defer gz.Close()

	sc := bufio.NewScanner(gz)
	// Some primaryTitle values are long; the default 64 KB ceiling is plenty but
	// the default 4 KB starting buffer causes needless growth.
	sc.Buffer(make([]byte, 64*1024), 1024*1024)

	n := 0
	for i := 0; sc.Scan(); i++ {
		if i == 0 {
			continue // header
		}
		if err := ctx.Err(); err != nil {
			return n, err
		}
		if err := fn(strings.Split(sc.Text(), "\t")); err != nil {
			return n, err
		}
		n++
	}
	if err := sc.Err(); err != nil && err != io.EOF {
		return n, fmt.Errorf("%s: %w", url, err)
	}
	return n, nil
}

func nullable(s string) string {
	if s == nullMarker {
		return ""
	}
	return s
}

func atoiOrZero(s string) int {
	if s == nullMarker {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
