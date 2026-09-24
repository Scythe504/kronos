package database

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/scythe504/kronos/internal/testutil"
)

func BenchmarkCreateTask(b *testing.B) {
	dbService, s, ctx := setupTestDB(b)

	// Seed the required worker slug for CreateTask foreign worker lookup
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, repo_url, repo_ref, entrypoint, task_unit)
		VALUES ($1, $2, 'https://github.com/test/repo', 'main', './worker', 'cpu')
		ON CONFLICT (slug) DO NOTHING
	`, "bench-slug", "Bench Worker")
	if err != nil {
		b.Fatalf("failed to seed benchmark worker: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := dbService.CreateTask(ctx, "bench-slug", json.RawMessage(`{"i": `+strconv.Itoa(i)+`}`), nil, nil, nil, nil, false)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetTasks_Contention(b *testing.B) {
	dbService, s, ctx := setupTestDB(b)

	// Seed worker and tasks using testutil.SeedBenchTasks
	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, repo_url, repo_ref, entrypoint, task_unit)
		VALUES ($1, $2, 'https://github.com/test/repo', 'main', './worker', 'cpu')
		ON CONFLICT (slug) DO NOTHING
	`, "bench-slug", "Bench Worker")
	if err != nil {
		b.Fatalf("failed to seed benchmark worker: %v", err)
	}

	err = testutil.SeedBenchTasks(ctx, s.pool, "bench-slug", 5000)
	if err != nil {
		b.Fatalf("failed to seed benchmark tasks: %v", err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := dbService.GetTasks(ctx, "bench-node", []TaskUnit{TaskUnitCPU}, nil)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
