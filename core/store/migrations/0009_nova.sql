-- Nova is a durable, owner-scoped view over business tasks, not another queue.
CREATE TABLE nova_runs (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    kind text NOT NULL CHECK (kind IN ('image','text')),
    mode text NOT NULL DEFAULT '',
    request_key text NOT NULL CHECK (length(request_key) BETWEEN 1 AND 128),
    request_hash text NOT NULL CHECK (length(request_hash)=64),
    created_at timestamptz NOT NULL DEFAULT now(),
    acknowledged_at timestamptz,
    cancel_requested_at timestamptz,
    cancel_dispatched_at timestamptz,
    UNIQUE(user_id,kind,request_key)
);
CREATE INDEX nova_runs_owner_history ON nova_runs(user_id,created_at DESC,id);
CREATE INDEX nova_runs_cancel_pending ON nova_runs(created_at,id)
    WHERE cancel_requested_at IS NOT NULL AND cancel_dispatched_at IS NULL;

CREATE TABLE nova_run_tasks (
    run_id text NOT NULL REFERENCES nova_runs(id) ON DELETE CASCADE,
    ordinal integer NOT NULL CHECK (ordinal BETWEEN 0 AND 3),
    task_id text NOT NULL UNIQUE REFERENCES tasks(id),
    PRIMARY KEY(run_id,ordinal)
);

CREATE TABLE nova_settings (
    scope text PRIMARY KEY,
    mode text NOT NULL DEFAULT '2' CHECK (mode IN ('1','2','3')),
    prompts jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(prompts)='array'),
    keywords jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(keywords)='array'),
    password_hash text NOT NULL DEFAULT '',
    revision bigint NOT NULL DEFAULT 1,
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE nova_gallery_grants (
    user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scope text NOT NULL REFERENCES nova_settings(scope) ON DELETE CASCADE,
    revision bigint NOT NULL,
    expires_at timestamptz NOT NULL,
    PRIMARY KEY(user_id,scope)
);
