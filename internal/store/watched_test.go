package store

import (
	"context"
	"testing"

	"github.com/sbeysenov/kino-cli/internal/model"
)

func year(n int) *int { return &n }

func seedFilms(t *testing.T, s *Store, films ...*model.Movie) {
	t.Helper()
	for _, m := range films {
		if _, err := s.UpsertMovie(context.Background(), m); err != nil {
			t.Fatal(err)
		}
	}
}

// A film named exactly what was asked for beats films that merely contain the
// word, so the common case needs no tmdb id.
func TestFindMoviesPrefersAnExactTitle(t *testing.T) {
	s := newTestStore(t)
	seedFilms(t, s,
		&model.Movie{TMDBID: 1, TitleRU: "Дрю Майкл: Красный, синий, зеленый", Title: "Drew Michael", Year: year(2021)},
		&model.Movie{TMDBID: 2, TitleRU: "Майкл", Title: "Michael", Year: year(2026)},
		&model.Movie{TMDBID: 3, TitleRU: "НЕИЗМЕННЫЙ: Майкл Дж. Фокс", Title: "Still", Year: year(2023)},
	)

	got, err := s.FindMovies(context.Background(), "майкл", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TMDBID != 2 {
		t.Fatalf("got %+v, want only the film actually named Майкл", got)
	}
}

// Without an exact match the ambiguity is real and must be shown, not guessed.
func TestFindMoviesKeepsGenuineAmbiguity(t *testing.T) {
	s := newTestStore(t)
	seedFilms(t, s,
		&model.Movie{TMDBID: 1, TitleRU: "Проект «Конец света»", Year: year(2026)},
		&model.Movie{TMDBID: 2, TitleRU: "Последнее замыкание. Конец света", Year: year(2023)},
	)

	got, err := s.FindMovies(context.Background(), "конец света", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d results, want both — neither is named exactly that", len(got))
	}
}

func TestFindMoviesMatchesOriginalTitleAndIgnoresCase(t *testing.T) {
	s := newTestStore(t)
	seedFilms(t, s, &model.Movie{TMDBID: 1, TitleRU: "Обсессия", Title: "Obsession", Year: year(2026)})

	for _, q := range []string{"ОБСЕССИЯ", "obsession", "Обсессия"} {
		got, err := s.FindMovies(context.Background(), q, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 {
			t.Errorf("query %q returned %d results, want 1", q, len(got))
		}
	}
}

func TestFindMoviesByTMDBID(t *testing.T) {
	s := newTestStore(t)
	seedFilms(t, s, &model.Movie{TMDBID: 936075, TitleRU: "Майкл", Year: year(2026)})

	got, err := s.FindMovies(context.Background(), "tmdb:936075", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].TMDBID != 936075 {
		t.Fatalf("got %+v", got)
	}
}
