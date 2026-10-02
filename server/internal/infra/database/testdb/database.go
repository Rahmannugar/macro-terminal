//go:build integration

package testdb

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Rahmannugar/macro-terminal/server/internal/infra/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/tern/v2/migrate"
	"github.com/testcontainers/testcontainers-go"
	containerpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// OpenMigratedDatabase starts a PostgreSQL container, applies every
// migration, and returns a ready pool torn down with the test.
func OpenMigratedDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx := t.Context()
	container, err := containerpostgres.Run(
		ctx,
		"postgres:17-alpine",
		containerpostgres.WithDatabase("macro_terminal_test"),
		containerpostgres.WithUsername("macro_terminal"),
		containerpostgres.WithPassword("macro_terminal"),
		// Docker Desktop can take longer than the library's one-minute
		// default while PostgreSQL initializes and restarts for the first
		// time.
		testcontainers.WithWaitStrategyAndDeadline(
			2*time.Minute,
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			wait.ForListeningPort("5432/tcp"),
		),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL container: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL container: %v", err)
		}
	})

	connectionString, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}

	pool, err := database.Open(ctx, connectionString, 20)
	if err != nil {
		t.Fatalf("open PostgreSQL pool: %v", err)
	}
	t.Cleanup(pool.Close)

	connection, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire migration connection: %v", err)
	}
	defer connection.Release()

	migrator, err := migrate.NewMigrator(ctx, connection.Conn(), "public.schema_version")
	if err != nil {
		t.Fatalf("create migrator: %v", err)
	}
	if err := migrator.LoadMigrations(os.DirFS(migrationsPath(t))); err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := migrator.Migrate(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	return pool
}

func migrationsPath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve database test path")
	}
	return filepath.Join(filepath.Dir(filename), "..", "..", "..", "..", "..", "db", "migrations")
}
