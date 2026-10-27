package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform-go/core/nova"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func novaTestStore(t *testing.T, edition config.Edition) (*Store, User) {
	t.Helper()
	s := &Store{Pool: testpg.Open(t), Edition: edition}
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Initialize(ctx, "nova-owner@example.invalid", "test-only-password-hash", "Nova fixture")
	if err != nil {
		t.Fatal(err)
	}
	if edition == config.Platform {
		if _, err = s.AdjustCredits(ctx, u.ID, 1000, "fixture", "nova-test", "Fixture", u.ID); err != nil {
			t.Fatal(err)
		}
	}
	return s, u
}

func novaTestSubmit(s *Store, user, key string, count int, cost int64, calls *atomic.Int32) func(pgx.Tx, string) ([]Task, error) {
	return func(tx pgx.Tx, _ string) ([]Task, error) {
		if calls != nil {
			calls.Add(1)
		}
		out := []Task{}
		for i := 0; i < count; i++ {
			t, _, err := s.SubmitProjectTaskTx(context.Background(), tx, SubmitTask{UserID: user, Capability: "image", Kind: "creative", Title: "Nova fixture", Request: json.RawMessage(`{"version":1,"image":{"prompt":"three blocks","size":"1024x1024"}}`), IdempotencyKey: fmt.Sprintf("nova-fixture:%s:%d", key, i), CreditsCost: cost})
			if err != nil {
				return nil, err
			}
			out = append(out, t)
		}
		return out, nil
	}
}

func TestPostgresNovaConcurrentReplayAtomicChargeAndReload(t *testing.T) {
	s, u := novaTestStore(t, config.Platform)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var calls atomic.Int32
	var wg sync.WaitGroup
	results := make(chan NovaRun, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, _, err := s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", "same-operation", json.RawMessage(`{"prompt":"test-only-input"}`), 4, novaTestSubmit(s, u.ID, "same", 4, 6, &calls))
			results <- n
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for n := range results {
		if id == "" {
			id = n.ID
		}
		if n.ID != id {
			t.Fatal("duplicate run")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("business callback replayed")
	}
	var tasks, charges int
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger WHERE biz_type='generation_charge'").Scan(&charges); err != nil {
		t.Fatal(err)
	}
	owner, _ := s.UserByID(ctx, u.ID)
	if tasks != 4 || charges != 4 || owner.Credits != 976 {
		t.Fatal("non-atomic or duplicate billing", tasks, charges, owner.Credits)
	}
	restarted := &Store{Pool: s.Pool, Edition: s.Edition}
	run, err := restarted.NovaRun(ctx, u.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := restarted.NovaRunTaskIDs(ctx, u.ID, id)
	if err != nil || len(ids) != 4 {
		t.Fatal("durable task mapping lost")
	}
	if _, _, err = restarted.NovaReplay(ctx, u.ID, "image", "same-operation", json.RawMessage(`{"prompt":"changed"}`)); !errors.Is(err, ErrConflict) {
		t.Fatal("key conflict accepted")
	}
	first, err := s.AcknowledgeNovaRun(ctx, u.ID, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.AcknowledgeNovaRun(ctx, u.ID, run.ID)
	if err != nil || !first.AcknowledgedAt.Equal(*second.AcknowledgedAt) {
		t.Fatal("ack was not idempotent")
	}
	queued, processing, err := s.NovaQueueCounts(ctx, u.ID)
	if err != nil || queued != 4 || processing != 0 {
		t.Fatal("ack changed queue")
	}
	other, err := s.CreateUser(ctx, "other-nova@example.invalid", "test-only-hash", "Other")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.NovaRunTaskIDs(ctx, other.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner isolation")
	}
	if _, err = s.AcknowledgeNovaRun(ctx, other.ID, id); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-owner ack")
	}
	list, err := s.NovaRuns(ctx, u.ID, "", 10)
	if err != nil || len(list) != 1 || list[0].ID != id {
		t.Fatal("reload listing")
	}
}

func TestPostgresNovaRollbackAndQueueLimit(t *testing.T) {
	s, u := novaTestStore(t, config.Platform)
	ctx := context.Background()
	sentinel := errors.New("test-only-rollback")
	_, _, err := s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", "rollback", json.RawMessage(`{}`), 1, func(tx pgx.Tx, id string) ([]Task, error) {
		_, err := novaTestSubmit(s, u.ID, "rollback", 1, 6, nil)(tx, id)
		if err != nil {
			return nil, err
		}
		return nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	var count int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&count)
	owner, _ := s.UserByID(ctx, u.ID)
	if count != 0 || owner.Credits != 1000 {
		t.Fatal("rollback left tasks or charges")
	}
	for i := 0; i < nova.MaxPending/nova.MaxParallel; i++ {
		key := fmt.Sprintf("fill-%d", i)
		if _, _, err = s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", key, json.RawMessage(`{}`), 4, novaTestSubmit(s, u.ID, key, 4, 0, nil)); err != nil {
			t.Fatal(err)
		}
	}
	var called atomic.Int32
	if _, _, err = s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", "overflow", json.RawMessage(`{}`), 1, novaTestSubmit(s, u.ID, "overflow", 1, 0, &called)); !errors.Is(err, ErrNovaQueueFull) || called.Load() != 0 {
		t.Fatal("queue cap was not atomic")
	}
	if _, created, err := s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", "fill-0", json.RawMessage(`{}`), 4, novaTestSubmit(s, u.ID, "fill-0", 4, 0, &called)); err != nil || created || called.Load() != 0 {
		t.Fatal("full queue rejected replay")
	}
}

func TestPostgresNovaCancellationRecoveryAndLiveLease(t *testing.T) {
	s, u := novaTestStore(t, config.Platform)
	ctx := context.Background()
	n, _, err := s.CreateNovaRun(ctx, u.ID, "image", "text-to-image", "cancel", json.RawMessage(`{}`), 2, novaTestSubmit(s, u.ID, "cancel", 2, 6, nil))
	if err != nil {
		t.Fatal(err)
	}
	lease, err := s.ClaimTask(ctx, "test-only-runner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// Crash after intent, before the first child cancellation.
	if _, err = s.Pool.Exec(ctx, "UPDATE nova_runs SET cancel_requested_at=now() WHERE id=$1", n.ID); err != nil {
		t.Fatal(err)
	}
	if err = s.ReconcileNovaCancellations(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CancelNovaRun(ctx, u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	current, err := s.Task(ctx, u.ID, lease.ID)
	if err != nil || current.Status != "running" || current.CancelRequestedAt == nil {
		t.Fatal("cancel falsely settled live work")
	}
	u1, _ := s.UserByID(ctx, u.ID)
	if u1.Credits != 994 {
		t.Fatal("live work refunded early", u1.Credits)
	}
	if _, err = s.FailTask(ctx, lease, "CANCELLED"); err != nil {
		t.Fatal(err)
	}
	u2, _ := s.UserByID(ctx, u.ID)
	if u2.Credits != 1000 {
		t.Fatal("shared settlement did not refund", u2.Credits)
	}
	if _, err = s.CancelNovaRun(ctx, u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	u3, _ := s.UserByID(ctx, u.ID)
	if u3.Credits != 1000 {
		t.Fatal("cancel replay refunded twice")
	}
}

func TestPostgresNovaGalleryRevisionScopeAndHashOnly(t *testing.T) {
	s, u := novaTestStore(t, config.Personal)
	ctx := context.Background()
	settings := nova.DefaultSettings()
	settings.Prompts = []nova.Prompt{{Title: "Fixture", Content: "Three blocks", Type: 1}}
	password := "test-only-gallery-password"
	n, err := s.PutNovaSettings(ctx, u.ID, settings, &password, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n.PasswordHash == password || !security.VerifyPassword(n.PasswordHash, password) {
		t.Fatal("password not hashed")
	}
	if _, err = s.PutNovaSettings(ctx, "platform", settings, nil, 0); !errors.Is(err, ErrConflict) {
		t.Fatal("personal platform settings accepted")
	}
	if allowed, err := s.NovaGalleryAllowed(ctx, u.ID, u.ID, n); err != nil || allowed {
		t.Fatal("gallery unlocked without grant")
	}
	if err = s.GrantNovaGallery(ctx, u.ID, u.ID, n.Revision); err != nil {
		t.Fatal(err)
	}
	if allowed, err := s.NovaGalleryAllowed(ctx, u.ID, u.ID, n); err != nil || !allowed {
		t.Fatal("grant missing")
	}
	if _, err = s.PutNovaSettings(ctx, u.ID, settings, nil, 0); !errors.Is(err, ErrConflict) {
		t.Fatal("stale settings revision")
	}
	next, err := s.PutNovaSettings(ctx, u.ID, settings, nil, n.Revision)
	if err != nil || next.PasswordHash != n.PasswordHash {
		t.Fatal("password preservation")
	}
	if allowed, err := s.NovaGalleryAllowed(ctx, u.ID, u.ID, next); err != nil || allowed {
		t.Fatal("old grant survived revision change")
	}
	if err = s.GrantNovaGallery(ctx, u.ID, u.ID, n.Revision); !errors.Is(err, ErrConflict) {
		t.Fatal("stale password verification granted")
	}
	settings.Mode = "1"
	public, err := s.PutNovaSettings(ctx, u.ID, settings, nil, next.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _ := s.NovaGalleryAllowed(ctx, u.ID, u.ID, public); !allowed {
		t.Fatal("always-visible mode locked")
	}
	settings.Mode = "3"
	hidden, err := s.PutNovaSettings(ctx, u.ID, settings, nil, public.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if allowed, _ := s.NovaGalleryAllowed(ctx, u.ID, u.ID, hidden); allowed {
		t.Fatal("hidden gallery exposed")
	}
}
