CREATE TABLE admin_risk_events (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  project_id text REFERENCES projects(id),
  task_id text REFERENCES tasks(id),
  request_key_hash text NOT NULL CHECK(length(request_key_hash)=64),
  feature text NOT NULL,
  risk_level text NOT NULL CHECK(risk_level IN ('high','medium','low')),
  keyword text NOT NULL CHECK(length(keyword) BETWEEN 1 AND 120),
  source text NOT NULL CHECK(source IN ('user_input','provider_error')),
  action text NOT NULL CHECK(action IN ('blocked','warned','recorded')),
  cleared_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,request_key_hash,source)
);
CREATE INDEX admin_risk_recent ON admin_risk_events(created_at DESC,id DESC);
CREATE INDEX admin_risk_user ON admin_risk_events(user_id) WHERE cleared_at IS NULL;

CREATE TABLE admin_user_activity (
  user_id text PRIMARY KEY REFERENCES users(id),
  last_request_at timestamptz NOT NULL
);

CREATE TABLE admin_task_retries (
  original_task_id text PRIMARY KEY REFERENCES tasks(id),
  retry_task_id text NOT NULL UNIQUE REFERENCES tasks(id),
  actor_id text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);

-- Provider accounting is explicit. Missing observations are unknown, not free.
CREATE TABLE admin_task_costs (
  observation_id text PRIMARY KEY,
  task_id text NOT NULL REFERENCES tasks(id),
  amount_cny numeric(20,6) NOT NULL CHECK(amount_cny >= 0),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_cost_task ON admin_task_costs(task_id);
CREATE TABLE admin_text_calls (
  id text PRIMARY KEY,
  user_id text REFERENCES users(id),
  task_id text REFERENCES tasks(id),
  feature text NOT NULL,
  target_type text NOT NULL DEFAULT '',
  provider text NOT NULL DEFAULT '',
  model text NOT NULL DEFAULT '',
  status text NOT NULL CHECK(status IN ('queued','success','failed','uncertain')),
  http_status integer NOT NULL DEFAULT 0 CHECK(http_status=0 OR http_status BETWEEN 100 AND 599),
  duration_ms bigint NOT NULL DEFAULT 0 CHECK(duration_ms >= 0),
  input_chars bigint NOT NULL DEFAULT 0 CHECK(input_chars >= 0),
  output_chars bigint NOT NULL DEFAULT 0 CHECK(output_chars >= 0),
  error_type text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_text_call_recent ON admin_text_calls(created_at DESC,id DESC);
CREATE INDEX admin_text_call_user ON admin_text_calls(user_id,created_at DESC,id DESC);
CREATE INDEX admin_tasks_recent ON tasks(created_at DESC,id DESC) WHERE deleted_at IS NULL;
CREATE INDEX admin_tasks_reporting ON tasks(capability,provider_id,created_at DESC);

CREATE TABLE admin_messages (
  id text PRIMARY KEY,
  title text NOT NULL CHECK(length(title) BETWEEN 1 AND 200),
  content text NOT NULL CHECK(length(content) BETWEEN 1 AND 20000),
  audience text NOT NULL CHECK(audience IN ('all','user','admin','super_admin','targeted')),
  recipient_id text REFERENCES users(id),
  enabled boolean NOT NULL DEFAULT true,
  published_by text NOT NULL REFERENCES users(id),
  published_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz,
  CHECK((audience='targeted') = (recipient_id IS NOT NULL)),
  CHECK(expires_at IS NULL OR expires_at > published_at)
);
CREATE INDEX admin_messages_recent ON admin_messages(published_at DESC,id DESC);
CREATE TABLE admin_message_reads (
  message_id text NOT NULL REFERENCES admin_messages(id),
  user_id text NOT NULL REFERENCES users(id),
  read_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(message_id,user_id)
);

CREATE TABLE admin_prompt_templates (
  id text PRIMARY KEY,
  name text NOT NULL CHECK(length(name) BETWEEN 1 AND 160),
  scene text NOT NULL CHECK(length(scene) BETWEEN 1 AND 80),
  content text NOT NULL CHECK(length(content) BETWEEN 1 AND 40000),
  enabled boolean NOT NULL DEFAULT true,
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX admin_prompts_scene ON admin_prompt_templates(scene,updated_at DESC,id DESC) WHERE enabled;
CREATE TABLE admin_image_configs (
  id text PRIMARY KEY,
  image_type text NOT NULL UNIQUE,
  image_name text NOT NULL,
  enabled boolean NOT NULL DEFAULT true,
  credits_cost bigint NOT NULL DEFAULT 6 CHECK(credits_cost BETWEEN 0 AND 1000000),
  default_model text NOT NULL DEFAULT '',
  fallback_model text NOT NULL DEFAULT '',
  default_size text NOT NULL DEFAULT '1024x1024',
  required_fields text NOT NULL DEFAULT '' CHECK(length(required_fields)<=1000),
  sort_order integer NOT NULL DEFAULT 0,
  version bigint NOT NULL DEFAULT 1,
  updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO admin_image_configs(id,image_type,image_name,sort_order) VALUES
 ('ic_main','main','Main image',10),('ic_scenario','scenario','Scenario',20),
 ('ic_features','features','Features',30),('ic_dimensions','dimensions','Dimensions',40),
 ('ic_steps','steps','Steps',50),('ic_comparison','comparison','Comparison',60),
 ('ic_package','package','Package',70);
