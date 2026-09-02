package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Secrets are the API keys written into the [secrets] block.
type Secrets struct {
	TMDBToken    string
	OMDBKey      string
	KinopoiskKey string
}

// Template renders a starter config.toml carrying the current defaults. It is
// written with the coefficients spelled out rather than omitted, so the file
// itself documents the model instead of hiding it behind code.
func Template(t Tuning) string { return TemplateWith(t, Secrets{}) }

// TemplateWith is Template with the keys filled in, for "kino setup".
func TemplateWith(t Tuning, s Secrets) string {
	return fmt.Sprintf(`# kino configuration.
# A real environment variable always wins over anything written here.

[secrets]
tmdb_token        = %s
omdb_api_key      = %s
kinopoisk_api_key = %s

[general]
region = "US"      # ISO 3166-1 region release dates are read against
lang   = "ru-RU"   # language for titles and overviews

[defaults]
period          = %d   # days covered by a bare "kino"
limit           = %d   # films in the TOP list
ru_period       = %d   # days for "kino ru": Russian digital releases are sparse
imdb_year_floor = %d   # skip older films when importing title.basics

# A film may be ranked if ANY threshold is met. Which one applies depends on
# where its audience lives: Russian cinema is rated on Kinopoisk, not IMDb.
[thresholds]
min_imdb_votes = %d
min_tmdb_votes = %d
min_kp_votes   = %d
min_runtime    = %d    # minutes; drops shorts

# Terms of the final score. Must sum to 1.0.
[weights]
imdb       = %.2f
tmdb       = %.2f
confidence = %.2f
mainstream = %.2f
freshness  = %.2f

# Bayesian smoothing: m is the vote count at which a film's own rating carries
# half the weight, mean is what a thinly-voted film is pulled towards.
[bayes]
imdb_m    = %.0f
imdb_mean = %.2f
tmdb_m    = %.0f
tmdb_mean = %.2f
kp_m      = %.0f   # used only by "kino ru"
kp_mean   = %.2f

# Arthouse probability to score penalty: none below low, full above high,
# linear in between.
[arthouse]
low         = %.2f
high        = %.2f
max_penalty = %.2f

# Скидка за расхождение с международной аудиторией. Только для "kino ru", где
# ранжирование идёт по Кинопоиску. Штрафуется лишь превышение КП над IMDb:
# обратное о фильме ничего плохого не говорит.
[gap_penalty]
low         = %.1f
high        = %.1f
max_penalty = %.1f

# Скидка за детское кино. Это настройка вкуса, а не исправление: семейные фильмы
# честно массовые, просто их оценивают те, для кого они сняты. Поставьте 0,
# чтобы ранжировать их наравне со всем остальным.
[kids]
penalty           = %.1f
animation_penalty = %.1f   # мягче: взрослая анимация существует

# Пороги предупреждений о расхождении оценок: на сколько баллов должны разойтись
# две аудитории и какая выборка нужна с каждой стороны, чтобы это считалось
# расхождением, а не шумом.
[signals]
confidence_floor   = %.0f
confidence_ceiling = %.0f
gap_points         = %.1f
gap_min_votes      = %d
`,
		tomlString(s.TMDBToken), tomlString(s.OMDBKey), tomlString(s.KinopoiskKey),
		t.Defaults.Period, t.Defaults.Limit, t.Defaults.RuPeriod, t.Defaults.IMDbYearFloor,
		t.Thresholds.MinIMDbVotes, t.Thresholds.MinTMDBVotes, t.Thresholds.MinKPVotes, t.Thresholds.MinRuntime,
		t.Weights.IMDb, t.Weights.TMDB, t.Weights.Confidence, t.Weights.Mainstream, t.Weights.Freshness,
		t.Bayes.IMDbM, t.Bayes.IMDbMean, t.Bayes.TMDBM, t.Bayes.TMDBMean, t.Bayes.KPM, t.Bayes.KPMean,
		t.Arthouse.Low, t.Arthouse.High, t.Arthouse.MaxPenalty,
		t.GapPenalty.Low, t.GapPenalty.High, t.GapPenalty.MaxPenalty,
		t.Kids.Penalty, t.Kids.AnimationPenalty,
		t.Signals.ConfidenceFloor, t.Signals.ConfidenceCeiling,
		t.Signals.GapPoints, t.Signals.GapMinVotes)
}

// tomlString quotes a value for a TOML basic string.
//
// API keys are alphanumeric in practice, but a pasted key can pick up stray
// characters, and an unescaped quote would produce a config file that no longer
// parses — with the key already lost from the terminal scrollback.
func tomlString(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(v) + `"`
}

// WriteTemplate creates the config file. It never overwrites an existing one:
// the file holds API keys, and silently replacing it would lose them.
func WriteTemplate(path string, t Tuning) error {
	return writeConfig(path, t, Secrets{}, false)
}

// WriteConfig writes tuning and keys together, for "kino setup". Overwriting is
// possible but never implicit: the caller must have asked, because the file it
// replaces holds keys that may exist nowhere else.
func WriteConfig(path string, t Tuning, s Secrets, overwrite bool) error {
	return writeConfig(path, t, s, overwrite)
}

func writeConfig(path string, t Tuning, s Secrets, overwrite bool) error {
	if _, err := os.Stat(path); err == nil {
		if !overwrite {
			return fmt.Errorf("%s already exists", path)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// 0600: the file holds API keys.
	return os.WriteFile(path, []byte(TemplateWith(t, s)), 0o600)
}
