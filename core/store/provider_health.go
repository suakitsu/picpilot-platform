package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

var ErrCircuitOpen = errors.New("provider circuit is open")

type CircuitPolicy struct {
	FailureThreshold int `json:"failure_threshold"`
	AuthSeconds      int `json:"auth_seconds"`
	TransientSeconds int `json:"transient_seconds"`
}

func ProviderCircuitPolicy(p Provider) CircuitPolicy {
	policy := CircuitPolicy{FailureThreshold: 1, AuthSeconds: 1800, TransientSeconds: 600}
	if p.Capability == "text" {
		policy.TransientSeconds = 120
	}
	var settings struct {
		CircuitBreaker json.RawMessage `json:"circuit_breaker"`
	}
	if json.Unmarshal(p.Settings, &settings) == nil && len(settings.CircuitBreaker) > 0 {
		var custom struct {
			Threshold *int `json:"failure_threshold"`
			Auth      *int `json:"auth_seconds"`
			Transient *int `json:"transient_seconds"`
		}
		if json.Unmarshal(settings.CircuitBreaker, &custom) == nil {
			if custom.Threshold != nil && *custom.Threshold >= 1 && *custom.Threshold <= 20 {
				policy.FailureThreshold = *custom.Threshold
			}
			if custom.Auth != nil && *custom.Auth >= 0 && *custom.Auth <= 86400 {
				policy.AuthSeconds = *custom.Auth
			}
			if custom.Transient != nil && *custom.Transient >= 0 && *custom.Transient <= 86400 {
				policy.TransientSeconds = *custom.Transient
			}
		}
	}
	return policy
}

func providerCircuitReady(ctx context.Context, tx pgx.Tx, p Provider) error {
	var open, probing bool
	err := tx.QueryRow(ctx, `SELECT COALESCE(open_until>clock_timestamp(),false),COALESCE(probe_until>clock_timestamp(),false) FROM provider_health WHERE owner_scope=$1 AND provider_id=$2`, p.OwnerScope, p.ID).Scan(&open, &probing)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if open || probing {
		return ErrCircuitOpen
	}
	_, err = tx.Exec(ctx, `UPDATE provider_health SET probe_until=now()+$3*interval '1 second' WHERE owner_scope=$1 AND provider_id=$2 AND open_until IS NOT NULL`, p.OwnerScope, p.ID, p.TimeoutSeconds+60)
	return err
}

// Results from a previous credential/configuration version cannot reopen the
// circuit after an administrator changes that provider's settings.
func (s *Store) RecordProviderOutcome(ctx context.Context, p Provider, code string, authenticationFailure bool) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	var version int64
	err = tx.QueryRow(ctx, "SELECT version FROM providers WHERE owner_scope=$1 AND id=$2 FOR UPDATE", p.OwnerScope, p.ID).Scan(&version)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if version != p.Version {
		return nil
	}
	if code == "" {
		_, err = tx.Exec(ctx, "DELETE FROM provider_health WHERE owner_scope=$1 AND provider_id=$2", p.OwnerScope, p.ID)
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	policy := ProviderCircuitPolicy(p)
	seconds := policy.TransientSeconds
	if authenticationFailure {
		seconds = policy.AuthSeconds
	}
	if seconds == 0 {
		return nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO provider_health(owner_scope,provider_id,consecutive_failures,open_until,last_error_code) VALUES($1,$2,1,CASE WHEN $5<=1 THEN now()+$4*interval '1 second' END,$3)
 ON CONFLICT(owner_scope,provider_id) DO UPDATE SET consecutive_failures=provider_health.consecutive_failures+1,
 open_until=CASE WHEN provider_health.consecutive_failures+1>=$5 THEN now()+$4*interval '1 second' ELSE NULL END,probe_until=NULL,last_error_code=$3,updated_at=now()`, p.OwnerScope, p.ID, code, seconds, policy.FailureThreshold)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type ProviderHealth struct {
	ProviderID          string     `json:"provider_id"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	OpenUntil           *time.Time `json:"open_until"`
	LastErrorCode       string     `json:"last_error_code"`
}

func (s *Store) ProviderHealth(ctx context.Context, scope string) ([]ProviderHealth, error) {
	rows, err := s.Pool.Query(ctx, "SELECT provider_id,consecutive_failures,open_until,last_error_code FROM provider_health WHERE owner_scope=$1", scope)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]ProviderHealth, 0)
	for rows.Next() {
		var h ProviderHealth
		if err := rows.Scan(&h.ProviderID, &h.ConsecutiveFailures, &h.OpenUntil, &h.LastErrorCode); err != nil {
			return nil, err
		}
		items = append(items, h)
	}
	return items, rows.Err()
}
