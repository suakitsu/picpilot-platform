package store

import (
	"context"
	"encoding/json"
	"testing"
)

func assertCommerceChildCharges(t *testing.T, s *Store, user, project string, want int, total int64) {
	t.Helper()
	var count, charged int
	var sum int64
	err := s.Pool.QueryRow(context.Background(), `
	SELECT count(*), count(*) FILTER (WHERE aggregate_charge), COALESCE(sum(credits_cost),0)
	FROM commerce_batch_items WHERE user_id=$1 AND project_id=$2`, user, project).Scan(&count, &charged, &sum)
	if err != nil {
		t.Fatal(err)
	}
	if count != want || charged != want || sum != total {
		t.Fatalf("child charge receipts count=%d charged=%d sum=%d want=%d/%d/%d", count, charged, sum, want, want, total)
	}
	var individual int
	if err := s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM credit_ledger c JOIN commerce_batch_items i ON i.task_id=c.biz_id WHERE c.user_id=$1 AND i.project_id=$2 AND c.biz_type='generation_charge'`, user, project).Scan(&individual); err != nil {
		t.Fatal(err)
	}
	if individual != 0 {
		t.Fatalf("aggregate batch created %d child generation charges", individual)
	}
}

// commerceLegacyBatchFixture models the old aggregate-charge shape so the
// compatibility refund path is tested without putting legacy behavior in the
// normal commerce submitter.
func commerceLegacyBatchFixture(t *testing.T, s *Store, user string) WorkflowResult {
	t.Helper()
	ctx := context.Background()
	in := commerceStoreBatch(t, user, "legacy-fixture", false)
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	p, err := s.CreateProjectTx(ctx, tx, in.Project)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commerce_batches(project_id,user_id,kind,credits_cost) VALUES($1,$2,'commerce_suite',6)`, p.ID, user); err != nil {
		t.Fatal(err)
	}
	if _, err = adjustCredits(ctx, tx, user, -6, "commerce_suite_generate", p.ID, "Legacy aggregate fixture", ""); err != nil {
		t.Fatal(err)
	}
	request, err := json.Marshal(in.Images[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := s.SubmitProjectTaskTx(ctx, tx, SubmitTask{UserID: user, ProjectID: p.ID, Capability: "image", Kind: in.Images[0].Slot, Title: in.Images[0].Name, Request: request, IdempotencyKey: "legacy-fixture-task", CreditsCost: 0})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE tasks SET credits_cost=6 WHERE id=$1", task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO commerce_batch_items(task_id,project_id,user_id,position,credits_cost,aggregate_charge) VALUES($1,$2,$3,0,6,true)`, task.ID, p.ID, user); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return WorkflowResult{ProjectID: p.ID, TaskIDs: []string{task.ID}}
}
