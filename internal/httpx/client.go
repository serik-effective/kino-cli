// Package httpx is the single place where outbound HTTP happens: rate limiting,
// retries and per-source call accounting all live here.
package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// Cache stores API responses so a repeated question costs nothing, and so a
// source being down degrades the answer instead of breaking it.
type Cache interface {
	// Lookup returns the cached body and when it was fetched.
	Lookup(source, key string) ([]byte, time.Time, bool)
	StoreResponse(source, key string, body []byte) error
}

type Client struct {
	Name       string
	HTTP       *http.Client
	MaxRetries int
	// OnCall is invoked once per HTTP request actually sent, retries included.
	OnCall func(source string, n int)

	// Cache, when set, is consulted before every request. Leave it nil for
	// sources whose answers must always be live.
	Cache Cache
	// TTL is how long a cached body is served without asking again.
	TTL time.Duration
	// OnStale is called when the network failed and a cached body older than
	// TTL was served instead. The caller is expected to tell the user: an
	// answer built on week-old data must not look like a fresh one.
	OnStale func(source string, age time.Duration)

	lim *rate.Limiter
}

// keyFn turns a URL into a cache key. It is a variable so the store can supply
// a hash: the raw URL sometimes carries an API key.
var keyFn = func(u string) string { return u }

// SetCacheKeyFunc installs the hashing used for cache keys.
func SetCacheKeyFunc(f func(string) string) { keyFn = f }

func New(name string, rps float64, burst int) *Client {
	return &Client{
		Name:       name,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		MaxRetries: 4,
		lim:        rate.NewLimiter(rate.Limit(rps), burst),
	}
}

// HTTPError carries a non-retryable status back to the caller.
type HTTPError struct {
	Status int
	Body   string
	URL    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: HTTP %d: %s", e.URL, e.Status, truncate(e.Body, 200))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func (c *Client) GetJSON(ctx context.Context, u string, headers map[string]string, out any) error {
	body, err := c.Get(ctx, u, headers)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%s: decode: %w", redact(u), err)
	}
	return nil
}

// Get returns the raw response body, retrying on 429 and 5xx.
func (c *Client) Get(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	var cacheKey string
	if c.Cache != nil {
		cacheKey = keyFn(u)
		if body, at, ok := c.Cache.Lookup(c.Name, cacheKey); ok && time.Since(at) < c.TTL {
			return body, nil
		}
	}

	body, err := c.fetch(ctx, u, headers)
	if err == nil {
		if c.Cache != nil {
			// A cache write failing must not fail the request: we have the answer.
			_ = c.Cache.StoreResponse(c.Name, cacheKey, body)
		}
		return body, nil
	}

	// The network is down or the source is broken: a stale answer beats none, as
	// long as the caller is told how stale it is. A 4xx is different — it is a
	// real answer, and serving old data over a bad key or a deleted record would
	// hide the problem instead of surfacing it.
	if c.Cache != nil && !isClientError(err) {
		if cached, at, ok := c.Cache.Lookup(c.Name, cacheKey); ok {
			if c.OnStale != nil {
				c.OnStale(c.Name, time.Since(at))
			}
			return cached, nil
		}
	}
	return nil, err
}

// isClientError reports a 4xx, which no amount of retrying or caching fixes.
func isClientError(err error) bool {
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status >= 400 && he.Status < 500
	}
	return false
}

func (c *Client) fetch(ctx context.Context, u string, headers map[string]string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.MaxRetries; attempt++ {
		if err := c.lim.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		for k, v := range headers {
			req.Header.Set(k, v)
		}

		if c.OnCall != nil {
			c.OnCall(c.Name, 1)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		}

		switch {
		case resp.StatusCode == http.StatusTooManyRequests:
			wait := backoff(attempt)
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if secs, err := strconv.Atoi(ra); err == nil {
					wait = time.Duration(secs) * time.Second
				}
			}
			lastErr = &HTTPError{Status: resp.StatusCode, Body: string(body), URL: redact(u)}
			if err := sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode >= 500:
			lastErr = &HTTPError{Status: resp.StatusCode, Body: string(body), URL: redact(u)}
			if err := sleep(ctx, backoff(attempt)); err != nil {
				return nil, err
			}
			continue
		case resp.StatusCode >= 400:
			return nil, &HTTPError{Status: resp.StatusCode, Body: string(body), URL: redact(u)}
		}
		return body, nil
	}
	return nil, fmt.Errorf("%s: giving up after %d attempts: %w", c.Name, c.MaxRetries+1, lastErr)
}

func backoff(attempt int) time.Duration {
	base := time.Duration(1<<attempt) * 500 * time.Millisecond
	return base + time.Duration(rand.Int64N(300))*time.Millisecond
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// redact strips an apikey query parameter so keys never reach logs or errors.
func redact(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	for _, k := range []string{"apikey", "api_key"} {
		if q.Get(k) != "" {
			q.Set(k, "***")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}
