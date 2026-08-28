# repositorytest — общая библиотека тестового харнесса PostgreSQL

Дата: 2026-08-28
Статус: подход A утверждён пользователем; спека на ревью.

## Контекст

Три сервиса (users, rooms, servers) содержат по ~166 строк идентичного
тестового харнесса `internal/repositorytest`: контейнер PostgreSQL
(testcontainers, один на процесс через `sync.Once`), изолированная схема на
тест (`t_xxxxxxxx` + `search_path` + LIFO-cleanup с `DROP SCHEMA CASCADE`),
миграции goose (walk-up от CWD до корня модуля + `migrations`), CI-детект
(без Docker — skip, при `CI` — fatal). Харнессы появились копированием
эталона users (фаза 5 clean-arch серий rooms/servers), отличие — импорт
репозитория сервиса, креды `<svc>_test` и doc-комментарий; servers добавил
заглушку таблицы users + `SeedUser` + `ExecOnDSN` с аргументами.

Библиотека `github.com/zvonilkaRU/libs` уже существует (пакеты `ptr`, `strs`,
тег v0.1.0, есть `.golangci.yml`, CI нет) и потребляется сервисами пином
версии без `replace` — как api-schema.

## Проблема

Любое исправление/улучшение харнесса (например, поправка таймаутов или
стратегии ожидания контейнера) требует трёх синхронных правок; конвенция
«verbatim-копия эталона users» уже сломана ответвлением servers.

## Цели

- Одна реализация жизненного цикла контейнера/схем/миграций — в либе.
- Сервисы сохраняют текущий публичный API своих `internal/repositorytest`
  (`Start(t)`, у servers ещё `Harness{Repo}` + `SeedUser`) — тестовые файлы
  сервисов не меняются.
- Семантика 1:1: порядок cleanup, skip/fatal-детект, креды, изоляция схем,
  параллельность тестов — как в текущем эталоне.

## Не-цели

- Переход на `go.work`/`replace` — пин версий остаётся единственным способом
  потребления (конвенция монорепо).
- Изменение тестов сервисов (ассертов, покрытия) — только источник харнесса.
- Поддержка СУБД кроме PostgreSQL.

## Решение (подход A: либа + тонкие обёртки)

### Пакет `libs/repositorytest`

Плоский пакет — как `ptr`/`strs`. API:

```go
// Options — параметры харнесса.
type Options struct {
    // Service — имя сервиса ("users", "rooms", "servers"): определяет креды
    // контейнера (<service>_test для БД, пользователя и пароля).
    Service string

    // MigrationsDir — каталог миграций goose. Пустое значение — walk-up от
    // CWD до каталога с go.mod + "migrations" (поведение эталона; работает,
    // потому что тесты сервиса выполняются из CWD внутри модуля сервиса).
    MigrationsDir string

    // ExtraDDL выполняется в схеме теста после миграций: заглушки таблиц
    // соседних сервисов (например, users для JOIN в servers).
    ExtraDDL []string
}

// Harness — изолированная тестовая схема поверх общего контейнера.
type Harness struct { /* dsn, schema — приватные */ }

// Start поднимает (один на процесс) контейнер postgres:16-alpine, создаёт
// изолированную схему, применяет миграции и ExtraDDL. Без Docker тест
// скипается локально и фаталит при CI. Схема удаляется в t.Cleanup.
func Start(t *testing.T, opts Options) *Harness

// DSN возвращает DSN с search_path изолированной схемы.
func (h *Harness) DSN() string

// Exec выполняет одиночный SQL-запрос с аргументами в схеме теста
// (сидинг данных в обход API репозитория).
func (h *Harness) Exec(ctx context.Context, query string, args ...any) error
```

Внутренности (`startShared`, `migrateSchema`, `schemaDSN`, walk-up)
переносятся из эталона users дословно, с заменой кредов на производные от
`opts.Service` и добавлением цикла `ExtraDDL` после `goose.UpContext`.
Пустой `opts.Service` — `t.Fatal` при старте (ошибка конфигурации теста).

### Обёртки в сервисах

Пакет `internal/repositorytest` в каждом сервисе сохраняет имя и API,
источник сжимается до тонкой обёртки:

- **users, rooms**: `Start(t) *repository.PostgresRepo` — зовёт
  `librepositorytest.Start(t, Options{Service: "<svc>"})`, строит репозиторий
  через `repository.New(ctx, h.DSN())`, регистрирует `repo.Close` в cleanup.
- **servers**: обёртка возвращает свой `Harness{Repo *repository.PostgresRepo}`,
  в `Options.ExtraDDL` передаёт `CREATE TABLE users (...)`, `SeedUser`
  реализован через `h.Exec`.

Импорт либы в обёртке — с алиасом (`librepositorytest`), имена пакетов
совпадают. Тестовые файлы сервисов (включая smoke-тесты
`internal/repositorytest/postgres_test.go`) не меняются — они продолжают
звать свой `repositorytest.Start(t)`.

### Зависимости и версионирование

`libs/go.mod` получает direct-зависимости: testcontainers-go (+modules/postgres),
goose/v3, pgx/v5 (stdlib-драйвер), uuid, testify. В сервисах эти модули
уходят в indirect, `libs` поднимается до **v0.2.0**; каждый сервис бампает
пин отдельным PR. Локальная разработка либы — на её собственных тестах
(см. ниже), без replace в сервисах.

## CI и линт libs

В libs нет CI — в тот же PR добавляется минимальный `.github/workflows/ci.yml`
по образцу сервисов: `lint` (golangci), `test` (`go test ./...`),
`itest` (`CI=true go test -tags itest ./... -count=1`). В `.golangci.yml`
добавляется `run.build-tags: [itest]` — харнесс-тесты либы лежат под тегом
`itest`, как в сервисах.

Собственные тесты либы (`repositorytest/postgres_test.go`, под тегом
`itest`) используют фикстуру `repositorytest/testdata/migrations/` с явным
`Options.MigrationsDir` (walk-up в либе нашёл бы корень libs, где `migrations`
нет): smoke — старт, применение миграции, ExtraDDL, `DSN()`, `Exec`.

## Порядок работ

1. Мерж rooms #38 (третий экземпляр харнесса входит в main) — пользователем.
2. PR в libs: пакет + тесты + CI + lint-конфиг. После мержа — тег **v0.2.0**.
3. Три маленьких PR (users, servers, rooms): обёртка вместо харнесса, бамп
   пина `libs v0.2.0`, `go mod tidy`. Гейты обоих режимов + lint обязательны.

## Осознанные решения

1. **Тонкие обёртки вместо дженерик-конструктора** (`Start[T](t, opts, newRepo)`):
   явный код сервиса читается лучше колбэков с generics, либа не знает о
   типах сервисов.
2. **Креды производные от `Service`**, а не три отдельных поля: устойчивая
   конвенция `<svc>_test` во всех трёх сервисах.
3. **`MigrationsDir` как override, walk-up по умолчанию**: поведение эталона
   сохраняется для сервисов, либа тестирует себя на фикстуре.
4. **`ExtraDDL` после миграций**: заглушка users в servers создаётся именно
   после `goose.UpContext`, как сегодня.
5. **Пакет плоский** (`libs/repositorytest`), не `libs/testing/...`: как
   `ptr`/`strs`, короче импорт.
6. **Тег `itest` в либе** — та же конвенция разделения, что во всех сервисах.

## Риски

- **Версионный лаг**: правка харнесса теперь требует тег либы + бамп в трёх
  сервисах. Цена осознанная — она и мотивация дедупликации; изменения
  ожидаются редкими.
- **CWD-зависимость walk-up**: если тест сервиса запущен из другого CWD
  (`go test -C`/IDE), миграции не найдутся. Свойство уже существует во всех
  трёх харнессах — не регрессия; `MigrationsDir` даёт явный обходной путь.
- **Косвенные зависимости сервисов**: testcontainers/goose станут indirect —
  `go mod tidy` аккуратен, диффы go.mod проверяются в PR.

## Проверка

- Либа: `go build ./...`, `go vet ./...`, unit (`go test ./...`),
  `CI=true go test -tags itest ./... -count=1` — smoke харнесса на реальном
  контейнере; `golangci-lint run` (с build-tags), `gofmt -l .` пусто.
- Сервисы после миграции: те же гейты, плюс diff-сверка поведения —
  интеграционные прогоны сервисов должны остаться зелёными без правок
  тестовых файлов.
