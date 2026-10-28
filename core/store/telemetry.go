package store

import (
	"context"
	"encoding/json"
	"unicode/utf8"

	"github.com/suakitsu/picpilot-platform/core/config"
)

// SyncTextCallTelemetry derives observability from durable execution state.
// It can replay after a crash; model calls and accounting never depend on it.
func (s *Store) SyncTextCallTelemetry(ctx context.Context, limit int) (int, error) {
	if s.Edition != config.Platform {
		return 0, nil
	}
	if limit < 1 || limit > 1000 {
		return 0, ErrConflict
	}
	rows, err := s.Pool.Query(ctx, `WITH source AS (
 SELECT a.id,t.user_id,t.id AS task_id,t.kind AS feature,t.kind AS target_type,a.provider_id AS provider,a.model,
 CASE a.status WHEN 'succeeded' THEN 'success' WHEN 'failed' THEN 'failed' WHEN 'uncertain' THEN 'uncertain' ELSE 'queued' END AS status,
 CASE WHEN a.status IN ('succeeded','failed','uncertain') THEN greatest(0,extract(epoch FROM (a.updated_at-a.submitted_at))*1000)::bigint ELSE 0 END AS duration_ms,
 a.error_code AS error_type,t.request_json AS input,t.result_json AS output,a.updated_at AS changed_at
 FROM task_attempts a JOIN tasks t ON t.id=a.task_id WHERE t.capability='text' AND a.submitted_at IS NOT NULL
 UNION ALL
 SELECT j.job_id,t.user_id,t.id,t.kind,t.kind,j.provider_id,j.model,
 CASE WHEN j.status='succeeded' THEN 'success' WHEN j.status='failed' THEN 'failed'
 WHEN j.status='cancelled' AND j.leased_at IS NULL THEN 'failed'
 WHEN j.status='cancelled' OR (j.status='expired' AND j.offline_action='fallback') THEN 'uncertain' ELSE 'queued' END,
 CASE WHEN j.status IN ('succeeded','failed','cancelled','expired') AND j.leased_at IS NOT NULL THEN greatest(0,extract(epoch FROM (coalesce(j.completed_at,j.updated_at)-j.leased_at))*1000)::bigint ELSE 0 END,
 j.error_code,t.request_json,j.result_json,j.updated_at
 FROM worker_jobs j JOIN tasks t ON t.id=j.task_id WHERE j.capability='text'
) SELECT s.id,s.user_id,s.task_id,s.feature,s.target_type,s.provider,s.model,s.status,s.duration_ms,s.error_type,s.input,s.output
 FROM source s LEFT JOIN admin_text_calls c ON c.id=s.id
 WHERE c.id IS NULL OR (c.status='queued' AND s.status<>'queued')
 ORDER BY s.changed_at,s.id LIMIT $1`, limit)
	if err != nil {
		return 0, err
	}
	type entry struct {
		metric        TextCallMetric
		input, output json.RawMessage
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		m := &e.metric
		if err = rows.Scan(&m.ID, &m.UserID, &m.TaskID, &m.Feature, &m.TargetType, &m.Provider, &m.Model, &m.Status, &m.DurationMS, &m.ErrorType, &e.input, &e.output); err != nil {
			rows.Close()
			return 0, err
		}
		m.InputChars = textCharacterCount(e.input, false)
		if m.Status == "success" {
			m.OutputChars = textCharacterCount(e.output, true)
		}
		m.Feature = telemetryLabel(m.Feature)
		m.TargetType = telemetryLabel(m.TargetType)
		m.Provider = telemetryLabel(m.Provider)
		m.Model = telemetryLabel(m.Model)
		m.ErrorType = telemetryLabel(m.ErrorType)
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	for i, e := range entries {
		if err := s.RecordTextCall(ctx, e.metric); err != nil {
			return i, err
		}
	}
	return len(entries), nil
}

func telemetryLabel(value string) string {
	if value == "" || adminLabel.MatchString(value) {
		return value
	}
	return "unavailable"
}

// Count semantic text fields, not base64, URLs, tool configuration or secrets.
func textCharacterCount(raw json.RawMessage, result bool) int64 {
	var root any
	if len(raw) > 1<<20 || json.Unmarshal(raw, &root) != nil {
		return 0
	}
	var count func(any) int64
	count = func(value any) int64 {
		switch v := value.(type) {
		case string:
			return int64(utf8.RuneCountInString(v))
		case []any:
			var n int64
			for _, item := range v {
				n += count(item)
			}
			return n
		case map[string]any:
			if text, ok := v["text"].(string); ok {
				return count(text)
			}
			if value, ok := v["content"]; ok {
				return count(value)
			}
			var n int64
			for _, key := range []string{"messages", "input", "message", "choices", "output", "result", "text"} {
				if value, ok := v[key]; ok {
					n += count(value)
				}
			}
			return n
		}
		return 0
	}
	if object, ok := root.(map[string]any); ok {
		if !result {
			root = object["text"]
		} else if text, ok := object["text"].(string); ok {
			return int64(utf8.RuneCountInString(text))
		}
	}
	return count(root)
}
