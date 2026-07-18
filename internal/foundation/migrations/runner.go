package migrations

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

const createTrackingTable = `
CREATE TABLE IF NOT EXISTS public.cytisus_schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
)`

func Run(ctx context.Context, databaseURL, directory string) error {
	files, err := filepath.Glob(filepath.Join(directory, "*.up.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no up migrations found in %s", directory)
	}
	sort.Strings(files)

	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer conn.Close(ctx)

	if _, err := conn.Exec(ctx, createTrackingTable); err != nil {
		return fmt.Errorf("create migration tracking table: %w", err)
	}

	for _, path := range files {
		if err := apply(ctx, conn, path); err != nil {
			return err
		}
	}
	return nil
}

func apply(ctx context.Context, conn *pgx.Conn, path string) error {
	version := strings.TrimSuffix(filepath.Base(path), ".up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", version, err)
	}

	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer tx.Rollback(ctx)

	var applied bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM public.cytisus_schema_migrations WHERE version = $1)", version).Scan(&applied); err != nil {
		return fmt.Errorf("check migration %s: %w", version, err)
	}
	if applied {
		return tx.Commit(ctx)
	}

	if _, err := tx.Exec(ctx, string(contents)); err != nil {
		return fmt.Errorf("execute migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, "INSERT INTO public.cytisus_schema_migrations (version) VALUES ($1)", version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}
	return nil
}
