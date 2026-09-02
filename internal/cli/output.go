package cli

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/sbeysenov/kino-cli/internal/model"
)

// writeJSON is the shared pretty-printer for list-shaped output.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeMovies(w io.Writer, movies []*model.Movie, format string) error {
	switch format {
	case "json":
		if movies == nil {
			movies = []*model.Movie{}
		}
		return writeJSON(w, movies)
	case "csv":
		cw := csv.NewWriter(w)
		defer cw.Flush()
		if err := cw.Write([]string{"date", "tmdb_id", "imdb_id", "title", "title_ru", "year", "lang", "countries", "tmdb", "tmdb_votes", "imdb", "kp"}); err != nil {
			return err
		}
		for _, m := range movies {
			if err := cw.Write([]string{
				m.MatchDate, strconv.Itoa(m.TMDBID), m.IMDbID, m.Title, m.TitleRU,
				intStr(m.Year), m.OrigLang, strings.Join(m.Countries, " "), floatStr(m.TMDBRating), intStr(m.TMDBVotes), floatStr(m.IMDbRating), floatStr(m.KPRating),
			}); err != nil {
				return err
			}
		}
		return cw.Error()
	case "table", "":
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "DATE\tTMDB\tVOTES\tIMDB\tKP\tLANG\tCOUNTRY\tTITLE")
		for _, m := range movies {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				dash(m.MatchDate), dash(floatStr(m.TMDBRating)), dash(intStr(m.TMDBVotes)),
				dash(floatStr(m.IMDbRating)), dash(floatStr(m.KPRating)), dash(m.OrigLang),
				dash(strings.Join(m.Countries, " ")), displayTitle(m))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
		if len(movies) == 0 {
			fmt.Fprintln(w, "(no rows)")
		}
		return nil
	default:
		return fmt.Errorf("unknown format %q, want table|json|csv", format)
	}
}

type movieDetail struct {
	*model.Movie
	Releases []model.Release `json:"releases"`
}

func writeMovieDetail(w io.Writer, m *model.Movie, rels []model.Release, format string) error {
	switch format {
	case "json":
		return writeJSON(w, movieDetail{Movie: m, Releases: rels})
	default:
		tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "title\t%s\n", displayTitle(m))
		fmt.Fprintf(tw, "original\t%s\n", m.OrigTitle)
		fmt.Fprintf(tw, "ids\ttmdb:%d  %s  kp:%s\n", m.TMDBID, dash(m.IMDbID), dash(intStr(m.KPID)))
		fmt.Fprintf(tw, "year / runtime\t%s / %s\n", dash(intStr(m.Year)), dash(intStr(m.Runtime)))
		fmt.Fprintf(tw, "language / released\t%s / %s\n", dash(m.OrigLang), dash(m.ReleaseDate))
		fmt.Fprintf(tw, "tmdb\t%s (%s votes)\n", dash(floatStr(m.TMDBRating)), dash(intStr(m.TMDBVotes)))
		fmt.Fprintf(tw, "imdb\t%s (%s votes)\n", dash(floatStr(m.IMDbRating)), dash(intStr(m.IMDbVotes)))
		fmt.Fprintf(tw, "metascore / rt\t%s / %s\n", dash(intStr(m.Metascore)), dash(intStr(m.RTScore)))
		fmt.Fprintf(tw, "kinopoisk\t%s (%s votes, %s views)\n",
			dash(floatStr(m.KPRating)), dash(intStr(m.KPVotes)), dash(intStr(m.KPViews)))
		fmt.Fprintf(tw, "смотреть в РФ\t%s\n", watchStatus(m))
		fmt.Fprintf(tw, "genres\t%v\n", m.Genres)
		fmt.Fprintf(tw, "countries\t%v\n", m.Countries)
		fmt.Fprintf(tw, "updated / enriched\t%s / %s\n", dash(m.UpdatedAt), dash(m.EnrichedAt))
		if err := tw.Flush(); err != nil {
			return err
		}
		if overview := firstNonEmpty(m.OverviewRU, m.Overview); overview != "" {
			fmt.Fprintf(w, "\n%s\n", overview)
		}
		if len(rels) > 0 {
			fmt.Fprintln(w, "\nreleases:")
			rt := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
			for _, r := range rels {
				fmt.Fprintf(rt, "  %s\t%s\t%s\t%s\n", r.Date, r.Region, releaseTypeName(r.Type), r.Note)
			}
			return rt.Flush()
		}
		return nil
	}
}

// watchStatus renders RU availability captured from the Kinopoisk player.
func watchStatus(m *model.Movie) string {
	if m.WatchOnline == nil {
		return "—"
	}
	if !*m.WatchOnline {
		return "нет онлайн"
	}
	if m.WatchOffer != "" {
		return m.WatchOffer
	}
	return "доступен онлайн"
}

func releaseTypeName(t int) string {
	switch t {
	case model.ReleasePremiere:
		return "premiere"
	case model.ReleaseLimited:
		return "theatrical (limited)"
	case model.ReleaseTheater:
		return "theatrical"
	case model.ReleaseDigital:
		return "digital"
	case model.ReleasePhysical:
		return "physical"
	case model.ReleaseTV:
		return "tv"
	}
	return strconv.Itoa(t)
}

func displayTitle(m *model.Movie) string {
	if m.TitleRU != "" && m.TitleRU != m.Title {
		return m.TitleRU + " / " + m.Title
	}
	return m.Title
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func dash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func intStr(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

func floatStr(v *float64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', 1, 64)
}
