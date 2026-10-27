package store

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/security"
)

func TestPostgresMaintenanceOnlyCollectsHiddenAbandonedCandidates(t *testing.T) {
	s := testStore(t, config.Platform)
	u := testUser(t, s, "maintenance@example.invalid")
	ctx := context.Background()
	create := func(kind string, old bool) Asset {
		id := security.NewID("asset")
		a, err := s.SaveAsset(ctx, Asset{ID: id, UserID: u.ID, ImageType: kind, StorageKey: "aa/" + id + ".png", ContentType: "image/png", FileSize: 3, SHA256: strings.Repeat("a", 64)})
		if err != nil {
			t.Fatal(err)
		}
		if old {
			if _, err = s.Pool.Exec(ctx, `UPDATE assets SET created_at=clock_timestamp()-interval '25 hours' WHERE id=$1`, id); err != nil {
				t.Fatal(err)
			}
		}
		return a
	}
	orphan := create("worker-candidate", true)
	fresh := create("worker-candidate", false)
	accepted := create("worker-output", true)
	reference := create("reference", true)
	if _, err := s.OwnedAsset(ctx, u.ID, orphan.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("unaccepted output exposed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.QueueAbandonedWorkerAssets(ctx); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	keys, err := s.PendingAssetDeletion(ctx)
	if err != nil || len(keys) != 1 || keys[0] != orphan.StorageKey {
		t.Fatal("cleanup queue", err, keys)
	}
	for _, a := range []Asset{fresh, accepted, reference} {
		exists, err := s.AssetStorageKeyExists(ctx, a.StorageKey)
		if err != nil || !exists {
			t.Fatal("live asset removed", a.ImageType)
		}
	}
	if err := s.AcknowledgeAssetDeletion(ctx, orphan.StorageKey); err != nil {
		t.Fatal(err)
	}
	keys, err = s.PendingAssetDeletion(ctx)
	if err != nil || len(keys) != 0 {
		t.Fatal("cleanup receipt not idempotent")
	}
	if err := s.AcknowledgeAssetDeletion(ctx, orphan.StorageKey); err != nil {
		t.Fatal(err)
	}
	if err := s.PurgeExpiredAuthStateBefore(ctx, time.Now().Add(-24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}
