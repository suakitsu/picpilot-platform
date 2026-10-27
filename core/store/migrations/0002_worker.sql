-- Worker protocol rows are separate from business tasks and never settle credits.
ALTER TABLE providers DROP CONSTRAINT providers_lease_seconds_check;
ALTER TABLE providers ADD CONSTRAINT providers_lease_seconds_check
  CHECK (lease_seconds BETWEEN 30 AND 1800);
ALTER TABLE providers DROP CONSTRAINT providers_timeout_seconds_check;
ALTER TABLE providers ADD CONSTRAINT providers_timeout_seconds_check
  CHECK (timeout_seconds BETWEEN 1 AND 3600);

CREATE TABLE worker_tokens (
  id text PRIMARY KEY,
  owner_scope text NOT NULL,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  token_hash text NOT NULL UNIQUE CHECK (length(token_hash)=64),
  token_prefix text NOT NULL CHECK (length(token_prefix)<=12),
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  last_used_at timestamptz,
  revoked_at timestamptz
);
CREATE INDEX worker_tokens_scope ON worker_tokens(owner_scope,created_at);
CREATE TABLE worker_instances (
  worker_id text PRIMARY KEY CHECK (worker_id ~ '^[A-Za-z0-9._:-]{3,120}$'),
  token_id text NOT NULL REFERENCES worker_tokens(id),
  capabilities jsonb NOT NULL CHECK (jsonb_typeof(capabilities)='array'),
  max_concurrency integer NOT NULL CHECK (max_concurrency BETWEEN 1 AND 32),
  last_heartbeat_at timestamptz NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE worker_jobs (
  job_id text PRIMARY KEY,
  task_id text NOT NULL REFERENCES tasks(id),
  provider_scope text NOT NULL,
  provider_id text NOT NULL,
  capability text NOT NULL CHECK (capability IN ('image','text')),
  protocol text NOT NULL CHECK (protocol IN ('openai_responses','openai_chat_completions','openai_images_generations','openai_images_edits')),
  model text NOT NULL,
  request_json jsonb NOT NULL CHECK (jsonb_typeof(request_json)='object'),
  request_hash text NOT NULL CHECK (length(request_hash)=64),
  input_sources jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(input_sources)='array'),
  offline_action text NOT NULL CHECK (offline_action IN ('queue','fallback')),
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','leased','succeeded','failed','cancelled','expired')),
  worker_id text REFERENCES worker_instances(worker_id),
  lease_hash text CHECK (lease_hash IS NULL OR length(lease_hash)=64),
  previous_lease_hash text CHECK (previous_lease_hash IS NULL OR length(previous_lease_hash)=64),
  lease_until timestamptz,
  renew_after timestamptz,
  grace_until timestamptz,
  attempt integer NOT NULL DEFAULT 0 CHECK (attempt>=0),
  queued_at timestamptz NOT NULL DEFAULT now(),
  leased_at timestamptz,
  completed_at timestamptz,
  completion_key text NOT NULL DEFAULT '',
  completion_hash text NOT NULL DEFAULT '',
  result_json jsonb NOT NULL DEFAULT '{}',
  error_code text NOT NULL DEFAULT '',
  error_message text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(task_id,provider_scope,provider_id,capability,request_hash),
  CHECK (status<>'leased' OR (worker_id IS NOT NULL AND lease_hash IS NOT NULL AND lease_until IS NOT NULL AND renew_after IS NOT NULL AND grace_until IS NOT NULL AND grace_until>=lease_until AND lease_until>=renew_after)),
  CHECK (status NOT IN ('succeeded','failed') OR (completion_key<>'' AND length(completion_hash)=64))
);
CREATE INDEX worker_jobs_fifo ON worker_jobs(provider_scope,provider_id,queued_at,job_id) WHERE status='queued';
CREATE INDEX worker_jobs_provider_active ON worker_jobs(provider_scope,provider_id,grace_until) WHERE status='leased';
CREATE INDEX worker_jobs_worker_active ON worker_jobs(worker_id,grace_until) WHERE status='leased';
CREATE INDEX worker_jobs_business ON worker_jobs(task_id,status);
CREATE UNIQUE INDEX worker_jobs_one_unfinished_per_task ON worker_jobs(task_id)
  WHERE status IN ('queued','leased','expired');
CREATE TABLE worker_artifacts (
  artifact_id text PRIMARY KEY,
  job_id text NOT NULL REFERENCES worker_jobs(job_id),
  worker_id text NOT NULL REFERENCES worker_instances(worker_id),
  lease_hash text NOT NULL CHECK (length(lease_hash)=64),
  idempotency_key text NOT NULL,
  asset_id text NOT NULL UNIQUE REFERENCES assets(id),
  response_json jsonb NOT NULL CHECK (jsonb_typeof(response_json)='object'),
  source_sha256 text NOT NULL CHECK (length(source_sha256)=64),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(job_id,idempotency_key)
);
