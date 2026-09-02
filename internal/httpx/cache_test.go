package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// memCache is an in-memory Cache for tests.
type memCache struct {
	mu   sync.Mutex
	body map[string][]byte
	at   map[string]time.Time
}

func newMemCache() *memCache {
	return &memCache{body: map[string][]byte{}, at: map[string]time.Time{}}
}

func (m *memCache) Lookup(source, key string) ([]byte, time.Time, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.body[source+key]
	return b, m.at[source+key], ok
}

func (m *memCache) StoreResponse(source, key string, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.body[source+key] = body
	m.at[source+key] = time.Now()
	return nil
}

// age backdates an entry so it looks stale.
func (m *memCache) age(source, key string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.at[source+key] = time.Now().Add(-d)
}

func testClient(cache Cache, ttl time.Duration) *Client {
	c := New("test", 1000, 1000)
	c.MaxRetries = 0
	c.Cache = cache
	c.TTL = ttl
	return c
}

func TestFreshCacheHitSkipsTheNetwork(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`{"n":1}`))
	}))
	defer srv.Close()

	c := testClient(newMemCache(), time.Hour)
	for i := 0; i < 3; i++ {
		body, err := c.Get(context.Background(), srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != `{"n":1}` {
			t.Fatalf("body = %q", body)
		}
	}
	if hits != 1 {
		t.Errorf("server saw %d requests, want 1 — the cache is not being used", hits)
	}
}

func TestExpiredCacheRefetches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(`ok`))
	}))
	defer srv.Close()

	cache := newMemCache()
	c := testClient(cache, time.Hour)
	if _, err := c.Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	cache.age("test", srv.URL, 2*time.Hour)
	if _, err := c.Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("server saw %d requests, want 2 — a stale entry must be refreshed", hits)
	}
}

// The resilience requirement: a dead source degrades the answer, it does not
// remove it, and the caller learns the data is old.
func TestDeadSourceServesStaleAndWarns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`cached-answer`))
	}))
	cache := newMemCache()
	c := testClient(cache, time.Hour)
	if _, err := c.Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	url := srv.URL
	srv.Close() // the source is now unreachable
	cache.age("test", url, 30*time.Hour)

	var gotAge time.Duration
	var gotSource string
	c.OnStale = func(source string, age time.Duration) { gotSource, gotAge = source, age }

	body, err := c.Get(context.Background(), url, nil)
	if err != nil {
		t.Fatalf("a stale answer should have been served, got error: %v", err)
	}
	if string(body) != "cached-answer" {
		t.Errorf("body = %q", body)
	}
	if gotSource != "test" || gotAge < 29*time.Hour {
		t.Errorf("OnStale(%q, %v) — expected the real age of the entry", gotSource, gotAge)
	}
}

// A 4xx is a real answer. Serving old data over a bad key or a deleted record
// would hide the problem instead of surfacing it.
func TestClientErrorDoesNotFallBackToCache(t *testing.T) {
	mode := "ok"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if mode == "ok" {
			w.Write([]byte(`fine`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cache := newMemCache()
	c := testClient(cache, time.Hour)
	if _, err := c.Get(context.Background(), srv.URL, nil); err != nil {
		t.Fatal(err)
	}
	cache.age("test", srv.URL, 30*time.Hour)
	mode = "denied"

	stale := false
	c.OnStale = func(string, time.Duration) { stale = true }

	_, err := c.Get(context.Background(), srv.URL, nil)
	if err == nil {
		t.Fatal("a 401 must surface, not be masked by the cache")
	}
	var he *HTTPError
	if !errors.As(err, &he) || he.Status != http.StatusUnauthorized {
		t.Errorf("err = %v, want an HTTPError 401", err)
	}
	if stale {
		t.Error("OnStale must not fire for a client error")
	}
}

func TestNoCacheConfiguredStillWorks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`live`))
	}))
	defer srv.Close()

	c := New("test", 1000, 1000)
	body, err := c.Get(context.Background(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "live" {
		t.Errorf("body = %q", body)
	}
}
