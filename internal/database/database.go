package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	_ "github.com/joho/godotenv/autoload"
	"github.com/pressly/goose/v3"
)

type Service interface {
	GetTask(ctx context.Context, taskId string) (Task, error)
	GetTasks(ctx context.Context, machineID string) ([]Task, error)
	FailTask(ctx context.Context, id uuid.UUID, lastError json.RawMessage, timestamp time.Time) (uuid.UUID, error)
	CompleteTask(ctx context.Context, id uuid.UUID, timestamp time.Time) (uuid.UUID, error)

	RegisterNode(ctx context.Context, n Node) (string, error)
	UpdateNodeStatus(ctx context.Context, machineID string, status NodeStatus) (string, error)
	GetNode(ctx context.Context, machineID string) (Node, error)
	GetNodes(ctx context.Context, page int, perPage int) ([]Node, error)
	UpdateNodeLastHBeat(ctx context.Context, machineID string) (string, error)

	Health() map[string]string
	Close()
}

type service struct {
	pool *pgxpool.Pool
}

var (
	dbURL      = os.Getenv("DB_URL")
	dbInstance *service
)

func New(ctx context.Context) Service {
	if dbInstance != nil {
		return dbInstance
	}

	config, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		log.Fatalf("Parse config failed: %v", err)
	}

	config.MaxConns = 10
	config.MinConns = 2

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		log.Fatalf("Create Pool: %v", err)
	}

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("Ping: %v", err)
	}

	dbInstance = &service{
		pool: pool,
	}

	if err := dbInstance.migrate(); err != nil {
		log.Fatal(err)
	}

	return dbInstance
}

func (s *service) migrate() error {
	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return err
	}

	return nil
}

// Health checks the health of the database connection by pinging the database.
// It returns a map with keys indicating various health statistics.
func (s *service) Health() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	db := stdlib.OpenDBFromPool(s.pool)
	defer db.Close()

	stats := make(map[string]string)

	// Ping the database
	err := db.PingContext(ctx)
	if err != nil {
		stats["status"] = "down"
		stats["error"] = fmt.Sprintf("db down: %v", err)
		log.Fatalf("db down: %v", err) // Log the error and terminate the program
		return stats
	}

	// Database is up, add more statistics
	stats["status"] = "up"
	stats["message"] = "It's healthy"

	// Get database stats (like open connections, in use, idle, etc.)
	dbStats := db.Stats()
	stats["open_connections"] = strconv.Itoa(dbStats.OpenConnections)
	stats["in_use"] = strconv.Itoa(dbStats.InUse)
	stats["idle"] = strconv.Itoa(dbStats.Idle)
	stats["wait_count"] = strconv.FormatInt(dbStats.WaitCount, 10)
	stats["wait_duration"] = dbStats.WaitDuration.String()
	stats["max_idle_closed"] = strconv.FormatInt(dbStats.MaxIdleClosed, 10)
	stats["max_lifetime_closed"] = strconv.FormatInt(dbStats.MaxLifetimeClosed, 10)

	// Evaluate stats to provide a health message
	if dbStats.OpenConnections > 40 { // Assuming 50 is the max for this example
		stats["message"] = "The database is experiencing heavy load."
	}

	if dbStats.WaitCount > 1000 {
		stats["message"] = "The database has a high number of wait events, indicating potential bottlenecks."
	}

	if dbStats.MaxIdleClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many idle connections are being closed, consider revising the connection pool settings."
	}

	if dbStats.MaxLifetimeClosed > int64(dbStats.OpenConnections)/2 {
		stats["message"] = "Many connections are being closed due to max lifetime, consider increasing max lifetime or revising the connection usage pattern."
	}

	return stats
}

// Close closes the database connection.
// It logs a message indicating the disconnection from the specific database.
// If the connection is successfully closed, it returns nil.
// If an error occurs while closing the connection, it returns the error.
func (s *service) Close() {
	log.Printf("Disconnected from database: %s", dbURL)
	s.pool.Close()
}
