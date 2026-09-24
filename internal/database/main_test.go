package database

import (
	"os"
	"testing"

	"github.com/scythe504/kronos/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.Cleanup()
	os.Exit(code)
}
