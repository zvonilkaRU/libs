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
