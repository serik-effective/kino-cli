package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withHome points both the TOML and the legacy config at a temp dir and clears
// the environment overrides, so each test starts from a known state.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("KINO_CONFIG", "")
	for _, k := range []string{"TMDB_TOKEN", "OMDB_API_KEY", "KINOPOISK_API_KEY",
		"KINO_REGION", "KINO_LANG", "KINO_DB", "KINO_IMDB_YEAR_FLOOR"} {
		t.Setenv(k, "")
	}
	if err := os.MkdirAll(filepath.Join(dir, ".config", "kino"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadUsesBuiltInTuningWhenNoFile(t *testing.T) {
	withHome(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.LoadedFrom != "" {
		t.Errorf("LoadedFrom = %q, want empty", c.LoadedFrom)
	}
	if c.Tuning != DefaultTuning() {
		t.Errorf("tuning drifted from defaults")
	}
}

// A partial file must override only the keys it mentions; everything else keeps
// the built-in value.
func TestPartialTOMLKeepsOtherDefaults(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "kino", "config.toml"), `
[thresholds]
min_kp_votes = 9999
`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Tuning.Thresholds.MinKPVotes != 9999 {
		t.Errorf("MinKPVotes = %d, want 9999", c.Tuning.Thresholds.MinKPVotes)
	}
	if want := DefaultTuning().Thresholds.MinIMDbVotes; c.Tuning.Thresholds.MinIMDbVotes != want {
		t.Errorf("MinIMDbVotes = %d, want untouched %d", c.Tuning.Thresholds.MinIMDbVotes, want)
	}
	if want := DefaultTuning().Weights.IMDb; c.Tuning.Weights.IMDb != want {
		t.Errorf("weights were clobbered by a thresholds-only file")
	}
}

func TestEnvBeatsTOML(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "kino", "config.toml"), `
[secrets]
tmdb_token = "from-file"
[general]
region = "US"
`)
	t.Setenv("TMDB_TOKEN", "from-env")
	t.Setenv("KINO_REGION", "RU")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDBToken != "from-env" {
		t.Errorf("TMDBToken = %q, want from-env", c.TMDBToken)
	}
	if c.Region != "RU" {
		t.Errorf("Region = %q, want RU", c.Region)
	}
}

// Installs predating config.toml keep working: keys still come from config.env.
func TestLegacyEnvFileStillSuppliesKeys(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "kino", "config.env"),
		"TMDB_TOKEN=legacy-token\nKINO_REGION=RU\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDBToken != "legacy-token" {
		t.Errorf("TMDBToken = %q, want legacy-token", c.TMDBToken)
	}
	if c.Region != "RU" {
		t.Errorf("Region = %q, want RU", c.Region)
	}
}

// config.toml is the newer file and outranks config.env when both are present.
func TestTOMLBeatsLegacyEnvFile(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "kino", "config.env"), "TMDB_TOKEN=legacy\n")
	writeFile(t, filepath.Join(home, ".config", "kino", "config.toml"), "[secrets]\ntmdb_token = \"toml\"\n")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDBToken != "toml" {
		t.Errorf("TMDBToken = %q, want toml", c.TMDBToken)
	}
}

// Weights that do not sum to 1 would silently skew every ranking, so loading
// must fail loudly rather than rank on a broken model.
func TestBadWeightsAreRejected(t *testing.T) {
	home := withHome(t)
	writeFile(t, filepath.Join(home, ".config", "kino", "config.toml"), `
[weights]
imdb       = 0.90
tmdb       = 0.25
confidence = 0.15
mainstream = 0.10
freshness  = 0.05
`)
	_, err := Load()
	if err == nil {
		t.Fatal("expected an error for weights summing to 1.45")
	}
	if !strings.Contains(err.Error(), "sum to 1.0") {
		t.Errorf("error should name the problem, got: %v", err)
	}
}

// The generated template must itself be valid and round-trip to the defaults;
// otherwise "config init" hands the user a broken file.
func TestTemplateRoundTripsToDefaults(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".config", "kino", "config.toml")
	if err := WriteTemplate(path, DefaultTuning()); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Tuning != DefaultTuning() {
		t.Errorf("template did not round-trip:\n got %+v\nwant %+v", c.Tuning, DefaultTuning())
	}
	if err := WriteTemplate(path, DefaultTuning()); err == nil {
		t.Error("WriteTemplate must refuse to overwrite an existing config")
	}
}
