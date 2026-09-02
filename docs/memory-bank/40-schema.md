# Схема SQLite (черновик v1)

Файл БД: `${KINO_DB:-~/.local/share/kino/kino.db}`. Миграции — нумерованные .sql, embed через `embed.FS`.

```sql
CREATE TABLE movies (
  tmdb_id        INTEGER PRIMARY KEY,
  imdb_id        TEXT UNIQUE,
  kp_id          INTEGER UNIQUE,
  title          TEXT NOT NULL,      -- en
  original_title TEXT,
  title_ru       TEXT,
  year           INTEGER,
  runtime        INTEGER,            -- минуты
  overview       TEXT,
  overview_ru    TEXT,
  poster_path    TEXT,
  genres         TEXT,               -- json array
  countries      TEXT,               -- json array ISO-3166-1 (миграция 004)
  release_date   TEXT,               -- основная дата релиза TMDB (миграция 003)
  orig_lang      TEXT,
  popularity     REAL,
  tmdb_rating    REAL, tmdb_votes INTEGER,
  imdb_rating    REAL, imdb_votes INTEGER,
  metascore      INTEGER,
  rt_score       INTEGER,
  kp_rating      REAL, kp_votes INTEGER,
  updated_at     TEXT NOT NULL,      -- RFC3339
  enriched_at    TEXT,               -- NULL = не обогащён (OMDb/poiskkino)
  ratings_at     TEXT,               -- свежесть рейтингов из rating.kinopoisk.ru (миграция 002)
  kp_views       INTEGER,            -- показов в плеере КП (миграция 006)
  watch_online   INTEGER,            -- 1 = доступен онлайн в РФ (миграция 006)
  watch_offer    TEXT                -- "С мультиподпиской Яндекс Плюс", "79 ₽" (миграция 006)
);
CREATE TABLE releases (
  tmdb_id INTEGER NOT NULL REFERENCES movies(tmdb_id) ON DELETE CASCADE,
  region  TEXT NOT NULL,             -- ISO-3166-1
  type    INTEGER NOT NULL,          -- TMDB release type, 4 = digital
  date    TEXT NOT NULL,             -- YYYY-MM-DD
  note    TEXT,
  PRIMARY KEY (tmdb_id, region, type, date)
);
CREATE INDEX idx_releases_digital ON releases(type, date, region);
CREATE INDEX idx_movies_enrich    ON movies(enriched_at);

CREATE TABLE sync_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  started_at TEXT, finished_at TEXT,
  command TEXT, params TEXT,         -- json
  found INTEGER, inserted INTEGER, updated INTEGER, skipped INTEGER,
  api_calls TEXT,                    -- json {"tmdb":123,"omdb":40}
  error TEXT
);
CREATE TABLE quota_usage (           -- дневной счётчик по источникам
  source TEXT NOT NULL, day TEXT NOT NULL, calls INTEGER NOT NULL,
  PRIMARY KEY (source, day)
);
```
Правило upsert: `ON CONFLICT(tmdb_id) DO UPDATE` — не затирать непустое значение NULL-ом (COALESCE(excluded.x, movies.x)).

## Тренды (миграция 005)
```sql
CREATE TABLE torrents (            -- один релиз на трекере
  source TEXT, ext_id TEXT, raw_title TEXT, title_ru TEXT, title_orig TEXT,
  year INTEGER, tags TEXT, quality TEXT,
  tmdb_id INTEGER REFERENCES movies(tmdb_id) ON DELETE SET NULL,
  match_state TEXT NOT NULL DEFAULT 'pending',   -- pending | matched | unmatched
  first_seen TEXT, last_seen TEXT,
  PRIMARY KEY (source, ext_id)
);
CREATE TABLE torrent_stats (       -- снапшот счётчиков; дельта считается по ним
  source TEXT, ext_id TEXT, captured_at TEXT, period TEXT,
  rank INTEGER, seeds INTEGER, leechers INTEGER, downloads INTEGER, comments INTEGER,
  PRIMARY KEY (source, ext_id, captured_at)
);
```
`downloads` — накопительный счётчик «Скачали» с kinozal, поэтому тренд = разница между снапшотами.

## Добавлено 2026-08-27/28

`movies`:
- `kp_views INTEGER` — показов в плеере Кинопоиска
- `watch_online INTEGER` — 1 доступен, 0 недоступен, **NULL = не знаем**; разница
  принципиальна, см. решение про `OnlineKnown`
- `watch_offer TEXT` — текст предложения (устаревает, тарифы меняются)
- `watch_seen_at TEXT` — когда впервые увидели в плеере; ставится один раз
- `watch_services TEXT` — JSON-список сервисов из каталога КП: Кинопоиск, Иви,
  КИОН, START. Отвечает на «где смотреть», а не только «есть ли на КП»

Новые таблицы:
- `imdb_ratings(imdb_id PK, rating, votes)` — 1.7 млн строк из датасета
- `imdb_titles(imdb_id PK, title, orig_title, year, runtime, genres)` — 229 тыс.
  после фильтра `titleType=movie AND isAdult=0 AND startYear>=floor`
- `imdb_datasets(name PK, updated_at, rows)` — свежесть локальных копий
- `api_cache(source, key PK, body, fetched_at)` — ключ = SHA-256 от URL, не URL:
  у OMDb ключ API лежит в query-строке
- `watched(tmdb_id PK, rating, watched_at)` — просмотренное пользователем
