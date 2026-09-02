package kinozal

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"golang.org/x/text/encoding/charmap"

	"github.com/serik-effective/kino-cli/internal/httpx"
)

const BaseURL = "https://kinozal.me"

// CategoryMovies is kinozal's category id for feature films.
const CategoryMovies = 1002

// Period selects which chart top.php returns.
type Period string

const (
	PeriodWeek     Period = "week"
	PeriodMonth    Period = "month"
	PeriodHalfYear Period = "halfyear"
	PeriodAllTime  Period = "all"
)

func (p Period) param() string {
	switch p {
	case PeriodWeek:
		return "1"
	case PeriodMonth:
		return "2"
	case PeriodHalfYear:
		return "6"
	default:
		return ""
	}
}

func ParsePeriod(s string) (Period, error) {
	switch s {
	case "week", "неделя", "7d":
		return PeriodWeek, nil
	case "month", "месяц", "1m":
		return PeriodMonth, nil
	case "halfyear", "полгода", "6m":
		return PeriodHalfYear, nil
	case "all", "":
		return PeriodAllTime, nil
	}
	return "", fmt.Errorf("unknown period %q, want week|month|halfyear|all", s)
}

type Client struct {
	h *httpx.Client
}

// New keeps the rate at one request per second: the site is a courtesy source,
// and its robots.txt allows exactly the listing pages we read.
func New(onCall func(string, int)) *Client {
	h := httpx.New("kinozal", 1, 1)
	h.OnCall = onCall
	return &Client{h: h}
}

// get fetches a page and converts it from Windows-1251.
func (c *Client) get(ctx context.Context, u string) (string, error) {
	body, err := c.h.Get(ctx, u, map[string]string{
		"User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0 Safari/537.36",
		"Accept":     "text/html",
	})
	if err != nil {
		return "", err
	}
	decoded, err := charmap.Windows1251.NewDecoder().Bytes(body)
	if err != nil {
		return string(body), nil // fall back to raw bytes rather than failing
	}
	return string(decoded), nil
}

// Top returns the ranked chart for the period. Non-movie releases are dropped.
func (c *Client) Top(ctx context.Context, p Period) ([]Item, error) {
	q := url.Values{"t": {"0"}, "d": {"0"}, "f": {"0"}, "c": {"0"}, "k": {"0"}, "j": {""}, "s": {"0"}}
	if v := p.param(); v != "" {
		q.Set("w", v)
	}
	body, err := c.get(ctx, BaseURL+"/top.php?"+q.Encode())
	if err != nil {
		return nil, err
	}
	return ParseTop(body), nil
}

// Browse returns one page of the movie category sorted by popularity.
func (c *Client) Browse(ctx context.Context, page int) ([]Item, error) {
	q := url.Values{
		"c": {strconv.Itoa(CategoryMovies)},
		"t": {"1"}, // most popular first
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	body, err := c.get(ctx, BaseURL+"/browse.php?"+q.Encode())
	if err != nil {
		return nil, err
	}
	items := ParseBrowse(body)
	for i := range items {
		items[i].Rank = page*50 + items[i].Rank
	}
	return items, nil
}

// Details fetches the per-release counters, including cumulative downloads.
func (c *Client) Details(ctx context.Context, extID string) (Stats, error) {
	body, err := c.get(ctx, BaseURL+"/details.php?id="+extID)
	if err != nil {
		return Stats{}, err
	}
	return ParseDetails(body), nil
}
