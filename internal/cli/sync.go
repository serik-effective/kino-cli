package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/serik-effective/kino-cli/internal/pipeline"
	"github.com/serik-effective/kino-cli/internal/source/kinozal"
)

// newSyncCmd is the one command worth putting in cron.
//
// Every step is optional in the sense that a failure is reported and the rest
// still runs: a daily job must not lose a week of ratings because one source
// was down for an hour.
func newSyncCmd() *cobra.Command {
	var (
		period     string
		details    bool
		window     string
		ruWindow   string
		kpYears    int
		limit      int
		withTrends bool
		imdbAfter  int
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Ежедневная рутина: свежие релизы в мире и на русском + бесплатные рейтинги",
		Long: `Ежедневная рутина одной командой.

Что делает:
  1. обновляет датасеты IMDb, если локальной копии больше --imdb-after дней;
  2. тянет мировые цифровые релизы за --window;
  3. тянет русскоязычные релизы за --ru-window, любого типа — у российского кино
     цифровых релизов в TMDB почти не бывает;
  4. забирает срез каталога Кинопоиска за последние --kp-years лет: kp_id,
     рейтинги и сервисы, где фильм доступен;
  5. бесплатно дотягивает рейтинги Кинопоиска.

Шаг 4 стоит считанные запросы: одна страница возвращает до 250 фильмов при
суточной квоте в 200 запросов.`,
		Example: "  kino sync\n  kino sync --window 2w --ru-window 2m\n  kino sync --with-trends",
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := kinozal.ParsePeriod(period)
			if err != nil {
				return err
			}
			from, to, err := parseWindow(window, "", "")
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

			ruFrom, ruTo, err := parseWindow(ruWindow, "", "")
			if err != nil {
				return err
			}

			// step runs one part of the routine and keeps going on failure, so
			// one unreachable source cannot cost the whole run.
			failed := 0
			step := func(name string, fn func() error) {
				if err := fn(); err != nil {
					failed++
					fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
				}
			}

			if stale, age := imdbStale(ctx, a, imdbAfter); stale {
				step("imdb", func() error {
					res, err := a.deps.ImportIMDb(ctx, pipeline.ImportIMDbOpts{
						YearFloor: a.cfg.Tuning.Defaults.IMDbYearFloor,
					})
					fmt.Fprintf(os.Stderr, "imdb: рейтингов %d, проставлено нашим %d%s\n",
						res.Ratings, res.Applied, age)
					return err
				})
			}

			if withTrends {
				step("trends", func() error {
					tres, err := a.deps.FetchTrends(ctx, pipeline.TrendsOpts{
						Period: p, Details: details, Concurrency: 4})
					if err != nil {
						return err
					}
					matched, unmatched, err := a.deps.MatchTorrents(ctx, tres.Found+50, 4)
					fmt.Fprintf(os.Stderr, "trends: %d releases, %d matched, %d unresolved\n",
						tres.Found, matched, unmatched)
					return err
				})
			}

			step("мир", func() error {
				ures, movies, err := a.deps.Update(ctx, pipeline.UpdateOpts{
					From: from, To: to, Region: a.cfg.Region,
					ReleaseTypes: []int{4}, Concurrency: 8,
				})
				fmt.Fprintf(os.Stderr, "мир, цифра %s..%s: найдено %d, новых %d\n",
					from, to, len(movies), ures.Inserted)
				return err
			})

			// Russian cinema is pulled by language and with no release-type
			// filter: TMDB records a digital release for almost none of it, so
			// asking for type 4 would return an empty list every time.
			step("русское", func() error {
				ures, movies, err := a.deps.Update(ctx, pipeline.UpdateOpts{
					From: ruFrom, To: ruTo, OrigLang: "ru", Concurrency: 8,
				})
				fmt.Fprintf(os.Stderr, "русскоязычное %s..%s: найдено %d, новых %d\n",
					ruFrom, ruTo, len(movies), ures.Inserted)
				return err
			})

			// The catalogue API is the only source that reports where a film can
			// actually be watched, and the only one that covers Russian cinema
			// properly. A year costs one or two requests out of 200 a day.
			step("каталог КП", func() error {
				res, err := a.deps.UpdateKP(ctx, pipeline.UpdateKPOpts{
					FromYear:   time.Now().Year() - kpYears + 1,
					MinVotes:   a.cfg.Tuning.Thresholds.MinKPVotes,
					AddMissing: true,
					// A cron job must never be able to drain the daily quota.
					MaxPages: defaultKPPages,
				})
				fmt.Fprintf(os.Stderr,
					"каталог КП: фильмов %d, новых %d, не опознано %d\n",
					res.Seen, res.Added, res.Unmatched)
				return err
			})

			step("рейтинги", func() error {
				rres, err := a.deps.RefreshRatings(ctx, pipeline.RefreshOpts{
					Limit: limit, StaleHours: 24, Concurrency: 2,
				})
				fmt.Fprintf(os.Stderr, "рейтинги КП: обновлено %d (бесплатно)\n", rres.Updated)
				return err
			})

			fmt.Fprintf(os.Stderr, "api calls: %v\n", a.deps.Calls.Snapshot())
			if failed > 0 {
				return fmt.Errorf("шагов с ошибкой: %d (остальные отработали)", failed)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&period, "period", "week", "срез трекера: week|month|halfyear|all")
	f.BoolVar(&details, "details", true, "тянуть счётчики загрузок (нужны для дельты трендов)")
	f.StringVar(&window, "window", "7d", "за какой период тянуть мировые цифровые релизы")
	f.StringVar(&ruWindow, "ru-window", "1m", "за какой период тянуть русскоязычные релизы")
	f.IntVar(&kpYears, "kp-years", 2, "сколько последних лет забирать из каталога Кинопоиска")
	f.IntVar(&limit, "limit", 200, "сколько фильмов обновлять рейтингами за раз")
	f.BoolVar(&withTrends, "with-trends", false, "заодно снять срез торрент-трекера")
	f.IntVar(&imdbAfter, "imdb-after", 7, "обновлять датасеты IMDb, если копия старше стольких дней")
	return cmd
}

// imdbStale reports whether the local IMDb copy is old enough to re-download,
// and a human phrase describing its age for the log.
func imdbStale(ctx context.Context, a *app, afterDays int) (bool, string) {
	sets, err := a.store.IMDbDatasets(ctx)
	if err != nil || len(sets) == 0 {
		return true, " (первая загрузка)"
	}
	oldest := time.Now()
	for _, d := range sets {
		if d.UpdatedAt.Before(oldest) {
			oldest = d.UpdatedAt
		}
	}
	age := time.Since(oldest)
	if age < time.Duration(afterDays)*24*time.Hour {
		return false, ""
	}
	return true, fmt.Sprintf(" (копии было %s)", humanAge(age))
}
