package server

import (
	"context"
	"errors"
	"os"
	"sync"
	"time"

	"github.com/suakitsu/picpilot-platform/core/store"
)

func (s *Server) RunBackground(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	runners := []func(context.Context) error{s.RunTasks, s.RunCreativeWorkflows, s.RunVideoTasks, s.RunMaintenance, s.RunBillingReconciliation, s.RunNovaMaintenance}
	var wg sync.WaitGroup
	errorsCh := make(chan error, len(runners))
	for _, run := range runners {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				errorsCh <- err
				cancel()
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	return <-errorsCh
}

func (s *Server) RunBillingReconciliation(ctx context.Context) error {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			run, cancel := context.WithTimeout(ctx, 45*time.Second)
			err := s.ReconcileBilling(run, 30)
			cancel()
			if err != nil && ctx.Err() == nil {
				s.Logger.Error("payment reconciliation failed", "sqlstate", store.SQLState(err))
			}
		}
	}
}

func (s *Server) RunMaintenance(ctx context.Context) error {
	recovery := time.NewTicker(15 * time.Second)
	cleanup := time.NewTicker(time.Hour)
	defer recovery.Stop()
	defer cleanup.Stop()
	s.maintainWorkerRecovery(ctx)
	s.maintainStorage(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-recovery.C:
			s.maintainWorkerRecovery(ctx)
		case <-cleanup.C:
			s.maintainStorage(ctx)
		}
	}
}

func (s *Server) maintainWorkerRecovery(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	scopes, err := s.Store.WorkerRecoveryScopes(ctx)
	if err != nil {
		s.maintenanceError(ctx, "worker recovery scopes", err)
		return
	}
	for _, scope := range scopes {
		if _, err := s.Store.RecoverWorkerJobs(ctx, scope); err != nil {
			s.maintenanceError(ctx, "worker recovery", err)
			return
		}
	}
	if _, err := s.Store.SyncTextCallTelemetry(ctx, 100); err != nil {
		s.maintenanceError(ctx, "text telemetry", err)
	}
}

func (s *Server) maintainStorage(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	if err := s.Store.PurgeExpiredAuthState(ctx); err != nil {
		s.maintenanceError(ctx, "auth cleanup", err)
	}
	if err := s.Store.QueueAbandonedWorkerAssets(ctx); err != nil {
		s.maintenanceError(ctx, "candidate cleanup", err)
	}
	keys, err := s.Store.PendingAssetDeletion(ctx)
	if err != nil {
		s.maintenanceError(ctx, "asset cleanup queue", err)
		return
	}
	for _, key := range keys {
		exists, err := s.Store.AssetStorageKeyExists(ctx, key)
		if err != nil {
			s.maintenanceError(ctx, "asset cleanup lookup", err)
			return
		}
		if exists {
			continue
		}
		if err := s.Files.Remove(key); err == nil || errors.Is(err, os.ErrNotExist) {
			if err = s.Store.AcknowledgeAssetDeletion(ctx, key); err != nil {
				s.maintenanceError(ctx, "asset cleanup receipt", err)
				return
			}
		}
	}
	// The quarantine exceeds every request/provider lease deadline. A database
	// outage never authorizes deleting a file; the next sweep can retry safely.
	before := time.Now().Add(-48 * time.Hour)
	keys, err = s.Files.CleanupCandidates(before, s.cleanupCursor, 100)
	if err != nil {
		s.maintenanceError(ctx, "orphan file scan", err)
		return
	}
	if len(keys) == 0 {
		s.cleanupCursor = ""
	}
	for _, key := range keys {
		if ctx.Err() != nil {
			return
		}
		exists, err := s.Store.AssetStorageKeyExists(ctx, key)
		if err != nil {
			s.maintenanceError(ctx, "orphan file lookup", err)
			return
		}
		if !exists {
			_ = s.Files.RemoveCleanupCandidate(key, before)
		}
		s.cleanupCursor = key
	}
}

func (s *Server) maintenanceError(ctx context.Context, operation string, err error) {
	if ctx.Err() == nil {
		s.Logger.Error("maintenance operation failed", "operation", operation, "sqlstate", store.SQLState(err))
	}
}
