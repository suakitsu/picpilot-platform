package store

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/suakitsu/picpilot-platform/core/config"
	"github.com/suakitsu/picpilot-platform/core/security"
)

type LedgerEntry struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	Change     int64     `json:"change_amount"`
	Balance    int64     `json:"balance_after"`
	Kind       string    `json:"biz_type"`
	BusinessID string    `json:"biz_id"`
	Remark     string    `json:"remark"`
	CreatedAt  time.Time `json:"created_at"`
}

func (s *Store) AdjustCredits(ctx context.Context, userID string, amount int64, kind, businessID, remark, actor string) (LedgerEntry, error) {
	if s.Edition != config.Platform {
		return LedgerEntry{}, ErrConflict
	}
	if kind == "" || businessID == "" || amount == 0 {
		return LedgerEntry{}, ErrConflict
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return LedgerEntry{}, err
	}
	defer tx.Rollback(context.Background())
	entry, err := adjustCredits(ctx, tx, userID, amount, kind, businessID, remark, actor)
	if err != nil {
		return LedgerEntry{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return LedgerEntry{}, err
	}
	return entry, nil
}

// Locking the balance serializes every credit change and the idempotency check.
func adjustCredits(ctx context.Context, tx pgx.Tx, userID string, amount int64, kind, businessID, remark, actor string) (LedgerEntry, error) {
	var balance int64
	if err := tx.QueryRow(ctx, "SELECT credits FROM users WHERE id=$1 FOR UPDATE", userID).Scan(&balance); err != nil {
		return LedgerEntry{}, normalizeError(err)
	}
	var entry LedgerEntry
	err := tx.QueryRow(ctx, `SELECT id,user_id,change_amount,balance_after,biz_type,biz_id,remark,created_at FROM credit_ledger WHERE user_id=$1 AND biz_type=$2 AND biz_id=$3`, userID, kind, businessID).Scan(&entry.ID, &entry.UserID, &entry.Change, &entry.Balance, &entry.Kind, &entry.BusinessID, &entry.Remark, &entry.CreatedAt)
	if err == nil {
		if entry.Change != amount {
			return LedgerEntry{}, ErrConflict
		}
		return entry, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return LedgerEntry{}, err
	}
	if amount == math.MinInt64 || (amount < 0 && balance < -amount) {
		return LedgerEntry{}, ErrInsufficientCredits
	}
	if amount > 0 && balance > math.MaxInt64-amount {
		return LedgerEntry{}, ErrConflict
	}
	balance += amount
	if _, err = tx.Exec(ctx, "UPDATE users SET credits=$2,updated_at=now() WHERE id=$1", userID, balance); err != nil {
		return LedgerEntry{}, err
	}
	entry = LedgerEntry{ID: security.NewID("led"), UserID: userID, Change: amount, Balance: balance, Kind: kind, BusinessID: businessID, Remark: remark}
	err = tx.QueryRow(ctx, `INSERT INTO credit_ledger(id,user_id,change_amount,balance_after,biz_type,biz_id,remark,actor_id) VALUES($1,$2,$3,$4,$5,$6,$7,NULLIF($8,'')) RETURNING created_at`, entry.ID, userID, amount, balance, kind, businessID, remark, actor).Scan(&entry.CreatedAt)
	return entry, err
}

func refundTask(ctx context.Context, tx pgx.Tx, t Task) error {
	if t.CreditsCost == 0 {
		return nil
	}
	// Older commerce batches charged one aggregate ledger entry instead of a
	// generation_charge per child. Recognize that shape before the ordinary
	// task receipt lookup so a failed historical child is refunded exactly once.
	if handled, err := refundCommerceTask(ctx, tx, t); handled {
		return err
	}
	var charged int64
	err := tx.QueryRow(ctx, "SELECT change_amount FROM credit_ledger WHERE user_id=$1 AND biz_type='generation_charge' AND biz_id=$2", t.UserID, t.ID).Scan(&charged)
	if err != nil {
		return err
	}
	if charged != -t.CreditsCost {
		return ErrConflict
	}
	_, err = adjustCredits(ctx, tx, t.UserID, t.CreditsCost, "generation_refund", t.ID, "Generation did not complete", "")
	return err
}
