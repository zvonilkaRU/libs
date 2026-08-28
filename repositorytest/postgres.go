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
	_, err = db.ExecContext(ctx, query, args...)
	if err != nil {
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
