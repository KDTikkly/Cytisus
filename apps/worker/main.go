package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/KDTikkly/Cytisus/internal/foundation/config"
)

func main() {
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		log.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf(`{"level":"info","service":"worker","environment":%q,"message":"ready"}`, cfg.Environment)
	<-ctx.Done()
	log.Print(`{"level":"info","service":"worker","message":"stopped"}`)
}
