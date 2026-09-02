package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/serik-effective/kino-cli/internal/pipeline"
	"github.com/serik-effective/kino-cli/internal/store"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Load data captured from other sources"}
	cmd.AddCommand(newImportKPCmd())
	return cmd
}

func newImportKPCmd() *cobra.Command {
	var (
		allTypes   bool
		addMissing bool
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "kp <file.json> [file.json...]",
		Short: "Import a Kinopoisk player payload: kp_id, rating, views, where to watch",
		Long: `Reads JSON captured from the Kinopoisk player and fills in kinopoisk id, rating,
view count and RU availability for movies already in the database.

Only metadata is read. The payload also contains signed, session-bound stream
URLs; those are never parsed, stored or used.`,
		Args:    cobra.MinimumNArgs(1),
		Example: "  kino import kp ~/kp.json\n  kino import kp ~/kp*.json --dry-run",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			ctx, stop := signalContext()
			defer stop()

			opts := pipeline.ImportKPOpts{
				Files: args, AllTypes: allTypes, AddMissing: addMissing, DryRun: dryRun,
			}
			runID, err := a.store.StartRun(ctx, "import kp", opts)
			if err != nil {
				return err
			}
			res, err := a.deps.ImportKP(ctx, opts)
			run := store.RunResult{
				Found: res.Items, Inserted: res.Added,
				Updated: res.ByKPID + res.ByTitle, Skipped: res.Skipped + res.Unmatched,
			}
			if err != nil {
				run.Err = err.Error()
			}
			if ferr := a.store.FinishRun(ctx, runID, run); ferr != nil {
				logf("finish run: %v", ferr)
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr,
				"записей %d, фильмов %d: по kp_id %d, по названию %d, добавлено с TMDB %d, не найдено %d, сериалов пропущено %d%s\n",
				res.Items, res.Movies, res.ByKPID, res.ByTitle, res.Added, res.Unmatched, res.Skipped, dryNote(dryRun))
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&allTypes, "all-types", false, "import series too, not just feature films")
	f.BoolVar(&addMissing, "add-missing", false, "look unknown films up on TMDB and add them to the database")
	f.BoolVar(&dryRun, "dry-run", false, "parse and match without writing")
	return cmd
}
