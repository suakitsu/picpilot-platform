CREATE TABLE project_asset_refs (
    project_id text NOT NULL REFERENCES projects(id),
    asset_id text NOT NULL REFERENCES assets(id),
    position integer NOT NULL CHECK (position >= 0),
    image_type text NOT NULL DEFAULT 'reference',
    PRIMARY KEY (project_id, asset_id)
);

CREATE TABLE creative_requests (
    user_id text NOT NULL REFERENCES users(id),
    request_key text NOT NULL,
    request_hash text NOT NULL,
    result jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, request_key)
);

CREATE TABLE creative_chats (
    id text PRIMARY KEY,
    user_id text NOT NULL REFERENCES users(id),
    title text NOT NULL,
    style text NOT NULL DEFAULT '',
    size text NOT NULL DEFAULT '1024x1024',
    language text NOT NULL DEFAULT 'zh-CN',
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX creative_chats_owner ON creative_chats(user_id, updated_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE creative_versions (
    id text PRIMARY KEY,
    chat_id text NOT NULL REFERENCES creative_chats(id),
    version_no integer NOT NULL CHECK (version_no > 0),
    project_id text NOT NULL REFERENCES projects(id),
    task_id text NOT NULL REFERENCES tasks(id),
    user_request text NOT NULL,
    final_prompt text NOT NULL,
    style text NOT NULL,
    size text NOT NULL,
    reference_asset_ids jsonb NOT NULL DEFAULT '[]',
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(chat_id, version_no)
);

CREATE TABLE creative_messages (
    id text PRIMARY KEY,
    chat_id text NOT NULL REFERENCES creative_chats(id),
    role text NOT NULL CHECK (role IN ('user','assistant')),
    content text NOT NULL,
    version_id text REFERENCES creative_versions(id),
    task_id text REFERENCES tasks(id),
    deleted_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX creative_messages_chat ON creative_messages(chat_id, created_at, id) WHERE deleted_at IS NULL;

CREATE TABLE creative_turns (
    id text PRIMARY KEY,
    chat_id text NOT NULL REFERENCES creative_chats(id),
    task_id text NOT NULL UNIQUE REFERENCES tasks(id),
    request jsonb NOT NULL,
    reference_asset_ids jsonb NOT NULL,
    status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','applied','failed')),
    error_code text NOT NULL DEFAULT '',
    result jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT now()
);
