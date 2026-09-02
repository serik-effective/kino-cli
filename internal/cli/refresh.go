package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/serik-effective/kino-cli/internal/pipeline"
)

func newRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "refresh", Short: "Update data already in the database"}
	cmd.AddCommand(newRefreshRatingsCmd(), newRefreshKPIDsCmd())
	return cmd
}

func newRefreshKPIDsCmd() *cobra.Command {
	var (
		limit  int
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:     "kp-ids",
		Aliases: []string{"kpids"},
		Short:   "Resolve Kinopoisk ids via Wikidata — no key, no quota",
		Long: `Looks up imdb-id → kinopoisk-id through the Wikidata Query Service (P345 → P2603).
Free and unmetered, and it resolves far more per run than the 200/day Kinopoisk
API allows. Whatever Wikidata does not know still falls back to "kino enrich
--sources kp". Once an id is known, "kino refresh ratings" keeps the score
current at no cost.`,
		Example: "  kino refresh kp-ids\n  kino refresh kp-ids --limit 500",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.ResolveOpts{Limit: limit, DryRun: dryRun}
			runID, err := a.store.StartRun(ctx, "refresh kp-ids", opts)
			if err != nil {
				return err
			}
			res, err := a.deps.ResolveKPIDs(ctx, opts)
			if err != nil {
				res.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, res); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "resolved %d of %d missing ids, %d unknown to Wikidata%s; api calls: %v\n",
				res.Updated, res.Found, res.Skipped, dryNote(dryRun), res.APICalls)
			return nil
		},
	}

	f := cmd.Flags()
	f.IntVar(&limit, "limit", 0, "max movies to resolve (0 = all missing)")
	f.BoolVar(&dryRun, "dry-run", false, "look up without writing")
	return cmd
}

func newRefreshRatingsCmd() *cobra.Command {
	var (
		limit         int
		staleHours    int
		concurrency   int
		overwriteIMDb bool
		dryRun        bool
	)

	cmd := &cobra.Command{
		Use:   "ratings",
		Short: "Refresh Kinopoisk (and missing IMDb) scores via rating.kinopoisk.ru — no key, no quota",
		Long: `Fetches https://rating.kinopoisk.ru/<kp_id>.xml for every movie whose Kinopoisk
id is already known. The endpoint needs no API key and has no daily limit, so
this is the cheap way to keep ratings current; only resolving an unknown kp_id
costs poiskkino quota (see "kino enrich").`,
		Example: "  kino refresh ratings --limit 500\n  kino refresh ratings --stale-hours 6 --overwrite-imdb",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.RefreshOpts{
				Limit: limit, StaleHours: staleHours, Concurrency: concurrency,
				OverwriteIMDb: overwriteIMDb, DryRun: dryRun,
			}
			runID, err := a.store.StartRun(ctx, "refresh ratings", opts)
			if err != nil {
				return err
			}
			res, err := a.deps.RefreshRatings(ctx, opts)
			if err != nil {
				res.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, res); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "refreshed %d of %d stale, skipped %d%s; api calls: %v\n",
				res.Updated, res.Found, res.Skipped, dryNote(dryRun), res.APICalls)
			return nil
		},
	}

	f := cmd.Flags()
	f.IntVar(&limit, "limit", 200, "max movies per run")
	f.IntVar(&staleHours, "stale-hours", 24, "refresh rows whose ratings are older than this")
	f.IntVar(&concurrency, "concurrency", 2, "parallel requests (kept low: undocumented endpoint)")
	f.BoolVar(&overwriteIMDb, "overwrite-imdb", false, "let the KP feed overwrite an IMDb rating from OMDb")
	f.BoolVar(&dryRun, "dry-run", false, "fetch without writing")
	return cmd
}
