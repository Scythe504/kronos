package main

import (
	"context"
	"crypto/rand"
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
	"github.com/scythe504/fluxd/internal/database"

	_ "github.com/joho/godotenv/autoload"
)

type payloadSlug string

func randInt(max *big.Int) *big.Int {
	r, err := rand.Int(rand.Reader, max)
	if err != nil {
		panic(err)
	}

	return r
}

const seedCount = 1000000

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
	res := resolutions[randInt(big.NewInt(int64(len(resolutions)))).Int64()]
	fmtStr := formats[randInt(big.NewInt(int64(len(formats)))).Int64()]
	fileID := randInt(big.NewInt(99999)).Int64()

	videoPayload := map[string]any{
		"source_uri":    fmt.Sprintf("s3://fluxd-incoming/video_%d.raw", fileID),
		"target_uri":    fmt.Sprintf("s3://fluxd-processed/video_%d.%s", fileID, fmtStr),
		"resolution":    res,
		"format":        fmtStr,
		"extract_audio": randInt(big.NewInt(2)).Int64() == 1, // 50% chance of true
	}

	raw, _ := json.Marshal(videoPayload)
	return raw
}

func getCsvPayload() json.RawMessage {
	layout := layouts[randInt(big.NewInt(int64(len(layouts)))).Int64()]
	fileID := randInt(big.NewInt(99999)).Int64()

	csvPayload := map[string]any{
		"source_uri":  fmt.Sprintf("s3://fluxd-incoming/report_%d.csv", fileID),
		"target_uri":  fmt.Sprintf("s3://fluxd-processed/report_%d.pdf", fileID),
		"layout":      layout,
		"has_headers": true,
		"font_size":   10 + randInt(big.NewInt(4)).Int64(), // Font size 10-13
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	New(ctx)

	// var count int
	// db.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks").Scan(&count)
	// if count > 0 {
	// 	log.Println("Already seeded, skipping")
	// 	return
	// }

	maxInt := big.NewInt(2)

	tasks := make([]database.Task, seedCount)

	for i := range len(tasks) {
		rInt := randInt(maxInt).Int64()

		switch int(rInt) {
		case 0:
			tasks[i].AllocatedUnit = database.TaskUnitGPU
			tasks[i].Payload = getVideoPayload()
			tasks[i].PayloadSlug = string(slugVidTranscoding)
			tasks[i].TaskType = database.TaskTypeOneOff
		case 1:
			tasks[i].AllocatedUnit = database.TaskUnitCPU
			tasks[i].Payload = getCsvPayload()
			tasks[i].PayloadSlug = string(slugCsvToPdf)
			tasks[i].TaskType = database.TaskTypeOneOff
		}
	}
	identifier := pgx.Identifier{"tasks"}
	columns := []string{"allocated_unit", "payload", "payload_slug", "task_type"}
	rowSrc := pgx.CopyFromSlice(len(tasks), func(i int) ([]any, error) {
		return []any{
			tasks[i].AllocatedUnit,
			tasks[i].Payload,
			tasks[i].PayloadSlug,
			tasks[i].TaskType,
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
