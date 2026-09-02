package imdb

import (
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// gzServer serves the given text gzipped, the way datasets.imdbws.com does.
func gzServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(buf.Bytes())
	}))
}

func TestRatingsSkipsHeaderAndMalformedRows(t *testing.T) {
	srv := gzServer(t, "tconst\taverageRating\tnumVotes\n"+
		"tt0000001\t7.4\t2100\n"+
		"tt0000002\tnot-a-number\t50\n"+ // malformed: skipped, not fatal
		"tt0000003\t6.1\t980\n")
	defer srv.Close()

	c := &Client{HTTP: srv.Client()}
	var got []Rating
	n, err := c.streamRatings(context.Background(), srv.URL, func(r Rating) error {
		got = append(got, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 || len(got) != 2 {
		t.Fatalf("kept %d rows (%v), want 2", n, got)
	}
	if got[0] != (Rating{ID: "tt0000001", Rating: 7.4, Votes: 2100}) {
		t.Errorf("first row = %+v", got[0])
	}
}

func TestTitlesKeepsOnlyFeatureFilms(t *testing.T) {
	const header = "tconst\ttitleType\tprimaryTitle\toriginalTitle\tisAdult\tstartYear\tendYear\truntimeMinutes\tgenres\n"
	srv := gzServer(t, header+
		"tt1\tmovie\tKeeper\tKeeper Orig\t0\t2020\t\\N\t118\tDrama\n"+
		"tt2\tshort\tShorty\t\\N\t0\t2020\t\\N\t9\tComedy\n"+ // not a feature
		"tt3\ttvEpisode\tEpisode\t\\N\t0\t2020\t\\N\t45\tDrama\n"+ // not a feature
		"tt4\tmovie\tAdult\t\\N\t1\t2020\t\\N\t90\tAdult\n"+ // adult
		"tt5\tmovie\tTooOld\t\\N\t0\t1998\t\\N\t100\tDrama\n"+ // before the floor
		"tt6\tmovie\tNoYear\t\\N\t0\t\\N\t\\N\t100\tDrama\n"+ // year unknown
		"tt7\tmovie\tNoRuntime\t\\N\t0\t2021\t\\N\t\\N\tHorror\n")
	defer srv.Close()

	c := &Client{HTTP: srv.Client()}
	var got []Title
	n, err := c.streamTitles(context.Background(), srv.URL, TitleFilter{YearFloor: 2015}, func(tl Title) error {
		got = append(got, tl)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("kept %d rows (%v), want 2 (tt1, tt7)", n, got)
	}
	if got[0].ID != "tt1" || got[0].OrigName != "Keeper Orig" || got[0].Runtime != 118 {
		t.Errorf("tt1 = %+v", got[0])
	}
	// \N must become a zero value, never the literal backslash-N.
	if got[1].ID != "tt7" || got[1].OrigName != "" || got[1].Runtime != 0 {
		t.Errorf("tt7 = %+v", got[1])
	}
}

func TestTitleFilterYearFloorZeroKeepsEveryYear(t *testing.T) {
	f := TitleFilter{}
	if !f.keep("movie", false, 1935) {
		t.Error("year floor 0 should keep old films")
	}
	if f.keep("movie", false, 0) {
		t.Error("a film with no year must be dropped")
	}
}
