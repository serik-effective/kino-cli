package cli

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/serik-effective/kino-cli/internal/model"
	"github.com/serik-effective/kino-cli/internal/pipeline"
)

func newUpdateCmd() *cobra.Command {
	imdbCmd := newUpdateIMDbCmd()
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Refresh the local database from the sources",
		Long: `Refresh the local database from the sources.

With no subcommand this refreshes the IMDb datasets, which is the bulk audience
rating everything else is scored from. "update movies" pulls new releases from
TMDB instead.`,
		// Bare "kino update" is the IMDb refresh: it is the one command a new
		// install must run before anything can be ranked.
		RunE: imdbCmd.RunE,
	}
	cmd.Flags().AddFlagSet(imdbCmd.Flags())
	cmd.AddCommand(newUpdateMoviesCmd(), imdbCmd, newUpdateKPCmd())
	return cmd
}

func newUpdateKPCmd() *cobra.Command {
	var (
		fromYear, toYear int
		minVotes         int
		countries        []string
		maxPages         int
		addMissing       bool
		dryRun           bool
	)

	cmd := &cobra.Command{
		Use:   "kp",
		Short: "Забрать срез каталога Кинопоиска: id, рейтинги и сервисы, где фильм идёт",
		Long: `Забрать срез каталога Кинопоиска через API.

Это дополнение к "update movies" для российского кино, которое TMDB покрывает
плохо: команда приносит kp_id, рейтинг с числом голосов и — чего не даёт больше
никто — список сервисов, где фильм реально доступен.

Одна отфильтрованная страница возвращает до 250 фильмов, так что год российских
релизов стоит один-два запроса из суточной квоты в 200.`,
		Example: `  kino update kp --from-year 2025
  kino update kp --from-year 2024 --min-votes 5000 --add-missing
  kino update kp --from-year 2026 --countries Казахстан --dry-run`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(true)
			if err != nil {
				return err
			}
			defer a.Close()

			res, err := a.deps.UpdateKP(cmd.Context(), pipeline.UpdateKPOpts{
				FromYear: fromYear, ToYear: toYear,
				MinVotes: minVotes, Countries: countries,
				MaxPages: maxPages, AddMissing: addMissing, DryRun: dryRun,
			})
			suffix := ""
			if dryRun {
				suffix = " (dry run, ничего не записано)"
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"фильмов %d: по kp_id %d, по imdb %d, по названию %d, добавлено с TMDB %d, не найдено %d%s\n",
				res.Seen, res.ByKPID, res.ByIMDb, res.ByTitle, res.Added, res.Unmatched, suffix)
			return err
		},
	}
	f := cmd.Flags()
	f.IntVar(&fromYear, "from-year", 0, "нижняя граница года выпуска")
	f.IntVar(&toYear, "to-year", 0, "верхняя граница года выпуска")
	f.IntVar(&minVotes, "min-votes", 3000, "минимум голосов на Кинопоиске")
	f.StringSliceVar(&countries, "countries", nil, "страны производства, например Россия,Казахстан")
	f.IntVar(&maxPages, "max-pages", defaultKPPages,
		"бюджет запросов: страниц по 250 фильмов (0 = без ограничения)")
	f.BoolVar(&addMissing, "add-missing", false, "добавлять неизвестные фильмы через TMDB")
	f.BoolVar(&dryRun, "dry-run", false, "показать, что было бы сделано")
	return cmd
}

// defaultKPPages bounds what one catalogue run may spend. The band walk lowers
// its vote ceiling to get past the tier's 10-page wall, and a catalogue where
// thousands of films share one vote count would descend a step at a time — 40
// pages is 10 000 films, far past anything useful, and still a fifth of the
// daily quota.
const defaultKPPages = 40

func newUpdateIMDbCmd() *cobra.Command {
	var (
		yearFloor   int
		ratingsOnly bool
	)

	cmd := &cobra.Command{
		Use:   "imdb",
		Short: "Download the IMDb Non-Commercial Datasets into the local database",
		Long: `Download the IMDb Non-Commercial Datasets into the local database.

title.ratings is imported in full: it is small and every row is an audience
rating we may need. title.basics is streamed and filtered to feature films from
--year-floor onwards, because the full file is 11+ million rows and roughly
900 MB expanded. Neither file is ever written to disk whole.

Ratings are then copied onto the films we already track, keyed by imdb_id.`,
		Example: `  kino update imdb
  kino update imdb --year-floor 2000
  kino update imdb --ratings-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			if yearFloor == 0 {
				yearFloor = a.cfg.Tuning.Defaults.IMDbYearFloor
			}
			res, err := a.deps.ImportIMDb(cmd.Context(), pipeline.ImportIMDbOpts{
				YearFloor:  yearFloor,
				SkipTitles: ratingsOnly,
			})
			// Report what landed even when the import died partway: a partial
			// dataset is still usable, and the numbers say how partial it is.
			fmt.Fprintf(cmd.OutOrStdout(),
				"imdb: рейтингов %d, фильмов %d, проставлено нашим %d\n",
				res.Ratings, res.Titles, res.Applied)
			return err
		},
	}
	cmd.Flags().IntVar(&yearFloor, "year-floor", 0, "skip films released before this year (default from config)")
	cmd.Flags().BoolVar(&ratingsOnly, "ratings-only", false, "import title.ratings and skip the much larger title.basics")
	return cmd
}

func newUpdateMoviesCmd() *cobra.Command {
	var (
		last, from, to string
		region         string
		releaseType    string
		origLang       string
		originCountry  string
		minRating      float64
		minVotes       int
		maxPages       int
		concurrency    int
		dryRun         bool
		enrichWith     string
	)

	cmd := &cobra.Command{
		Use:     "movies",
		Aliases: []string{"digital"},
		Short:   "Pull movies released in the given window",
		Example: `  kino update movies --last 7d
  kino update movies --last 7d --min-rating 6.5 --min-votes 50
  kino update movies --last 1m --original-language ru --release-type any
  kino update movies --from 2026-08-01 --to 2026-08-27 --region US --enrich omdb,kp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromDate, toDate, err := parseWindow(last, from, to)
			if err != nil {
				return err
			}
			types, err := parseReleaseTypes(releaseType)
			if err != nil {
				return err
			}
			a, err := openApp(true)
			if err != nil {
				return err
			}
			defer a.Close()

			if region == "" {
				region = a.cfg.Region
			}
			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.UpdateOpts{
				From: fromDate, To: toDate, Region: region,
				ReleaseTypes: types, OrigLang: origLang, OriginCountry: originCountry,
				MinRating: minRating, MinVotes: minVotes,
				MaxPages: maxPages, Concurrency: concurrency, DryRun: dryRun,
			}
			runID, err := a.store.StartRun(ctx, "update movies", opts)
			if err != nil {
				return err
			}

			res, movies, err := a.deps.Update(ctx, opts)
			if err != nil {
				res.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, res); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}

			if err := writeMovies(os.Stdout, movies, flagFormat); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "\n%s..%s [%s, %s]: found %d, kept %d, inserted %d, updated %d, skipped %d%s\n",
				fromDate, toDate, scopeLabel(region, types), releaseType,
				res.Found, len(movies), res.Inserted, res.Updated, res.Skipped, dryNote(dryRun))
			fmt.Fprintf(os.Stderr, "api calls: %v\n", res.APICalls)

			if enrichWith != "" && !dryRun {
				return runEnrich(ctx, a, strings.Split(enrichWith, ","), len(movies), 30, concurrency)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&last, "last", "", "window relative to today: 7d, 2w, 1m, 1y")
	f.StringVar(&from, "from", "", "window start, YYYY-MM-DD")
	f.StringVar(&to, "to", "", "window end, YYYY-MM-DD (default today)")
	f.StringVar(&region, "region", "", "ISO 3166-1 region the release date belongs to (default from config)")
	f.StringVar(&releaseType, "release-type", "digital",
		"release types to match: digital|theatrical|limited|premiere|physical|tv|any, comma-separated")
	f.StringVar(&origLang, "original-language", "", "ISO 639-1 original language, e.g. ru")
	f.StringVar(&originCountry, "origin-country", "", "ISO 3166-1 production country, e.g. KZ")
	f.Float64Var(&minRating, "min-rating", 0, "minimum TMDB rating")
	f.IntVar(&minVotes, "min-votes", 0, "minimum TMDB vote count")
	f.IntVar(&maxPages, "max-pages", 0, "stop after N discover pages (0 = all)")
	f.IntVar(&concurrency, "concurrency", 8, "parallel detail requests")
	f.BoolVar(&dryRun, "dry-run", false, "fetch and print without writing to the database")
	f.StringVar(&enrichWith, "enrich", "", "after the update, enrich the new rows: omdb,kp")
	return cmd
}

// parseReleaseTypes accepts names, numbers, or "any" for no type filter at all.
func parseReleaseTypes(s string) ([]int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" || s == "any" || s == "all" {
		return nil, nil
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if id, ok := model.TypeNames[part]; ok {
			seen[id] = true
			continue
		}
		n, err := strconv.Atoi(part)
		if err != nil || n < model.ReleasePremiere || n > model.ReleaseTV {
			return nil, fmt.Errorf("unknown release type %q, want digital|theatrical|limited|premiere|physical|tv|any or 1..6", part)
		}
		seen[n] = true
	}
	if len(seen) == 0 {
		return nil, nil
	}
	out := make([]int, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Ints(out)
	return out, nil
}

// scopeLabel makes clear that region only constrains typed releases.
func scopeLabel(region string, types []int) string {
	if len(types) == 0 {
		return "any region"
	}
	return region
}

func dryNote(dry bool) string {
	if dry {
		return " (dry run, nothing written)"
	}
	return ""
}
