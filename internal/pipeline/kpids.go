package pipeline

import (
	"context"

	"github.com/sbeysenov/kino-cli/internal/store"
)

type ResolveOpts struct {
	Limit  int
	DryRun bool
}

// ResolveKPIDs fills in Kinopoisk ids through Wikidata. This is the cheap half
// of Kinopoisk coverage: no key, no daily quota. Once an id is known, ratings
// come from the free XML feed, so the paid API is only needed for whatever
// Wikidata does not know yet.
func (d *Deps) ResolveKPIDs(ctx context.Context, o ResolveOpts) (store.RunResult, error) {
	res := store.RunResult{}
	if d.Wikidata == nil {
		return res, nil
	}

	missing, err := d.Store.MissingKPID(ctx, o.Limit)
	if err != nil {
		return res, err
	}
	res.Found = len(missing)
	if len(missing) == 0 {
		return res, nil
	}
	d.logf("resolving %d imdb ids via wikidata (%d per query)", len(missing), 250)

	found, err := d.Wikidata.KinopoiskIDs(ctx, missing)
	if err != nil {
		res.APICalls = d.Calls.Snapshot()
		return res, err
	}
	res.Skipped = len(missing) - len(found)

	if o.DryRun {
		res.Updated = len(found)
		res.APICalls = d.Calls.Snapshot()
		return res, nil
	}
	written, err := d.Store.SetKPIDs(ctx, found)
	if err != nil {
		return res, err
	}
	res.Updated = written
	res.APICalls = d.Calls.Snapshot()
	return res, nil
}
