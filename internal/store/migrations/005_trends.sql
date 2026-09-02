CREATE TABLE torrents (
  source      TEXT NOT NULL,
  ext_id      TEXT NOT NULL,
  raw_title   TEXT NOT NULL,
  title_ru    TEXT,
  title_orig  TEXT,
  year        INTEGER,
  tags        TEXT,
  quality     TEXT,
  tmdb_id     INTEGER REFERENCES movies(tmdb_id) ON DELETE SET NULL,
  match_state TEXT NOT NULL DEFAULT 'pending',   -- pending | matched | unmatched
  first_seen  TEXT NOT NULL,
  last_seen   TEXT NOT NULL,
  PRIMARY KEY (source, ext_id)
);
CREATE INDEX idx_torrents_tmdb  ON torrents(tmdb_id);
CREATE INDEX idx_torrents_state ON torrents(match_state);

CREATE TABLE torrent_stats (
  source      TEXT NOT NULL,
  ext_id      TEXT NOT NULL,
  captured_at TEXT NOT NULL,
  period      TEXT,
  rank        INTEGER,
  seeds       INTEGER,
  leechers    INTEGER,
  downloads   INTEGER,
  comments    INTEGER,
  PRIMARY KEY (source, ext_id, captured_at)
);
CREATE INDEX idx_torrent_stats_time ON torrent_stats(captured_at);
