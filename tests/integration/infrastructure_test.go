//go:build integration

package integration

import (
	"bufio"
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KDTikkly/Cytisus/internal/foundation/migrations"
	"github.com/jackc/pgx/v5"
)

func TestPostgreSQLIsReachable(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, requiredEnv(t, "DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if err := conn.Ping(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRedisIsReachable(t *testing.T) {
	connection, err := net.DialTimeout("tcp", requiredEnv(t, "REDIS_ADDR"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(10 * time.Second))

	if _, err := connection.Write([]byte("*1\r\n$4\r\nPING\r\n")); err != nil {
		t.Fatal(err)
	}
	response, err := bufio.NewReader(connection).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(response) != "+PONG" {
		t.Fatalf("unexpected Redis response: %q", response)
	}
}

func TestFoundationMigrationUpDownUp(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	databaseURL := requiredEnv(t, "DATABASE_URL")
	directory := filepath.Join("..", "..", "db", "migrations")

	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("apply up migration: %v", err)
	}
	conn, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)

	assertSchemaExists(t, ctx, conn, true)
	down, err := os.ReadFile(filepath.Join(directory, "000001_foundation.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, string(down)); err != nil {
		t.Fatalf("apply down migration: %v", err)
	}
	if _, err := conn.Exec(ctx, "DELETE FROM public.cytisus_schema_migrations WHERE version = $1", "000001_foundation"); err != nil {
		t.Fatalf("reset migration tracking: %v", err)
	}
	assertSchemaExists(t, ctx, conn, false)

	if err := migrations.Run(ctx, databaseURL, directory); err != nil {
		t.Fatalf("reapply up migration: %v", err)
	}
	assertSchemaExists(t, ctx, conn, true)
}

func assertSchemaExists(t *testing.T, ctx context.Context, conn *pgx.Conn, expected bool) {
	t.Helper()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'foundation')").Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists != expected {
		t.Fatalf("expected foundation schema existence %t, got %t", expected, exists)
	}
}

func requiredEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s is required", key)
	}
	return value
}
