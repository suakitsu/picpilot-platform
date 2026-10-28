// Package testpg provides disposable schemas exclusively in explicitly named
// test databases. It never connects unless a test DSN is supplied.
package testpg

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suakitsu/picpilot-platform/core/security"
)

func Open(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PICPILOT_TEST_DATABASE_URL")
	if file := os.Getenv("PICPILOT_TEST_DATABASE_CONFIG_FILE"); file != "" && dsn == "" {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal("cannot read test database config")
		}
		var c struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(data, &c) != nil {
			t.Fatal("invalid test database config")
		}
		dsn = c.URL
	}
	if dsn == "" {
		t.Skip("PostgreSQL integration: set PICPILOT_TEST_DATABASE_URL to a picpilot_test_* database")
	}
	c, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid test database URL")
	}
	if !strings.HasPrefix(c.ConnConfig.Database, "picpilot_test_") {
		t.Fatal("refusing a database without picpilot_test_ prefix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		t.Fatal("test database connection failed")
	}
	schema := security.NewID("test")
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal("test schema creation failed")
	}
	c.ConnConfig.RuntimeParams["search_path"] = schema
	c.MaxConns = 8
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		_, _ = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE")
		admin.Close()
		t.Fatal("test pool initialization failed")
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("test schema cleanup failed")
		}
		admin.Close()
	})
	return pool
}
