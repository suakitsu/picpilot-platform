package store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
)

func TestPostgresCircuitAndConfigVersionIsolation(t *testing.T) {
	s := testStore(t, config.Personal)
	u := testUser(t, s, "fixture@example.invalid")
	p := providerFixture(t, s, u.ID, 2)
	ctx := context.Background()
	if err := s.RecordProviderOutcome(ctx, p, "AUTHENTICATION", true); err != nil {
		t.Fatal(err)
	}
	health, err := s.ProviderHealth(ctx, u.ID)
	if err != nil || len(health) != 1 || health[0].OpenUntil == nil || time.Until(*health[0].OpenUntil) < 29*time.Minute {
		t.Fatal("auth circuit duration incorrect")
	}
	submitFixture(t, s, u, "blocked", 0)
	lease, _ := s.ClaimTask(ctx, "test", time.Minute)
	if _, err := s.ReserveAttempt(ctx, lease, u.ID, p.ID); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal("open circuit accepted task")
	}
	updated, err := s.PutProvider(ctx, p, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordProviderOutcome(ctx, p, "STALE_CONFIG_FAILURE", true); err != nil {
		t.Fatal(err)
	}
	health, _ = s.ProviderHealth(ctx, u.ID)
	if len(health) != 0 {
		t.Fatal("old configuration reopened circuit")
	}
	a, err := s.ReserveAttempt(ctx, lease, u.ID, p.ID)
	if err != nil || a.Provider.Version != updated.Version {
		t.Fatal("attempt did not capture latest provider version")
	}
}

func TestCircuitDefaultsAndOverrides(t *testing.T) {
	if p := ProviderCircuitPolicy(Provider{Capability: "text"}); p.TransientSeconds != 120 || p.AuthSeconds != 1800 {
		t.Fatal("text defaults changed")
	}
	if p := ProviderCircuitPolicy(Provider{Capability: "image"}); p.TransientSeconds != 600 {
		t.Fatal("image defaults changed")
	}
	p := ProviderCircuitPolicy(Provider{Capability: "image", Settings: json.RawMessage(`{"circuit_breaker":{"failure_threshold":3,"transient_seconds":60,"auth_seconds":300}}`)})
	if p.FailureThreshold != 3 || p.TransientSeconds != 60 || p.AuthSeconds != 300 {
		t.Fatal("policy overrides ignored")
	}
}
