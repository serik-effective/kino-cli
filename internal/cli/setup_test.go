package cli

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/sbeysenov/kino-cli/internal/config"
)

func scripted(input string) *asker {
	return &asker{in: bufio.NewScanner(strings.NewReader(input)), out: io.Discard}
}

// Pressing Enter through the whole interview must leave the shipped model
// exactly as it is. A wizard that retunes an installation the user did not
// steer is worse than no wizard.
func TestEmptyAnswersKeepTheDefaultModel(t *testing.T) {
	a := scripted("\n\n\n\n\n")
	got := askTaste(a).Tuning()
	if a.err != nil {
		t.Fatal(a.err)
	}
	if got != config.DefaultTuning() {
		t.Errorf("defaults changed:\n got %+v\nwant %+v", got, config.DefaultTuning())
	}
}

func TestAnswersMapToTheChosenOptions(t *testing.T) {
	a := scripted("3\n3\n2\n1\n5\n")
	ans := askTaste(a)
	if a.err != nil {
		t.Fatal(a.err)
	}
	want := config.Answers{
		Taste: config.TasteNiche,
		Kids:  config.KidsAnime,
		Ru:    config.RuTrust,
		Fresh: config.FreshHigh,
		Limit: 5,
	}
	if ans != want {
		t.Errorf("got %+v, want %+v", ans, want)
	}
	if err := ans.Tuning().Validate(); err != nil {
		t.Errorf("chosen answers produced an invalid model: %v", err)
	}
}

// Out-of-range and non-numeric input must re-ask rather than fall through to a
// default the user did not pick.
func TestBadChoiceIsReAsked(t *testing.T) {
	a := scripted("9\nабв\n0\n3\n")
	if n := a.choice("q", 1, []string{"a", "b", "c"}); n != 3 {
		t.Errorf("got %d, want 3 after three rejected answers", n)
	}
	if a.err != nil {
		t.Fatal(a.err)
	}
}

// Enter on a key question keeps what is already stored. Losing a key to a
// re-run of the wizard would mean digging it out of a telegram bot again.
func TestExistingKeysSurviveEmptyAnswers(t *testing.T) {
	a := scripted("\n\n\n")
	known := config.Secrets{TMDBToken: "tmdb-old", KinopoiskKey: "kp-old", OMDBKey: "omdb-old"}
	if got := askKeys(a, known); got != known {
		t.Errorf("got %+v, want the stored keys %+v", got, known)
	}
}

func TestPastedKeyLosesTrailingJunk(t *testing.T) {
	a := scripted("abc123 (token)\n\n\n")
	got := askKeys(a, config.Secrets{})
	if got.TMDBToken != "abc123" {
		t.Errorf("token %q, want %q", got.TMDBToken, "abc123")
	}
}

// The interview reads a terminal. Under a pipe it must refuse rather than let
// whatever is on stdin answer questions about someone's taste.
func TestSetupRefusesWhenInputIsNotATerminal(t *testing.T) {
	t.Setenv("KINO_CONFIG", t.TempDir()+"/config.toml")
	root := newRootCmd()
	root.SetArgs([]string{"setup"})
	root.SetOut(io.Discard)
	err := root.Execute()
	if err == nil {
		t.Fatal("expected setup to refuse a non-terminal stdin")
	}
	if !strings.Contains(err.Error(), "config init") {
		t.Errorf("error should point at the scriptable path, got: %v", err)
	}
}

func TestSetupRefusesToClobberAnExistingConfig(t *testing.T) {
	path := t.TempDir() + "/config.toml"
	t.Setenv("KINO_CONFIG", path)
	if err := config.WriteConfig(path, config.DefaultTuning(),
		config.Secrets{TMDBToken: "keep-me"}, false); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	root.SetArgs([]string{"setup"})
	root.SetOut(io.Discard)
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a refusal naming --force, got: %v", err)
	}
	// And the file must still be there with the key in it.
	cfg, lerr := config.Load()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if cfg.TMDBToken != "keep-me" {
		t.Errorf("token %q — the refused setup touched the file", cfg.TMDBToken)
	}
}

// The wizard finishes by running the real commands from a fresh command tree
// built inside the tree already executing. Cobra is not obviously safe to
// re-enter, so the mechanism is worth a test of its own — with a cheap command
// standing in for the ten-minute one.
func TestFirstFillRunsNestedCommands(t *testing.T) {
	t.Setenv("KINO_CONFIG", t.TempDir()+"/config.toml")
	var buf strings.Builder
	if err := firstFill(&buf, [][]string{{"config", "show"}, {"config", "show"}}); err != nil {
		t.Fatalf("nested execution failed: %v", err)
	}
	if n := strings.Count(buf.String(), "=== kino config show ==="); n != 2 {
		t.Errorf("announced %d steps, want 2", n)
	}
}

// A failing step must stop the run and say which command failed, not report a
// successful setup over a database that never filled.
func TestFirstFillStopsOnFailure(t *testing.T) {
	var buf strings.Builder
	err := firstFill(&buf, [][]string{{"нет-такой-команды"}})
	if err == nil {
		t.Fatal("expected the failing step to surface")
	}
	if !strings.Contains(err.Error(), "нет-такой-команды") {
		t.Errorf("error should name the step, got: %v", err)
	}
}
