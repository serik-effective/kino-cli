-- Local cache of API responses.
--
-- The key is a hash of the request URL, never the URL itself: some sources
-- (OMDb) carry the API key as a query parameter, and a cache table is not a
-- place to keep secrets.
CREATE TABLE api_cache (
  source     TEXT NOT NULL,
  key        TEXT NOT NULL,
  body       BLOB NOT NULL,
  fetched_at TEXT NOT NULL,
  PRIMARY KEY (source, key)
);

CREATE INDEX idx_api_cache_fetched ON api_cache(fetched_at);
