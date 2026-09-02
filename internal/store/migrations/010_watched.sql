-- Films the user has already seen. Kept out of recommendations, and the seed of
-- the personalisation the spec sketches for later: a rating here is the only
-- signal we will ever have about this particular viewer's taste.
CREATE TABLE watched (
  tmdb_id    INTEGER PRIMARY KEY REFERENCES movies(tmdb_id) ON DELETE CASCADE,
  rating     INTEGER,
  watched_at TEXT NOT NULL
);
