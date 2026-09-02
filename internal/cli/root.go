// Package cli wires cobra commands to the pipeline.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/sbeysenov/kino-cli/internal/config"
	"github.com/sbeysenov/kino-cli/internal/httpx"
	"github.com/sbeysenov/kino-cli/internal/pipeline"
	"github.com/sbeysenov/kino-cli/internal/source/kinozal"
	"github.com/sbeysenov/kino-cli/internal/source/kp"
	"github.com/sbeysenov/kino-cli/internal/source/omdb"
	"github.com/sbeysenov/kino-cli/internal/source/tmdb"
	"github.com/sbeysenov/kino-cli/internal/source/wikidata"
	"github.com/sbeysenov/kino-cli/internal/store"
)

var (
	flagDB      string
	flagFormat  string
	flagVerbose bool
)

func Execute() error { return newRootCmd().Execute() }

// newRootCmd builds the command tree. It is a constructor rather than inline
// setup so "kino setup" can run a fresh tree of its own — filling the database
// after the wizard means running the real sync, not a copy of it.
func newRootCmd() *cobra.Command {
	// Cache keys are hashes: some source URLs carry an API key, and the cache
	// table must not become a place secrets are kept.
	httpx.SetCacheKeyFunc(store.CacheKey)

	root := &cobra.Command{
		Use:   "kino",
		Short: "Что из свежего уже можно посмотреть сегодня вечером",
		Long: `kino отвечает на один вопрос: какие фильмы, вышедшие в цифре за последние
дни, стоит посмотреть.

Ранжирование опирается на зрительские оценки, а не на критиков, и не использует
рейтинг Кинопоиска в общей выдаче. Российское кино участвует на равных; для него
есть отдельная дорожка "kino ru", которая ранжирует по Кинопоиску — там живёт его
массовая аудитория.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagDB, "db", "", "path to the SQLite database (default from config)")
	root.PersistentFlags().StringVar(&flagFormat, "format", "table", "output format: table|json|csv")
	root.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "log progress to stderr")

	root.AddCommand(newUpdateCmd(), newListCmd(), newShowCmd(), newEnrichCmd(),
		newRefreshCmd(), newTrendsCmd(), newConfigCmd(), newSyncCmd(), newImportCmd(), newWatchedCmd())
	root.AddCommand(presetCmds()...)
	root.AddCommand(newSetupCmd())
	bindTop(root)
	return root
}

// app bundles everything a command needs.
type app struct {
	cfg   *config.Config
	store *store.Store
	deps  *pipeline.Deps
}

func (a *app) Close() {
	if a.store != nil {
		a.store.Close()
	}
}

func openApp(needTMDB bool) (*app, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	if needTMDB {
		if err := cfg.RequireTMDB(); err != nil {
			return nil, err
		}
	}
	if flagDB != "" {
		cfg.DBPath = flagDB
	}
	st, err := store.Open(cfg.DBPath)
	if err != nil {
		return nil, err
	}

	counter := pipeline.NewCounter()
	counter.Persist = func(source string, n int) { _ = st.AddQuota(source, n) }
	onCall := counter.Add

	deps := &pipeline.Deps{Store: st, Lang: cfg.Lang, Calls: counter, Log: logf}
	if cfg.TMDBToken != "" {
		deps.TMDB = tmdb.New(cfg.TMDBToken, onCall)
		// Discovery answers change slowly; a day-old page is fine and saves the
		// quota. When TMDB is down the cache keeps the tool usable, and the
		// warning makes clear the answer is not fresh.
		deps.TMDB.UseCache(st, cacheTTL, func(source string, age time.Duration) {
			fmt.Fprintf(os.Stderr,
				"внимание: %s недоступен, отвечаю из кэша (данным %s)\n",
				source, humanAge(age))
		})
	}
	if cfg.OMDBKey != "" {
		deps.OMDB = omdb.New(cfg.OMDBKey, onCall)
	}
	if cfg.KinopoiskKey != "" {
		deps.KP = kp.New(cfg.KinopoiskKey, onCall)
	}
	// rating.kinopoisk.ru needs no key, so this client is always available.
	deps.KPRating = kp.NewRatingClient(onCall)
	deps.Kinozal = kinozal.New(onCall)
	deps.Wikidata = wikidata.New(onCall)
	return &app{cfg: cfg, store: st, deps: deps}, nil
}

func logf(format string, args ...any) {
	if flagVerbose {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// signalContext cancels on Ctrl-C so a long update stops cleanly.
func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// parseWindow turns --last / --from / --to into an inclusive date range.
func parseWindow(last, from, to string) (string, string, error) {
	if last != "" && (from != "" || to != "") {
		return "", "", fmt.Errorf("--last cannot be combined with --from/--to")
	}
	if last != "" {
		d, err := parseDays(last)
		if err != nil {
			return "", "", err
		}
		end := time.Now().UTC()
		return end.AddDate(0, 0, -d+1).Format("2006-01-02"), end.Format("2006-01-02"), nil
	}
	if from == "" {
		return "", "", fmt.Errorf("specify --last or --from")
	}
	if to == "" {
		to = time.Now().UTC().Format("2006-01-02")
	}
	for _, s := range []string{from, to} {
		if _, err := time.Parse("2006-01-02", s); err != nil {
			return "", "", fmt.Errorf("bad date %q, want YYYY-MM-DD", s)
		}
	}
	if from > to {
		return "", "", fmt.Errorf("--from %s is after --to %s", from, to)
	}
	return from, to, nil
}

// parseDays accepts 7d, 2w, 3m or a bare number of days.
func parseDays(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := 1
	if len(s) > 0 {
		switch s[len(s)-1] {
		case 'd':
			s = s[:len(s)-1]
		case 'w':
			mult, s = 7, s[:len(s)-1]
		case 'm':
			mult, s = 30, s[:len(s)-1]
		case 'y':
			mult, s = 365, s[:len(s)-1]
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("bad period %q, want e.g. 7d, 2w, 3m", s)
	}
	return n * mult, nil
}

// cacheTTL is how long a TMDB response is reused without asking again.
const cacheTTL = 24 * time.Hour

// humanAge renders a cache age the way a person would say it.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d мин", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d ч", int(d.Hours()))
	default:
		return fmt.Sprintf("%d дн", int(d.Hours()/24))
	}
}
