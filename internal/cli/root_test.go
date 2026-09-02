package cli

import "testing"

func TestParseDays(t *testing.T) {
	cases := map[string]int{"7d": 7, "2w": 14, "3m": 90, "1y": 365, "10": 10}
	for in, want := range cases {
		got, err := parseDays(in)
		if err != nil || got != want {
			t.Errorf("parseDays(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "0d", "-3d", "week"} {
		if _, err := parseDays(bad); err == nil {
			t.Errorf("parseDays(%q) succeeded; want error", bad)
		}
	}
}

func TestParseWindow(t *testing.T) {
	if _, _, err := parseWindow("7d", "2026-01-01", ""); err == nil {
		t.Error("--last with --from succeeded; want error")
	}
	if _, _, err := parseWindow("", "", ""); err == nil {
		t.Error("empty window succeeded; want error")
	}
	if _, _, err := parseWindow("", "2026-08-27", "2026-08-01"); err == nil {
		t.Error("reversed range succeeded; want error")
	}
	from, to, err := parseWindow("", "2026-08-01", "2026-08-27")
	if err != nil || from != "2026-08-01" || to != "2026-08-27" {
		t.Errorf("parseWindow = %q..%q, %v", from, to, err)
	}
	from, to, err = parseWindow("7d", "", "")
	if err != nil || from > to || len(from) != 10 || len(to) != 10 {
		t.Errorf("parseWindow(7d) = %q..%q, %v", from, to, err)
	}
}

func TestParseID(t *testing.T) {
	if id, imdb := parseID("tt1285016"); id != 0 || imdb != "tt1285016" {
		t.Errorf("parseID(tt…) = %d, %q", id, imdb)
	}
	if id, imdb := parseID("tmdb:9799"); id != 9799 || imdb != "" {
		t.Errorf("parseID(tmdb:9799) = %d, %q", id, imdb)
	}
	if id, _ := parseID("123"); id != 123 {
		t.Errorf("parseID(123) = %d", id)
	}
}

// The help text lists a few suffixes; the parser accepts one more. Documenting
// "--last 1y" for the initial database fill only works if it really parses.
func TestParseDaysAcceptsEverySuffix(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int
	}{
		{"7d", 7}, {"2w", 14}, {"3m", 90}, {"1y", 365}, {"30", 30}, {"1Y", 365},
	} {
		got, err := parseDays(c.in)
		if err != nil {
			t.Errorf("%q: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%q = %d days, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "0d", "-5", "неделя"} {
		if _, err := parseDays(bad); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
