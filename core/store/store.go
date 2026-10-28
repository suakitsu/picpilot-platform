package store

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suakitsu/picpilot-platform/core/config"
)

var (
	ErrNotFound            = errors.New("record not found")
	ErrConflict            = errors.New("conflicting request")
	ErrAlreadySetup        = errors.New("instance already initialized")
	ErrNotSetup            = errors.New("instance not initialized")
	ErrInsufficientCredits = errors.New("insufficient credits")
	ErrDisabled            = errors.New("account disabled")
	ErrLeaseInvalid        = errors.New("lease invalid or expired")
	ErrTerminalConflict    = errors.New("task already terminal")
	ErrEditionMismatch     = errors.New("database belongs to a different PicPilot edition")
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct {
	Pool    *pgxpool.Pool
	Edition config.Edition
}

func Open(ctx context.Context, databaseURL string, edition config.Edition) (*Store, error) {
	c, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("invalid database configuration")
	}
	c.MaxConns = 12
	c.MinConns = 1
	c.MaxConnLifetime = 30 * time.Minute
	c.MaxConnIdleTime = 5 * time.Minute
	c.ConnConfig.ConnectTimeout = 10 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, c)
	if err != nil {
		return nil, errors.New("cannot initialize database connection")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("database is unavailable")
	}
	s := &Store{Pool: pool, Edition: edition}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() { s.Pool.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if s.Edition != config.Platform && s.Edition != config.Personal {
		return errors.New("invalid database edition")
	}
	conn, err := s.Pool.Acquire(ctx)
	if err != nil {
		return errors.New("database migration connection unavailable")
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	// Every migration runs in one transaction under an instance-wide lock.
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(712496152241)"); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version text PRIMARY KEY, checksum text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrations, "migrations")
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	known := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			known[entry.Name()] = true
		}
	}
	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return err
		}
		if !known[version] {
			rows.Close()
			return errors.New("database schema is newer than this binary; restore the matching binary or a backup")
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, entry := range entries {
		if !known[entry.Name()] {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		checksum := hex.EncodeToString(hash[:])
		var stored string
		err = tx.QueryRow(ctx, "SELECT checksum FROM schema_migrations WHERE version=$1", entry.Name()).Scan(&stored)
		if err == nil {
			if stored != checksum {
				return fmt.Errorf("migration checksum mismatch: %s", entry.Name())
			}
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if _, err = tx.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("migration %s failed (SQLSTATE %s)", entry.Name(), SQLState(err))
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version,checksum) VALUES($1,$2)", entry.Name(), checksum); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `INSERT INTO instance_state(singleton,edition) VALUES(true,$1) ON CONFLICT DO NOTHING`, string(s.Edition)); err != nil {
		return err
	}
	var edition string
	if err = tx.QueryRow(ctx, "SELECT edition FROM instance_state WHERE singleton=true").Scan(&edition); err != nil {
		return err
	}
	if edition != string(s.Edition) {
		return ErrEditionMismatch
	}
	return tx.Commit(ctx)
}

func SQLState(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code
	}
	return "unknown"
}

func normalizeError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if SQLState(err) == "23505" {
		return ErrConflict
	}
	return err
}
