# kino-cli

Go CLI: агрегирует фильмы из TMDB/OMDb/Kinopoisk в локальную SQLite для быстрых запросов агентами.

**Перед работой читай [docs/memory-bank/00-index.md](docs/memory-bank/00-index.md)** — там индекс, дальше грузи только нужный файл.
После значимых изменений обновляй `docs/memory-bank/90-progress.md`, при смене решения — дописывай строку в `50-decisions.md`.

Секреты: только env / `~/.config/kino/config.yaml`. Никогда не коммить ключи.
