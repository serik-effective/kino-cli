package cli

import (
	"fmt"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newWatchedCmd records what the viewer has already seen, so recommendations
// stop suggesting it. A rating is optional today and is stored for the
// personalisation the spec leaves for later.
func newWatchedCmd() *cobra.Command {
	var (
		list   bool
		forget bool
	)

	cmd := &cobra.Command{
		Use:   "watched [фильм] [оценка]",
		Short: "Отметить фильм просмотренным, чтобы он не попадал в рекомендации",
		Long: `Отметить фильм просмотренным.

Фильм ищется по названию, так что id знать не нужно. Если под запрос подходит
несколько фильмов, команда покажет их и ничего не изменит.`,
		Example: `  kino watched коммерсант
  kino watched "сергей орлов" 8
  kino watched --list
  kino watched --forget коммерсант`,
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a, err := openApp(false)
			if err != nil {
				return err
			}
			defer a.Close()
			ctx := cmd.Context()
			out := cmd.OutOrStdout()

			if list {
				films, err := a.store.ListWatched(ctx)
				if err != nil {
					return err
				}
				if len(films) == 0 {
					fmt.Fprintln(out, "просмотренных фильмов пока нет")
					return nil
				}
				w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
				for _, f := range films {
					rating := "—"
					if f.Rating > 0 {
						rating = strconv.Itoa(f.Rating)
					}
					fmt.Fprintf(w, "%s\t%d\tоценка %s\n", f.Title, f.Year, rating)
				}
				return w.Flush()
			}

			if len(args) == 0 {
				return fmt.Errorf("укажите фильм: kino watched коммерсант")
			}

			// A trailing number is the viewer's own rating, not part of the title.
			query := args[0]
			rating := 0
			if len(args) > 1 {
				n, err := strconv.Atoi(args[len(args)-1])
				if err != nil || n < 1 || n > 10 {
					return fmt.Errorf("оценка должна быть числом от 1 до 10, получено %q", args[len(args)-1])
				}
				rating = n
				query = joinArgs(args[:len(args)-1])
			} else {
				query = joinArgs(args)
			}

			found, err := a.store.FindMovies(ctx, query, 10)
			if err != nil {
				return err
			}
			switch len(found) {
			case 0:
				return fmt.Errorf("не нашёл фильм по запросу %q", query)
			case 1:
				// unambiguous, act on it
			default:
				fmt.Fprintf(out, "под запрос %q подходит несколько фильмов, уточните:\n", query)
				for _, f := range found {
					fmt.Fprintf(out, "  %s (%d)  tmdb:%d\n", f.Title, f.Year, f.TMDBID)
				}
				return nil
			}

			f := found[0]
			if forget {
				ok, err := a.store.ForgetWatched(ctx, f.TMDBID)
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintf(out, "%s и так не был отмечен\n", f.Title)
					return nil
				}
				fmt.Fprintf(out, "забыл: %s (%d)\n", f.Title, f.Year)
				return nil
			}
			if err := a.store.MarkWatched(ctx, f.TMDBID, rating); err != nil {
				return err
			}
			if rating > 0 {
				fmt.Fprintf(out, "отмечено: %s (%d), ваша оценка %d\n", f.Title, f.Year, rating)
			} else {
				fmt.Fprintf(out, "отмечено: %s (%d)\n", f.Title, f.Year)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&list, "list", false, "показать отмеченные фильмы")
	cmd.Flags().BoolVar(&forget, "forget", false, "снять отметку")
	return cmd
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}
