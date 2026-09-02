CREATE TABLE movies (
  tmdb_id        INTEGER PRIMARY KEY,
  imdb_id        TEXT,
  kp_id          INTEGER,
  title          TEXT NOT NULL,
  original_title TEXT,
  title_ru       TEXT,
  year           INTEGER,
  runtime        INTEGER,
  overview       TEXT,
  overview_ru    TEXT,
  poster_path    TEXT,
  genres         TEXT,
  orig_lang      TEXT,
  popularity     REAL,
  tmdb_rating    REAL,
  tmdb_votes     INTEGER,
  imdb_rating    REAL,
  imdb_votes     INTEGER,
  metascore      INTEGER,
  rt_score       INTEGER,
  kp_rating      REAL,
  kp_votes       INTEGER,
  updated_at     TEXT NOT NULL,
  enriched_at    TEXT
);
CREATE INDEX idx_movies_imdb    ON movies(imdb_id);
CREATE INDEX idx_movies_enrich  ON movies(enriched_at);

CREATE TABLE releases (
  tmdb_id INTEGER NOT NULL REFERENCES movies(tmdb_id) ON DELETE CASCADE,
  region  TEXT NOT NULL,
  type    INTEGER NOT NULL,
  date    TEXT NOT NULL,
  note    TEXT,
  PRIMARY KEY (tmdb_id, region, type, date)
);
CREATE INDEX idx_releases_digital ON releases(type, date, region);

CREATE TABLE sync_runs (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at  TEXT NOT NULL,
  finished_at TEXT,
  command     TEXT,
  params      TEXT,
  found       INTEGER DEFAULT 0,
  inserted    INTEGER DEFAULT 0,
  updated     INTEGER DEFAULT 0,
  skipped     INTEGER DEFAULT 0,
  api_calls   TEXT,
  error       TEXT
);

CREATE TABLE quota_usage (
  source TEXT NOT NULL,
  day    TEXT NOT NULL,
  calls  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (source, day)
);
