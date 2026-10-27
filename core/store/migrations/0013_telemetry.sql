ALTER TABLE task_attempts ADD COLUMN submitted_at timestamptz;
CREATE INDEX task_attempts_text_telemetry ON task_attempts(updated_at,id)
  WHERE submitted_at IS NOT NULL;
