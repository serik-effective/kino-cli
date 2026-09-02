// Package config loads settings from ~/.config/kino/config.toml and the
// environment. Real environment variables always win over the file.
//
// The older config.env is still read for API keys so existing installs keep
// working, but only config.toml carries the ranking coefficients: the algorithm
// must be tunable from one readable file rather than from scattered constants.
package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	TMDBToken    string
	OMDBKey      string
	KinopoiskKey string
	Region       string
	Lang         string
	DBPath       string

	// Tuning is every coefficient the ranking uses.
	Tuning Tuning
	// LoadedFrom is the config file actually read, empty when none existed.
	// Reported by "kino config check" so a surprising ranking can be traced to
	// the file that produced it.
	LoadedFrom string
}

// tomlFile mirrors the on-disk layout of config.toml.
type tomlFile struct {
	Secrets struct {
		TMDBToken    string `toml:"tmdb_token"`
		OMDBKey      string `toml:"omdb_api_key"`
		KinopoiskKey string `toml:"kinopoisk_api_key"`
	} `toml:"secrets"`
	General struct {
		Region string `toml:"region"`
		Lang   string `toml:"lang"`
		DBPath string `toml:"db_path"`
	} `toml:"general"`
	Tuning
}

// DefaultPath is the TOML config consulted when KINO_CONFIG is unset.
func DefaultPath() string { return configPath("config.toml") }

// LegacyPath is the pre-TOML key file, still read for API keys.
func LegacyPath() string { return configPath("config.env") }

func configPath(name string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "kino", name)
}

// Load builds the effective configuration. Precedence, lowest first: built-in
// defaults, config.toml, the legacy config.env, then real environment variables.
func Load() (*Config, error) {
	c := &Config{Tuning: DefaultTuning()}

	tomlPath := os.Getenv("KINO_CONFIG")
	if tomlPath == "" {
		tomlPath = DefaultPath()
	}
	if err := c.loadTOML(tomlPath); err != nil {
		return nil, err
	}

	// The legacy file only ever held keys and a few scalars; it never carried
	// coefficients, so nothing here can change the ranking.
	legacy, err := readEnvFile(LegacyPath())
	if err != nil {
		return nil, err
	}
	get := func(key string) string {
		if v := os.Getenv(key); v != "" {
			return v
		}
		return legacy[key]
	}

	setIfEmpty(&c.TMDBToken, get("TMDB_TOKEN"))
	setIfEmpty(&c.OMDBKey, get("OMDB_API_KEY"))
	setIfEmpty(&c.KinopoiskKey, get("KINOPOISK_API_KEY"))
	setIfEmpty(&c.Region, get("KINO_REGION"))
	setIfEmpty(&c.Lang, get("KINO_LANG"))
	setIfEmpty(&c.DBPath, get("KINO_DB"))

	// A real environment variable outranks any file, including config.toml.
	overrideFromEnv(&c.TMDBToken, "TMDB_TOKEN")
	overrideFromEnv(&c.OMDBKey, "OMDB_API_KEY")
	overrideFromEnv(&c.KinopoiskKey, "KINOPOISK_API_KEY")
	overrideFromEnv(&c.Region, "KINO_REGION")
	overrideFromEnv(&c.Lang, "KINO_LANG")
	overrideFromEnv(&c.DBPath, "KINO_DB")
	if v := os.Getenv("KINO_IMDB_YEAR_FLOOR"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("KINO_IMDB_YEAR_FLOOR: %w", err)
		}
		c.Tuning.Defaults.IMDbYearFloor = n
	}

	if c.Region == "" {
		c.Region = "US"
	}
	if c.Lang == "" {
		c.Lang = "ru-RU"
	}
	if c.DBPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		c.DBPath = filepath.Join(home, ".local", "share", "kino", "kino.db")
	}
	c.DBPath = expandHome(c.DBPath)

	if err := c.Tuning.Validate(); err != nil {
		where := c.LoadedFrom
		if where == "" {
			where = "built-in defaults"
		}
		return nil, fmt.Errorf("%s: %w", where, err)
	}
	return c, nil
}

// loadTOML overlays the file onto the defaults already in c. A missing file is
// not an error: the built-in tuning is a complete, working configuration.
func (c *Config) loadTOML(path string) error {
	if path == "" {
		return nil
	}
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Decoding into a value seeded with the defaults means a partial file only
	// overrides the keys it actually mentions.
	f := tomlFile{Tuning: c.Tuning}
	if _, err := toml.Decode(string(buf), &f); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	c.Tuning = f.Tuning
	c.TMDBToken = f.Secrets.TMDBToken
	c.OMDBKey = f.Secrets.OMDBKey
	c.KinopoiskKey = f.Secrets.KinopoiskKey
	c.Region = f.General.Region
	c.Lang = f.General.Lang
	c.DBPath = f.General.DBPath
	c.LoadedFrom = path
	return nil
}

func setIfEmpty(dst *string, v string) {
	if *dst == "" {
		*dst = v
	}
}

func overrideFromEnv(dst *string, key string) {
	if v := os.Getenv(key); v != "" {
		*dst = v
	}
}

// RequireTMDB reports a friendly error when the mandatory token is missing.
func (c *Config) RequireTMDB() error {
	if c.TMDBToken == "" {
		// First run is the likeliest reason to be here, so name the command
		// that fixes it rather than only the file that is missing.
		return fmt.Errorf("нет ключа TMDB — начните с: kino setup\n"+
			"вручную: %s, ключ tmdb_token в [secrets], либо переменная TMDB_TOKEN", DefaultPath())
	}
	return nil
}

func readEnvFile(path string) (map[string]string, error) {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		out[strings.TrimSpace(key)] = val
	}
	return out, sc.Err()
}

// expandHome turns a leading ~ into the home directory.
//
// A config file is written by hand, and "~/..." is what a person writes there.
// The shell expands it for a flag but not for a value inside a file, so without
// this the path is taken literally and SQLite creates a directory actually
// named "~" in whatever the working directory happens to be — a database that
// silently appears in two places depending on where the command was run.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}
