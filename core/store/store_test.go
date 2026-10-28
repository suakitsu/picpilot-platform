package store

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestPostgresMigrationsAreIdempotentAndEditionBound(t *testing.T) {
	s := testStore(t, config.Platform)
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	other := &Store{Pool: s.Pool, Edition: config.Personal}
	if !errors.Is(other.Migrate(ctx), ErrEditionMismatch) {
		t.Fatal("cross-edition database accepted")
	}
	var n int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil || n < 1 {
		t.Fatal("missing migrations")
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE schema_migrations SET checksum='changed' WHERE version='0001_foundation.sql'"); err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(ctx); err == nil {
		t.Fatal("tampered migration accepted")
	}
}

func TestPostgresFirstOwnerIsAtomic(t *testing.T) {
	s := testStore(t, config.Personal)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u, err := s.Initialize(context.Background(), "fixture@example.invalid", "fixture", "Owner")
			if err == nil {
				wins.Add(1)
				if u.Role != "owner" {
					t.Error("personal owner role wrong")
				}
			} else if !errors.Is(err, ErrAlreadySetup) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("expected exactly one owner, got %d", wins.Load())
	}
	if _, err := s.CreateUser(context.Background(), "second@example.invalid", "fixture", "Second"); !errors.Is(err, ErrConflict) {
		t.Fatal("personal account registration accepted")
	}
}

func TestPostgresPasswordChangeInvalidatesSessionVersion(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "fixture@example.invalid")
	ctx := context.Background()
	if err := s.ChangePassword(ctx, u.ID, u.AuthVersion, "new-fixture"); err != nil {
		t.Fatal(err)
	}
	if err := s.ChangePassword(ctx, u.ID, u.AuthVersion, "late-fixture"); !errors.Is(err, ErrConflict) {
		t.Fatal("stale reset overwrote password")
	}
	after, err := s.UserByID(ctx, u.ID)
	if err != nil || after.AuthVersion != u.AuthVersion+1 {
		t.Fatal("sessions not invalidated")
	}
}
