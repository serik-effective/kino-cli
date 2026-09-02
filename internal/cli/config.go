package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/serik-effective/kino-cli/internal/config"
	"github.com/serik-effective/kino-cli/internal/source/omdb"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect the local setup"}
	cmd.AddCommand(newConfigInitCmd(), newConfigShowCmd())
	cmd.AddCommand(&cobra.Command{
		Use:   "check",
		Short: "Show which keys are set, database stats and today's quota usage",
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			from := a.cfg.LoadedFrom
			if from == "" {
				from = config.DefaultPath() + " (missing — using built-in defaults)"
			}
			fmt.Fprintf(w, "config file\t%s\n", from)
			fmt.Fprintf(w, "database\t%s\n", a.cfg.DBPath)
			fmt.Fprintf(w, "region / lang\t%s / %s\n", a.cfg.Region, a.cfg.Lang)
			fmt.Fprintf(w, "TMDB_TOKEN\t%s\n", mask(a.cfg.TMDBToken))
			fmt.Fprintf(w, "OMDB_API_KEY\t%s\n", mask(a.cfg.OMDBKey))
			fmt.Fprintf(w, "KINOPOISK_API_KEY\t%s\n", mask(a.cfg.KinopoiskKey))

			for _, t := range []string{"movies", "releases"} {
				n, err := a.store.Count(cmd.Context(), t)
				if err != nil {
					return err
				}
				fmt.Fprintf(w, "rows in %s\t%d\n", t, n)
			}

			if rows, oldest, err := a.store.CacheStats(cmd.Context()); err == nil && rows > 0 {
				fmt.Fprintf(w, "кэш API\t%d ответов, старейшему %s\n",
					rows, humanAge(time.Since(oldest)))
			}

			used, err := a.store.QuotaToday("omdb")
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "omdb calls today\t%d / %d\n", used, omdb.DailyLimit)

			if a.deps.KP != nil {
				info, err := a.deps.KP.TokenInfo(cmd.Context())
				if err != nil {
					fmt.Fprintf(w, "kinopoisk quota\terror: %v\n", err)
				} else {
					fmt.Fprintf(w, "kinopoisk quota\t%d / %d used, %d left, reset %s\n",
						info.Used, info.Limit, info.Remaining, info.ResetAt)
				}
			}
			return w.Flush()
		},
	})
	return cmd
}

func mask(s string) string {
	if s == "" {
		return "— not set"
	}
	if len(s) <= 8 {
		return "set (" + s[:2] + "…)"
	}
	return fmt.Sprintf("set (%s…%s, %d chars)", s[:4], s[len(s)-2:], len(s))
}

// newConfigInitCmd writes a starter config.toml with every coefficient spelled
// out, so the ranking model is visible and editable from one place.
func newConfigInitCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Write a starter config.toml with the default tuning",
		RunE: func(cmd *cobra.Command, args []string) error {
			if path == "" {
				path = configTarget()
			}
			if err := config.WriteTemplate(path, config.DefaultTuning()); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "написан %s — впишите ключи в [secrets]\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "",
		"куда записать (по умолчанию KINO_CONFIG или ~/.config/kino/config.toml)")
	return cmd
}

// configTarget is the file the config commands write to. It has to agree with
// what Load reads: honouring KINO_CONFIG when reading but writing somewhere
// else means "kino config init" quietly creates a file the next run ignores —
// or, worse, overwrites one nobody meant to touch.
func configTarget() string {
	if p := os.Getenv("KINO_CONFIG"); p != "" {
		return p
	}
	return config.DefaultPath()
}

// newConfigShowCmd prints the tuning actually in effect. "kino --why" explains
// one film's score; this explains the model behind every score.
func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the ranking coefficients currently in effect",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			t := cfg.Tuning
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			from := cfg.LoadedFrom
			if from == "" {
				from = "built-in defaults (no config.toml)"
			}
			fmt.Fprintf(w, "source\t%s\n", from)
			fmt.Fprintf(w, "\nperiod / limit\t%d дней / %d фильмов\n", t.Defaults.Period, t.Defaults.Limit)
			fmt.Fprintf(w, "ru period\t%d дней\n", t.Defaults.RuPeriod)
			fmt.Fprintf(w, "imdb year floor\t%d\n", t.Defaults.IMDbYearFloor)
			fmt.Fprintf(w, "\nдопуск (любой из)\timdb>=%d, tmdb>=%d, kp>=%d\n",
				t.Thresholds.MinIMDbVotes, t.Thresholds.MinTMDBVotes, t.Thresholds.MinKPVotes)
			fmt.Fprintf(w, "мин. хронометраж\t%d мин\n", t.Thresholds.MinRuntime)
			gap := "выключен"
			if t.Thresholds.MaxReleaseGapYears > 0 {
				gap = fmt.Sprintf("не старше %d лет на момент цифры", t.Thresholds.MaxReleaseGapYears)
			}
			fmt.Fprintf(w, "переиздания\t%s\n", gap)
			fmt.Fprintf(w, "\nвеса\timdb %.2f · tmdb %.2f · confidence %.2f · mainstream %.2f · freshness %.2f\n",
				t.Weights.IMDb, t.Weights.TMDB, t.Weights.Confidence, t.Weights.Mainstream, t.Weights.Freshness)
			fmt.Fprintf(w, "сумма весов\t%.2f\n", t.Weights.Sum())
			fmt.Fprintf(w, "\nбайес imdb\tm=%.0f mean=%.2f\n", t.Bayes.IMDbM, t.Bayes.IMDbMean)
			fmt.Fprintf(w, "байес tmdb\tm=%.0f mean=%.2f\n", t.Bayes.TMDBM, t.Bayes.TMDBMean)
			fmt.Fprintf(w, "байес kp (только kino ru)\tm=%.0f mean=%.2f\n", t.Bayes.KPM, t.Bayes.KPMean)
			fmt.Fprintf(w, "\nартхаус\tlow %.2f · high %.2f · max penalty %.2f\n",
				t.Arthouse.Low, t.Arthouse.High, t.Arthouse.MaxPenalty)
			return w.Flush()
		},
	}
}
