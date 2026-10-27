ALTER TABLE providers DROP CONSTRAINT providers_lease_seconds_check;
ALTER TABLE providers ADD CONSTRAINT providers_lease_seconds_check CHECK (lease_seconds BETWEEN 30 AND 1800);
ALTER TABLE providers DROP CONSTRAINT providers_timeout_seconds_check;
ALTER TABLE providers ADD CONSTRAINT providers_timeout_seconds_check CHECK (timeout_seconds BETWEEN 1 AND 3600);
CREATE UNIQUE INDEX task_attempts_worker_receipt ON task_attempts ((metadata->>'worker_job_id'))
  WHERE metadata ? 'worker_job_id';
CREATE INDEX tasks_worker_wait ON tasks(next_run_at,created_at,id) WHERE status='waiting_worker';
