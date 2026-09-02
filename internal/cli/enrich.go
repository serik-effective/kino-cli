package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sbeysenov/kino-cli/internal/pipeline"
)

func newEnrichCmd() *cobra.Command {
	var (
		sources     string
		limit       int
		staleDays   int
		concurrency int
		dryRun      bool
	)

	cmd := &cobra.Command{
		Use:     "enrich",
		Short:   "Fill IMDb/Metacritic/RT and Kinopoisk ratings within the daily quotas",
		Example: "  kino enrich --limit 100\n  kino enrich --sources kp --limit 50",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.EnrichOpts{
				Limit: limit, StaleDays: staleDays, Concurrency: concurrency, DryRun: dryRun,
			}
			for _, s := range strings.Split(sources, ",") {
				switch strings.TrimSpace(s) {
				case "omdb":
					opts.UseOMDB = true
				case "kp", "kinopoisk":
					opts.UseKP = true
				case "":
				default:
					return fmt.Errorf("unknown source %q, want omdb or kp", s)
				}
			}
			if opts.UseOMDB && a.deps.OMDB == nil {
				return fmt.Errorf("OMDB_API_KEY is not set")
			}
			if opts.UseKP && a.deps.KP == nil {
				return fmt.Errorf("KINOPOISK_API_KEY is not set")
			}

			runID, err := a.store.StartRun(ctx, "enrich", opts)
			if err != nil {
				return err
			}
			res, err := a.deps.Enrich(ctx, opts)
			if err != nil {
				res.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, res); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "enriched %d of %d pending, skipped %d%s; api calls: %v\n",
				res.Updated, res.Found, res.Skipped, dryNote(dryRun), res.APICalls)
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&sources, "sources", "omdb,kp", "which sources to use: omdb,kp")
	f.IntVar(&limit, "limit", 50, "max movies to enrich in this run")
	f.IntVar(&staleDays, "stale-days", 30, "re-enrich rows older than this many days")
	f.IntVar(&concurrency, "concurrency", 3, "parallel enrichment requests")
	f.BoolVar(&dryRun, "dry-run", false, "fetch without writing")
	return cmd
}

// runEnrich backs `update digital --enrich`.
func runEnrich(ctx context.Context, a *app, sources []string, limit, staleDays, concurrency int) error {
	opts := pipeline.EnrichOpts{Limit: limit, StaleDays: staleDays, Concurrency: min(concurrency, 3)}
	for _, s := range sources {
		switch strings.TrimSpace(s) {
		case "omdb":
			opts.UseOMDB = a.deps.OMDB != nil
		case "kp", "kinopoisk":
			opts.UseKP = a.deps.KP != nil
		}
	}
	res, err := a.deps.Enrich(ctx, opts)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "enriched %d of %d pending; api calls: %v\n", res.Updated, res.Found, res.APICalls)
	return nil
}
