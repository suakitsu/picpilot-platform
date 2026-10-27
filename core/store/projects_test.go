package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/internal/testpg"
)

func TestPostgresProjectReplacementRetainsCommerceMembership(t *testing.T) {
	ctx := context.Background()
	s := testStore(t, config.Platform)
	u := testUser(t, s, "project-commerce-link@example.invalid")
	if _, err := s.AdjustCredits(ctx, u.ID, 100, "fixture", "membership-seed", "", ""); err != nil {
		t.Fatal(err)
	}
	batch, _, err := s.SubmitCommerceBatch(ctx, commerceStoreBatch(t, u.ID, "project-commerce-membership", false))
	if err != nil {
		t.Fatal(err)
	}
	old, err := s.Task(ctx, u.ID, batch.TaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	result, _, err := s.CreativeWrite(ctx, u.ID, "project-replacement-link", json.RawMessage(`{"fixture":"replacement"}`), func(tx pgx.Tx) (WorkflowResult, error) {
		next, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: u.ID, ProjectID: batch.ProjectID, Capability: old.Capability, Kind: old.Kind, Title: old.Title, Request: old.Request, IdempotencyKey: "replacement-link-task", CreditsCost: 6})
		if err != nil {
			return WorkflowResult{}, err
		}
		// Only the bookkeeping hook is under test; refund/eligibility belongs
		// to the shared retry service and is not simulated as successful here.
		if err = s.ReplaceCreativeTaskTx(ctx, tx, u.ID, batch.ProjectID, old.ID, next.ID, nil, "fixture retry"); err != nil {
			return WorkflowResult{}, err
		}
		return WorkflowResult{ProjectID: batch.ProjectID, TaskIDs: []string{next.ID}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var replaces string
	var aggregate bool
	var position int
	var cost int64
	if err = s.Pool.QueryRow(ctx, `SELECT replaces_task_id,aggregate_charge,position,credits_cost FROM commerce_batch_items WHERE task_id=$1`, result.TaskIDs[0]).Scan(&replaces, &aggregate, &position, &cost); err != nil {
		t.Fatal(err)
	}
	if replaces != old.ID || aggregate || position != 0 || cost != 6 {
		t.Fatal("replacement lost commerce allocation identity")
	}
	tasks, err := s.ProjectTasks(ctx, u.ID, batch.ProjectID)
	if err != nil || len(tasks) != 2 {
		t.Fatal("replacement inflated visible project tasks", err)
	}
}

func TestPostgresProjectTransactionHashParityRollbackAndCancellation(t *testing.T) {
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: config.Platform}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Initialize(ctx, "project-store@example.invalid", "fixture-hash", "Test owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(ctx, "UPDATE users SET credits=20 WHERE id=$1", u.ID); err != nil {
		t.Fatal(err)
	}
	in := SubmitTask{UserID: u.ID, Capability: "image", Kind: "creative", Title: "Fixture", Request: json.RawMessage(`{"version":1,"image":{"prompt":"Fixture","size":"1024x1024","reference_asset_ids":["asset_normalized"]}}`), RequestIdentity: json.RawMessage(`{"transient":"never-store-this-value"}`), IdempotencyKey: "project-hash-parity", CreditsCost: 6}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	first, created, err := s.SubmitProjectTaskTx(ctx, tx, in)
	if err != nil || !created {
		t.Fatal("transaction insert failed", err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	// Shared SubmitTask must recognize the transaction helper's identical hash.
	second, created, err := s.SubmitTask(ctx, in)
	if err != nil || created || second.ID != first.ID {
		t.Fatal("shared hash/idempotency contract diverged", err)
	}
	if strings.Contains(string(second.Request), "never-store") {
		t.Fatal("pre-normalization identity was persisted")
	}
	in.RequestIdentity = json.RawMessage(`{"transient":"different"}`)
	if _, _, err = s.SubmitTask(ctx, in); !errors.Is(err, ErrConflict) {
		t.Fatal("identity conflict not rejected", err)
	}
	expected := errors.New("intentional test rollback")
	_, _, err = s.CreativeWrite(ctx, u.ID, "rollback-project-request", json.RawMessage(`{"fixture":true}`), func(tx pgx.Tx) (WorkflowResult, error) {
		p, err := s.CreateProjectTx(ctx, tx, Project{UserID: u.ID, ProductName: "Rollback fixture", Language: "en-US"})
		if err != nil {
			return WorkflowResult{}, err
		}
		in.IdempotencyKey = "rolled-back-image"
		in.ProjectID = p.ID
		if _, _, err = s.SubmitProjectTaskTx(ctx, tx, in); err != nil {
			return WorkflowResult{}, err
		}
		return WorkflowResult{}, expected
	})
	if !errors.Is(err, expected) {
		t.Fatal("rollback fixture failed", err)
	}
	var projects, tasks, credits int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM projects").Scan(&projects)
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM tasks").Scan(&tasks)
	_ = s.Pool.QueryRow(ctx, "SELECT credits FROM users WHERE id=$1", u.ID).Scan(&credits)
	if projects != 0 || tasks != 1 || credits != 14 {
		t.Fatal("transaction rollback leaked business writes", projects, tasks, credits)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	_, _, err = s.CreativeWrite(cancelled, u.ID, "cancelled-project-request", json.RawMessage(`{}`), func(pgx.Tx) (WorkflowResult, error) { called = true; return WorkflowResult{}, nil })
	if err == nil || called {
		t.Fatal("cancelled transaction executed callback")
	}
}

func TestPostgresProjectWorkerCandidatesStayPrivateUntilAccepted(t *testing.T) {
	ctx := context.Background()
	s := &Store{Pool: testpg.Open(t), Edition: config.Personal}
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	user, err := s.Initialize(ctx, "candidate-test@example.invalid", "fixture-hash", "Test owner")
	if err != nil {
		t.Fatal(err)
	}
	fixture, _, err := s.CreativeWrite(ctx, user.ID, "candidate-fixture", json.RawMessage(`{}`), func(tx pgx.Tx) (WorkflowResult, error) {
		source, err := s.CreateProjectTx(ctx, tx, Project{UserID: user.ID, ProductName: "Source", Language: "en-US"})
		if err != nil {
			return WorkflowResult{}, err
		}
		target, err := s.CreateProjectTx(ctx, tx, Project{UserID: user.ID, ProductName: "Target", Language: "en-US"})
		if err != nil {
			return WorkflowResult{}, err
		}
		tasks := make([]string, 0, 2)
		for _, project := range []Project{source, target} {
			task, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: user.ID, ProjectID: project.ID, Capability: "image", Kind: "creative", Request: json.RawMessage(`{"version":1,"image":{"prompt":"Fixture","size":"1024x1024"}}`), IdempotencyKey: "candidate-task-" + project.ID})
			if err != nil {
				return WorkflowResult{}, err
			}
			tasks = append(tasks, task.ID)
		}
		chat, err := s.SaveCreativeChatTx(ctx, tx, CreativeChat{UserID: user.ID, Title: "Fixture", Size: "1024x1024", Language: "en-US"})
		if err != nil {
			return WorkflowResult{}, err
		}
		if _, err = s.AddCreativeVersionTx(ctx, tx, user.ID, CreativeVersion{ChatID: chat.ID, ProjectID: source.ID, TaskID: tasks[0], UserRequest: "Fixture", FinalPrompt: "Fixture", Size: "1024x1024"}); err != nil {
			return WorkflowResult{}, err
		}
		return WorkflowResult{ProjectID: source.ID, AplusID: target.ID, ChatID: chat.ID, TaskIDs: tasks}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range []Asset{
		{ID: "asset_candidate_direct", ProjectID: fixture.ProjectID, ImageType: "worker-candidate"},
		{ID: "asset_candidate_linked", ProjectID: fixture.AplusID, TaskID: fixture.TaskIDs[1], ImageType: "worker-candidate"},
		{ID: "asset_public_reference", ProjectID: fixture.ProjectID, ImageType: "reference"},
	} {
		asset.UserID = user.ID
		asset.StorageKey = asset.ID + ".png"
		asset.OriginalFilename = "fixture.png"
		asset.ContentType = "image/png"
		asset.FileSize = 1
		asset.Width = 1
		asset.Height = 1
		asset.SHA256 = strings.Repeat("0", 64)
		if _, err = s.SaveAsset(ctx, asset); err != nil {
			t.Fatal(err)
		}
	}
	// A stale link's display type must not mask the underlying candidate type.
	if _, err = s.Pool.Exec(ctx, `INSERT INTO project_asset_refs(project_id,asset_id,position,image_type) VALUES($1,'asset_candidate_linked',0,'reference')`, fixture.ProjectID); err != nil {
		t.Fatal(err)
	}
	assets, err := s.ProjectAssets(ctx, user.ID, fixture.ProjectID)
	if err != nil || len(assets) != 1 || assets[0].ID != "asset_public_reference" {
		t.Fatal("raw project assets exposed an unaccepted worker artifact", err)
	}
	for _, id := range []string{"asset_candidate_direct", "asset_candidate_linked"} {
		allowed, err := s.IsCreativeChatAsset(ctx, user.ID, fixture.ChatID, id)
		if err != nil || allowed {
			t.Fatal("candidate accepted as chat history reference", id, err)
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		linkErr := s.LinkProjectAssetsTx(ctx, tx, user.ID, fixture.AplusID, []string{id}, "main")
		if err = tx.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(linkErr, ErrNotFound) {
			t.Fatal("candidate accepted as project reference", id, linkErr)
		}
	}
	// Simulate only the visibility transition; receipt atomicity is worker-owned.
	if _, err = s.Pool.Exec(ctx, "UPDATE assets SET image_type='worker-output' WHERE id='asset_candidate_linked'"); err != nil {
		t.Fatal(err)
	}
	allowed, err := s.IsCreativeChatAsset(ctx, user.ID, fixture.ChatID, "asset_candidate_linked")
	if err != nil || !allowed {
		t.Fatal("accepted output remained unavailable to its chat", err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.LinkProjectAssetsTx(ctx, tx, user.ID, fixture.AplusID, []string{"asset_candidate_linked"}, "reference"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assets, err = s.ProjectAssets(ctx, user.ID, fixture.AplusID)
	if err != nil || len(assets) != 1 || assets[0].ID != "asset_candidate_linked" {
		t.Fatal("accepted output could not be linked", err)
	}
	assets, err = s.ProjectAssets(ctx, user.ID, fixture.ProjectID)
	if err != nil || len(assets) != 2 {
		t.Fatal("candidate promotion did not update project visibility", err)
	}
}
