package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/serik-effective/kino-cli/internal/config"
	"github.com/serik-effective/kino-cli/internal/score"
	"github.com/serik-effective/kino-cli/internal/store"
)

func TestParseTopArgs(t *testing.T) {
	a := &app{cfg: testConfig()}
	tn := a.cfg.Tuning

	cases := []struct {
		name string
		args []string
		all  bool
		want topOpts
	}{
		{name: "bare kino", args: nil,
			want: topOpts{days: tn.Defaults.Period, limit: tn.Defaults.Limit}},
		{name: "explicit period", args: []string{"30"},
			want: topOpts{days: 30, limit: tn.Defaults.Limit}},
		// "ru" carries its own window because Russian digital releases are sparse.
		{name: "ru uses its own default", args: []string{"ru"},
			want: topOpts{days: tn.Defaults.RuPeriod, limit: tn.Defaults.Limit, ru: true}},
		{name: "ru with explicit period", args: []string{"ru", "90"},
			want: topOpts{days: 90, limit: tn.Defaults.Limit, ru: true}},
		// An explicit period must survive being written before the track word.
		{name: "period before ru", args: []string{"90", "ru"},
			want: topOpts{days: 90, limit: tn.Defaults.Limit, ru: true}},
		{name: "all removes the limit", args: nil, all: true,
			want: topOpts{days: tn.Defaults.Period, limit: 0}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := parseTopArgs(c.args, a, c.all, false)
			if err != nil {
				t.Fatal(err)
			}
			if got.days != c.want.days || got.limit != c.want.limit || got.ru != c.want.ru {
				t.Errorf("got days=%d limit=%d ru=%v, want days=%d limit=%d ru=%v",
					got.days, got.limit, got.ru, c.want.days, c.want.limit, c.want.ru)
			}
		})
	}
}

func TestParseTopArgsGenreAndErrors(t *testing.T) {
	a := &app{cfg: testConfig()}

	got, err := parseTopArgs([]string{"thriller", "14"}, a, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.days != 14 || got.genre == "" {
		t.Errorf("days=%d genre=%q, want 14 and a genre", got.days, got.genre)
	}
	// The stored spelling is what matters; both must be offered to the query.
	if len(got.genreAny) < 2 {
		t.Errorf("genreAny = %v, want both the Russian and English spelling", got.genreAny)
	}

	if _, err := parseTopArgs([]string{"банан"}, a, false, false); err == nil {
		t.Error("an unknown word must be rejected, not silently ignored")
	}
	if _, err := parseTopArgs([]string{"0"}, a, false, false); err == nil {
		t.Error("a zero-day period must be rejected")
	}
}

func TestPrintTopSaysWhenFewerThanAsked(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	out := []ranked{{
		c: store.Candidate{Title: "Один", Countries: []string{"US"}, Runtime: 100,
			IMDbRating: 7.5, IMDbVotes: 50000, Digital: now.AddDate(0, 0, -1)},
		b: score.Breakdown{Final: 7.5},
	}}
	printTop(&buf, out, topOpts{days: 7, limit: 3}, now.AddDate(0, 0, -7), now, 5)

	got := buf.String()
	// Padding the list with weak films is exactly what the spec forbids.
	if !strings.Contains(got, "1 из 3") {
		t.Errorf("output must admit it found fewer than asked:\n%s", got)
	}
}

func TestPrintTopEmptyExplainsItself(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	printTop(&buf, nil, topOpts{days: 7, limit: 3}, now.AddDate(0, 0, -7), now, 12)

	got := buf.String()
	if !strings.Contains(got, "не прошёл порог") {
		t.Errorf("empty output must say why:\n%s", got)
	}
	// Knowing the pool was non-empty is what tells a user it is a threshold
	// problem and not an empty database.
	if !strings.Contains(got, "12") {
		t.Errorf("empty output should report the candidate pool:\n%s", got)
	}
}

// The Russian track must announce its rating source: the spec bars Kinopoisk
// from the general ranking, so a list that uses it has to be marked.
func TestRussianTrackLabelsItsSource(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 8, 27, 0, 0, 0, 0, time.UTC)
	printTop(&buf, nil, topOpts{days: 30, limit: 3, ru: true}, now.AddDate(0, 0, -30), now, 0)
	if !strings.Contains(buf.String(), "Кинопоиск") {
		t.Errorf("ru output must name its rating source:\n%s", buf.String())
	}
}

func TestCriticGapDirection(t *testing.T) {
	// Critics well above the audience earns a warning.
	high := store.Candidate{IMDbRating: 6.0, Metascore: 85}
	if gap, ok := criticGap(high); !ok || gap < 1.5 {
		t.Errorf("gap = %.2f ok=%v, want a large positive gap", gap, ok)
	}
	// The reverse is not a problem: the tool ranks for viewers.
	low := store.Candidate{IMDbRating: 8.0, Metascore: 45}
	if gap, ok := criticGap(low); !ok || gap > -1.5 {
		t.Errorf("gap = %.2f ok=%v, want a large negative gap", gap, ok)
	}
	// No critic score at all must never influence anything.
	if _, ok := criticGap(store.Candidate{IMDbRating: 7.0}); ok {
		t.Error("a film without critic scores must report no gap")
	}
}

func TestCompactVotes(t *testing.T) {
	for _, c := range []struct {
		n    int
		want string
	}{{42, "42"}, {2843, "3K"}, {156317, "156K"}, {2653162, "2.7M"}} {
		if got := compactVotes(c.n); got != c.want {
			t.Errorf("compactVotes(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// testConfig is the default tuning, which is what the argument parser reads.
func testConfig() *config.Config {
	return &config.Config{Tuning: config.DefaultTuning()}
}

func TestAudienceGapNeedsRealSamplesOnBothSides(t *testing.T) {
	tn := config.DefaultTuning()

	// 49 votes against 156 000 is one audience and some noise, not a disagreement.
	noise := store.Candidate{KPRating: 8.3, KPVotes: 156000, IMDbRating: 2.9, IMDbVotes: 49}
	if _, ok := audienceGap(noise, tn); ok {
		t.Error("49 IMDb votes is below the minimum sample and must not be reported")
	}

	// 127 votes is a small sample but a 2.2 point gap is far outside what
	// sampling error could produce, so this one is worth saying out loud.
	wide := store.Candidate{KPRating: 8.2, KPVotes: 202000, IMDbRating: 6.0, IMDbVotes: 127}
	gap, ok := audienceGap(wide, tn)
	if !ok || gap < tn.Signals.GapPoints {
		t.Errorf("gap = %.2f ok = %v, want a reportable positive gap", gap, ok)
	}

	// Silence when one side is missing entirely.
	if _, ok := audienceGap(store.Candidate{KPRating: 8.0, KPVotes: 50000}, tn); ok {
		t.Error("a film rated on only one site has no gap to report")
	}
}

func TestRuRangeShowsYearOnlyWhenSpanned(t *testing.T) {
	d := func(s string) time.Time {
		v, err := time.Parse("2006-01-02", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// Within one year the year is noise. Across one it is the whole point:
	// without it a 365-day window reads as a single day.
	if got, want := ruRange(d("2026-07-29"), d("2026-08-28")), "29 июля — 28 августа"; got != want {
		t.Errorf("same year: got %q, want %q", got, want)
	}
	if got, want := ruRange(d("2025-08-28"), d("2026-08-28")), "28 августа 2025 — 28 августа 2026"; got != want {
		t.Errorf("spanned: got %q, want %q", got, want)
	}
}

// An empty database and a quiet week both produce no films. Telling them apart
// is the difference between "try a wider window" and "you have not set kino up".
func TestEmptyDatabaseSuggestsSetup(t *testing.T) {
	var buf bytes.Buffer
	now := time.Now()
	printTop(&buf, nil, topOpts{days: 7, limit: 3, empty: true}, now.AddDate(0, 0, -7), now, 0)
	if !strings.Contains(buf.String(), "kino setup") {
		t.Errorf("empty database should point at setup, got:\n%s", buf.String())
	}

	buf.Reset()
	printTop(&buf, nil, topOpts{days: 7, limit: 3}, now.AddDate(0, 0, -7), now, 12)
	if strings.Contains(buf.String(), "kino setup") {
		t.Errorf("a stocked database must not be told to run setup, got:\n%s", buf.String())
	}
}

// The cards print Russian genres, the README promises Russian genres, and the
// user types Russian genres. The shorthand map is keyed in English for the
// subcommand list, so the Russian spelling has to resolve through it.
func TestGenreShorthandsAcceptBothLanguages(t *testing.T) {
	for _, c := range []struct{ word, want string }{
		{"comedy", "комедия"},
		{"комедия", "комедия"},
		{"Комедия", "комедия"},
		{"war", "военный"},
		{"военный", "военный"},
		{"documentary", "документальный"},
		{"документальный", "документальный"},
	} {
		got, ok := lookupGenre(c.word)
		if !ok {
			t.Errorf("%q not recognised as a genre", c.word)
			continue
		}
		if got[0] != c.want {
			t.Errorf("%q resolved to %q, want %q", c.word, got[0], c.want)
		}
	}
	if _, ok := lookupGenre("нежанр"); ok {
		t.Error("an unknown word must not resolve to a genre")
	}
}
