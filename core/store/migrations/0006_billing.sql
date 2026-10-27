CREATE TABLE recharge_plans (
  id text PRIMARY KEY,
  name text NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
  credits bigint NOT NULL CHECK (credits > 0),
  price_cents bigint NOT NULL CHECK (price_cents >= 0),
  currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
  enabled boolean NOT NULL DEFAULT false,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE payment_channels (
  id text PRIMARY KEY,
  code text NOT NULL UNIQUE,
  name text NOT NULL,
  visible boolean NOT NULL DEFAULT false,
  enabled boolean NOT NULL DEFAULT false,
  sort_order integer NOT NULL DEFAULT 0,
  config_ciphertext text NOT NULL DEFAULT '',
  version bigint NOT NULL DEFAULT 1,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE recharge_orders (
  id text PRIMARY KEY,
  user_id text NOT NULL REFERENCES users(id),
  plan_id text NOT NULL REFERENCES recharge_plans(id),
  channel_id text NOT NULL REFERENCES payment_channels(id),
  channel_code text NOT NULL,
  channel_version bigint NOT NULL,
  config_ciphertext text NOT NULL DEFAULT '',
  adapter text NOT NULL,
  account_hash text NOT NULL DEFAULT '',
  amount_cents bigint NOT NULL CHECK (amount_cents >= 0),
  credits bigint NOT NULL CHECK (credits > 0),
  currency text NOT NULL,
  status text NOT NULL CHECK (status IN ('pending','channel_disabled','paid','failed','cancelled')),
  remark text NOT NULL DEFAULT '',
  external_trade_no text NOT NULL DEFAULT '',
  request_key text,
  expires_at timestamptz NOT NULL,
  next_check_at timestamptz NOT NULL DEFAULT now(),
  paid_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(user_id,request_key)
);
CREATE INDEX recharge_orders_user_history ON recharge_orders(user_id,created_at DESC,id DESC);
CREATE INDEX recharge_orders_pending ON recharge_orders(expires_at,id) WHERE status='pending';
CREATE INDEX recharge_orders_reconcile ON recharge_orders(next_check_at,id) WHERE status='pending' AND adapter IN ('afdian','ykmcn');

-- Provider identities, not channel IDs, prevent replay through two channel aliases.
CREATE TABLE billing_receipts (
  adapter text NOT NULL,
  account_hash text NOT NULL,
  trade_no text NOT NULL,
  order_id text NOT NULL UNIQUE REFERENCES recharge_orders(id),
  amount_cents bigint NOT NULL CHECK (amount_cents >= 0),
  currency text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(adapter,account_hash,trade_no)
);

CREATE TABLE benefit_batches (
  id text PRIMARY KEY,
  name text NOT NULL,
  exclusive boolean NOT NULL DEFAULT false,
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE redemption_codes (
  id text PRIMARY KEY,
  code text NOT NULL UNIQUE CHECK (length(code) BETWEEN 1 AND 64),
  credits bigint NOT NULL CHECK (credits > 0),
  max_uses integer NOT NULL CHECK (max_uses BETWEEN 1 AND 10000),
  used_count integer NOT NULL DEFAULT 0 CHECK (used_count >= 0 AND used_count <= max_uses),
  expires_at timestamptz,
  enabled boolean NOT NULL DEFAULT true,
  benefit_type text NOT NULL DEFAULT '',
  batch_id text REFERENCES benefit_batches(id),
  note text NOT NULL DEFAULT '',
  created_by text NOT NULL REFERENCES users(id),
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX redemption_codes_history ON redemption_codes(created_at DESC,id DESC);
CREATE TABLE redemption_records (
  id text PRIMARY KEY,
  code_id text NOT NULL REFERENCES redemption_codes(id),
  batch_id text REFERENCES benefit_batches(id),
  batch_exclusive boolean NOT NULL DEFAULT false,
  user_id text NOT NULL REFERENCES users(id),
  user_email_masked text NOT NULL,
  user_nickname text NOT NULL,
  credits bigint NOT NULL CHECK (credits > 0),
  redeemed_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(code_id,user_id)
);
CREATE UNIQUE INDEX redemption_exclusive_batch ON redemption_records(batch_id,user_id) WHERE batch_exclusive;
CREATE INDEX redemption_records_history ON redemption_records(redeemed_at DESC,id DESC);

-- Defense in depth: even direct billing writes cannot activate Personal business.
CREATE FUNCTION billing_require_platform() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM instance_state WHERE singleton AND edition='platform') THEN
    RAISE EXCEPTION 'billing is unavailable in this edition' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER recharge_plans_platform BEFORE INSERT OR UPDATE ON recharge_plans FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER payment_channels_platform BEFORE INSERT OR UPDATE ON payment_channels FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER recharge_orders_platform BEFORE INSERT OR UPDATE ON recharge_orders FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER billing_receipts_platform BEFORE INSERT OR UPDATE ON billing_receipts FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER benefit_batches_platform BEFORE INSERT OR UPDATE ON benefit_batches FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER redemption_codes_platform BEFORE INSERT OR UPDATE ON redemption_codes FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
CREATE TRIGGER redemption_records_platform BEFORE INSERT OR UPDATE ON redemption_records FOR EACH ROW EXECUTE FUNCTION billing_require_platform();
