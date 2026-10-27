-- Historical snapshots only: providers remains the canonical runtime configuration.
CREATE TABLE admin_ai_config_versions (
    version bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    snapshot_ciphertext text NOT NULL,
    actor_id text NOT NULL REFERENCES users(id),
    action text NOT NULL CHECK (action IN ('save','rollback')),
    created_at timestamptz NOT NULL DEFAULT now()
);
