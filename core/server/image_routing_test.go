package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/suakitsu/picpilot-platform-go/core/config"
	"github.com/suakitsu/picpilot-platform-go/core/jobs"
	"github.com/suakitsu/picpilot-platform-go/core/provider"
	"github.com/suakitsu/picpilot-platform-go/core/store"
)

func TestImageModelSelectionOrdersPrimaryWithoutIncludingUnrequestedModels(t *testing.T) {
	providers := []store.Provider{
		{ID: "fallback", Capability: "image", Model: "secondary"},
		{ID: "unrelated", Capability: "image", Model: "different"},
		{ID: "primary-local", Capability: "image", Model: "primary"},
		{ID: "primary-http", Capability: "image", Model: "primary"},
		{ID: "text", Capability: "text", Model: "primary"},
	}
	for _, test := range []struct {
		input jobs.ImageInput
		ids   []string
	}{
		{jobs.ImageInput{Model: "primary", FallbackModel: "secondary"}, []string{"primary-local", "primary-http", "fallback"}},
		{jobs.ImageInput{Model: "primary"}, []string{"primary-local", "primary-http"}},
		{jobs.ImageInput{Model: "missing"}, []string{}},
		{jobs.ImageInput{}, []string{"fallback", "unrelated", "primary-local", "primary-http"}},
	} {
		ids := []string{}
		for _, p := range orderedImageProviders(providers, test.input) {
			ids = append(ids, p.ID)
		}
		if !reflect.DeepEqual(ids, test.ids) {
			t.Fatal("routing constraints changed", ids, test.ids)
		}
	}
}

func TestPostgresNovaExplicitImageModelReachesOnlySelectedProvider(t *testing.T) {
	for _, edition := range []config.Edition{config.Platform, config.Personal} {
		t.Run(string(edition), func(t *testing.T) {
			s, token, user := novaTestServer(t, edition)
			ctx := context.Background()
			var calls, unrelatedCalls atomic.Int32
			mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct {
					Model string `json:"model"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "fixture-image" {
					t.Error("selected model was not forwarded")
					w.WriteHeader(400)
					return
				}
				calls.Add(1)
				mockImageReply(w)
			}))
			defer mock.Close()
			unrelated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				unrelatedCalls.Add(1)
				w.WriteHeader(500)
			}))
			defer unrelated.Close()
			projectProvider(t, s, user, "image", mock.URL)
			scope := "platform"
			if edition == config.Personal {
				scope = user.ID
			}
			p, err := s.Store.Provider(ctx, scope, "image")
			if err != nil {
				t.Fatal(err)
			}
			p.ID, p.Model, p.BaseURL, p.Priority, p.Version = "unrelated", "other-image", unrelated.URL, -10, 0
			secret, err := s.Vault.Encrypt(scope+"/"+p.ID, "fixture-only-key")
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Store.PutProvider(ctx, p, &secret); err != nil {
				t.Fatal(err)
			}
			before, err := s.Store.UserByID(ctx, user.ID)
			if err != nil {
				t.Fatal(err)
			}
			in := novaImageFixture()
			in.Model = "missing-image"
			novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "unknown-image-model", in), 409)
			after, err := s.Store.UserByID(ctx, user.ID)
			if err != nil || after.Credits != before.Credits {
				t.Fatal("invalid selection charged credits", err)
			}
			in.Model = "fixture-image"
			id := novaObject(t, novaRequest(s, "POST", "/api/nova/tasks", token, "selected-image-model", in), 202)["taskId"].(string)
			lease, err := s.Store.ClaimTask(ctx, "fixture-model-selector", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			var payload jobs.Payload
			if json.Unmarshal(lease.Request, &payload) != nil || payload.Image.Model != in.Model {
				t.Fatal("selector was not persisted")
			}
			gateway, err := provider.NewGateway(provider.Options{AllowLocalDevelopment: true})
			if err != nil {
				t.Fatal(err)
			}
			defer gateway.CloseIdleConnections()
			s.processHTTPModelTask(ctx, lease, gateway)
			view := novaObject(t, novaRequest(s, "GET", "/api/nova/tasks/"+id, token, "", nil), 200)
			if view["status"] != "completed" || calls.Load() != 1 || unrelatedCalls.Load() != 0 {
				t.Fatal("model routing violated selection", view["status"], calls.Load(), unrelatedCalls.Load())
			}
		})
	}
}
