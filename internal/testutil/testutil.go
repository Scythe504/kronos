package testutil

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
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	containerOnce sync.Once
	containerErr  error
	pgContainer   *postgres.PostgresContainer
	maintPool     *pgxpool.Pool
	baseConnURL   string
)

const templateDBName = "kronos_template"

// initContainer initializes the PostgreSQL environment (using TEST_DB_URL if provided, or Testcontainers).
// It creates the template database and executes migrations once.
func initContainer() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var connStr string

	if envURL := os.Getenv("TEST_DB_URL"); envURL != "" {
		connStr = envURL
	} else {
		container, err := postgres.Run(ctx,
			"postgres:17",
			postgres.WithDatabase("postgres"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("password"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(20*time.Second),
			),
		)
		if err != nil {
			return fmt.Errorf("failed to start postgres testcontainer: %w", err)
		}

		cStr, err := container.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			_ = testcontainers.TerminateContainer(container)
			return fmt.Errorf("failed to get connection string: %w", err)
		}

		pgContainer = container
		connStr = cStr
	}

	pool, err := pgxpool.New(ctx, connStr)
	if err != nil {
		if pgContainer != nil {
			_ = testcontainers.TerminateContainer(pgContainer)
		}
		return fmt.Errorf("failed to connect to maintenance pool: %w", err)
	}

	// Check if template database already exists (relevant when TEST_DB_URL is reused)
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateDBName).Scan(&exists)
	if err != nil {
		pool.Close()
		return fmt.Errorf("failed to check template database existence: %w", err)
	}

	if !exists {
		_, err = pool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName))
		if err != nil {
			pool.Close()
			return fmt.Errorf("failed to create %s database: %w", templateDBName, err)
		}

		// Run Goose migrations on kronos_template
		templateURL, err := replaceDBName(connStr, templateDBName)
		if err != nil {
			pool.Close()
			return err
		}

		if err := runMigrations(templateURL); err != nil {
			pool.Close()
			return fmt.Errorf("failed to run migrations on template database: %w", err)
		}
	}

	maintPool = pool
	baseConnURL = connStr
	return nil
}

// ensureContainer ensures the PostgreSQL container and template database are ready.
func ensureContainer(tb testing.TB) {
	tb.Helper()
	containerOnce.Do(func() {
		containerErr = initContainer()
	})
	if containerErr != nil {
		tb.Fatalf("testutil: postgres initialization failed: %v", containerErr)
	}
}

// GetTestDBURL returns a connection string to an ephemeral, isolated PostgreSQL database
// cloned instantly from kronos_template. The database is automatically dropped via tb.Cleanup.
// Accepts testing.TB to support both tests (*testing.T) and benchmarks (*testing.B).
func GetTestDBURL(tb testing.TB) string {
	tb.Helper()
	ensureContainer(tb)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uniqueID := strings.ReplaceAll(uuid.New().String(), "-", "")
	dbName := fmt.Sprintf("test_%s", uniqueID)

	_, err := maintPool.Exec(ctx, fmt.Sprintf("CREATE DATABASE %q TEMPLATE %s", dbName, templateDBName))
	if err != nil {
		tb.Fatalf("testutil: failed to create ephemeral database from template: %v", err)
	}

	tb.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()

		// Force drop database to terminate any hanging connections
		query := fmt.Sprintf("DROP DATABASE IF EXISTS %q WITH (FORCE)", dbName)
		if _, err := maintPool.Exec(cleanCtx, query); err != nil {
			tb.Logf("testutil: warning: failed to drop test database %q: %v", dbName, err)
		}
	})

	testDBURL, err := replaceDBName(baseConnURL, dbName)
	if err != nil {
		tb.Fatalf("testutil: failed to build connection URL for %s: %v", dbName, err)
	}

	return testDBURL
}

// GetBaseConnURL returns the base connection URL (useful for benchmark harnesses or external runners).
func GetBaseConnURL() string {
	return baseConnURL
}

// SeedBenchTasks seeds n queued tasks efficiently using COPY FROM for load testing and benchmarks.
func SeedBenchTasks(ctx context.Context, pool *pgxpool.Pool, slug string, count int) error {
	rows := make([][]any, count)
	for i := 0; i < count; i++ {
		rows[i] = []any{slug, []byte(fmt.Sprintf(`{"index":%d}`, i)), "queued", "cpu"}
	}
	_, err := pool.CopyFrom(
		ctx,
		pgx.Identifier{"tasks"},
		[]string{"payload_slug", "payload", "status", "allocated_unit"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// IsolateConfigDir isolates Kronos config, preventing test writes to ~/.kronos or /etc/kronos.
// Accepts testing.TB to support both tests (*testing.T) and benchmarks (*testing.B).
func IsolateConfigDir(tb testing.TB) string {
	tb.Helper()
	tempDir := tb.TempDir()
	tb.Setenv("KRONOS_CONFIG_DIR", tempDir)
	return tempDir
}

// Cleanup can be called explicitly in TestMain to terminate the container and maintenance pool.
func Cleanup() {
	if maintPool != nil {
		maintPool.Close()
	}
	if pgContainer != nil {
		_ = testcontainers.TerminateContainer(pgContainer)
	}
}

func replaceDBName(rawURL string, newDBName string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse database URL %q: %w", rawURL, err)
	}
	u.Path = "/" + newDBName
	return u.String(), nil
}

func runMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("failed to open sql connection for migration: %w", err)
	}
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("failed to set goose dialect: %w", err)
	}

	migrationsDir, err := findMigrationsDir()
	if err != nil {
		return err
	}

	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("goose migration failed: %w", err)
	}

	return nil
}

func findMigrationsDir() (string, error) {
	dir := "migrations"
	for range 6 {
		if fi, err := os.Stat(dir); err == nil && fi.IsDir() {
			return filepath.Abs(dir)
		}
		dir = filepath.Join("..", dir)
	}
	return "", fmt.Errorf("migrations directory not found in current or parent paths")
}
