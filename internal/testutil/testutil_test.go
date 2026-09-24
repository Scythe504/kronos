package testutil

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

func TestGetTestDBURL(t *testing.T) {
	url1 := GetTestDBURL(t)
	assert.NotEmpty(t, url1)

	url2 := GetTestDBURL(t)
	assert.NotEmpty(t, url2)
	assert.NotEqual(t, url1, url2)

	conn, err := pgx.Connect(context.Background(), url1)
	assert.NoError(t, err)
	defer conn.Close(context.Background())

	var tableName string
	err = conn.QueryRow(context.Background(), "SELECT table_name FROM information_schema.tables WHERE table_name = 'nodes'").Scan(&tableName)
	assert.NoError(t, err)
	assert.Equal(t, "nodes", tableName)
}
