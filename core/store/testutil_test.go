package store

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func testStore(t *testing.T, edition config.Edition) *Store {
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
		t.Fatal("refusing to test against a database without picpilot_test_ prefix")
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
		t.Fatal("test connection setup failed")
	}
	s := &Store{Pool: pool, Edition: edition}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("test schema cleanup failed")
		}
		admin.Close()
	})
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return s
}

func testUser(t *testing.T, s *Store, email string) User {
	t.Helper()
	u, err := s.Initialize(context.Background(), email, "fixture-hash-not-a-password", "Test owner")
	if err != nil {
		t.Fatal(err)
	}
	return u
}
