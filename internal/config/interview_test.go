package config

import (
	"path/filepath"
	"testing"
)

// Every questionnaire the wizard can produce must survive Validate. The weight
// sum is the trap: answers move terms independently, and a config that fails
// validation would break the CLI for a user who answered honestly.
func TestEveryAnswerCombinationValidates(t *testing.T) {
	tastes := []Taste{TasteWide, TasteBalanced, TasteNiche}
	kids := []KidsMode{KidsNone, KidsWith, KidsAnime}
	rus := []RuMode{RuCrossCheck, RuTrust}
	fresh := []FreshMode{FreshHigh, FreshNormal, FreshAny}
	limits := []int{0, 3, 5, 10}

	n := 0
	for _, ta := range tastes {
		for _, k := range kids {
			for _, r := range rus {
				for _, f := range fresh {
					for _, l := range limits {
						a := Answers{Taste: ta, Kids: k, Ru: r, Fresh: f, Limit: l}
						if err := a.Tuning().Validate(); err != nil {
							t.Errorf("%+v: %v", a, err)
						}
						n++
					}
				}
			}
		}
	}
	if n != 216 {
		t.Fatalf("covered %d combinations, expected 216", n)
	}
}

func TestDefaultAnswersReproduceDefaults(t *testing.T) {
	// The middle answer to every question must leave the model untouched,
	// otherwise the wizard quietly retunes an installation that asked for
	// nothing in particular.
	got := Answers{Taste: TasteBalanced, Kids: KidsNone, Ru: RuCrossCheck, Fresh: FreshNormal}.Tuning()
	if got != DefaultTuning() {
		t.Errorf("neutral answers changed the model:\ngot  %+v\nwant %+v", got, DefaultTuning())
	}
}

func TestTasteMovesTheVoteTerm(t *testing.T) {
	wide := Answers{Taste: TasteWide}.Tuning()
	niche := Answers{Taste: TasteNiche}.Tuning()

	if !(wide.Weights.Confidence > niche.Weights.Confidence) {
		t.Errorf("confidence: wide %.3f must exceed niche %.3f",
			wide.Weights.Confidence, niche.Weights.Confidence)
	}
	if !(wide.Thresholds.MinIMDbVotes > niche.Thresholds.MinIMDbVotes) {
		t.Errorf("imdb gate: wide %d must exceed niche %d",
			wide.Thresholds.MinIMDbVotes, niche.Thresholds.MinIMDbVotes)
	}
	if niche.Arthouse.MaxPenalty != 0 {
		t.Errorf("niche must not penalise arthouse, got %.2f", niche.Arthouse.MaxPenalty)
	}
}

func TestAnimeAnswerSparesAnimationOnly(t *testing.T) {
	// The distinction the "аниме" answer exists for: adult animation stops
	// being charged, family films keep their penalty.
	a := Answers{Kids: KidsAnime}.Tuning()
	if a.Kids.AnimationPenalty != 0 {
		t.Errorf("animation penalty %.2f, want 0", a.Kids.AnimationPenalty)
	}
	if a.Kids.Penalty != DefaultTuning().Kids.Penalty {
		t.Errorf("family penalty %.2f, want it untouched at %.2f",
			a.Kids.Penalty, DefaultTuning().Kids.Penalty)
	}
}

func TestFreshAnyWidensTheWindow(t *testing.T) {
	a := Answers{Fresh: FreshAny}.Tuning()
	if a.Weights.Freshness != 0 {
		t.Errorf("freshness weight %.3f, want 0", a.Weights.Freshness)
	}
	if a.Defaults.Period <= DefaultTuning().Defaults.Period {
		t.Errorf("period %d must exceed the default %d",
			a.Defaults.Period, DefaultTuning().Defaults.Period)
	}
}

// The wizard writes what it computed; the CLI reads it back on the next run.
// Two decimals in the template mean the weights survive that trip only because
// normalize snaps them — without it a wizard could emit a file its own
// Validate rejects.
func TestInterviewTuningSurvivesTheConfigFile(t *testing.T) {
	for _, a := range []Answers{
		{Taste: TasteWide, Fresh: FreshHigh},
		{Taste: TasteNiche, Fresh: FreshHigh, Kids: KidsWith},
		{Taste: TasteNiche, Fresh: FreshAny, Ru: RuTrust, Limit: 10},
		{Taste: TasteWide, Fresh: FreshAny},
	} {
		home := withHome(t)
		path := filepath.Join(home, ".config", "kino", "config.toml")
		want := a.Tuning()
		if err := WriteConfig(path, want, Secrets{}, false); err != nil {
			t.Fatal(err)
		}
		c, err := Load()
		if err != nil {
			t.Fatalf("%+v: %v", a, err)
		}
		if c.Tuning != want {
			t.Errorf("%+v did not round-trip:\n got %+v\nwant %+v", a, c.Tuning, want)
		}
		if err := c.Tuning.Validate(); err != nil {
			t.Errorf("%+v: reloaded config is invalid: %v", a, err)
		}
	}
}

// A key with a quote in it must not be able to break the file it is written to.
func TestSecretsAreEscaped(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".config", "kino", "config.toml")
	key := `ab"c\d`
	if err := WriteConfig(path, DefaultTuning(), Secrets{TMDBToken: key}, false); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("config with a quoted key does not parse: %v", err)
	}
	if c.TMDBToken != key {
		t.Errorf("token round-trip: got %q, want %q", c.TMDBToken, key)
	}
}

// Overwriting is destructive — the file may hold the only copy of a key — so it
// has to be asked for explicitly.
func TestWriteConfigRefusesToOverwriteUnlessAsked(t *testing.T) {
	home := withHome(t)
	path := filepath.Join(home, ".config", "kino", "config.toml")
	if err := WriteConfig(path, DefaultTuning(), Secrets{TMDBToken: "first"}, false); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfig(path, DefaultTuning(), Secrets{TMDBToken: "second"}, false); err == nil {
		t.Fatal("second write must be refused without overwrite")
	}
	if err := WriteConfig(path, DefaultTuning(), Secrets{TMDBToken: "second"}, true); err != nil {
		t.Fatal(err)
	}
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.TMDBToken != "second" {
		t.Errorf("token %q, want the overwritten value", c.TMDBToken)
	}
}
