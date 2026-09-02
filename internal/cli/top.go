package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/serik-effective/kino-cli/internal/config"
	"github.com/serik-effective/kino-cli/internal/score"
	"github.com/serik-effective/kino-cli/internal/store"
)

// ranked pairs a candidate with the verdict on it.
type ranked struct {
	c store.Candidate
	b score.Breakdown
	// tuning travels with the row so the card's thresholds come from the config
	// rather than from constants buried in the renderer.
	tuning config.Tuning
}

type topOpts struct {
	days  int
	limit int // 0 means every qualifying film
	genre string
	// seen keeps already-watched films in the list.
	seen bool
	// genreAny holds every spelling of the genre to match against.
	genreAny []string
	ru       bool
	why      bool
	// empty marks a database with no films at all, which needs different
	// advice from a window that simply had no winners.
	empty bool
	// reissues keeps catalogue re-releases in the list.
	reissues bool
	nowFn    func() time.Time
}

// runTop is the whole product in one function: pick the window, take everything
// released into it, drop what cannot be judged, score the rest, print the best.
func runTop(ctx context.Context, w io.Writer, a *app, o topOpts) error {
	t := a.cfg.Tuning
	now := time.Now()
	if o.nowFn != nil {
		now = o.nowFn()
	}
	from := now.AddDate(0, 0, -o.days)
	window := time.Duration(o.days) * 24 * time.Hour

	mode := score.General
	if o.ru {
		mode = score.Russian
	}

	cands, err := a.store.Candidates(ctx, store.CandidateQuery{
		From:           from,
		To:             now,
		RussianTrack:   o.ru,
		Genres:         o.genreAny,
		MinRuntime:     t.Thresholds.MinRuntime,
		MaxYearGap:     yearGap(t, o),
		IncludeWatched: o.seen,
	})
	if err != nil {
		return err
	}

	var out []ranked
	for _, c := range cands {
		in := score.Input{
			IMDbRating: c.IMDbRating, IMDbVotes: c.IMDbVotes,
			TMDBRating: c.TMDBRating, TMDBVotes: c.TMDBVotes,
			KPRating: c.KPRating, KPVotes: c.KPVotes,
			Popularity: c.Popularity, Runtime: c.Runtime, Genres: c.Genres,
			Released: c.Digital, Now: now, Window: window,
		}
		if !score.Eligible(in, t) {
			continue
		}
		out = append(out, ranked{c: c, b: score.Score(in, t, mode), tuning: t})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].b.Final > out[j].b.Final })

	limit := o.limit
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	if len(out) == 0 && len(cands) == 0 {
		// Only worth a query when there is nothing to show anyway.
		if n, err := a.store.Count(ctx, "movies"); err == nil && n == 0 {
			o.empty = true
		}
	}
	if flagFormat == "json" {
		return writeTopJSON(w, out, o, from, now, len(cands))
	}
	printTop(w, out, o, from, now, len(cands))
	return nil
}

func printTop(w io.Writer, out []ranked, o topOpts, from, to time.Time, pool int) {
	// The two tracks answer different questions and must not claim otherwise.
	// The general one really is about digital releases. The Russian one ranks
	// on release date, because most Russian cinema has no Digital release
	// recorded anywhere, so some of it is still only in cinemas — calling that
	// list "в цифре" would be false.
	//
	// "русскоязычные", not "российские": the filter is on original language, so
	// the list legitimately carries Kazakh and Belarusian films made in Russian.
	what := "новые фильмы в цифре"
	if o.ru {
		what = "новые русскоязычные фильмы"
	}
	head := "TOP — " + what
	if o.limit > 0 {
		head = fmt.Sprintf("TOP %d — %s", o.limit, what)
	}
	if o.genre != "" {
		head += " · " + o.genre
	}
	fmt.Fprintln(w, head)
	fmt.Fprintf(w, "%s\n", ruRange(from, to))
	if o.ru {
		// The spec forbids Kinopoisk from deciding the general ranking, so a
		// list that does use it has to say so out loud.
		fmt.Fprintln(w, "ранжировано по оценкам Кинопоиска · часть фильмов пока только в прокате")
	}
	fmt.Fprintln(w)

	if len(out) == 0 {
		if o.empty {
			// An empty database is a setup problem, not a quiet week, and the
			// two look identical from here unless we say which one it is.
			fmt.Fprintln(w, "База пуста — фильмы ещё не загружены.")
			fmt.Fprintln(w, "Первый запуск:  kino setup")
			return
		}
		fmt.Fprintf(w, "За последние %d дней ни один фильм не прошёл порог качества.\n", o.days)
		if pool > 0 {
			fmt.Fprintf(w, "Кандидатов в окне было %d — им не хватает зрительских оценок.\n", pool)
		}
		return
	}

	for i, r := range out {
		printCard(w, i+1, r, o.why, o.ru)
	}

	// Never pad the list with weak films: a short honest answer beats three.
	if o.limit > 0 && len(out) < o.limit {
		fmt.Fprintf(w, "Больше фильмов, проходящих порог качества, за этот период нет (%d из %d).\n",
			len(out), o.limit)
	}
}

func printCard(w io.Writer, n int, r ranked, why, ru bool) {
	c, b := r.c, r.b
	title := c.TitleRU
	if title == "" {
		title = c.Title
	}
	fmt.Fprintf(w, "%d. %-40s %5.1f\n", n, trim(title, 40), b.Final)

	if line := ratingsLine(c); line != "" {
		fmt.Fprintf(w, "   %s\n", line)
	}
	fmt.Fprintf(w, "   %s\n", factsLine(c))
	if !c.Digital.IsZero() {
		// On the Russian track the date is the film's release, not a digital
		// drop we can confirm, so it must not be labelled "Digital".
		label := "Digital"
		if ru {
			label = "Релиз"
		}
		fmt.Fprintf(w, "   %s: %s\n", label, ruDate(c.Digital))
	}

	for _, note := range notes(c, b, r.tuning) {
		fmt.Fprintf(w, "\n   %s\n", note)
	}
	if why {
		printWhy(w, b)
	}
	fmt.Fprintln(w)
}

// ratingsLine shows the raw numbers the score was built from, so a reader can
// disagree with the ranking on the evidence rather than on faith.
func ratingsLine(c store.Candidate) string {
	var parts []string
	if c.IMDbVotes > 0 {
		parts = append(parts, fmt.Sprintf("IMDb %.1f / %s", c.IMDbRating, compactVotes(c.IMDbVotes)))
	}
	if c.TMDBVotes > 0 {
		parts = append(parts, fmt.Sprintf("TMDB %.1f / %s", c.TMDBRating, compactVotes(c.TMDBVotes)))
	}
	if c.KPVotes > 0 {
		parts = append(parts, fmt.Sprintf("КП %.1f / %s", c.KPRating, compactVotes(c.KPVotes)))
	}
	return strings.Join(parts, " · ")
}

func factsLine(c store.Candidate) string {
	var parts []string
	if f := flagsOf(c); f != "" {
		parts = append(parts, f)
	}
	if len(c.Genres) > 0 {
		parts = append(parts, strings.ToLower(c.Genres[0]))
	}
	if c.Runtime > 0 {
		parts = append(parts, fmt.Sprintf("%d мин", c.Runtime))
	}
	return strings.Join(parts, " · ")
}

// notes are the badges the spec asks for: they explain a ranking that might
// otherwise look wrong.
func notes(c store.Candidate, b score.Breakdown, t config.Tuning) []string {
	var out []string
	limit := t.Signals.GapPoints

	// Critics far above the audience is a warning, the reverse is not: the tool
	// ranks for viewers, and a crowd-pleaser the critics disliked is fine.
	if gap, ok := criticGap(c); ok {
		switch {
		case gap >= limit:
			out = append(out, "⚠ Критики оценили заметно выше зрителей")
		case gap <= -limit:
			out = append(out, "🔥 Зрителям нравится значительно больше, чем критикам")
		}
	}

	// Kinopoisk and IMDb are two different audiences, and on Russian releases
	// they can disagree by two full points. Saying so is honest; hiding it
	// behind a single number would not be.
	if gap, ok := audienceGap(c, t); ok {
		switch {
		case gap >= limit:
			out = append(out, "⚠ Международная аудитория оценила заметно ниже")
		case gap <= -limit:
			out = append(out, "⚠ На Кинопоиске оценили заметно ниже, чем за рубежом")
		}
	}
	if b.Confidence >= 0.75 && b.IMDbWeighted >= 7 {
		out = append(out, "🔥 Хорошо принят зрителями")
	}
	// A penalty too small to show would print as "−0.0" and read like a bug.
	if b.KidsPenalty > 0 {
		label := "👶 Детское / семейное"
		if !isKidsGenre(c.Genres) {
			label = "👶 Анимация"
		}
		out = append(out, label)
	}
	if b.Penalty >= 0.05 {
		out = append(out, fmt.Sprintf("🎨 Риск артхауса: %s (−%.1f к оценке)",
			arthouseWord(b.Arthouse), b.Penalty))
	}
	switch {
	// Say only what we can stand behind: the film is watchable on Kinopoisk.
	// Which subscription tier it needs is their business and changes often.
	case c.WatchOnline:
		out = append(out, "▶ Есть на Кинопоиске")
	// Only claim a film is cinema-only when someone actually told us it is not
	// streaming. TMDB records no digital release for most Russian and Kazakh
	// cinema — that absence is why this track exists at all, and reading it as
	// "not available" labelled 588 films we simply never checked.
	case c.WatchKnown && !c.WatchOnline && c.HasTheatrical && !c.HasDigital:
		out = append(out, "🎬 Только в прокате")
	}
	return out
}

// criticGap is the critics' score minus the audience's, on the 0..10 scale.
// It returns false when we have no critic number at all, which must never
// affect a film's placing.
func criticGap(c store.Candidate) (float64, bool) {
	audience := c.IMDbRating
	if audience == 0 {
		audience = c.TMDBRating
	}
	if audience == 0 {
		return 0, false
	}
	switch {
	case c.Metascore > 0:
		return float64(c.Metascore)/10 - audience, true
	case c.RTScore > 0:
		return float64(c.RTScore)/10 - audience, true
	}
	return 0, false
}

// audienceGap is the Kinopoisk rating minus the IMDb one. It reports nothing
// unless both sides carry a real sample: 8.3 from 156 000 against 2.9 from 49
// is not a disagreement between audiences, it is one audience and some noise.
func audienceGap(c store.Candidate, t config.Tuning) (float64, bool) {
	min := t.Signals.GapMinVotes
	if c.KPVotes < min || c.IMDbVotes < min || c.KPRating == 0 || c.IMDbRating == 0 {
		return 0, false
	}
	return c.KPRating - c.IMDbRating, true
}

// isKidsGenre mirrors the scorer's family test so the badge can say which of
// the two discounts applied.
func isKidsGenre(genres []string) bool {
	for _, g := range genres {
		switch strings.ToLower(strings.TrimSpace(g)) {
		case "family", "семейный", "kids", "детский":
			return true
		}
	}
	return false
}

func arthouseWord(v float64) string {
	switch {
	case v >= 0.6:
		return "высокий"
	case v >= 0.3:
		return "средний"
	default:
		return "низкий"
	}
}

func printWhy(w io.Writer, b score.Breakdown) {
	fmt.Fprintln(w)
	fmt.Fprintf(w, "   IMDb weighted:  %6.2f\n", b.IMDbWeighted)
	fmt.Fprintf(w, "   TMDB weighted:  %6.2f\n", b.TMDBWeighted)
	if b.KPWeighted > 0 {
		fmt.Fprintf(w, "   КП weighted:    %6.2f\n", b.KPWeighted)
	}
	fmt.Fprintf(w, "   Confidence:     %6.2f\n", b.Confidence)
	fmt.Fprintf(w, "   Mainstream:     %6.2f\n", b.Mainstream)
	fmt.Fprintf(w, "   Freshness:      %6.2f\n", b.Freshness)
	fmt.Fprintf(w, "   Arthouse:       %6.2f  (штраф %.2f)\n", b.Arthouse, b.Penalty)
	if b.Gap != 0 {
		fmt.Fprintf(w, "   Разрыв КП/IMDb: %6.2f  (штраф %.2f)\n", b.Gap, b.GapPenalty)
	}
	if b.KidsPenalty != 0 {
		fmt.Fprintf(w, "   Детское:        штраф %.2f\n", b.KidsPenalty)
	}
	fmt.Fprintf(w, "   Источники:      %s\n", strings.Join(b.RatingSources, ", "))
	fmt.Fprintf(w, "   Final score:    %6.2f\n", b.Final)
}

// countries are stored as ISO 3166-1 codes. Only the ones we actually see are
// named; anything else falls back to its bare code, which is honest rather than
// a guessed flag.
var countryNames = map[string]struct{ flag, name string }{
	"RU": {"🇷🇺", "Россия"}, "US": {"🇺🇸", "США"}, "KZ": {"🇰🇿", "Казахстан"},
	"GB": {"🇬🇧", "Великобритания"}, "FR": {"🇫🇷", "Франция"}, "DE": {"🇩🇪", "Германия"},
	"JP": {"🇯🇵", "Япония"}, "KR": {"🇰🇷", "Южная Корея"}, "CN": {"🇨🇳", "Китай"},
	"IN": {"🇮🇳", "Индия"}, "IT": {"🇮🇹", "Италия"}, "ES": {"🇪🇸", "Испания"},
	"CA": {"🇨🇦", "Канада"}, "AU": {"🇦🇺", "Австралия"}, "UZ": {"🇺🇿", "Узбекистан"},
	"BY": {"🇧🇾", "Беларусь"}, "MX": {"🇲🇽", "Мексика"}, "BR": {"🇧🇷", "Бразилия"},
	"SE": {"🇸🇪", "Швеция"}, "DK": {"🇩🇰", "Дания"}, "NO": {"🇳🇴", "Норвегия"},
	"PL": {"🇵🇱", "Польша"}, "TR": {"🇹🇷", "Турция"}, "IE": {"🇮🇪", "Ирландия"},
	"NZ": {"🇳🇿", "Новая Зеландия"}, "AR": {"🇦🇷", "Аргентина"}, "NL": {"🇳🇱", "Нидерланды"},
}

func flagsOf(c store.Candidate) string {
	var out []string
	for _, code := range c.Countries {
		if v, ok := countryNames[code]; ok {
			out = append(out, v.flag+" "+v.name)
		} else {
			out = append(out, code)
		}
		if len(out) == 2 { // a co-production shows two, not twelve
			break
		}
	}
	return strings.Join(out, " / ")
}

func compactVotes(n int) string {
	switch {
	case n >= 1000000:
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	case n >= 1000:
		return fmt.Sprintf("%.0fK", float64(n)/1000)
	default:
		return fmt.Sprintf("%d", n)
	}
}

var ruMonths = [...]string{"января", "февраля", "марта", "апреля", "мая", "июня",
	"июля", "августа", "сентября", "октября", "ноября", "декабря"}

func ruDate(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return fmt.Sprintf("%d %s", t.Day(), ruMonths[int(t.Month())-1])
}

// ruRange labels the window. The year is normally noise — everyone knows what
// year it is — but a window that spans one makes "28 августа — 28 августа"
// read as a single day, so it earns its place there.
func ruRange(from, to time.Time) string {
	if from.IsZero() || to.IsZero() {
		return ruDate(from) + " — " + ruDate(to)
	}
	if from.Year() == to.Year() {
		return ruDate(from) + " — " + ruDate(to)
	}
	return fmt.Sprintf("%s %d — %s %d", ruDate(from), from.Year(), ruDate(to), to.Year())
}

func trim(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// yearGap is the re-release cutoff actually applied. "--reissues" turns it off,
// which is the only way to ask for an old film that just became watchable.
func yearGap(t config.Tuning, o topOpts) int {
	if o.reissues {
		return 0
	}
	return t.Thresholds.MaxReleaseGapYears
}
