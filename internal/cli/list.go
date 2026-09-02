package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sbeysenov/kino-cli/internal/store"
)

// listDefaults seeds a list command. Presets differ from `list` only by these
// values; every flag stays overridable.
type listDefaults struct {
	use, short, example string
	aliases             []string
	since               string
	releaseType         string
	origLang            string
	country             string
	region              string
	ratingSource        string
	sort                string
	minRating           float64
	minVotes            int
	limit               int
}

func newListCmd() *cobra.Command {
	return newListCmdWith(listDefaults{
		use:   "list",
		short: "Query the local database, no network involved",
		example: `  kino list --since 7d
  kino list --since 1m --original-language ru --release-type any
  kino list --since 30d --min-rating 7 --sort rating`,
		aliases:      []string{"ls"},
		since:        "",
		releaseType:  "digital",
		ratingSource: "tmdb",
		sort:         "date",
	})
}

func newListCmdWith(def listDefaults) *cobra.Command {
	var (
		since, from, to string
		region          string
		releaseType     string
		origLang        string
		country         string
		ratingSource    string
		minRating       float64
		minVotes        int
		sortBy          string
		asc             bool
		limit           int
	)

	cmd := &cobra.Command{
		Use:     def.use,
		Aliases: def.aliases,
		Short:   def.short,
		Example: def.example,
		RunE: func(cmd *cobra.Command, args []string) error {
			fromDate, toDate, err := parseWindow(since, from, to)
			if err != nil {
				return err
			}
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			if region == "" {
				region = a.cfg.Region
			}
			types, err := parseReleaseTypes(releaseType)
			if err != nil {
				return err
			}
			movies, err := a.store.List(cmd.Context(), store.ListOpts{
				Region: region, ReleaseTypes: types, Since: fromDate, Until: toDate,
				OrigLang: origLang, Country: country, RatingSource: ratingSource, MinRating: minRating, MinVotes: minVotes,
				Sort: sortBy, Desc: !asc, Limit: limit,
			})
			if err != nil {
				return err
			}
			if err := writeMovies(os.Stdout, movies, flagFormat); err != nil {
				return err
			}
			logf("%d rows, %s..%s [%s, %s]", len(movies), fromDate, toDate, scopeLabel(region, types), releaseType)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&since, "since", def.since, "released within the last: 7d, 2w, 1m")
	f.StringVar(&from, "from", "", "window start, YYYY-MM-DD")
	f.StringVar(&to, "to", "", "window end, YYYY-MM-DD (default today)")
	f.StringVar(&region, "region", def.region, `region the release date belongs to, or "any" (default from config)`)
	f.StringVar(&releaseType, "release-type", def.releaseType,
		"release types to match: digital|theatrical|limited|premiere|physical|tv|any, comma-separated")
	f.StringVar(&origLang, "original-language", def.origLang, "ISO 639-1 original language, e.g. ru")
	f.StringVar(&country, "country", def.country, "ISO 3166-1 production country, e.g. KZ")
	f.StringVar(&ratingSource, "rating-source", def.ratingSource, "which rating --min-rating applies to: tmdb|imdb|kp")
	f.Float64Var(&minRating, "min-rating", def.minRating, "minimum rating")
	f.IntVar(&minVotes, "min-votes", def.minVotes, "minimum vote count")
	f.StringVar(&sortBy, "sort", def.sort, "sort by: date|rating|tmdb|imdb|kp|popularity|votes|title")
	f.BoolVar(&asc, "asc", false, "sort ascending")
	f.IntVar(&limit, "limit", def.limit, "max rows (0 = no limit)")
	return cmd
}

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "show <tt1234567|tmdb:123|123>",
		Aliases: []string{"i"},
		Short:   "Show one movie with all its release dates",
		Args:    cobra.ExactArgs(1),
		Example: "  kino show tt1285016\n  kino show tmdb:9799",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			tmdbID, imdbID := parseID(args[0])
			m, err := a.store.Get(cmd.Context(), tmdbID, imdbID)
			if err != nil {
				return err
			}
			if m == nil {
				return fmt.Errorf("%s not found in the local database", args[0])
			}
			rels, err := a.store.Releases(cmd.Context(), m.TMDBID)
			if err != nil {
				return err
			}
			return writeMovieDetail(os.Stdout, m, rels, flagFormat)
		},
	}
	return cmd
}

func parseID(s string) (int, string) {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "tt") {
		return 0, s
	}
	s = strings.TrimPrefix(s, "tmdb:")
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
		return n, ""
	}
	return 0, s
}
