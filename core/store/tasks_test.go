package store

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/suakitsu/picpilot-platform-go/core/config"
)

func submitFixture(t *testing.T, s *Store, u User, key string, cost int64) Task {
	t.Helper()
	task, _, err := s.SubmitTask(context.Background(), SubmitTask{UserID: u.ID, Capability: "image", Kind: "creative", Title: "Fixture", IdempotencyKey: key, CreditsCost: cost, Request: json.RawMessage(`{"prompt":"mock only","size":"1024x1024"}`)})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func fundFixture(t *testing.T, s *Store, u User) {
	t.Helper()
	if _, err := s.AdjustCredits(context.Background(), u.ID, 100, "fixture", "fixture-funding", "Fixture credits", u.ID); err != nil {
		t.Fatal(err)
	}
}

func providerFixture(t *testing.T, s *Store, scope string, max int) Provider {
	t.Helper()
	p, err := s.PutProvider(context.Background(), Provider{ID: "mock-image", OwnerScope: scope, Name: "Mock", Capability: "image", Protocol: "openai_images_generations", Profile: "openai", BaseURL: "https://example.invalid", Model: "gpt-image-2", Enabled: true, MaxConcurrency: max, TimeoutSeconds: 300, ExecutionMode: "http", OfflineAction: "fallback", LeaseSeconds: 120}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestPostgresConcurrentSubmitChargesOnce(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "fixture@example.invalid")
	fundFixture(t, s, u)
	var wg sync.WaitGroup
	var created atomic.Int32
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, newTask, err := s.SubmitTask(context.Background(), SubmitTask{UserID: u.ID, Capability: "image", Kind: "creative", IdempotencyKey: "same-click", CreditsCost: 6, Request: json.RawMessage(`{"prompt":"mock"}`)})
			if err != nil {
				t.Error(err)
			}
			if newTask {
				created.Add(1)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("duplicate task created")
	}
	after, _ := s.UserByID(context.Background(), u.ID)
	if after.Credits != 94 {
		t.Fatalf("expected 94 credits, got %d", after.Credits)
	}
	if _, _, err := s.SubmitTask(context.Background(), SubmitTask{UserID: u.ID, Capability: "image", Kind: "creative", IdempotencyKey: "same-click", CreditsCost: 6, Request: json.RawMessage(`{"prompt":"different"}`)}); !errors.Is(err, ErrConflict) {
		t.Fatal("idempotency conflict not detected")
	}
}

func TestPostgresTwoInstanceClaimIsAtomic(t *testing.T) {
	s := testStore(t, config.Personal)
	u := testUser(t, s, "fixture@example.invalid")
	submitFixture(t, s, u, "one", 0)
	pool, err := pgxpool.NewWithConfig(context.Background(), s.Pool.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	other := &Store{Pool: pool, Edition: config.Personal}
	var count atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			node := s
			if i%2 == 0 {
				node = other
			}
			_, err := node.ClaimTask(context.Background(), "instance", time.Minute)
			if err == nil {
				count.Add(1)
			} else if !errors.Is(err, ErrNotFound) {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatalf("one task claimed %d times", count.Load())
	}
}

func TestPostgresDuplicateCompleteAndFailAreSafe(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "fixture@example.invalid")
	fundFixture(t, s, u)
	submitFixture(t, s, u, "success", 6)
	ctx := context.Background()
	lease, err := s.ClaimTask(ctx, "one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.CompleteTask(ctx, lease, json.RawMessage(`{"asset_id":"test-asset"}`)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if _, err := s.FailTask(ctx, lease, "LATE_FAIL"); !errors.Is(err, ErrTerminalConflict) {
		t.Fatal("late failure changed successful task")
	}
	after, _ := s.UserByID(ctx, u.ID)
	if after.Credits != 94 {
		t.Fatal("success was refunded")
	}
	submitFixture(t, s, u, "failure", 6)
	lease, err = s.ClaimTask(ctx, "one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.FailTask(ctx, lease, "MOCK_REJECTED"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	after, _ = s.UserByID(ctx, u.ID)
	if after.Credits != 94 {
		t.Fatal("duplicate refund or missing refund")
	}
}

func TestPostgresExpiryRejectsLateResultAndHoldsUnknownSubmission(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "fixture@example.invalid")
	fundFixture(t, s, u)
	p := providerFixture(t, s, "platform", 2)
	submitFixture(t, s, u, "unknown", 6)
	ctx := context.Background()
	lease, err := s.ClaimTask(ctx, "one", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.ReserveAttempt(ctx, lease, "platform", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BeginSubmission(ctx, lease, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(ctx, "UPDATE tasks SET lease_until=now()-interval '1 minute' WHERE id=$1", lease.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CompleteTask(ctx, lease, json.RawMessage(`{}`)); !errors.Is(err, ErrLeaseInvalid) {
		t.Fatal("late result accepted")
	}
	if n, err := s.RecoverExpiredTaskLeases(ctx); err != nil || n != 1 {
		t.Fatal("recovery failed", err)
	}
	task, _ := s.Task(ctx, u.ID, lease.ID)
	if task.Status != "uncertain" {
		t.Fatal("submitted task was automatically retried")
	}
	if _, err := s.ClaimTask(ctx, "two", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("unknown task became claimable")
	}
	after, _ := s.UserByID(ctx, u.ID)
	if after.Credits != 94 {
		t.Fatal("unknown paid result was automatically refunded")
	}
}

func TestPostgresSafeFallbackAndGlobalProviderCapacity(t *testing.T) {
	s := testStore(t, config.Personal)
	u := testUser(t, s, "fixture@example.invalid")
	p := providerFixture(t, s, u.ID, 1)
	submitFixture(t, s, u, "first", 0)
	submitFixture(t, s, u, "second", 0)
	ctx := context.Background()
	one, _ := s.ClaimTask(ctx, "one", time.Minute)
	two, _ := s.ClaimTask(ctx, "two", time.Minute)
	a, err := s.ReserveAttempt(ctx, one, u.ID, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveAttempt(ctx, two, u.ID, p.ID); !errors.Is(err, ErrProviderBusy) {
		t.Fatal("global provider capacity exceeded")
	}
	if err := s.BeginSubmission(ctx, one, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RejectAttempt(ctx, one, a.ID, "UPSTREAM_REJECTED", true); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReserveAttempt(ctx, two, u.ID, p.ID); err != nil {
		t.Fatal("definitive failure did not release capacity", err)
	}
	if _, err := s.ReserveAttempt(ctx, one, "platform", p.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("personal task accessed platform keys")
	}
}

func TestPostgresCancelledQueueRefundsExactlyOnce(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "fixture@example.invalid")
	fundFixture(t, s, u)
	task := submitFixture(t, s, u, "cancel", 6)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := s.CancelTask(ctx, u.ID, task.ID); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := s.UserByID(ctx, u.ID)
	if after.Credits != 100 {
		t.Fatal("cancel refund was not idempotent")
	}
	if _, err := s.ClaimTask(ctx, "one", time.Minute); !errors.Is(err, ErrNotFound) {
		t.Fatal("cancelled task was claimed")
	}
}
