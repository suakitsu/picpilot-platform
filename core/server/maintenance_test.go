package server

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform/core/config"
	wp "github.com/suakitsu/picpilot-platform/core/workerprotocol"
)

func TestWorkerOuterMiddlewareKeepsProtocolEnvelope(t *testing.T) {
	s := testServer(t, config.Platform)
	for _, method := range []string{"OPTIONS", "POST"} {
		r := httptest.NewRequest(method, "https://fixture.invalid"+wp.BasePath+"/heartbeat", nil)
		r.Header.Set("Origin", "https://untrusted.invalid")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		workerHTTPError(t, w, 400, "REQUEST_SCHEMA_INVALID")
	}
}

func TestBackgroundMaintenanceShutdown(t *testing.T) {
	s := testServer(t, config.Personal)
	initialize(t, s)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.RunBackground(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("background services did not join")
	}
}
