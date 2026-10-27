# Platform billing

## Parent integration contracts

- Call `s.registerBillingRoutes()` from `(*Server).routes()`. Registration is idempotent and does nothing unless both the server and store editions are Platform. No billing, benefit, QR, or payment callback routes are registered in Personal, including before authentication. Store methods additionally check the immutable database edition; SQL triggers reject Personal billing writes.
- The existing embedded migration runner discovers `0006_billing.sql`. No shared schema, dependency, config, task, credit, or server bootstrap file was changed by this slice.
- For automatic provider reconciliation, invoke `s.ReconcileBilling(ctx, 30)` roughly once per minute from the parent's cancellable platform lifecycle scheduler. It reserves due orders with `SKIP LOCKED`, moves their next-check time forward, and performs network queries without holding database locks. Concurrent instances are supported. Without this hook, explicit sync and provider callbacks still work, but automatic background reconciliation is not started.
- `adjustCredits(ctx, tx, ...)`, the existing auth/admin/envelope helpers, `s.Vault`, and `saveImage` are reused. No new shared fields or helpers are required. Admin user credit-adjustment routing belongs to the admin slice, not this one.
- Tests can install `registerBillingRoutesWithGateway(&billing.Mock{})` before the ordinary routes. `billing.Client` also accepts an injected HTTP client and never follows redirects. Mock only returns explicitly supplied payments; it never assumes an order was paid.

## HTTP compatibility

Responses use the existing Go success/error envelope and snake_case legacy fields, except provider callbacks, which preserve their provider-specific response formats.

| Area | Routes |
| --- | --- |
| Authenticated billing | `GET /api/billing/{summary,plans,channels,orders,ledger}`, `POST /api/billing/orders`, `POST /api/billing/orders/{id}/{sync,cancel}`, `POST /api/billing/redeem` |
| Admin catalog | `GET /api/admin/billing/{plans,channels}`, `POST /api/admin/billing/plans/{id}`, `POST /api/admin/billing/channels/{id}` |
| Admin orders | `GET /api/admin/billing/orders`, `PATCH /api/admin/billing/orders/{id}/{confirm,cancel}` |
| Codes | `GET/POST /api/admin/billing/redemption-codes`, `GET /api/admin/benefits/redemption-codes`, `GET /api/admin/benefits/redemption-records`, `POST /api/admin/benefits/card-waves` |
| Benefits | `GET/POST /api/admin/benefits/config`, `POST /api/admin/benefits/qr`, `GET /api/public/benefit-config`, `GET /api/public/benefits/qr/{id}` |
| Providers | `POST /api/billing/afdian/webhook`, `GET/POST /api/billing/ykmcn/notify` |

Pagination returns `rows`, `total`, `page`, `page_size`, and `total_pages`, including out-of-range empty pages. User defaults are five rows, capped at 50; admin defaults are 20, capped at 100. Admin orders accept keyword, status, channel, from, and to filters. Date-only bounds use UTC and include the whole end date. Ledger and order reads are owner-scoped. Counts and rows use one repeatable-read snapshot.

Catalog POST supports partial updates to existing records and explicit creation at a caller-chosen ID. There are no seeded prices, merchant accounts, payment domains, enabled payment channels, or community accounts. A fresh instance needs administrator-created plans and channels; catalog-creation UI is a frontend integration responsibility. Channel codes are immutable after creation; optional `version` rejects stale updates.

Order creation accepts legacy snake_case fields and camelCase aliases. Optional `Idempotency-Key` retries reuse one order for the same user, plan, and channel; reuse with another request conflicts. Orders snapshot price, credits, currency, channel version, encrypted channel configuration, and provider account identity. Editing plans or rotating channel keys does not change existing order snapshots. Closed or paid orders never receive a checkout link on an idempotent replay.

The legacy `turnstile_token` input is accepted without imposing a new billing CAPTCHA: the existing credits page deliberately sends an empty token, and the Java billing action does not verify it. Configuring login CAPTCHA does not break recharge. Authentication, same-origin cookie protection, and per-user rate limits protect order creation; adding a billing CAPTCHA requires coordinated frontend/config work outside this slice.

Disabled visible channels create `channel_disabled` orders with no checkout links. Hidden channels and disabled plans cannot create orders. Channel `allowed_plan_ids`, when present, is enforced, including an empty allowlist. User cancellation produces legacy `failed`, allowing a subsequently verified late payment; administrator cancellation produces terminal `cancelled` and cannot cancel a paid order. Ykmcn user cancellation first checks for an already-completed payment.

## Configuration and credentials

Channel `config` remains a JSON-encoded string for the existing admin frontend. The complete parsed configuration, including merchant IDs, is encrypted using `s.Vault` with associated scope `billing/channel/{channel-id}`. Ciphertext is never returned. Admin reads replace nonempty `pid`, `key`, `user_id`, `api_token`, and `webhook_secret` with `[REDACTED]`; user channel lists return `config: "{}"`. Blank or redacted credential fields preserve the previous value on update. Supply a new value to rotate a credential. Disable the channel to stop new checkout orders; callbacks for already-issued orders remain valid.

Supported configuration fields:

- Common: `instructions`, `label`, `timeout_seconds` (up to 60), `payment_window_minutes` (up to 1440), `allowed_plan_ids`.
- Manual: `payment_url` and/or `links: [{label,url}]`. `manual` and custom non-provider channel codes require administrator confirmation; a sync does not invent payment success.
- Ykmcn: channel `ykmcn`, `ykmcn_alipay`, `ykmcn_wxpay`, or `mode: "ykmcn_legacy"`; explicit `base_url`, `pid`, `key`, `notify_url`, `return_url_template`, optional `type`, `name_template`, `param_template`. The specialized WeChat channel defaults to `wxpay`; otherwise the default payment type is `alipay`.
- Afdian: code `afdian`; explicit `base_url`, `user_id`, `api_token`, `webhook_secret`, optional `webhook_max_skew_seconds` (default 300; maximum 900). Checkout supports `checkout_url`, `plan_id`, `product_type`, `remark_template`, `custom_order_id_template`, `return_url_template`, `sku`, `viokrz_ex`, or a configured `payment_url`/`links`.
- Mock: code `mock`, with an explicitly injected `billing.Mock` for tests. The ordinary runtime never manufactures mock settlements.

Provider channels only accept CNY plans. URLs must be HTTPS without embedded user credentials, or explicitly configured loopback HTTP URLs for local tests. There is no default provider domain. HTTP queries have bounded timeouts, a 1 MiB response limit, no redirects, and sanitized errors without response bodies or credential-bearing request URLs.

Ykmcn checkout links contain only the provider-required merchant ID, order data, and signature, never the signing key. Callback verification rejects duplicate parameters, bad signatures, mismatched merchant/order/type/currency, missing trade IDs, and underpayment. All money parsing uses integer cents, rejects signs/exponents/fractional-cent rounding and overflow, and preserves the legacy acceptance of overpayment without granting extra credits.

Afdian callbacks accept legacy `X-Openimage-Webhook-Secret`/`secret` authentication or the timestamped HMAC-SHA256 headers `X-Openimage-Timestamp` and `X-Openimage-Signature`. A valid callback still triggers an authenticated merchant query: callback-supplied amounts and local order IDs are never settlement authority. The query must return a paid trade with the correct order, provider account, amount, and currency. Callbacks use the current Afdian merchant configuration; old orders can also be synced using their encrypted snapshots. Coordinate Afdian merchant/webhook-secret changes with the provider while old payments remain outstanding.

Ykmcn callbacks return plain `success` only after commit, otherwise `fail` with an error HTTP status. Afdian returns `{"ec":200,"em":""}` only after verified settlement commits. No network payment requests are issued by route registration, catalog editing, or checkout URL construction.

## Transactions and benefits

Settlement locks the order and atomically records the provider receipt, invokes `adjustCredits`, marks the order paid, and audits the transition. Receipt uniqueness is scoped to adapter, gateway host, merchant identity, and trade number, independent of channel aliases; each order can bind only one receipt. The shared unique ledger business key is the final balance idempotency backstop. Replays return the original ledger/balance; mismatched receipt replays fail. Receipt rows, ledger rows, balance, and order status all roll back on failure, including balance overflow.

Code generation is atomic: ordinary batches allow 1-200 codes, waves 1-500, and each code allows 1-10,000 uses. Codes contain 128 random bits. Positive credits, expiry, tier sizes, and batch exclusivity are validated before insertion. Existing batch IDs must retain the same name and exclusivity. No email or model call is involved.

Redemption locks the code and user. Expiry, enabled state, capacity, active user, prior code usage, and exclusive-wave usage are checked in the transaction. Unique constraints protect `(code,user)` and exclusive `(batch,user)` claims. Record insertion, usage consumption, ledger entry, and balance change commit together. An invalid code, repeated claim, full wave, expired code, or overflow never consumes a code or grants credits. Code status and record counts are returned with the legacy listing shape. Redemption records contain masked email addresses.

Benefit configuration defaults to disabled with empty account/link fields. Partial updates are serialized and audited. QR uploads accept one normalized PNG/JPEG/WebP image up to 3 MiB and return a stable public URL. Only the currently referenced benefit QR assets are publicly served; ordinary uploads and superseded QR assets are not made public by this route.

## Verification

Use the configured Go toolchain and cache environment, and point `PICPILOT_TEST_DATABASE_CONFIG_FILE` at the isolated test database JSON. Never print that file or its URL. The existing harness refuses database names without `picpilot_test_`, creates a disposable schema per test, and drops it afterward.

From `core`, focused verification is:

```text
go test ./billing ./store ./server -run 'Billing|MoneyExact|Ykmcn|Afdian|Client|ChannelConfig' -count=1
```

The tests cover integer-money boundaries; canonical signatures; timestamp/body tampering; redirect, oversized, and malformed provider responses; encrypted/redacted configuration and credential snapshots; concurrent manual/provider settlement; cross-channel trade replay; cancel/settle races; overflow rollback; code exhaustion, same-user and exclusive-wave races; pagination and ownership; HTTP callbacks and manual confirmation; benefits and QR publication; fair concurrent reconciliation reservations; CSRF; and every billing route returning 404 in Personal. Provider integration tests use local `httptest` servers or explicit `billing.Mock` fixtures, never live payment, model, or email services.

Live merchant credentials, payment-provider acceptance, real-money settlement, deployment, and frontend behavior are deliberately not exercised by this slice. Parent route and lifecycle wiring remain required as described above.

Handoff verification: the full `core` suite (`go test ./... -count=1`) passed, the 22 billing-focused tests passed three consecutive runs against isolated PostgreSQL schemas, and `go vet ./billing ./store ./server` passed. `go test -race` could not run because this Windows toolchain environment has CGO disabled and no C compiler available; the PostgreSQL goroutine/HTTP concurrency tests did run. No live paid calls, commits, or deployments were made.
