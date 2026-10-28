package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/nova"
	"github.com/suakitsu/picpilot-platform/core/security"
)

var ErrNovaQueueFull = errors.New("nova pending task limit reached")

type NovaRun struct {
	ID                 string
	UserID             string
	Kind               string
	Mode               string
	RequestKey         string
	RequestHash        string
	CreatedAt          time.Time
	AcknowledgedAt     *time.Time
	CancelRequestedAt  *time.Time
	CancelDispatchedAt *time.Time
}

const novaRunColumns = `id,user_id,kind,mode,request_key,request_hash,created_at,acknowledged_at,cancel_requested_at,cancel_dispatched_at`

func scanNovaRun(row pgx.Row) (NovaRun, error) {
	var n NovaRun
	err := row.Scan(&n.ID, &n.UserID, &n.Kind, &n.Mode, &n.RequestKey, &n.RequestHash, &n.CreatedAt, &n.AcknowledgedAt, &n.CancelRequestedAt, &n.CancelDispatchedAt)
	return n, normalizeError(err)
}

func novaHash(identity json.RawMessage) (string, error) {
	if len(identity) > nova.MaxTextBody {
		return "", ErrConflict
	}
	b, err := CanonicalJSON(identity)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func (s *Store) NovaReplay(ctx context.Context, user, kind, key string, identity json.RawMessage) (NovaRun, bool, error) {
	hash, err := novaHash(identity)
	if err != nil {
		return NovaRun{}, false, err
	}
	n, err := scanNovaRun(s.Pool.QueryRow(ctx, "SELECT "+novaRunColumns+" FROM nova_runs WHERE user_id=$1 AND kind=$2 AND request_key=$3", user, kind, key))
	if errors.Is(err, ErrNotFound) {
		return NovaRun{}, false, nil
	}
	if err != nil {
		return NovaRun{}, false, err
	}
	if n.RequestHash != hash {
		return NovaRun{}, false, ErrConflict
	}
	return n, true, nil
}

// CreateNovaRun atomically links the operation to existing business submissions.
// fn must use the supplied transaction and shared SubmitProjectTaskTx; never
// call a provider or acquire another database connection while holding it.
func (s *Store) CreateNovaRun(ctx context.Context, user, kind, mode, key string, identity json.RawMessage, slots int, fn func(pgx.Tx, string) ([]Task, error)) (NovaRun, bool, error) {
	if user == "" || nova.ValidateKey(key) != nil || (kind != "image" && kind != "text") || len(mode) > 40 || slots < 1 || slots > nova.MaxParallel || kind == "text" && slots != 1 || fn == nil {
		return NovaRun{}, false, ErrConflict
	}
	hash, err := novaHash(identity)
	if err != nil {
		return NovaRun{}, false, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return NovaRun{}, false, err
	}
	defer tx.Rollback(context.Background())
	var status string
	if err = tx.QueryRow(ctx, "SELECT status FROM users WHERE id=$1 FOR UPDATE", user).Scan(&status); err != nil {
		return NovaRun{}, false, normalizeError(err)
	}
	if status != "active" {
		return NovaRun{}, false, ErrDisabled
	}
	existing, err := scanNovaRun(tx.QueryRow(ctx, "SELECT "+novaRunColumns+" FROM nova_runs WHERE user_id=$1 AND kind=$2 AND request_key=$3", user, kind, key))
	if err == nil {
		if existing.RequestHash != hash {
			return NovaRun{}, false, ErrConflict
		}
		return existing, false, tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return NovaRun{}, false, err
	}
	var pending int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM nova_runs n JOIN nova_run_tasks nt ON nt.run_id=n.id JOIN tasks t ON t.id=nt.task_id
 WHERE n.user_id=$1 AND t.status NOT IN ('succeeded','failed','cancelled')`, user).Scan(&pending); err != nil {
		return NovaRun{}, false, err
	}
	if pending+slots > nova.MaxPending {
		return NovaRun{}, false, ErrNovaQueueFull
	}
	n, err := scanNovaRun(tx.QueryRow(ctx, "INSERT INTO nova_runs(id,user_id,kind,mode,request_key,request_hash) VALUES($1,$2,$3,$4,$5,$6) RETURNING "+novaRunColumns, security.NewID("nova"), user, kind, mode, key, hash))
	if err != nil {
		return NovaRun{}, false, err
	}
	tasks, err := fn(tx, n.ID)
	if err != nil {
		return NovaRun{}, false, err
	}
	if len(tasks) != slots {
		return NovaRun{}, false, ErrConflict
	}
	for i, t := range tasks {
		var owns bool
		if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM tasks WHERE id=$1 AND user_id=$2 AND capability=$3 AND deleted_at IS NULL)", t.ID, user, kind).Scan(&owns); err != nil {
			return NovaRun{}, false, err
		}
		if !owns {
			return NovaRun{}, false, ErrNotFound
		}
		if _, err = tx.Exec(ctx, "INSERT INTO nova_run_tasks(run_id,ordinal,task_id) VALUES($1,$2,$3)", n.ID, i, t.ID); err != nil {
			return NovaRun{}, false, normalizeError(err)
		}
	}
	return n, true, tx.Commit(ctx)
}

func (s *Store) NovaRun(ctx context.Context, user, id string) (NovaRun, error) {
	return scanNovaRun(s.Pool.QueryRow(ctx, "SELECT "+novaRunColumns+" FROM nova_runs WHERE id=$1 AND user_id=$2", id, user))
}

func (s *Store) NovaRunTaskIDs(ctx context.Context, user, id string) ([]string, error) {
	if _, err := s.NovaRun(ctx, user, id); err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, "SELECT nt.task_id FROM nova_run_tasks nt JOIN nova_runs n ON n.id=nt.run_id WHERE n.id=$1 AND n.user_id=$2 ORDER BY nt.ordinal", id, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) NovaRuns(ctx context.Context, user, before string, limit int) ([]NovaRun, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrConflict
	}
	rows, err := s.Pool.Query(ctx, "SELECT "+novaRunColumns+` FROM nova_runs WHERE user_id=$1
 AND ($2='' OR (created_at,id)<(SELECT created_at,id FROM nova_runs WHERE id=$2 AND user_id=$1))
 ORDER BY created_at DESC,id DESC LIMIT $3`, user, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []NovaRun{}
	for rows.Next() {
		n, err := scanNovaRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) AcknowledgeNovaRun(ctx context.Context, user, id string) (NovaRun, error) {
	return scanNovaRun(s.Pool.QueryRow(ctx, "UPDATE nova_runs SET acknowledged_at=COALESCE(acknowledged_at,now()) WHERE id=$1 AND user_id=$2 RETURNING "+novaRunColumns, id, user))
}

// CancelNovaRun records intent before invoking the business cancellation API.
// In-flight/uncertain work remains pending until that API's runner settles it.
// ReconcileNovaCancellations resumes a crash between child cancellations.
func (s *Store) CancelNovaRun(ctx context.Context, user, id string) (NovaRun, error) {
	n, err := scanNovaRun(s.Pool.QueryRow(ctx, "UPDATE nova_runs SET cancel_requested_at=COALESCE(cancel_requested_at,now()) WHERE id=$1 AND user_id=$2 RETURNING "+novaRunColumns, id, user))
	if err != nil {
		return n, err
	}
	if n.CancelDispatchedAt != nil {
		return n, nil
	}
	ids, err := s.NovaRunTaskIDs(ctx, user, id)
	if err != nil {
		return n, err
	}
	for _, task := range ids {
		if _, err = s.CancelTask(ctx, user, task); err != nil && !errors.Is(err, ErrTerminalConflict) {
			return n, err
		}
	}
	return scanNovaRun(s.Pool.QueryRow(ctx, "UPDATE nova_runs SET cancel_dispatched_at=COALESCE(cancel_dispatched_at,now()) WHERE id=$1 AND user_id=$2 RETURNING "+novaRunColumns, id, user))
}

func (s *Store) ReconcileNovaCancellations(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return ErrConflict
	}
	rows, err := s.Pool.Query(ctx, "SELECT "+novaRunColumns+" FROM nova_runs WHERE cancel_requested_at IS NOT NULL AND cancel_dispatched_at IS NULL ORDER BY created_at,id LIMIT $1", limit)
	if err != nil {
		return err
	}
	pending := []NovaRun{}
	for rows.Next() {
		n, e := scanNovaRun(rows)
		if e != nil {
			rows.Close()
			return e
		}
		pending = append(pending, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, n := range pending {
		if _, err = s.CancelNovaRun(ctx, n.UserID, n.ID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) NovaQueueCounts(ctx context.Context, user string) (queued, processing int, err error) {
	err = s.Pool.QueryRow(ctx, `SELECT count(*) FILTER(WHERE t.status='queued'),
 count(*) FILTER(WHERE t.status NOT IN ('queued','succeeded','failed','cancelled'))
 FROM nova_runs n JOIN nova_run_tasks nt ON nt.run_id=n.id JOIN tasks t ON t.id=nt.task_id WHERE n.user_id=$1`, user).Scan(&queued, &processing)
	return
}
