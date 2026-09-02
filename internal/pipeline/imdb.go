package pipeline

import (
	"context"
	"fmt"

	"github.com/sbeysenov/kino-cli/internal/source/imdb"
)

type ImportIMDbOpts struct {
	// YearFloor drops older films from title.basics. Zero keeps every year.
	YearFloor int
	// SkipTitles imports ratings only, which is the fast path: ratings are a
	// few megabytes, basics is a couple of hundred.
	SkipTitles bool
}

type ImportIMDbResult struct {
	Ratings int
	Titles  int
	// Applied counts movies of ours that picked up a rating from the dataset.
	Applied int
}

// ImportIMDb loads the IMDb Non-Commercial Datasets into the local database and
// then copies the ratings onto the films we track. Nothing is held in memory:
// both files are streamed and written in batches.
func (d *Deps) ImportIMDb(ctx context.Context, o ImportIMDbOpts) (ImportIMDbResult, error) {
	var res ImportIMDbResult
	c := imdb.New()

	d.logf("imdb: title.ratings…")
	rw := d.Store.NewRatingsWriter()
	n, err := c.Ratings(ctx, func(r imdb.Rating) error {
		return rw.Add(ctx, r.ID, r.Rating, r.Votes)
	})
	// Close first: a partial import is still worth keeping, and the writer holds
	// an open transaction that must be flushed either way.
	if cerr := rw.Close(); err == nil {
		err = cerr
	}
	res.Ratings = rw.Total()
	if err != nil {
		return res, fmt.Errorf("title.ratings after %d rows: %w", res.Ratings, err)
	}
	if err := d.Store.MarkIMDbDataset(ctx, "title.ratings", n); err != nil {
		return res, err
	}
	d.logf("imdb: title.ratings — %d rows", res.Ratings)

	if !o.SkipTitles {
		d.logf("imdb: title.basics (streaming, filtered)…")
		tw := d.Store.NewTitlesWriter()
		kept, err := c.Titles(ctx, imdb.TitleFilter{YearFloor: o.YearFloor}, func(t imdb.Title) error {
			return tw.Add(ctx, t.ID, t.Title, nullIfEmpty(t.OrigName), t.Year,
				nullIfZero(t.Runtime), nullIfEmpty(t.Genres))
		})
		if cerr := tw.Close(); err == nil {
			err = cerr
		}
		res.Titles = tw.Total()
		if err != nil {
			return res, fmt.Errorf("title.basics after %d rows: %w", res.Titles, err)
		}
		if err := d.Store.MarkIMDbDataset(ctx, "title.basics", kept); err != nil {
			return res, err
		}
		d.logf("imdb: title.basics — %d feature films kept", res.Titles)
	}

	applied, err := d.Store.ApplyIMDbRatings(ctx)
	if err != nil {
		return res, err
	}
	res.Applied = applied
	return res, nil
}

// nullIfEmpty keeps empty strings out of the database so "no value" is one
// thing (NULL) rather than two.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}
