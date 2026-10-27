CREATE TABLE video_task_state (
  task_id text PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
  progress integer NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
  submitted_at timestamptz,
  hidden_at timestamptz,
  pending_result jsonb
);

CREATE UNIQUE INDEX video_upstream_identity ON task_attempts(provider_scope,provider_id,upstream_task_id)
  WHERE upstream_task_id IS NOT NULL AND protocol IN ('apimart_video','hfsy_video','agnes_video');
CREATE INDEX video_poll_claim ON tasks(next_run_at,created_at,id)
  WHERE capability='video' AND status='polling';

-- Authenticated callbacks can precede the acknowledgement of the create POST.
-- Retain only a bounded, normalized result, never the raw webhook body.
CREATE TABLE video_callback_inbox (
  upstream_task_id text PRIMARY KEY,
  result_json jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  CHECK (octet_length(result_json::text) <= 16384)
);
