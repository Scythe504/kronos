package main

import (
	"context"
	"log"
	"os"

	"github.com/scythe504/kronos/internal/database"
)

func main() {
	srv := database.New(context.Background(), os.Getenv("DB_URL"))

	err := srv.Migrate()

	if err != nil {
		log.Fatalln("[ERR_MIGRATING]", err)
	}
}