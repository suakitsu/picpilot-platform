-- Business allocations only. Tasks, leases, attempts and assets stay shared.
CREATE TABLE commerce_batches (
  project_id text PRIMARY KEY REFERENCES projects(id),
  user_id text NOT NULL REFERENCES users(id),
  kind text NOT NULL CHECK (kind IN ('commerce_suite','image_translation')),
  credits_cost bigint NOT NULL CHECK (credits_cost BETWEEN 0 AND 64000000),
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(project_id,user_id)
);
CREATE TABLE commerce_batch_items (
  task_id text PRIMARY KEY REFERENCES tasks(id),
  project_id text NOT NULL,
  user_id text NOT NULL,
  position integer NOT NULL CHECK (position BETWEEN 0 AND 63),
  credits_cost bigint NOT NULL CHECK (credits_cost BETWEEN 0 AND 1000000),
  aggregate_charge boolean NOT NULL DEFAULT true,
  replaces_task_id text UNIQUE REFERENCES tasks(id),
  created_at timestamptz NOT NULL DEFAULT now(),
  FOREIGN KEY(project_id,user_id) REFERENCES commerce_batches(project_id,user_id)
);
CREATE INDEX commerce_batch_items_project ON commerce_batch_items(project_id,position);

-- Dedicated feature configs avoid borrowing unrelated listing slot prices.
INSERT INTO admin_image_configs(id,image_type,image_name,sort_order)
VALUES ('ic_commerce_suite','commerce_suite','Commerce suite',80),
       ('ic_image_translation','image_translation','Image translation',90)
ON CONFLICT DO NOTHING;
