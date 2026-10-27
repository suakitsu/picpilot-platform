package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
)

func TestPostgresRiskGatePrecedesTaskChargeAndDeduplicates(t *testing.T) {
	s := testServer(t, config.Platform)
	token := initialize(t, s)
	u, err := s.Store.UserByEmail(context.Background(), "owner@example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.AdminRiskKeyword(context.Background(), u.ID, "fixture-risk-config", "high", "fixture-blockword", false); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Store.Pool.Exec(context.Background(), `UPDATE users SET credits=50 WHERE id=$1`, u.ID); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		r := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(`{"capability":"image","payload":{"version":1,"image":{"prompt":"fixture-blockword","size":"1024x1024"}}}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Idempotency-Key", "fixture-risk-retry")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 422 || !strings.Contains(w.Body.String(), "CONTENT_BLOCKED") {
			t.Fatal("risk gate not applied", w.Code)
		}
	}
	var tasks, ledger, events, credits int
	if err = s.Store.Pool.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM tasks),(SELECT count(*) FROM credit_ledger),(SELECT count(*) FROM admin_risk_events),(SELECT credits FROM users WHERE id=$1)`, u.ID).Scan(&tasks, &ledger, &events, &credits); err != nil {
		t.Fatal(err)
	}
	if tasks != 0 || ledger != 0 || events != 1 || credits != 50 {
		t.Fatal("risk hit charged or duplicated", tasks, ledger, events, credits)
	}
}

func TestDirectTextScreeningIncludesInstructionsButNotImageURLs(t *testing.T) {
	p := jobs.Payload{Version: 1, Text: &jobs.TextInput{Input: json.RawMessage(`[{"role":"user","content":[{"type":"input_text","text":"user intent"},{"type":"input_image","image_url":"https://fixture.invalid/not-a-prompt"}]}]`), Options: map[string]json.RawMessage{"instructions": json.RawMessage(`"user supplied instruction"`)}}}
	text := directUserText(p)
	if !strings.Contains(text, "user intent") || !strings.Contains(text, "user supplied instruction") || strings.Contains(text, "https:") {
		t.Fatal("text screening extraction mismatch")
	}
}
