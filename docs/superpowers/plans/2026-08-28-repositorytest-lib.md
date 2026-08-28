# libs repositorytest — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Вынести дублируемый тестовый харнесс PostgreSQL (контейнер + изолированные схемы + миграции goose) из users/rooms/servers в общую библиотеку `github.com/zvonilkaRU/libs`.

**Architecture:** Либа даёт универсальный `repositorytest.Start(t, Options) *Harness` с `DSN()`/`Exec()`; каждый сервис сохраняет свой `internal/repositorytest` как тонкую обёртку с прежним публичным API (`Start(t)`; у servers — `Harness{Repo}` + `SeedUser`), поэтому тестовые файлы сервисов не меняются. Внутренности переносятся из эталона users дословно.

**Tech Stack:** Go 1.26, testcontainers-go v0.44.0 (+modules/postgres), goose v3.27.3, pgx v5.10.0, testify.

**Spec:** `docs/superpowers/specs/2026-08-28-repositorytest-lib-design.md` (ветка `feat/repositorytest-lib`, уже в git).

## Global Constraints

- Комментарии в новом коде — на русском; doc-комментарии с имени символа.
- Conventional commits (англ.), БЕЗ Co-Authored-By и любых трейлеров.
- Версии зависимостей либы — равны сервисным пинам: `github.com/testcontainers/testcontainers-go/modules/postgres v0.44.0`, `github.com/pressly/goose/v3 v3.27.3`, `github.com/jackc/pgx/v5 v5.10.0`, `github.com/google/uuid v1.6.0`, `github.com/stretchr/testify v1.11.1`.
- Потребление либы сервисами — пином версии, БЕЗ `replace` и `go.work` (конвенция монорепо).
- Харнесс-тесты либы — под билд-тегом `itest`; `.golangci.yml` либы получает `run.build-tags: [itest]`.
- Файлы на /mnt/c: git status показывает CRLF-шум (все файлы «M») — коммитить только файлы задачи; изменения в нетронутых файлах — шум, откатывать только их.
- Окружение (проверено): Go `~/go-toolchain/bin/go` (полный путь), линтер `/mnt/c/Users/shyge/go/bin/golangci-lint.exe` (из корня модуля), Docker 29.1.3 есть; `make` в WSL-шелле нет.
- Гейты (все обязательны, коммит — только зелёные):
  ```bash
  ~/go-toolchain/bin/go build ./... && ~/go-toolchain/bin/go vet ./...
  ~/go-toolchain/bin/go test ./... -count=1            # unit
  CI=true ~/go-toolchain/bin/go test -tags itest ./... -count=1   # интеграционные, 0 SKIP
  /mnt/c/Users/shyge/go/bin/golangci-lint.exe run
  ~/go-toolchain/bin/gofmt -l .                        # пустой вывод
  ```

## File Structure

**Фаза A — libs (worktree `.worktrees/libs-repositorytest`, ветка `feat/repositorytest-lib`, уже содержит спеку):**
- Create: `repositorytest/postgres.go` — универсальный харнесс
- Create: `repositorytest/postgres_test.go` — smoke-тест либы (тег `itest`)
- Create: `repositorytest/testdata/migrations/001_things.sql` — фикстура миграций
- Create: `.github/workflows/ci.yml` — CI либы (go-ci + test + itest)
- Modify: `.golangci.yml` — `run.build-tags: [itest]`
- Modify: `go.mod`/`go.sum` — зависимости

**Фаза B — сервисы (после тега v0.2.0; rooms — после мержа #38). Каждый таск — свой worktree от свежего `origin/main` соответствующего репо и свой PR:**
- Rewrite: `<svc>/internal/repositorytest/postgres.go` — обёртка (полная замена файла)
- Modify: `<svc>/go.mod`/`go.sum` — пин `github.com/zvonilkaRU/libs v0.2.0`

**Между фазами (шаг контроллера, не задача):** после мержа PR фазы A — поставить тег `v0.2.0` на main либы и запушить тег.

---

### Task 1: Пакет repositorytest в libs

**Files:**
- Create: `repositorytest/postgres.go`
- Create: `repositorytest/postgres_test.go`
- Create: `repositorytest/testdata/migrations/001_things.sql`
- Modify: `.golangci.yml`
- Modify: `go.mod`, `go.sum`

**Interfaces:**
- Consumes: ничего (первая задача).
- Produces: `repositorytest.Start(t *testing.T, opts Options) *Harness`; `Options{Service string; MigrationsDir string; ExtraDDL []string}`; `(*Harness).DSN() string`; `(*Harness).Exec(ctx context.Context, query string, args ...any) error` — на эти имена опираются обёртки фазы B.

Working directory: `/mnt/c/Users/shyge/GolandProjects/zvonilka/.worktrees/libs-repositorytest`.

- [ ] **Step 1: Добавить зависимости**

```bash
cd /mnt/c/Users/shyge/GolandProjects/zvonilka/.worktrees/libs-repositorytest
~/go-toolchain/bin/go get github.com/testcontainers/testcontainers-go/modules/postgres@v0.44.0 github.com/pressly/goose/v3@v3.27.3 github.com/jackc/pgx/v5@v5.10.0 github.com/google/uuid@v1.6.0 github.com/stretchr/testify@v1.11.1
~/go-toolchain/bin/go mod tidy
```

- [ ] **Step 2: Написать харнесс** `repositorytest/postgres.go`

```go
// Package repositorytest поднимает PostgreSQL в Docker-контейнере и применяет
// миграции goose: общая библиотека тестового харнесса для интеграционных
// тестов сервисов zvonilkaRU. Сервис оборачивает Start тонкой обёрткой,
// подставляющей собственный репозиторий.
package repositorytest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	// Драйвер pgx для database/sql: используется в sql.Open и goose.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const (
	containerStartTimeout = 2 * time.Minute
	connectTimeout        = 10 * time.Second
	migrationsDirName     = "migrations"
)

var (
	containerOnce sync.Once
	shared        *postgres.PostgresContainer
	errShared     error
)

// Options — параметры харнесса.
type Options struct {
	// Service — имя сервиса ("users", "rooms", "servers"): определяет креды
	// контейнера (<service>_test для БД, пользователя и пароля). Контейнер
	// один на процесс — креды берутся из первого вызова Start.
	Service string

	// MigrationsDir — каталог миграций goose. Пустое значение — walk-up от
	// CWD до каталога с go.mod + "migrations" (тесты сервиса выполняются из
	// CWD внутри его модуля).
	MigrationsDir string

	// ExtraDDL выполняется в схеме теста после миграций: заглушки таблиц
	// соседних сервисов (например, users для JOIN в servers).
	ExtraDDL []string
}

// Harness — изолированная тестовая схема поверх общего контейнера.
type Harness struct {
	dsn    string // базовый DSN контейнера (без search_path).
	schema string // изолированная схема теста.
}

// Start поднимает (один на тестовый бинарник) контейнер postgres:16-alpine,
// создаёт изолированную схему, применяет к ней миграции goose и ExtraDDL.
// Схема удаляется в t.Cleanup; тесты можно запускать параллельно. В окружении
// без Docker тест пропускается; в CI (переменная CI) недоступность контейнера
// — фатальная ошибка.
func Start(t *testing.T, opts Options) *Harness {
	t.Helper()

	if opts.Service == "" {
		t.Fatal("repositorytest: Options.Service пуст")
	}

	dsn := startShared(t, opts)
	schema := "t_" + uuid.NewString()[:8]

	migrateSchema(t, dsn, schema, opts)

	return &Harness{dsn: dsn, schema: schema}
}

// DSN возвращает DSN с search_path изолированной схемы — для конструктора
// репозитория сервиса.
func (h *Harness) DSN() string {
	return schemaDSN(h.dsn, h.schema)
}

// Exec выполняет одиночный SQL-запрос с аргументами в схеме теста: сидинг
// данных в обход API репозитория.
func (h *Harness) Exec(ctx context.Context, query string, args ...any) error {
	db, err := sql.Open("pgx", h.DSN())
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// startShared стартует общий контейнер PostgreSQL (один на процесс) и
// возвращает базовый DSN.
func startShared(t *testing.T, opts Options) string {
	t.Helper()

	containerOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), containerStartTimeout)
		defer cancel()
		creds := opts.Service + "_test"
		shared, errShared = postgres.Run(ctx, "postgres:16-alpine",
			postgres.WithDatabase(creds),
			postgres.WithUsername(creds),
			postgres.WithPassword(creds),
			// Модуль не ждёт готовности PG по умолчанию: без этого goose ловит
			// connection reset в окне перезапуска после initdb.
			postgres.BasicWaitStrategies(),
		)
	})
	if errShared != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("postgres container: %v", errShared)
		}

		t.Skipf("postgres container (Docker не запущен?): %v", errShared)
	}

	dsn, err := shared.ConnectionString(context.Background(), "sslmode=disable")
	require.NoError(t, err)

	return dsn
}

// migrateSchema создаёт изолированную схему, применяет к ней миграции goose
// и ExtraDDL.
func migrateSchema(t *testing.T, dsn, schema string, opts Options) {
	t.Helper()

	createCtx, createCancel := context.WithTimeout(context.Background(), connectTimeout)
	defer createCancel()
	err := execOnDSN(createCtx, dsn, fmt.Sprintf(`CREATE SCHEMA %q`, schema))
	require.NoError(t, err)

	// Drop регистрируется сразу после создания схемы — детерминированная
	// очистка даже при падении миграций; выполнится после закрытия пула
	// репозитория сервиса (Cleanup — LIFO).
	t.Cleanup(func() {
		// Best-effort очистка тестовой схемы.
		dropCtx, dropCancel := context.WithTimeout(context.Background(), connectTimeout)
		defer dropCancel()
		_ = execOnDSN(dropCtx, dsn, fmt.Sprintf(`DROP SCHEMA IF EXISTS %q CASCADE`, schema)) //nolint:errcheck // best-effort очистка тестовой схемы.
	})

	migCtx, migCancel := context.WithTimeout(context.Background(), connectTimeout)
	defer migCancel()
	migDB, err := sql.Open("pgx", schemaDSN(dsn, schema))
	require.NoError(t, err)
	defer migDB.Close()
	err = goose.UpContext(migCtx, migDB, migrationsDir(t, opts))
	require.NoError(t, err)

	for _, stmt := range opts.ExtraDDL {
		extraCtx, extraCancel := context.WithTimeout(context.Background(), connectTimeout)
		err := execOnDSN(extraCtx, schemaDSN(dsn, schema), stmt)
		extraCancel()
		require.NoError(t, err)
	}
}

// execOnDSN выполняет одиночный SQL-запрос по DSN.
func execOnDSN(ctx context.Context, dsn, query string, args ...any) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// schemaDSN добавляет к DSN параметр search_path, чтобы все соединения пула
// работали в изолированной схеме.
func schemaDSN(dsn, schema string) string {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}

	return dsn + sep + "search_path=" + url.QueryEscape(schema)
}

// migrationsDir ищет каталог migrations от корня модуля (CWD теста лежит в
// internal/... на разной глубине); явный Options.MigrationsDir имеет
// приоритет.
func migrationsDir(t *testing.T, opts Options) string {
	t.Helper()

	if opts.MigrationsDir != "" {
		return opts.MigrationsDir
	}

	dir, err := os.Getwd()
	require.NoError(t, err)
	for {
		_, statErr := os.Stat(filepath.Join(dir, "go.mod"))
		if statErr == nil {
			return filepath.Join(dir, migrationsDirName)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("корень модуля (go.mod) не найден при поиске migrations")
		}
		dir = parent
	}
}
```

- [ ] **Step 3: Написать фикстру миграций** `repositorytest/testdata/migrations/001_things.sql`

```sql
-- +goose Up
CREATE TABLE things (id UUID PRIMARY KEY, name TEXT NOT NULL);

-- +goose Down
DROP TABLE things;
```

- [ ] **Step 4: Написать smoke-тест** `repositorytest/postgres_test.go`

```go
//go:build itest

package repositorytest_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/zvonilkaRU/libs/repositorytest"
)

// TestHarness_Smoke: старт харнесса на фикстурных миграциях, применение
// ExtraDDL, DSN и Exec в изолированной схеме.
func TestHarness_Smoke(t *testing.T) {
	t.Parallel()

	h := repositorytest.Start(t, repositorytest.Options{
		Service:       "libtest",
		MigrationsDir: "testdata/migrations",
		ExtraDDL:      []string{`CREATE TABLE extras (id UUID PRIMARY KEY)`},
	})

	ctx := context.Background()
	require.NoError(t, h.Exec(ctx, `INSERT INTO things (id, name) VALUES ($1, $2)`, uuid.New(), "first"))
	require.NoError(t, h.Exec(ctx, `INSERT INTO extras (id) VALUES ($1)`, uuid.New()))

	db, err := sql.Open("pgx", h.DSN())
	require.NoError(t, err)
	defer db.Close()

	var things, extras int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM things`).Scan(&things))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM extras`).Scan(&extras))
	assert.Equal(t, 1, things)
	assert.Equal(t, 1, extras)
}
```

- [ ] **Step 5: Поправить линт-конфиг** — в `.golangci.yml` секцию `run:` привести к виду:

```yaml
run:
  timeout: 5m
  build-tags: [itest]
  modules-download-mode: readonly
```

(остальной файл не трогать; если `modules-download-mode` отсутствует — не добавлять, только `build-tags`).

- [ ] **Step 6: Прогнать smoke-тест** (первый запуск тянет postgres:16-alpine — долго, один раз)

```bash
CI=true ~/go-toolchain/bin/go test -tags itest ./repositorytest/ -count=1 -v
```
Expected: PASS, не SKIP.

- [ ] **Step 7: Полные гейты** (см. Global Constraints) — все зелёные; unit-прогон показывает `repositorytest [no test files]`.

- [ ] **Step 8: Commit**

```bash
git add repositorytest/ .golangci.yml go.mod go.sum
git commit -m "feat: shared repositorytest harness (testcontainers + goose + isolated schemas)"
```

### Task 2: CI либы

**Files:**
- Create: `.github/workflows/ci.yml`

**Interfaces:**
- Consumes: пакет из Task 1 (джобы его прогоняют).
- Produces: ничего для соседних задач.

- [ ] **Step 1: Написать** `.github/workflows/ci.yml`

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  go-ci:
    uses: zvonilkaRU/ci/.github/workflows/go-service.yml@main
    with:
      service: libs
    secrets:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}

  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Unit tests
        run: go test ./... -count=1
        env:
          CI: "true"

  itest:
    name: ITest
    runs-on: ubuntu-latest
    steps:
      - name: Checkout
        uses: actions/checkout@v4
      - name: Setup Go
        uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Integration tests
        run: go test -tags itest ./... -count=1
        env:
          CI: "true"
```

(libs не имеет приватных зависимостей — шаги git-config/GOPRIVATE не нужны.)

- [ ] **Step 2: Проверить YAML-синтаксис**

```bash
python3 -c "import yaml; yaml.safe_load(open('.github/workflows/ci.yml'))"
```
Expected: без вывода и ошибок.

- [ ] **Step 3: Полные гейты** (файл не влияет на Go — быстрая перепроверка).

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/ci.yml
git commit -m "ci: lint, unit and itest jobs for libs"
```

---

**Между фазами (контроллер):** мерж PR фазы A → тег `v0.2.0` на main либы (`git tag v0.2.0 && git push origin v0.2.0`). Фаза B стартует только после этого; задача rooms — после мержа rooms#38.

---

### Task 3: users — обёртка над либой

**Files:**
- Rewrite: `users/internal/repositorytest/postgres.go` (полная замена файла)
- Modify: `users/go.mod`, `users/go.sum`
- НЕ менять: `users/internal/repositorytest/postgres_test.go` и все прочие тестовые файлы.

**Interfaces:**
- Consumes: `github.com/zvonilkaRU/libs v0.2.0`: `repositorytest.Start(t, Options{Service: "users"})`, `(*Harness).DSN()`.
- Produces: прежний API `repositorytest.Start(t) *repository.PostgresRepo` — тестовые файлы users не меняются.

Worktree: `git -C /mnt/c/Users/shyge/GolandProjects/zvonilka/users fetch origin --prune && git -C ... worktree add .worktrees/users-repotest -b refactor/users-repositorytest-lib origin/main`; работаем в `/mnt/c/Users/shyge/GolandProjects/zvonilka/.worktrees/users-repotest`.

- [ ] **Step 1: Бампнуть зависимость**

```bash
GOPRIVATE=github.com/zvonilkaRU ~/go-toolchain/bin/go get github.com/zvonilkaRU/libs@v0.2.0
~/go-toolchain/bin/go mod tidy
```
(если fetch приватного модуля падает — проверить git-редирект токена: `git config --global url."https://x-access-token:<токен из .tkns.txt>@github.com/".insteadOf "https://github.com/"`; токен НЕ коммитить никуда).

- [ ] **Step 2: Заменить** `internal/repositorytest/postgres.go` целиком на:

```go
// Package repositorytest — тонкая обёртка над общей библиотекой
// github.com/zvonilkaRU/libs/repositorytest: подставляет репозиторий users
// в универсальный харнесс (контейнер, изолированные схемы, миграции goose).
package repositorytest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	librepositorytest "github.com/zvonilkaRU/libs/repositorytest"

	"github.com/zvonilkaRU/users/internal/adapter/repository"
)

const connectTimeout = 10 * time.Second

// Start возвращает *repository.PostgresRepo, подключённый к PostgreSQL-
// контейнеру с применёнными миграциями users. Каждый вызов создаёт
// изолированную схему через search_path — тесты можно запускать параллельно;
// схема удаляется в t.Cleanup, пул закрывается в t.Cleanup. Контейнер один на
// тестовый бинарник. В окружении без Docker тест пропускается; в CI
// (переменная CI) недоступность контейнера — фатальная ошибка.
func Start(t *testing.T) *repository.PostgresRepo {
	t.Helper()

	h := librepositorytest.Start(t, librepositorytest.Options{Service: "users"})

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	repo, err := repository.New(ctx, h.DSN())
	require.NoError(t, err)
	t.Cleanup(repo.Close)

	return repo
}
```

- [ ] **Step 3: Полные гейты** — все зелёные; интеграционные прогоны сервисных тестов (репо, usecase-integration) проходят на харнессе либы, 0 SKIP; unit-режон без Docker не меняется.

- [ ] **Step 4: Commit + push + PR** (base main, заголовок `refactor: repositorytest moves to shared libs harness`)

```bash
git add internal/repositorytest/postgres.go go.mod go.sum
git commit -m "refactor: repositorytest moves to shared libs harness"
git push -u origin refactor/users-repositorytest-lib
```

Тело PR — краткое: что (обёртка над `libs/repositorytest v0.2.0` вместо локальной копии харнесса), почему (дедупликация трёх копий), проверка (гейты обоих режимов, 0 SKIP, тестовые файлы не менялись — дифф только postgres.go/go.mod/go.sum).

### Task 4: servers — обёртка с заглушкой users

**Files:**
- Rewrite: `servers/internal/repositorytest/postgres.go` (полная замена файла)
- Modify: `servers/go.mod`, `servers/go.sum`
- НЕ менять: тестовые файлы (включая `server_test.go`/`member_test.go`/`channel_test.go` и smoke).

**Interfaces:**
- Consumes: `libs v0.2.0`: `Start(t, Options{Service: "servers", ExtraDDL: [...]})`, `(*Harness).DSN()`, `(*Harness).Exec(ctx, query, args...)`.
- Produces: прежний API `Start(t) *Harness` с полем `Repo *repository.PostgresRepo` и методом `SeedUser(t, id, nickname, tag)` — тестовые файлы servers не меняются.

Worktree: `git -C /mnt/c/Users/shyge/GolandProjects/zvonilka/servers fetch origin --prune && git worktree add /mnt/c/Users/shyge/GolandProjects/zvonilka/.worktrees/servers-repotest -b refactor/servers-repositorytest-lib origin/main`.

- [ ] **Step 1: Бампнуть зависимость** — как в Task 3 Step 1.

- [ ] **Step 2: Заменить** `internal/repositorytest/postgres.go` целиком на:

```go
// Package repositorytest — обёртка над общей библиотекой
// github.com/zvonilkaRU/libs/repositorytest: репозиторий servers плюс
// заглушка таблицы users — GetMember/ListMembers джойнят users через
// search_path, и тестовая схема обязана предоставить это отношение.
package repositorytest

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	librepositorytest "github.com/zvonilkaRU/libs/repositorytest"

	"github.com/zvonilkaRU/servers/internal/adapter/repository"
)

const connectTimeout = 10 * time.Second

// stubUsersDDL — заглушка таблицы users в схеме теста.
const stubUsersDDL = `CREATE TABLE users (
	id       UUID PRIMARY KEY,
	nickname VARCHAR(32) NOT NULL,
	tag      CHAR(7) NOT NULL
)`

// Harness — тестовое окружение одного теста: репозиторий в изолированной
// схеме и сидинг данных, недоступных через API репозитория (профили users).
type Harness struct {
	Repo *repository.PostgresRepo
	h    *librepositorytest.Harness
}

// Start возвращает Harness с репозиторием, подключённым к PostgreSQL-
// контейнеру с применёнными миграций servers и заглушкой users. Семантика
// изоляции и очистки — общей библиотеки (схема на тест, LIFO-cleanup).
func Start(t *testing.T) *Harness {
	t.Helper()

	h := librepositorytest.Start(t, librepositorytest.Options{
		Service:  "servers",
		ExtraDDL: []string{stubUsersDDL},
	})

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	repo, err := repository.New(ctx, h.DSN())
	require.NoError(t, err)
	t.Cleanup(repo.Close)

	return &Harness{Repo: repo, h: h}
}

// SeedUser вставляет профиль пользователя в заглушку users (для JOIN в
// GetMember/ListMembers). Дубликат id — фатальная ошибка теста.
func (hs *Harness) SeedUser(t *testing.T, id uuid.UUID, nickname, tag string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	require.NoError(t, hs.h.Exec(ctx, `INSERT INTO users (id, nickname, tag) VALUES ($1, $2, $3)`, id, nickname, tag))
}
```

- [ ] **Step 3: Полные гейты** — как в Task 3 Step 3 (репо-тесты servers гоняются на либе, 0 SKIP).

- [ ] **Step 4: Commit + push + PR** — как в Task 3 (ветка `refactor/servers-repositorytest-lib`).

### Task 5: rooms — обёртка над либой (после мержа rooms#38)

**Files:**
- Rewrite: `rooms/internal/repositorytest/postgres.go` (полная замена файла)
- Modify: `rooms/go.mod`, `rooms/go.sum`
- НЕ менять: тестовые файлы (smoke, `adapter/repository/postgres_test.go`, `adapter/http/server_test.go`).

**Interfaces:**
- Consumes: `libs v0.2.0`: `Start(t, Options{Service: "rooms"})`, `(*Harness).DSN()`.
- Produces: прежний API `Start(t) *repository.PostgresRepo`.

Worktree: `git -C /mnt/c/Users/shyge/GolandProjects/zvonilka/rooms fetch origin --prune && git worktree add /mnt/c/Users/shyge/GolandProjects/zvonilka/.worktrees/rooms-repotest -b refactor/rooms-repositorytest-lib origin/main` (обязательно ПОСЛЕ мержа #38 — в main должен лежать харнесс из PR5).

- [ ] **Step 1: Бампнуть зависимость** — как в Task 3 Step 1.

- [ ] **Step 2: Заменить** `internal/repositorytest/postgres.go` целиком на (идентичен Task 3, кроме имени сервиса и doc-строки):

```go
// Package repositorytest — тонкая обёртка над общей библиотекой
// github.com/zvonilkaRU/libs/repositorytest: подставляет репозиторий rooms
// в универсальный харнесс (контейнер, изолированные схемы, миграции goose).
package repositorytest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	librepositorytest "github.com/zvonilkaRU/libs/repositorytest"

	"github.com/zvonilkaRU/rooms/internal/adapter/repository"
)

const connectTimeout = 10 * time.Second

// Start возвращает *repository.PostgresRepo, подключённый к PostgreSQL-
// контейнеру с применёнными миграциями rooms. Каждый вызов создаёт
// изолированную схему через search_path — тесты можно запускать параллельно;
// схема удаляется в t.Cleanup, пул закрывается в t.Cleanup. Контейнер один на
// тестовый бинарник. В окружении без Docker тест пропускается; в CI
// (переменная CI) недоступность контейнера — фатальная ошибка.
func Start(t *testing.T) *repository.PostgresRepo {
	t.Helper()

	h := librepositorytest.Start(t, librepositorytest.Options{Service: "rooms"})

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	repo, err := repository.New(ctx, h.DSN())
	require.NoError(t, err)
	t.Cleanup(repo.Close)

	return repo
}
```

- [ ] **Step 3: Полные гейты** — как в Task 3 Step 3.

- [ ] **Step 4: Commit + push + PR** — как в Task 3 (ветка `refactor/rooms-repositorytest-lib`).

## Осознанные решения плана

- Обёртки сохраняют пакет `internal/repositorytest` и его API — дифф сервисов ограничен одним файлом + go.mod; smoke-тесты сервисов остаются и гоняют либу+обёртку.
- Креды контейнера производятся от `Options.Service` (конвенция `<svc>_test`); контейнер один на процесс — креды первого вызова.
- ExtraDDL выполняется после `goose.UpContext` — порядок создания заглушки users как в текущем servers.
- CI либы переиспользует `zvonilkaRU/ci/go-service.yml` с `service: libs` (workflow чекаутит `zvonilkaRU/<service>` — имя репо совпадает); без git-config/GOPRIVATE (зависимости либы публичные).

## Self-Review

- Спека покрыта: API (Task 1), семантика 1:1 (Task 1, перенос дословно), обёртки (Tasks 3–5), CI+lint (Tasks 1 Step 5, 2), версионирование v0.2.0 (межфазный шаг), порядок работ (гейты фаз).
- Placeholders: отсутствуют, все шаги содержат полный код/команды.
- Типы согласованы: `Options{Service, MigrationsDir, ExtraDDL}`, `Start(t, Options) *Harness`, `DSN()`, `Exec(ctx, query, args ...any) error` — одинаковы в Task 1 и обёртках Tasks 3–5.
