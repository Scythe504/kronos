package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"

	_ "github.com/joho/godotenv/autoload"
)

type payloadSlug string

const seedCount = 100000

const (
	slugVidTranscoding payloadSlug = "video_transcode"
	slugCsvToPdf       payloadSlug = "csv_to_pdf"
)

var (
	resolutions = []string{"720p", "1080p", "1440p", "4k"}
	formats     = []string{"mp4", "webm", "mkv"}
	layouts     = []string{"portrait", "landscape"}
)

func getVideoPayload() json.RawMessage {
	// Pick random resolution and format
	res := resolutions[utils.RandInt(big.NewInt(int64(len(resolutions))))]
	fmtStr := formats[utils.RandInt(big.NewInt(int64(len(formats))))]
	fileID := utils.RandInt(big.NewInt(99999))

	videoPayload := map[string]any{
		"source_uri":    fmt.Sprintf("s3://kronos-incoming/video_%d.raw", fileID),
		"target_uri":    fmt.Sprintf("s3://kronos-processed/video_%d.%s", fileID, fmtStr),
		"resolution":    res,
		"format":        fmtStr,
		"extract_audio": utils.RandInt(big.NewInt(2)) == 1, // 50% chance of true
	}

	raw, _ := json.Marshal(videoPayload)
	return raw
}

func getCsvPayload() json.RawMessage {
	layout := layouts[utils.RandInt(big.NewInt(int64(len(layouts))))]
	fileID := utils.RandInt(big.NewInt(99999))

	csvPayload := map[string]any{
		"source_uri":  fmt.Sprintf("s3://kronos-incoming/report_%d.csv", fileID),
		"target_uri":  fmt.Sprintf("s3://kronos-processed/report_%d.pdf", fileID),
		"layout":      layout,
		"has_headers": true,
		"font_size":   10 + utils.RandInt(big.NewInt(4)), // Font size 10-13
	}

	raw, _ := json.Marshal(csvPayload)
	return raw
}

type service struct {
	pool *pgxpool.Pool
}

var (
	dbURL = os.Getenv("DB_URL")
	db    *service
)

func New(ctx context.Context) *service {
	if db != nil {
		return db
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

	db = &service{
		pool: pool,
	}

	if err := db.migrate(); err != nil {
		log.Fatal(err)
	}

	return db
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

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	New(ctx)

	// Ensure the two seed workers exist in the workers table
	workerInsertQuery := `
		INSERT INTO workers (slug, name, repo_url, repo_ref, entrypoint, task_unit, task_timeout_seconds)
		VALUES 
			('video_transcode', 'Video Transcoder', 'https://github.com/scythe504/kronos.git', 'main', 'main.go', 'gpu', 300),
			('csv_to_pdf', 'CSV to PDF Generator', 'https://github.com/scythe504/kronos.git', 'main', 'main.go', 'cpu', 120)
		ON CONFLICT (slug) DO NOTHING;
	`
	if _, err := db.pool.Exec(ctx, workerInsertQuery); err != nil {
		log.Printf("[WARN] Failed to insert seed workers: %v", err)
	}

	maxInt := big.NewInt(2)

	tasks := make([]database.Task, seedCount)

	for i := range len(tasks) {
		rInt := utils.RandInt(maxInt)

		switch int(rInt) {
		case 0:
			tasks[i].AllocatedUnit = database.TaskUnitGPU
			tasks[i].Payload = getVideoPayload()
			tasks[i].PayloadSlug = string(slugVidTranscoding)
		case 1:
			tasks[i].AllocatedUnit = database.TaskUnitCPU
			tasks[i].Payload = getCsvPayload()
			tasks[i].PayloadSlug = string(slugCsvToPdf)
		}
	}
	identifier := pgx.Identifier{"tasks"}
	columns := []string{"allocated_unit", "payload", "payload_slug"}
	rowSrc := pgx.CopyFromSlice(len(tasks), func(i int) ([]any, error) {
		return []any{
			tasks[i].AllocatedUnit,
			tasks[i].Payload,
			tasks[i].PayloadSlug,
		}, nil
	})

	opts := pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadWrite,
	}

	tx, err := db.pool.BeginTx(ctx, opts)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx)

	rowsAffected, err := tx.CopyFrom(ctx, identifier, columns, rowSrc)
	if err != nil || rowsAffected != int64(len(tasks)) {
		log.Fatal(err)
	}

	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}

	log.Println("Data Seeded:", rowsAffected)
}
