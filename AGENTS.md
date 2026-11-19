# AGENTS.md

Инструкции для AI-агентов и разработчиков, работающих с этим проектом.

## О проекте

«Готовность» — личное веб-приложение на Go для работы с рисками и стремлениями:
кейсы, действия с весами, шаги, промпты для нейросети (оценка весов, разбивка шага
«уточнить шаг», генерация рисков). Один бинарник, внешних зависимостей нет, шаблоны
и статика вшиты в бинарник, htmx подгружается с CDN в браузере.

## Структура

Слоистая архитектура (по гайду inDrive), зависимости строго сверху вниз:

```
cmd/gotovnost/main.go   точка входа: флаги, вызов app.Run
internal/
  app/                  сборка зависимостей и запуск (Run)
  models/               данные и правила: model.go (структуры/расчёт/проверки),
                        percent.go (веса), format.go (числа), ids.go
  services/             бизнес-логика промптов: estimate.go (веса действий),
                        refine.go («уточнить шаг»), risks.go (риски) + *.tmpl
  database/             хранение: store.go (JSON, атомарная запись, миграция),
                        accounts.go (учётки, пароли, сессии)
  transport/            HTTP: routes.go, cases.go, prompts.go, auth.go, risks.go,
                        views.go + templates/ и static/
test/                   внешние black-box тесты, по пакетам
scripts/run.sh          скрипт запуска
data/, backups/         данные и бэкапы (в git не попадают)
```

Направление зависимостей: `models` ← `services`, `models` ← `database`,
всё ← `transport` ← `app`. Циклов нет: вышестоящий слой знает о нижестоящем, не наоборот.

## Запуск

```bash
go run ./cmd/gotovnost                     # http://127.0.0.1:8080
go run ./cmd/gotovnost -addr 127.0.0.1:9000
./scripts/run.sh                           # в Git Bash (или Linux/macOS)
```

Сборка и проверка:

```bash
go build ./...
go vet ./...
```

## Тесты

Тесты лежат в корневой папке `test/`, разбиты по пакетам (`models`, `services`,
`database`, `transport`) и являются внешними black-box тестами.

```bash
go test ./...                 # все тесты
go test ./test/models/        # конкретный пакет
go test -run TestValidate ./test/models/   # конкретный тест
```

## Покрытие

Тесты внешние (в отдельной папке `test/`), поэтому обычный `go test -cover ./...`
показывает 0% для исходных пакетов. Покрытие меряется только через `-coverpkg`
с явным списком исходных пакетов.

По одному пакету:

```bash
go test -coverpkg=gotovnost/internal/models ./test/models/
go test -coverpkg=gotovnost/internal/services ./test/services/
go test -coverpkg=gotovnost/internal/database ./test/database/
go test -coverpkg=gotovnost/internal/transport ./test/transport/
```

Всё вместе + отчёты:

```bash
go test -coverpkg=gotovnost/internal/models,gotovnost/internal/services,gotovnost/internal/database,gotovnost/internal/transport \
    -coverprofile=coverage.out ./test/...
go tool cover -func=coverage.out     # сводка по функциям и общий процент
go tool cover -html=coverage.out     # HTML-отчёт в браузер
```

## Конвенции

- Go 1.22+, без внешних зависимостей (PBKDF2 реализован руками — см. `database/accounts.go`).
- Данные приложения — только в `data/` (gitignored), в коде никаких реальных данных не хранить.
- Новую логику клади в подходящий слой по схеме выше; не нарушай направление зависимостей.
- Промпт-шаблоны (`.tmpl`) лежат в `internal/services/` и вшиты через `//go:embed`.
