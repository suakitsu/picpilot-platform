package taskrunner

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/internal/testpg"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func TestPostgresRunnerProcessesOnceAndJoins(t *testing.T) {
	s := &store.Store{Pool: testpg.Open(t), Edition: config.Personal}
	ctx := context.Background()
	if err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	u, err := s.Initialize(ctx, "fixture@example.invalid", "fixture", "Owner")
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = s.SubmitTask(ctx, store.SubmitTask{UserID: u.ID, Capability: "image", Kind: "fixture", IdempotencyKey: "one", Request: json.RawMessage(`{"version":1,"image":{"prompt":"mock","size":"1024x1024"}}`)})
	if err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var calls atomic.Int32
	finished := make(chan struct{}, 1)
	r := Runner{Store: s, Concurrency: 6, IdleDelay: 50 * time.Millisecond, Process: func(ctx context.Context, task store.Task) {
		calls.Add(1)
		if _, err := s.CompleteTask(ctx, task, json.RawMessage(`{"mock":true}`)); err != nil {
			t.Error(err)
		}
		finished <- struct{}{}
	}}
	done := make(chan error, 1)
	go func() { done <- r.Run(runCtx) }()
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not process task")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runner did not stop")
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate processing")
	}
}

func TestPayloadValidation(t *testing.T) {
	if (jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "mock", Size: "1024x1024"}}).Validate("image") != nil {
		t.Fatal("valid input rejected")
	}
	if (jobs.Payload{Version: 1, Image: &jobs.ImageInput{Prompt: "mock"}, Text: &jobs.TextInput{Input: json.RawMessage(`"mock"`)}}).Validate("image") == nil {
		t.Fatal("mixed task accepted")
	}
}
