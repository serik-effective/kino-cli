package cli

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/sbeysenov/kino-cli/internal/model"
	"github.com/sbeysenov/kino-cli/internal/pipeline"
	"github.com/sbeysenov/kino-cli/internal/source/kinozal"
	"github.com/sbeysenov/kino-cli/internal/store"
)

func newTrendsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "trends",
		Aliases: []string{"t"},
		Short:   "Track what is popular on public trackers and map it to movies",
		Long: `Reads public popularity data (titles, ranks, seed and download counters) from
kinozal.me, maps each release to a TMDB movie and stores a snapshot. Comparing
two snapshots turns cumulative download counts into a per-period trend.

Only metadata is collected — no torrents and no content are downloaded.`,
	}
	cmd.AddCommand(newTrendsFetchCmd(), newTrendsListCmd())
	return cmd
}

func newTrendsFetchCmd() *cobra.Command {
	var (
		period      string
		pages       int
		details     bool
		limit       int
		noMatch     bool
		concurrency int
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "Capture a popularity snapshot and match releases to movies",
		Example: `  kino trends fetch --period week
  kino trends fetch --period month --details
  kino trends fetch --pages 2 --details --limit 60`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := kinozal.ParsePeriod(period)
			if err != nil {
				return err
			}
			a, err := openApp(true)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.TrendsOpts{
				Period: p, Pages: pages, Details: details, Limit: limit,
				Concurrency: concurrency, DryRun: dryRun,
			}
			runID, err := a.store.StartRun(ctx, "trends fetch", opts)
			if err != nil {
				return err
			}
			res, err := a.deps.FetchTrends(ctx, opts)
			if err != nil {
				res.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, res); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "snapshot: %d releases seen, %d stored, %d skipped%s\n",
				res.Found, res.Inserted, res.Skipped, dryNote(dryRun))

			if noMatch || dryRun {
				return nil
			}
			matched, unmatched, err := a.deps.MatchTorrents(ctx, res.Found+50, min(concurrency, 6))
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "matched %d releases to TMDB, %d unresolved; api calls: %v\n",
				matched, unmatched, a.deps.Calls.Snapshot())
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&period, "period", "week", "tracker chart: week|month|halfyear|all")
	f.IntVar(&pages, "pages", 0, "read N pages of the movie category instead of the chart")
	f.BoolVar(&details, "details", false, "also fetch per-release download counters (one request each)")
	f.IntVar(&limit, "limit", 0, "cap the number of releases per run (0 = no cap)")
	f.BoolVar(&noMatch, "no-match", false, "skip TMDB matching")
	f.IntVar(&concurrency, "concurrency", 4, "parallel requests")
	f.BoolVar(&dryRun, "dry-run", false, "fetch without writing")
	return cmd
}

type trendDefaults struct {
	use, short, example string
	aliases             []string
	metric              string
	since               string
	limit               int
}

func newTrendsListCmd() *cobra.Command {
	return newTrendsListCmdWith(trendDefaults{
		use:   "list",
		short: "Show trending movies from the captured snapshots",
		example: `  kino trends list --limit 20
  kino trends list --since 7d --metric delta
  kino trends list --unmatched`,
		aliases: []string{"ls"},
		metric:  "delta",
		since:   "7d",
		limit:   25,
	})
}

func newTrendsListCmdWith(def trendDefaults) *cobra.Command {
	var (
		since     string
		metric    string
		limit     int
		unmatched bool
	)

	cmd := &cobra.Command{
		Use:     def.use,
		Aliases: def.aliases,
		Short:   def.short,
		Example: def.example,
		RunE: func(cmd *cobra.Command, args []string) error {
			days, err := parseDays(orDefault(since, "7d"))
			if err != nil {
				return err
			}
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			rows, err := a.store.Trending(cmd.Context(), store.TrendOpts{
				Source:    pipeline.SourceKinozal,
				Since:     time.Now().UTC().AddDate(0, 0, -days).Format(time.RFC3339),
				Metric:    metric,
				Limit:     limit,
				Unmatched: unmatched,
			})
			if err != nil {
				return err
			}
			return writeTrends(os.Stdout, rows, flagFormat)
		},
	}

	f := cmd.Flags()
	f.StringVar(&since, "since", def.since, "baseline for the download delta: 7d, 2w, 1m")
	f.StringVar(&metric, "metric", def.metric, "sort by: delta|downloads|seeds|rank")
	f.IntVar(&limit, "limit", def.limit, "max rows")
	f.BoolVar(&unmatched, "unmatched", false, "list releases that could not be mapped to a movie")
	return cmd
}

func writeTrends(w io.Writer, rows []*model.TrendRow, format string) error {
	if format == "json" {
		return writeJSON(w, rows)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "#\tDELTA\tDOWNLOADS\tSEEDS\tREL\tTMDB\tIMDB\tKP\tTITLE")
	for i, r := range rows {
		title := r.RawTitle
		var tmdbR, imdbR, kpR string
		if r.Movie != nil {
			title = displayTitle(r.Movie)
			tmdbR, imdbR, kpR = floatStr(r.Movie.TMDBRating), floatStr(r.Movie.IMDbRating), floatStr(r.Movie.KPRating)
		}
		fmt.Fprintf(tw, "%d\t%s\t%d\t%d\t%d\t%s\t%s\t%s\t%s\n",
			i+1, plus(r.DownloadsDelta), r.Downloads, r.Seeds, r.Releases,
			dash(tmdbR), dash(imdbR), dash(kpR), title)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "(no rows — run `kino trends fetch` first)")
	}
	return nil
}

// plus marks a delta as growth; a zero delta means there is no earlier snapshot
// to compare against yet.
func plus(v int) string {
	if v == 0 {
		return "—"
	}
	return fmt.Sprintf("+%d", v)
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
