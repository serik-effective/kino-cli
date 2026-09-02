# Архитектура

## Дерево
```
cmd/kino/main.go          точка входа
internal/cli/             команды (cobra): update, list, show, enrich, config
internal/config/          загрузка env/файла, валидация ключей
internal/store/           SQLite: миграции, репозитории, запросы
internal/source/tmdb/     клиент + discover/details
internal/source/omdb/     клиент
internal/source/kp/       клиент poiskkino.dev: v1.5 по одному фильму, v1.4 каталог
                          по фильтрам (discover.go) + rating.kinopoisk.ru XML (rating.go)
internal/source/imdb/     потоковое чтение gz-датасетов IMDb, без записи на диск
internal/score/           чистая модель ранжирования: не знает про БД и сеть
internal/source/kinozal/  парсер публичных чартов kinozal.me (только метаданные)
internal/source/wikidata/ SPARQL-резолв imdb_id → kp_id (без ключа и квоты)
internal/pipeline/        оркестрация: discover -> verify -> upsert -> enrich
internal/model/           доменные типы (Movie, Release, Ratings)
internal/httpx/           общий http-клиент: retry, backoff, rate-limit, ETag-кэш
docs/memory-bank/         этот меморибанк
```

## Поток данных (фича №1 `update digital`)
1. `pipeline.Digital(from, to, region, filters)`
2. TMDB `/discover/movie` постранично → кандидаты (id, базовые поля).
3. Для каждого кандидата TMDB `/movie/{id}?append_to_response=release_dates,external_ids`.
4. Верификация: есть ли `type=4` для `region` внутри окна `[from,to]`. Нет → отбросить.
5. Пред-фильтр по `vote_average`/`vote_count` (дешевле, чем обогащать).
6. Upsert в SQLite (movies + releases), лог в `sync_runs`.
7. Опционально `--enrich`: OMDb по `imdb_id`, KP по `externalId.imdb` — в пределах дневной квоты.

## CLI-контракт (черновик)
```
kino update digital --from 2026-08-20 --to 2026-08-27 --region US \
    --min-rating 6.5 --min-votes 50 --enrich omdb,kp --dry-run
kino update digital --last 7d            # сахар для окна
kino list --digital-since 7d --min-rating 7 --sort rating --format json
kino show tt1285016 | kino show tmdb:9799
kino enrich --pending --limit 100        # догнать необогащённые в рамках квоты
kino config check                        # какие ключи есть, какие квоты
```
Общие флаги: `--format table|json|csv`, `--db <path>`, `--concurrency`, `--verbose`.

## Реальный CLI (реализовано)
```
kino update movies --last 7d [--from --to --region --release-type --original-language --origin-country --min-rating --min-votes --max-pages --concurrency --dry-run --enrich omdb,kp]   # алиас: update digital
kino list  --since 7d [--from --to --region any|US --release-type --original-language --country --rating-source tmdb|imdb|kp --min-rating --min-votes --sort date|rating|tmdb|imdb|kp|popularity|votes|title --asc --limit]
kino show  tt1285016 | tmdb:9799 | 9799
kino enrich [--sources omdb,kp --limit --stale-days --concurrency --dry-run]
kino refresh ratings [--limit --stale-hours --concurrency --overwrite-imdb --dry-run]   # бесплатно, без квоты
kino refresh kp-ids  [--limit --dry-run]                                                # Wikidata, бесплатно
kino trends fetch [--period week|month|halfyear|all --pages N --details --limit --no-match --dry-run]
kino trends list  [--since 7d --metric delta|downloads|seeds|rank --limit --unmatched]
kino import kp <file.json>...  [--all-types --dry-run]   # выгрузка плеера КП: kp_id, рейтинг, показы, где смотреть
kino config check
kino sync                                # тренды + свежая цифра + рейтинги, одна команда для cron
```

## Пресеты (internal/cli/presets.go)
`new`, `ru`, `kz`, `kp`, `digital` — тонкие обёртки над `newListCmdWith(listDefaults{...})`; `hot`, `rising`, `live` — над `newTrendsListCmdWith(trendDefaults{...})`. Флаги базовой команды сохраняются и перебивают дефолты. Добавить новый пресет = дописать структуру в `presetCmds()`, без дублирования логики.

## Поток тренда
`trends fetch` → чарт kinozal → фильтр «это фильм?» → (опц.) details за счётчиком «Скачали» → `torrents` + снапшот в `torrent_stats` → матчинг названия в TMDB → `movies`. Дальше рейтинги подтягивают обычные `enrich` / `refresh ratings`. `trends list` группирует релизы по `tmdb_id` и считает дельту скачиваний между снапшотами.

## Технические решения
- Go 1.26, cobra (CLI), `modernc.org/sqlite` (чистый Go, без cgo → простой билд).
- Конкурентность: worker pool + `golang.org/x/time/rate` на источник.
- Ретраи: 429/5xx → exp backoff + jitter, уважать `Retry-After`.
- Всё, что уходит наружу — через `internal/httpx`, чтобы лимиты/кэш были в одном месте.


## Ранжирование (internal/score)

Пакет намеренно не имеет доступа ни к БД, ни к сети: на вход `Input` со
значениями, на выход `Breakdown`. Поэтому модель тестируется таблицами и
объясняется пользователю через `--why` теми же числами, которыми считается.

Два режима, `General` и `Russian`, отличаются набором источников и наличием
штрафа за разрыв Кинопоиск ↔ IMDb. Порядок вычисления:

1. байесовское сглаживание каждого источника: `(v/(v+m))·R + (m/(v+m))·C`
2. смешивание по весам; **источник с нулём голосов выбрасывается, а не
   сглаживается к среднему** — его вес перераспределяется между остальными
3. сигналы 0..1 (уверенность, массовость, свежесть) умножаются на 10 и
   подмешиваются с их весами
4. вычитаются штрафы: артхаус, разрыв оценок, детское/анимация
5. clamp в 0..10

Уверенность логарифмическая: разница между 300 и 3000 голосов должна значить
больше, чем между 300 000 и 303 000.

Словари жанров **двуязычные**. В БД жанры лежат по-русски («криминал»,
«драма»), и англоязычная карта молча давала ноль массовости и незаслуженный
штраф артхауса всему каталогу. Тест прогоняет один и тот же фильм с русскими и
английскими жанрами и требует одинаковый результат.

## CLI-контракт рекомендатора (internal/cli/topcmd.go)

Позиционная грамматика, без флагов для главного сценария:

```
kino            kino 30         kino ru        kino ru 90     kino триллер 60
```

Разбор: `ru` переключает дорожку, число — окно в днях, остальное ищется в
`genreAliases` (русские и английские написания одного жанра).
