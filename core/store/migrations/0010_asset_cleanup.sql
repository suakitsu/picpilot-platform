CREATE TABLE asset_deletion_queue (
  storage_key text PRIMARY KEY,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX assets_worker_candidates ON assets(created_at)
  WHERE image_type='worker-candidate';
