package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/commerce"
	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/creative"
)

func commerceStoreBatch(t *testing.T, user, key string, personal bool) CommerceBatch {
	t.Helper()
	c := commerce.DefaultCatalog()
	p, templates, e, err := c.Select(commerce.EstimateRequest{Platform: "taobao_tmall", SelectedSlots: []string{"main_image", "detail"}}, personal)
	if err != nil {
		t.Fatal(err)
	}
	in := commerce.GenerateRequest{Platform: p.Code, Product: commerce.Product{ProductName: "Fixture"}}
	images, err := c.Build(p, templates, e, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	return CommerceBatch{Project: Project{UserID: user, SourceType: "commerce_suite", ProductName: "Fixture", Status: "images_queued"}, Images: images, References: []string{}, Key: key, Identity: creative.JSON(in)}
}

func TestPostgresCommerceBatchConcurrentExactlyOnceChildCharges(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "commerce@example.invalid")
	ctx := context.Background()
	if _, err := s.AdjustCredits(ctx, u.ID, 100, "fixture", "seed", "", ""); err != nil {
		t.Fatal(err)
	}
	in := commerceStoreBatch(t, u.ID, "fixture-batch-exactly-once", false)
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, _, err := s.SubmitCommerceBatch(ctx, in)
			ids <- res.ProjectID
			errs <- err
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	project := ""
	for id := range ids {
		if project == "" {
			project = id
		}
		if project != id {
			t.Fatal("duplicate project")
		}
	}
	var projects, tasks, charges, allocations int
	var balance, sum int64
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM projects),(SELECT count(*) FROM tasks),(SELECT count(*) FROM credit_ledger WHERE change_amount<0),(SELECT count(*) FROM commerce_batch_items),(SELECT credits FROM users WHERE id=$1),(SELECT sum(credits_cost) FROM tasks)`, u.ID).Scan(&projects, &tasks, &charges, &allocations, &balance, &sum); err != nil {
		t.Fatal(err)
	}
	if projects != 1 || tasks != 2 || charges != 1 || allocations != 2 || balance != 88 || sum != 12 {
		t.Fatal(projects, tasks, charges, allocations, balance, sum)
	}
	assertCommerceChildCharges(t, s, u.ID, project, 2, 12)
	in.Identity = json.RawMessage(`{"changed":true}`)
	if _, _, err := s.SubmitCommerceBatch(ctx, in); !errors.Is(err, ErrConflict) {
		t.Fatal("key reuse accepted", err)
	}
}

func TestPostgresCommerceInsufficientAndLateFailureRollbackWholeBatch(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "rollback@example.invalid")
	ctx := context.Background()
	if _, err := s.AdjustCredits(ctx, u.ID, 10, "fixture", "seed", "", ""); err != nil {
		t.Fatal(err)
	}
	in := commerceStoreBatch(t, u.ID, "fixture-atomic-rollback", false)
	if _, _, err := s.SubmitCommerceBatch(ctx, in); !errors.Is(err, ErrInsufficientCredits) {
		t.Fatal("partial batch accepted", err)
	}
	if _, err := s.AdjustCredits(ctx, u.ID, 10, "fixture", "second", "", ""); err != nil {
		t.Fatal(err)
	}
	in.Images[1].Payload.Image.ReferenceAssetIDs = []string{"asset_foreign"}
	if _, _, err := s.SubmitCommerceBatch(ctx, in); !errors.Is(err, ErrNotFound) {
		t.Fatal("invalid late member accepted", err)
	}
	var tasks, projects, charges, requests int
	var balance int64
	if err := s.Pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM projects),(SELECT count(*) FROM credit_ledger WHERE change_amount<0),(SELECT count(*) FROM creative_requests),(SELECT credits FROM users WHERE id=$1)`, u.ID).Scan(&tasks, &projects, &charges, &requests, &balance); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || projects != 0 || charges != 0 || requests != 0 || balance != 20 {
		t.Fatal("rollback incomplete", tasks, projects, charges, requests, balance)
	}
}

func TestPostgresCommerceFailureCancelRefundConcurrentAndRetry(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "refund@example.invalid")
	ctx := context.Background()
	if _, err := s.AdjustCredits(ctx, u.ID, 100, "fixture", "seed", "", ""); err != nil {
		t.Fatal(err)
	}
	res, _, err := s.SubmitCommerceBatch(ctx, commerceStoreBatch(t, u.ID, "fixture-refund-batch", false))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.TaskIDs) != 2 {
		t.Fatal("batch did not create both queued members")
	}
	assertCommerceChildCharges(t, s, u.ID, res.ProjectID, 2, 12)
	lease, err := s.ClaimTask(ctx, "fixture-runner", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if lease.Status != "running" || lease.ProjectID != res.ProjectID || lease.ID != res.TaskIDs[0] && lease.ID != res.TaskIDs[1] {
		t.Fatal("claim returned a different or non-running task")
	}
	other := res.TaskIDs[0]
	if other == lease.ID {
		other = res.TaskIDs[1]
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 6; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, e := s.FailTask(ctx, lease, "MOCK_FAILURE")
			if e != nil {
				e = fmt.Errorf("FailTask after successful claim and verified child charge: %w", e)
			}
			errs <- e
		}()
		go func() {
			defer wg.Done()
			_, e := s.CancelTask(ctx, u.ID, other)
			if e != nil {
				e = fmt.Errorf("CancelTask for queued sibling after verified child charge: %w", e)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var balance int64
	var refunds int
	if err = s.Pool.QueryRow(ctx, `SELECT credits,(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_refund') FROM users WHERE id=$1`, u.ID).Scan(&balance, &refunds); err != nil || balance != 100 || refunds != 2 {
		t.Fatal("refund mismatch", balance, refunds, err)
	}
	var payload commerce.Payload
	if json.Unmarshal(lease.Request, &payload) != nil {
		t.Fatal("payload")
	}
	var results = make(chan WorkflowResult, 8)
	errs = make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, _, e := s.RetryCommerceTask(ctx, u.ID, lease.ID, "fixture-member-retry", json.RawMessage(`{}`), payload, 6)
			results <- r
			errs <- e
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	retry := ""
	for r := range results {
		if retry == "" {
			retry = r.TaskIDs[0]
		}
		if r.TaskIDs[0] != retry {
			t.Fatal("duplicate retry")
		}
	}
	tasks, err := s.ProjectTasks(ctx, u.ID, res.ProjectID)
	if err != nil || len(tasks) != 2 {
		t.Fatal("retry inflated linked image count", len(tasks), err)
	}
	if _, err = s.Task(ctx, u.ID, lease.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("replaced task visible")
	}
	claimed, err := s.ClaimTask(ctx, "retry-runner", time.Minute)
	if err != nil || claimed.ID != retry {
		t.Fatal(err)
	}
	if _, err = s.FailTask(ctx, claimed, "MOCK_RETRY_FAILURE"); err != nil {
		t.Fatal(err)
	}
	u, err = s.UserByID(ctx, u.ID)
	if err != nil || u.Credits != 100 {
		t.Fatal("retry refund mismatch", err)
	}
}

func TestPostgresCommercePersonalNoLedgerAndCatalogIsolation(t *testing.T) {
	s := testStore(t, config.Personal)
	u := testUser(t, s, "personal@example.invalid")
	ctx := context.Background()
	res, _, err := s.SubmitCommerceBatch(ctx, commerceStoreBatch(t, u.ID, "personal-commerce", true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CancelTask(ctx, u.ID, res.TaskIDs[0]); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = s.Pool.QueryRow(ctx, "SELECT count(*) FROM credit_ledger").Scan(&count); err != nil || count != 0 {
		t.Fatal("personal business credits", err)
	}
	if _, err = s.UpdateCommerceCatalog(ctx, u.ID, "fixture", func(c *commerce.Catalog) error { return nil }); !errors.Is(err, ErrAdminForbidden) {
		t.Fatal("personal platform admin accepted", err)
	}
	if _, _, err = s.SubmitCommerceBatch(ctx, commerceStoreBatch(t, u.ID, "charged-personal-commerce", false)); !errors.Is(err, ErrConflict) {
		t.Fatal("personal charged batch accepted", err)
	}
}

func TestPostgresCommerceLegacyAggregateRefundProofConcurrent(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "receipt@example.invalid")
	ctx := context.Background()
	if _, err := s.AdjustCredits(ctx, u.ID, 100, "fixture", "seed", "", ""); err != nil {
		t.Fatal(err)
	}
	res := commerceLegacyBatchFixture(t, s, u.ID)
	task, err := s.Task(ctx, u.ID, res.TaskIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if handled, e := commerceTaskRefunded(ctx, tx, task); !handled || !errors.Is(e, ErrUnsafeRetry) {
		t.Fatal("unrefunded batch accepted as settled", handled, e)
	}
	bad := task
	bad.CreditsCost++
	if handled, e := refundCommerceTask(ctx, tx, bad); !handled || !errors.Is(e, ErrConflict) {
		t.Fatal("cost mismatch refunded", handled, e)
	}
	_ = tx.Rollback(ctx)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tx, e := s.Pool.Begin(ctx)
			if e != nil {
				errs <- e
				return
			}
			defer tx.Rollback(ctx)
			handled, e := refundCommerceTask(ctx, tx, task)
			if !handled && e == nil {
				e = ErrConflict
			}
			if e == nil {
				e = tx.Commit(ctx)
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var balance int64
	var count int
	if err = s.Pool.QueryRow(ctx, `SELECT credits,(SELECT count(*) FROM credit_ledger WHERE biz_type='generation_refund') FROM users WHERE id=$1`, u.ID).Scan(&balance, &count); err != nil || balance != 100 || count != 1 {
		t.Fatal("allocation refunded more than once", balance, count, err)
	}
	tx, err = s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if handled, e := commerceTaskRefunded(ctx, tx, task); !handled || e != nil {
		t.Fatal("settled receipt missing", handled, e)
	}
	ordinary := task
	ordinary.ID = "task_noncommerce"
	if handled, e := refundCommerceTask(ctx, tx, ordinary); handled || e != nil {
		t.Fatal("unrelated refund intercepted", handled, e)
	}
}
