package config

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestSecretsPersistAndAreIndependent(t *testing.T) {
	dir := t.TempDir()
	a, err := loadSecrets(dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadSecrets(dir)
	if err != nil || a != b {
		t.Fatal("secrets changed on restart")
	}
	if a.Auth == a.Encryption || a.WorkerPepper == a.WorkerSigning || a.Setup == "" {
		t.Fatal("secrets not independent")
	}
	data, _ := os.ReadFile(filepath.Join(dir, "instance-secrets.json"))
	if !bytes.Contains(data, []byte(a.Auth)) {
		t.Fatal("secret persistence failed")
	}
}

func TestConcurrentInitializersShareOneKeySet(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	results := make(chan secrets, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := loadSecrets(dir)
			if err != nil {
				t.Error(err)
				return
			}
			results <- s
		}()
	}
	wg.Wait()
	close(results)
	var first secrets
	for s := range results {
		if first.Auth == "" {
			first = s
		}
		if s != first {
			t.Fatal("instances generated different keys")
		}
	}
}

func TestLoadRequiresExplicitDatabaseAndSafePublicOrigin(t *testing.T) {
	t.Setenv("PICPILOT_DATA_DIR", t.TempDir())
	t.Setenv("DATABASE_URL", "")
	t.Setenv("PICPILOT_ALLOW_PRIVATE_PROVIDERS", "false")
	if _, err := Load(Platform); err == nil {
		t.Fatal("missing DB accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://test.invalid/fixture")
	t.Setenv("PICPILOT_PUBLIC_URL", "https://user:pass@example.invalid")
	if _, err := Load(Personal); err == nil {
		t.Fatal("credential-bearing origin accepted")
	}
	t.Setenv("PICPILOT_PUBLIC_URL", "http://localhost:8080")
	c, err := Load(Personal)
	if err != nil {
		t.Fatal(err)
	}
	if c.RegistrationEnabled || c.DemoMode || c.AllowPrivateProviders {
		t.Fatal("unsafe default feature enabled")
	}
}

func TestLocalOriginEnablesPrivateProvidersInAutoMode(t *testing.T) {
	t.Setenv("PICPILOT_DATA_DIR", t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://test.invalid/fixture")
	t.Setenv("PICPILOT_ALLOW_PRIVATE_PROVIDERS", "auto")
	t.Setenv("PICPILOT_BIND_ADDRESS", "127.0.0.1")
	for _, origin := range []string{"http://localhost:8080", "http://studio.localhost:8080", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		t.Setenv("PICPILOT_PUBLIC_URL", origin)
		c, err := Load(Personal)
		if err != nil || !c.AllowPrivateProviders {
			t.Fatalf("local origin %s did not enable auto local providers: %v", origin, err)
		}
	}
	t.Setenv("PICPILOT_PUBLIC_URL", "https://picpilot.example.com")
	c, err := Load(Personal)
	if err != nil || c.AllowPrivateProviders {
		t.Fatalf("public origin enabled auto local providers: %v", err)
	}
	t.Setenv("PICPILOT_PUBLIC_URL", "http://localhost:8080")
	t.Setenv("PICPILOT_BIND_ADDRESS", "0.0.0.0")
	c, err = Load(Personal)
	if err != nil || c.AllowPrivateProviders {
		t.Fatalf("public bind enabled auto local providers: %v", err)
	}
	t.Setenv("PICPILOT_ALLOW_PRIVATE_PROVIDERS", "true")
	c, err = Load(Personal)
	if err != nil || !c.AllowPrivateProviders {
		t.Fatalf("explicit local provider opt-in failed: %v", err)
	}
}

func TestWorkerTimingBounds(t *testing.T) {
	t.Setenv("PICPILOT_DATA_DIR", t.TempDir())
	t.Setenv("DATABASE_URL", "postgres://test.invalid/fixture")
	t.Setenv("PICPILOT_PUBLIC_URL", "https://fixture.invalid")
	t.Setenv("PICPILOT_WORKER_LEASE_GRACE_SECONDS", "15")
	t.Setenv("PICPILOT_WORKER_INPUT_TTL_SECONDS", "300")
	c, err := Load(Personal)
	if err != nil || c.WorkerGraceSeconds != 15 || c.WorkerInputTTLSeconds != 300 {
		t.Fatalf("worker defaults: %v", err)
	}
	for _, field := range []string{"PICPILOT_WORKER_LEASE_GRACE_SECONDS", "PICPILOT_WORKER_INPUT_TTL_SECONDS"} {
		for _, value := range []string{"-1", "0", "9999", "invalid"} {
			t.Setenv(field, value)
			if _, err := Load(Personal); err == nil {
				t.Fatalf("invalid timing %s accepted", field)
			}
		}
		t.Setenv(field, "30")
	}
}
