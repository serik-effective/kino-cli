package cli

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

// genreAliases map a CLI shorthand to the genre name as actually stored, which
// follows the configured language — Russian by default. Both spellings are kept
// so the shorthands keep working if the catalogue is rebuilt in English.
var genreAliases = map[string][]string{
	"comedy":    {"комедия", "Comedy"},
	"thriller":  {"триллер", "Thriller"},
	"horror":    {"ужасы", "Horror"},
	"sci-fi":    {"фантастика", "Science Fiction"},
	"scifi":     {"фантастика", "Science Fiction"},
	"action":    {"боевик", "Action"},
	"animation": {"мультфильм", "Animation"},
	"drama":     {"драма", "Drama"},
	"crime":     {"криминал", "Crime"},
	"fantasy":   {"фэнтези", "Fantasy"},
	"family":    {"семейный", "Family"},
	"adventure": {"приключения", "Adventure"},
}

// bindTop wires the ranking onto the root command. A bare "kino" is the whole
// product: three films worth watching tonight.
func bindTop(root *cobra.Command) {
	var (
		all  bool
		why  bool
		seen bool
	)

	run := func(cmd *cobra.Command, args []string) error {
		a, err := openApp(false)
		if err != nil {
			return err
		}
		defer a.Close()

		o, err := parseTopArgs(args, a, all, why)
		if err != nil {
			return err
		}
		o.seen = seen
		return runTop(cmd.Context(), cmd.OutOrStdout(), a, o)
	}

	root.RunE = run
	root.Args = cobra.ArbitraryArgs
	root.Flags().BoolVar(&all, "all", false, "show every film that passes, not just the top few")
	root.Flags().BoolVar(&why, "why", false, "print the score breakdown for each film")
	root.Flags().BoolVar(&seen, "seen", false, "не скрывать фильмы, отмеченные просмотренными")
	root.Example = `  kino              TOP-3 за 7 дней
  kino 30           TOP-3 за 30 дней
  kino ru           российское кино за 30 дней
  kino ru 90        российское кино за 90 дней
  kino thriller 30  триллеры за 30 дней
  kino --all --why  всё, что прошло порог, с разбором оценки

Просмотренные фильмы в выдачу не попадают — отмечайте их "kino watched <название>".`

	// The genre shorthands are real subcommands so they show up in --help.
	for alias := range genreAliases {
		alias := alias
		root.AddCommand(&cobra.Command{
			Use:   alias + " [дней]",
			Short: "TOP-3 в жанре " + genreAliases[alias][0],
			Args:  cobra.MaximumNArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				return run(cmd, append([]string{alias}, args...))
			},
		})
	}
}

// parseTopArgs reads the positional grammar: an optional track or genre word,
// then an optional number of days.
func parseTopArgs(args []string, a *app, all, why bool) (topOpts, error) {
	t := a.cfg.Tuning
	o := topOpts{days: t.Defaults.Period, limit: t.Defaults.Limit, why: why}
	if all {
		o.limit = 0
	}

	for _, arg := range args {
		if n, err := strconv.Atoi(arg); err == nil {
			if n < 1 {
				return o, fmt.Errorf("период должен быть положительным, получено %d", n)
			}
			o.days = n
			continue
		}
		switch {
		case arg == "ru":
			o.ru = true
			// Russian digital releases are sparse enough that a week is usually
			// empty, so the track carries its own default window.
			if o.days == t.Defaults.Period {
				o.days = t.Defaults.RuPeriod
			}
		default:
			g, ok := genreAliases[arg]
			if !ok {
				return o, fmt.Errorf("не понимаю аргумент %q: ожидается число дней, \"ru\" или жанр", arg)
			}
			o.genre = g[0]
			o.genreAny = g
		}
	}
	return o, nil
}
