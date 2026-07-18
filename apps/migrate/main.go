package main

import (
	"context"
	"log"
	"os"

	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}
	directory := os.Getenv("MIGRATIONS_DIR")
	if directory == "" {
		directory = "db/migrations"
	}

	if err := migrations.Run(context.Background(), databaseURL, directory); err != nil {
		log.Fatal(err)
	}
	log.Print(`{"level":"info","service":"migrate","message":"migrations applied"}`)
}
