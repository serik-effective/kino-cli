-- IMDb Non-Commercial Datasets, loaded in bulk by "kino update imdb".
-- Kept in their own tables rather than merged into movies: they cover far more
-- titles than we track, and a re-import must never touch our own columns.

CREATE TABLE imdb_ratings (
  imdb_id TEXT PRIMARY KEY,
  rating  REAL NOT NULL,
  votes   INTEGER NOT NULL
);

-- Only feature films survive the import filter; see source/imdb for the rules.
CREATE TABLE imdb_titles (
  imdb_id    TEXT PRIMARY KEY,
  title      TEXT NOT NULL,
  orig_title TEXT,
  year       INTEGER,
  runtime    INTEGER,
  genres     TEXT
);

-- One row per dataset so the CLI can report how stale the local copy is when
-- imdb.com is unreachable.
CREATE TABLE imdb_datasets (
  name       TEXT PRIMARY KEY,
  updated_at TEXT NOT NULL,
  rows       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_imdb_titles_year ON imdb_titles(year);
