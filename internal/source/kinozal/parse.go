// Package kinozal reads public popularity data from kinozal.me. It collects
// metadata only — titles, ranks, seed and download counters — and never
// downloads torrents or their content.
package kinozal

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// Item is one torrent release parsed from a listing page.
type Item struct {
	ExtID     string
	Rank      int // position in the listing, 1-based
	RawTitle  string
	TitleRU   string
	TitleOrig string
	Year      int
	Tags      string // voice-over and subtitle markers: ДБ, ПМ, СТ…
	Quality   string // WEB-DLRip, BDRip (720p)…
	Seeds     int
	Leechers  int
	Comments  int
}

var (
	topItemRe = regexp.MustCompile(`href='/details\.php\?id=(\d+)' title='([^']+)'`)
	rowRe     = regexp.MustCompile(`(?s)<tr class=bg>.*?</tr>`)
	rowIDRe   = regexp.MustCompile(`details\.php\?id=(\d+)`)
	rowNameRe = regexp.MustCompile(`class="r\d+">([^<]+)<`)
	rowNumRe  = regexp.MustCompile(`<td class='(?:s|sl_s|sl_p)'>([^<]*)</td>`)
	yearRe    = regexp.MustCompile(`^(19|20)\d{2}$`)
	// Non-movie releases: series, collections, books, discographies.
	rejectRe = regexp.MustCompile(`(?i)(\d+\s*сезон|серии из|серия из|книг(?:а|и)? из|\(\d+\s*CD\)|Том\s+\d|Коллекция|Антология|Сборник|Дискография|полное собрание)`)
	// Audio-only, e-book and software releases never carry a movie.
	badFormatRe = regexp.MustCompile(`(?i)(\b(MP3|FLAC|APE|WavPack|AAC|FB2|EPUB|PDF|DjVu|x64|x86|RePack|Portable|MacOS|Linux|Android|iOS)\b|PC \(Windows\))`)
)

// ParseTop extracts the ranked poster grid from top.php.
func ParseTop(body string) []Item {
	var out []Item
	for i, m := range topItemRe.FindAllStringSubmatch(body, -1) {
		it := Item{ExtID: m[1], Rank: i + 1, RawTitle: html.UnescapeString(m[2])}
		if !parseTitle(&it) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// ParseBrowse extracts rows from browse.php, which also carry seed counters.
func ParseBrowse(body string) []Item {
	var out []Item
	for i, row := range rowRe.FindAllString(body, -1) {
		id := rowIDRe.FindStringSubmatch(row)
		name := rowNameRe.FindStringSubmatch(row)
		if id == nil || name == nil {
			continue
		}
		it := Item{ExtID: id[1], Rank: i + 1, RawTitle: html.UnescapeString(name[1])}
		if !parseTitle(&it) {
			continue
		}
		// Columns are: comments, size, seeds, leechers.
		nums := rowNumRe.FindAllStringSubmatch(row, -1)
		if len(nums) >= 4 {
			it.Comments = atoi(nums[0][1])
			it.Seeds = atoi(nums[2][1])
			it.Leechers = atoi(nums[3][1])
		}
		out = append(out, it)
	}
	return out
}

// parseTitle splits "RU / Original / Year / tags / quality" and reports whether
// the release looks like a single movie.
func parseTitle(it *Item) bool {
	title := strings.TrimSpace(it.RawTitle)
	if rejectRe.MatchString(title) || badFormatRe.MatchString(title) {
		return false
	}

	parts := strings.Split(title, " / ")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}

	yearAt := -1
	for i, p := range parts {
		if yearRe.MatchString(p) {
			yearAt = i
			break
		}
	}
	if yearAt <= 0 {
		return false // no year, or the title itself is a year — not a movie row
	}
	it.Year, _ = strconv.Atoi(parts[yearAt])

	it.TitleRU = parts[0]
	if yearAt >= 2 {
		// Everything between the Russian title and the year is the original
		// title, which may itself contain " / " for alternate names.
		it.TitleOrig = strings.Join(parts[1:yearAt], " / ")
	}
	rest := parts[yearAt+1:]
	if len(rest) > 0 {
		it.Quality = rest[len(rest)-1]
		it.Tags = strings.Join(rest[:len(rest)-1], ", ")
	}
	return it.TitleRU != ""
}

var (
	seedsRe     = regexp.MustCompile(`Раздают\s+([\d\s]+)`)
	leechersRe  = regexp.MustCompile(`Скачивают\s+([\d\s]+)`)
	downloadsRe = regexp.MustCompile(`Скачали\s+([\d\s]+)`)
	commentsRe  = regexp.MustCompile(`Комментариев\s+([\d\s]+)`)
	tagRe       = regexp.MustCompile(`(?s)<[^>]+>`)
	spaceRe     = regexp.MustCompile(`\s+`)
)

// Stats are the counters shown on details.php.
type Stats struct {
	Seeds     int
	Leechers  int
	Downloads int
	Comments  int
}

// ParseDetails pulls the counters out of a details page. "Скачали" is the
// cumulative download count — the number that makes a trend once sampled twice.
func ParseDetails(body string) Stats {
	text := spaceRe.ReplaceAllString(tagRe.ReplaceAllString(body, " "), " ")
	grab := func(re *regexp.Regexp) int {
		if m := re.FindStringSubmatch(text); m != nil {
			return atoi(m[1])
		}
		return 0
	}
	return Stats{
		Seeds:     grab(seedsRe),
		Leechers:  grab(leechersRe),
		Downloads: grab(downloadsRe),
		Comments:  grab(commentsRe),
	}
}

func atoi(s string) int {
	s = strings.NewReplacer(" ", "", " ", "", ",", "", ".", "").Replace(strings.TrimSpace(s))
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
