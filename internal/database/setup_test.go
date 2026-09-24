package database

import (
	"context"
	"testing"

	"github.com/scythe504/kronos/internal/testutil"
)

// setupTestDB provisions an isolated ephemeral database cloned from the migrated template database.
// Supports both tests (*testing.T, with t.Parallel()) and benchmarks (*testing.B).
func setupTestDB(tb testing.TB) (Service, *service, context.Context) {
	if t, ok := tb.(*testing.T); ok {
		t.Parallel()
	}
	ctx := context.Background()
	dbURL := testutil.GetTestDBURL(tb)

	dbService := New(ctx, dbURL)
	s := dbService.(*service)

	return dbService, s, ctx
}
