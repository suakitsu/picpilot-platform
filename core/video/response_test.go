package video

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func nested(path string, value any) map[string]any {
	parts := strings.Split(path, ".")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "0" {
			value = []any{value}
		} else {
			value = map[string]any{parts[i]: value}
		}
	}
	return value.(map[string]any)
}

func fixtureTask(t *testing.T, provider Provider) Submission {
	t.Helper()
	model, err := ResolveModel(provider, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return Submission{UpstreamTaskID: "task_1", Provider: provider, Model: model}
}

func TestCreateIDVariants(t *testing.T) {
	for provider, paths := range map[Provider][]string{
		Gemini:   {"data.0.task_id", "data.0.id", "task_id", "data.task_id", "id", "data.id"},
		Seedance: {"data.0.task_id", "data.0.id", "task_id", "data.task_id", "id", "data.id"},
		Agnes:    {"video_id", "data.video_id", "id", "data.id"},
		HFSY:     {"task_id", "data.task_id", "id", "data.id", "result.task_id", "data.result.task_id"},
	} {
		for _, path := range paths {
			t.Run(string(provider)+"/"+path, func(t *testing.T) {
				g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
					_ = json.NewEncoder(w).Encode(nested(path, "task_1"))
				})
				s, err := g.Submit(context.Background(), validRequest())
				if err != nil || s.UpstreamTaskID != "task_1" {
					t.Fatalf("%+v %v", s, err)
				}
			})
		}
	}
}

func TestPollURLVariants(t *testing.T) {
	apimartPaths := []string{
		"data.result.videos.0.url.0", "data.result.videos.0.url", "result.videos.0.url.0", "result.videos.0.url",
		"data.result.video_url", "data.result.url", "data.video_url", "data.url", "result.video_url", "result.url", "video_url", "url",
	}
	for provider, paths := range map[Provider][]string{
		Gemini: apimartPaths, Seedance: apimartPaths,
		HFSY:  {"result_url", "data.result_url", "result.url", "data.result.url", "video_url", "data.video_url", "url", "data.url"},
		Agnes: {"metadata.url", "data.metadata.url", "url", "data.url"},
	} {
		for _, path := range paths {
			t.Run(string(provider)+"/"+path, func(t *testing.T) {
				body := nested(path, fixtureVideo)
				body["status"] = "SUCCESS"
				body["seconds"] = "10.2500"
				g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
					if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+fixtureKey {
						t.Error("incorrect poll method/auth")
					}
					switch provider {
					case HFSY:
						if r.URL.Path != "/v1/video/query" || r.URL.Query().Get("id") != "task_1" {
							t.Error("incorrect HFSY query")
						}
					case Agnes:
						if r.URL.Path != "/agnesapi" || r.URL.Query().Get("video_id") != "task_1" {
							t.Error("incorrect Agnes query")
						}
					default:
						if r.URL.Path != "/v1/tasks/task_1" {
							t.Error("incorrect APIMart query")
						}
					}
					_ = json.NewEncoder(w).Encode(body)
				})
				result, err := g.Poll(context.Background(), fixtureTask(t, provider))
				if err != nil || result.Status != Completed || result.Progress != 100 || result.ResultURL != fixtureVideo || result.Seconds != "10.2500" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			})
		}
	}
}

func TestPollStatusProgressAndFailures(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance, HFSY, Agnes} {
		for raw, want := range map[string]Status{
			"QUEUED": Submitted, "pending": Submitted, "SUBMITTED": Submitted,
			"running": Generating, "processing": Generating, "": Generating, "incomplete": Generating,
			"FAILED": Failed, "ERROR": Failed, "cancelled": Failed, "unsuccessful": Failed,
			"SUCCESS": Completed, "succeeded": Completed, "completed": Completed, "finished": Completed, "done": Completed, "not_completed": Generating,
		} {
			t.Run(string(provider)+"/"+raw, func(t *testing.T) {
				g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) {
					body := map[string]any{"data": map[string]any{"state": raw, "progress": "74%", "url": fixtureVideo}}
					if want == Failed {
						body["error"] = map[string]any{"message": "private full prompt " + fixtureKey}
						body["fail_reason"] = "private full prompt " + fixtureKey
					}
					_ = json.NewEncoder(w).Encode(body)
				})
				result, err := g.Poll(context.Background(), fixtureTask(t, provider))
				if err != nil || result.Status != want {
					t.Fatalf("status=%s err=%v", result.Status, err)
				}
				progress := 74
				if want == Failed || want == Completed {
					progress = 100
				}
				if result.Progress != progress {
					t.Errorf("progress=%d want=%d", result.Progress, progress)
				}
				if want == Failed && result.Error != "upstream video generation failed" {
					t.Error("failure not safely normalized")
				}
				encoded, _ := json.Marshal(result)
				if strings.Contains(string(encoded), fixtureKey) || strings.Contains(string(encoded), "private full prompt") {
					t.Fatal("sensitive failure contents exposed")
				}
			})
		}
	}
	for _, tc := range []struct {
		progress any
		want     int
	}{{-20, 10}, {1000, 95}, {"100%", 95}, {"3%", 10}, {nil, 10}, {"invalid", 10}, {"42", 42}, {42.75, 42}} {
		g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"status": "running", "progress": tc.progress})
		})
		result, err := g.Poll(context.Background(), fixtureTask(t, Gemini))
		if err != nil || result.Progress != tc.want {
			t.Fatalf("progress=%d err=%v", result.Progress, err)
		}
	}
}

func TestAgnesReturnedSizeAndMetadata(t *testing.T) {
	for _, body := range []string{
		`{"status":"completed","metadata":{"url":"` + fixtureVideo + `","size_mapping":{"width":704,"height":1280,"resolution":"720p"},"cost":"0.22"},"seconds":"10"}`,
		`{"data":{"status":"completed","metadata":{"url":"` + fixtureVideo + `","size_mapping":{"width":"704","height":"1280","resolution":"720p"},"cost":"0.22"},"seconds":10}}`,
		`{"status":"completed","url":"` + fixtureVideo + `","size":"704 X 1280","seconds":10,"cost":0.22}`,
		`{"data":{"state":"finished","url":"` + fixtureVideo + `","width":704,"height":1280,"seconds":10,"cost":"0.22"}}`,
	} {
		g, _ := gatewayFor(t, Agnes, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
		result, err := g.Poll(context.Background(), fixtureTask(t, Agnes))
		if err != nil || result.Width == nil || *result.Width != 704 || result.Height == nil || *result.Height != 1280 || result.Seconds != "10" || result.Metadata["actual_cost"] != "0.22" {
			t.Fatalf("size/metadata mismatch: %+v %v", result, err)
		}
	}
}

func TestHFSYReturnedDimensionsAndCosts(t *testing.T) {
	for model, cost := range map[string]string{"sd-2.0-mini": "1.200000", "sd-2.0-fast-480": "2.000000", "sd-2.0-fast": "3.000000", "sd-2.0-480": "3.000000", "sd-2.0": "4.000000"} {
		g, _ := gatewayFor(t, HFSY, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{"status":"SUCCESS","result":{"url":"` + fixtureVideo + `","width":1280,"height":720},"duration":8,"quota":12}}`))
		})
		task := fixtureTask(t, HFSY)
		task.Model = model
		result, err := g.Poll(context.Background(), task)
		if err != nil || result.Width == nil || *result.Width != 1280 || result.Height == nil || *result.Height != 720 || result.Seconds != "8" || result.Metadata["actual_cost"] != cost || result.Metadata["upstream_quota"] != "12" {
			t.Fatalf("HFSY metadata=%+v err=%v", result, err)
		}
	}
}

func TestAPIMartCallbacksAndCostNormalization(t *testing.T) {
	for _, provider := range []Provider{Gemini, Seedance} {
		g, _ := gatewayFor(t, provider, func(w http.ResponseWriter, r *http.Request) { t.Error("callback parser performed I/O") })
		for _, idPath := range []string{"task_id", "data.task_id", "data.0.task_id", "id", "data.id", "data.0.id"} {
			body := nested(idPath, "task_callback")
			body["status"] = "completed"
			body["result"] = map[string]any{"videos": []any{map[string]any{"url": []string{fixtureVideo}, "expires_at": 1785164334}}}
			body["credits_cost"], body["cost"], body["actual_time"], body["completed"] = 5.28, 0.528, 187, 1785077934
			payload, _ := json.Marshal(body)
			callback, err := g.ParseCallback(context.Background(), payload)
			if err != nil || callback.UpstreamTaskID != "task_callback" || callback.Result.Status != Completed || callback.Result.ResultURL != fixtureVideo {
				t.Fatalf("callback=%+v err=%v", callback, err)
			}
			want := map[string]string{
				"actual_cost": "3.585701", "upstream_cost": "5.28", "upstream_cost_unit": "APIMART_CREDIT",
				"cost_conversion_rate": "0.679110", "cost_source": "upstream_credits_cost", "upstream_credits_cost": "5.28",
				"upstream_actual_time_seconds": "187", "upstream_completed_epoch": "1785077934", "upstream_result_expires_at": "1785164334",
			}
			if !reflect.DeepEqual(callback.Result.Metadata, want) {
				t.Fatalf("metadata=%v", callback.Result.Metadata)
			}
		}
		for _, wrap := range []string{"", "data", "data.0"} {
			body := map[string]any{"id": "task_callback", "status": "completed", "cost": 0.528, "result": map[string]any{"videos": []any{map[string]any{"url": fixtureVideo}}}}
			if wrap != "" {
				body = nested(wrap, body)
			}
			payload, _ := json.Marshal(body)
			callback, err := g.ParseCallback(context.Background(), payload)
			if err != nil || callback.Result.Metadata["actual_cost"] != "3.585701" || callback.Result.Metadata["upstream_cost_unit"] != "USD" {
				t.Fatalf("USD callback=%+v err=%v", callback, err)
			}
		}
	}
}

func TestPollAndCallbackMalformedResults(t *testing.T) {
	for i, body := range []string{
		`{"status":"completed"}`,
		`{"status":"completed","url":"file:///private.mp4"}`,
		`{"status":"completed","url":"https://user:password@cdn.example.test/video.mp4"}`,
		`{"status":"completed","url":"https://cdn.example.test/video.mp4?api_key=key"}`,
		`{"status":"completed","url":"https://cdn.example.test/video.mp4?prompt=private"}`,
		`{"status":"completed","url":"https://cdn.example.test/` + fixtureKey + `"}`,
		`{"status":"completed","url":"https://cdn.example.test/video.mp4#fragment"}`,
		`{"data":[{},{}]}`,
		`{"data":[1]}`,
		`{"code":500,"message":"private full prompt"}`,
		`{"id":"wrong-task","status":"running"}`,
		`null`, `[]`, `{}`, `{"status":"running"} {"extra":true}`,
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			g, _ := gatewayFor(t, Gemini, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) })
			_, err := g.Poll(context.Background(), fixtureTask(t, Gemini))
			if body == "{}" {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, ErrProtocol) {
				t.Fatalf("expected protocol error, got %v", err)
			}
			if _, err := g.ParseCallback(context.Background(), []byte(body)); err == nil {
				if strings.Contains(body, `"id":"wrong-task"`) {
					return // Correlation with persisted IDs is the parent's responsibility.
				}
				t.Error("invalid callback accepted")
			}
		})
	}
}
