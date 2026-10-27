CREATE TABLE instance_state (
  singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
  edition text NOT NULL CHECK (edition IN ('personal','platform')),
  setup_completed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE users (
  id text PRIMARY KEY,
  email text NOT NULL,
  password_hash text NOT NULL,
  nickname text NOT NULL DEFAULT '',
  role text NOT NULL CHECK (role IN ('user','admin','super_admin','owner')),
  status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled')),
  credits bigint NOT NULL DEFAULT 0 CHECK (credits >= 0),
  auth_version bigint NOT NULL DEFAULT 0,
  ui_language text NOT NULL DEFAULT 'zh-CN',
  theme text NOT NULL DEFAULT 'system',
  avatar_asset_id text,
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_unique ON users (lower(email));

CREATE TABLE auth_challenges (
  id text PRIMARY KEY,
  email text NOT NULL,
  purpose text NOT NULL CHECK (purpose IN ('register','reset_password','change_email')),
  token_hash text NOT NULL,
  attempts integer NOT NULL DEFAULT 0,
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_challenge_lookup ON auth_challenges (lower(email),purpose,created_at DESC);
CREATE TABLE oauth_identities (
  provider text NOT NULL,
  subject text NOT NULL,
  user_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider,subject)
);

CREATE TABLE providers (
  owner_scope text NOT NULL,
  id text NOT NULL,
  name text NOT NULL,
  capability text NOT NULL CHECK (capability IN ('text','image','video')),
  protocol text NOT NULL,
  profile text NOT NULL DEFAULT 'openai',
  base_url text NOT NULL DEFAULT '',
  secret_ciphertext text NOT NULL DEFAULT '',
  model text NOT NULL,
  enabled boolean NOT NULL DEFAULT false,
  priority integer NOT NULL DEFAULT 100,
  execution_mode text NOT NULL DEFAULT 'http' CHECK (execution_mode IN ('http','pull_worker')),
  max_concurrency integer NOT NULL DEFAULT 1 CHECK (max_concurrency BETWEEN 1 AND 100),
  timeout_seconds integer NOT NULL DEFAULT 300 CHECK (timeout_seconds BETWEEN 5 AND 3600),
  local_wait_seconds integer NOT NULL DEFAULT 30 CHECK (local_wait_seconds BETWEEN 0 AND 3600),
  offline_action text NOT NULL DEFAULT 'fallback' CHECK (offline_action IN ('queue','fallback')),
  lease_seconds integer NOT NULL DEFAULT 120 CHECK (lease_seconds BETWEEN 30 AND 600),
  settings jsonb NOT NULL DEFAULT '{}',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_scope,id),
  CHECK (execution_mode <> 'pull_worker' OR capability IN ('image','text'))
);
CREATE INDEX providers_selection ON providers(owner_scope,capability,priority,id) WHERE enabled;
CREATE TABLE provider_health (
  owner_scope text NOT NULL,
  provider_id text NOT NULL,
  consecutive_failures integer NOT NULL DEFAULT 0,
  open_until timestamptz,
  probe_until timestamptz,
  last_error_code text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (owner_scope,provider_id),
  FOREIGN KEY (owner_scope,provider_id) REFERENCES providers(owner_scope,id) ON DELETE CASCADE
);

CREATE TABLE projects (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  source_type text NOT NULL DEFAULT 'listing',
  product_name text NOT NULL DEFAULT '',
  brand text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '',
  language text NOT NULL DEFAULT 'zh-CN',
  status text NOT NULL DEFAULT 'draft',
  original_info jsonb NOT NULL DEFAULT '{}',
  generated_copy jsonb NOT NULL DEFAULT '{}',
  confirmed_facts jsonb NOT NULL DEFAULT '{}',
  metadata jsonb NOT NULL DEFAULT '{}',
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX projects_owner_list ON projects(user_id,created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE tasks (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  project_id text REFERENCES projects(id),
  capability text NOT NULL CHECK (capability IN ('image','text','video')),
  kind text NOT NULL,
  title text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','waiting_worker','polling','uncertain','succeeded','failed','cancelled')),
  request_json jsonb NOT NULL,
  request_hash text NOT NULL,
  idempotency_key text NOT NULL,
  credits_cost bigint NOT NULL DEFAULT 0 CHECK (credits_cost >= 0),
  result_json jsonb NOT NULL DEFAULT '{}',
  error_code text NOT NULL DEFAULT '',
  provider_scope text NOT NULL DEFAULT '',
  provider_id text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  attempt integer NOT NULL DEFAULT 0,
  lease_owner text,
  lease_id text,
  lease_until timestamptz,
  next_run_at timestamptz NOT NULL DEFAULT now(),
  cancel_requested_at timestamptz,
  completed_at timestamptz,
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id,idempotency_key)
);
CREATE INDEX tasks_claim ON tasks(next_run_at,created_at,id) WHERE status='queued';
CREATE INDEX tasks_owner_list ON tasks(user_id,created_at DESC);
CREATE INDEX tasks_project_list ON tasks(project_id,created_at);
CREATE TABLE task_attempts (
  id text PRIMARY KEY,
  task_id text NOT NULL REFERENCES tasks(id),
  attempt integer NOT NULL,
  provider_scope text NOT NULL,
  provider_id text NOT NULL,
  protocol text NOT NULL,
  model text NOT NULL,
  status text NOT NULL CHECK (status IN ('reserved','submitting','submitted','succeeded','failed','uncertain')),
  upstream_task_id text,
  request_hash text NOT NULL,
  error_code text NOT NULL DEFAULT '',
  retryable boolean NOT NULL DEFAULT false,
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (task_id,attempt)
);
CREATE UNIQUE INDEX task_one_active_attempt ON task_attempts(task_id) WHERE status IN ('reserved','submitting','submitted','uncertain');
CREATE TABLE credit_ledger (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  change_amount bigint NOT NULL,
  balance_after bigint NOT NULL CHECK (balance_after >= 0),
  biz_type text NOT NULL,
  biz_id text NOT NULL,
  remark text NOT NULL DEFAULT '',
  actor_id text REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE (user_id,biz_type,biz_id)
);
CREATE INDEX credit_ledger_user_history ON credit_ledger(user_id,created_at DESC);

CREATE TABLE assets (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  project_id text REFERENCES projects(id),
  task_id text REFERENCES tasks(id),
  image_type text NOT NULL DEFAULT 'reference',
  storage_key text NOT NULL UNIQUE,
  original_filename text NOT NULL DEFAULT '',
  content_type text NOT NULL,
  file_size bigint NOT NULL CHECK (file_size > 0),
  width integer NOT NULL DEFAULT 0 CHECK (width >= 0),
  height integer NOT NULL DEFAULT 0 CHECK (height >= 0),
  sha256 text NOT NULL CHECK (length(sha256)=64),
  metadata jsonb NOT NULL DEFAULT '{}',
  deleted_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assets_owner_history ON assets(user_id,created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX assets_project ON assets(project_id,created_at);

CREATE TABLE app_settings (
  key text PRIMARY KEY,
  value jsonb NOT NULL,
  secret_ciphertext text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  updated_by text REFERENCES users(id),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE audit_events (
  id text PRIMARY KEY,
  actor_id text REFERENCES users(id),
  action text NOT NULL,
  target_type text NOT NULL,
  target_id text NOT NULL DEFAULT '',
  request_id text NOT NULL DEFAULT '',
  metadata jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_recent ON audit_events(created_at DESC);
