# libs

Общая библиотека микросервисов zvonilkaRU: микро-хелперы, дублирующиеся
между сервисами (`ptrOrNil`, `parseOrigins` и им подобные). Один репо —
один модуль `github.com/zvonilkaRU/libs`, пакеты внутри по назначению.

Подключение — как у `api-schema`: пин версии в `go.mod` сервисов,
`GOPRIVATE=github.com/zvonilkaRU/*`.

## Пакеты

### `ptr`

Указатели при сборке DTO (домен со значениями ↔ OpenAPI с указателями):

```go
import "github.com/zvonilkaRU/libs/ptr"

new(42)          // *int — нативно с Go 1.26, хелпер не нужен
ValOrNil("x")    // *string — nil для zero-значения (бывший ptrOrNil)
Val(p)           // string — разыменование с zero-фолбэком для nil
```

### `strs`

Строки конфигурации:

```go
import "github.com/zvonilkaRU/libs/strs"

strs.SplitCSV(raw) // []string — пусто → nil (не задано), иначе split по запятой
```

### `repositorytest`

Тестовый харнесс репозиториев PostgreSQL: один на процесс контейнер
`postgres:16-alpine` (testcontainers), изолированная схема на тест
(`t_xxxxxxxx` через `search_path`), миграции goose и LIFO-cleanup
(`repo.Close` раньше `DROP SCHEMA CASCADE`). Сервисы зовут его через тонкую
обёртку своего `internal/repositorytest`; без Docker тест скипается локально
и фаталит при `CI=true`.

```go
import librepositorytest "github.com/zvonilkaRU/libs/repositorytest"

h := librepositorytest.Start(t, librepositorytest.Options{
    Service:  "users",                      // креды контейнера users_test
    ExtraDDL: []string{stubUsersDDL},       // заглушки таблиц после миграций
})
repo, err := repository.New(ctx, h.DSN())   // схема уже в search_path
err = h.Exec(ctx, `INSERT ... VALUES ($1)`, id) // сидинг в обход API репозитория
```

`MigrationsDir` пуст — walk-up от CWD до корня модуля + `migrations`
(поведение тестов сервисов); собственные тесты либы задают его явно на
`testdata/migrations` и лежат под тегом `itest`.

## Правила

- Новый пакет появляется, когда хелпер дублируется минимум в двух
  сервисах; до этого живёт в сервисе (YAGNI).
- Комментарии и godoc — на русском; тесты табличные с `t.Parallel`.
- Гейты: `go build ./... && go vet ./... && go test ./... -count=1`,
  `golangci-lint run`, `gofmt -l .` (пустой вывод).
