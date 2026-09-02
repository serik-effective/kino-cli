package cli

import "github.com/spf13/cobra"

// Presets are the ten everyday questions, pre-wired. Each is a normal command:
// every flag of `list` / `trends list` still applies and overrides the default.
func presetCmds() []*cobra.Command {
	return []*cobra.Command{
		newListCmdWith(listDefaults{
			use:          "new",
			aliases:      []string{"n"},
			short:        "Worthwhile digital releases of the last week",
			example:      "  kino new\n  kino new --since 2w --min-rating 6",
			since:        "7d",
			releaseType:  "digital",
			ratingSource: "tmdb",
			minRating:    7,
			minVotes:     50,
			sort:         "rating",
			limit:        25,
		}),
		newListCmdWith(listDefaults{
			use:          "ru-list",
			short:        "Every Russian-language release of the last month (a listing, not a ranking)",
			example:      "  kino ru-list\n  kino ru-list --since 3m --min-votes 5",
			since:        "1m",
			releaseType:  "any",
			origLang:     "ru",
			ratingSource: "tmdb",
			sort:         "date",
			limit:        50,
		}),
		newListCmdWith(listDefaults{
			use:          "kz",
			short:        "Releases produced in Kazakhstan, any language",
			example:      "  kino kz\n  kino kz --since 6m",
			since:        "1m",
			releaseType:  "any",
			country:      "KZ",
			ratingSource: "tmdb",
			sort:         "date",
			limit:        50,
		}),
		newListCmdWith(listDefaults{
			use:          "kp",
			short:        "Well rated on Kinopoisk — the yardstick that actually covers Russian cinema",
			example:      "  kino kp\n  kino kp --min-rating 7 --since 6m",
			since:        "3m",
			releaseType:  "any",
			ratingSource: "kp",
			minRating:    6.5,
			sort:         "kp",
			limit:        25,
		}),
		newListCmdWith(listDefaults{
			use:          "digital",
			aliases:      []string{"d"},
			short:        "Digital releases of the last month in one region",
			example:      "  kino digital\n  kino digital --region RU --since 2w",
			since:        "1m",
			releaseType:  "digital",
			ratingSource: "tmdb",
			sort:         "date",
			limit:        50,
		}),
		newTrendsListCmdWith(trendDefaults{
			use:     "hot",
			short:   "Most downloaded right now",
			example: "  kino hot\n  kino hot --limit 50",
			metric:  "downloads",
			since:   "7d",
			limit:   20,
		}),
		newTrendsListCmdWith(trendDefaults{
			use:     "rising",
			aliases: []string{"up"},
			short:   "Gaining downloads fastest (needs two snapshots)",
			example: "  kino rising\n  kino rising --since 2w",
			metric:  "delta",
			since:   "7d",
			limit:   20,
		}),
		newTrendsListCmdWith(trendDefaults{
			use:     "live",
			short:   "Most seeded — what people are watching today",
			example: "  kino live",
			metric:  "seeds",
			since:   "7d",
			limit:   20,
		}),
	}
}
