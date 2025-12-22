# Учёт доходов и расходов (Go + SQLite)

Локальный JSON API для ведения категорий, транзакций, бюджетов и предупреждений о превышении лимитов.

## Технологии
- Go 1.25
- SQLite через драйвер `modernc.org/sqlite` (файл `data/ledger.db`)
- Чистый HTTP JSON (без фронтенда)

## Запуск
```bash
cd /Users/ilyaazarov/Go/sandbox
GOCACHE=$(pwd)/.cache GOMODCACHE=$(pwd)/.gocache go run .
# сервер слушает http://localhost:8080
```

Тесты (офлайн):
```bash
GOCACHE=$(pwd)/.cache GOPROXY=off go test ./...
```

## Основные эндпоинты
- `GET/POST /categories` — список и создание категорий (`name`).
- `POST /transactions` — добавить операцию: `category_id`, `amount_rub` (строка, например "-32.5"), `occurred_at` (`YYYY-MM-DD`), `note`.
- `GET /transactions?from=YYYY-MM-DD&to=YYYY-MM-DD` — операции за период.
- `GET /summary?from=...&to=...` — агрегаты по категориям (доход/расход/итог в рублях, количество операций).
- `GET/POST /budgets` — список и установка/обновление лимитов (`limit_rub`).
- `GET /alerts?from=...&to=...` — превышения бюджетов за период.

## Примеры `curl`
```bash
# создать категорию
curl -X POST http://localhost:8080/categories \
  -H "Content-Type: application/json" \
  -d '{"name":"Транспорт"}'

# установить бюджет 5000 руб
curl -X POST http://localhost:8080/budgets \
  -H "Content-Type: application/json" \
  -d '{"category_id":1,"limit_rub":"5000"}'

# добавить расход -32 руб 2024-09-01
curl -X POST http://localhost:8080/transactions \
  -H "Content-Type: application/json" \
  -d '{"category_id":1,"amount_rub":"-32","occurred_at":"2024-09-01","note":"автобус"}'

# сводка и алерты
curl "http://localhost:8080/summary?from=2024-09-01&to=2024-09-30"
curl "http://localhost:8080/alerts?from=2024-09-01&to=2024-09-30"
```

## План следующих шагов
- Добавить простой фронтенд (HTML/JS) для работы с API.
- Фильтр и пагинация для `/transactions`, 404 для неизвестных категорий.
- Цели/лимиты по периодам (неделя/месяц) и расширенные отчёты.
