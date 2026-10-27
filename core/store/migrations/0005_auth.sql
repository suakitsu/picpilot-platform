ALTER TABLE auth_challenges DROP CONSTRAINT auth_challenges_purpose_check;
ALTER TABLE auth_challenges ADD CONSTRAINT auth_challenges_purpose_check
  CHECK (purpose IN ('register','login','reset_password','change_email'));
CREATE INDEX auth_challenge_expiry ON auth_challenges(expires_at);

CREATE TABLE oauth_states (
  id text PRIMARY KEY,
  token_hash text NOT NULL UNIQUE,
  browser_hash text NOT NULL,
  next_path text NOT NULL DEFAULT '/',
  expires_at timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now()
);
