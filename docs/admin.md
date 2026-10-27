# Admin, Risk, Messages, and Reporting

This bounded slice adds `core/store/migrations/0007_admin.sql`, the
`core/store/admin*.go` files, `core/server/admin.go`,
`core/server/admin_messages.go`, and `core/risk`. It does not implement or
register billing, benefits, provider configuration, worker, video-management,
CommerceSuite, AdminCommerceSuite, ImageTranslation, registration,
password-recovery, email, or OAuth endpoints. Commerce/translation files and
migration `0011` belong to Linnaeus and are not touched. The Java checkout is
a read-only reference. No production databases or paid provider calls are used.

## Parent Integration

The parent owns the shared server and execution paths. Required hooks:

1. Call `s.registerAdminRoutes()` and `s.registerMessageRoutes()` from the shared
   bootstrap. Both registrations are idempotent; the former registers nothing
   in the personal edition. AI configuration is separately documented in
   [ai-config.md](ai-config.md) and mounted with `s.registerAdminAIRoutes()`.
2. Before reserving credits or submitting any user-authored input, call
   `risk.Service{Backend: s.Store}.Check(ctx, risk.Request{...})`. Supply the
   authenticated `UserID`, owned optional project/task IDs, a fixed machine
   `Feature`, `Source: "user_input"`, the actual user input, and a stable
   8-128-character `RequestKey` for the operation. Do this for every generation
   entry point, including synchronous text and delegated workflows. Never label
   generated/system prompt text as user input. Treat configuration/DB errors as
   failures, map `risk.ErrBlocked` to a safe 422 response and
   `risk.ErrAccountRestricted` to 403, and do not reserve credits on either error.
3. For definitive provider moderation failures, optionally call
   `Service.RecordProviderError` with the owned task ID. Classification happens
   in memory; only a fixed keyword is persisted. The task ID deduplicates repeated
   provider-error reports. Ordinary task completion/refund logic remains the
   only authority for billing. This hook never retries, refunds, or calls a provider.
4. Record real text-call telemetry using `Store.RecordTextCall(ctx,
   store.TextCallMetric{...})`. Use a stable ID, and reuse it for `queued` to
   `success`/`failed`/`uncertain` transitions. A terminal record can only be replayed
   unchanged. Optional task attribution must belong to the user and have the text
   capability. Only identifiers, fixed classifications, timing and character
   counts are accepted; no prompts, outputs, headers, or credentials belong here.
5. Record confirmed provider charges through
   `Store.RecordTaskCost(ctx, observationID, taskID, amountCNY)`, with a stable
   observation/attempt ID and a nonnegative decimal string of at most six decimal
   places. Replays are idempotent; changing a recorded amount/task conflicts.
   Convert currency outside this hook only when a trustworthy conversion is
   available. Missing cost observations are unknown, not zero.
6. Consume `Store.ActivePrompt(ctx, scene)` and
   `Store.RuntimeImageConfig(ctx, imageType)` in the owning prompt/planning and
   pricing code. These are trusted runtime helpers, not public disclosure APIs.
   They return `ErrNotFound` in personal edition without querying admin tables.
   `required_fields` is the Java/frontend-compatible descriptive string, not a
   machine-enforced schema. The library has seven image types and no invented
   provider credentials or placeholder prompt templates.
7. Optionally call `Store.RecordUserActivity(ctx, userID)` after authentication.
   It coalesces writes to once per minute and stores no IP, header, or body. Until
   this is wired, `last_request_at` is null, not an invented activity timestamp.

No shared bootstrap, auth, runner, pricing, or frontend files were edited by this
slice. Consequently route availability, generation-time risk enforcement,
runtime template/config consumption, activity tracking and telemetry capture
require the parent hooks above. Risk notification emails are not sent here.

## HTTP Contract

All paths below are under `/api`. Admin endpoints use the existing `auth(admin)`
guards, plus active-admin and platform checks inside store methods.

| Routes | Behavior |
| --- | --- |
| `GET /admin/dashboard`, `/admin/dashboard/trends` | Filtered task counts, credit balances, recorded costs, daily trends |
| `GET /admin/users`, `/admin/users/{id}` | Explicit safe projection; full email only after admin authorization |
| `POST /admin/users/{id}/credits` | `change_amount`, `remark`, `request_token`; also accepts `Idempotency-Key` |
| `PATCH /admin/users/{id}/status` | `active` or `disabled`; legacy `banned` maps to `disabled` |
| `POST /admin/users/{id}/password` | Existing Go password policy, 12-72 UTF-8 bytes; revokes all old sessions |
| `GET /admin/ledger`, `/admin/credit-ledger`, `/admin/users/{id}/ledger` | Searchable, filtered ledger history; no billing writes |
| `GET /admin/risk-events` | Cleared and uncleared records; no raw matched input |
| `GET /admin/risk-keywords`, `/admin/risk-config` | Effective risk policy |
| `PATCH /admin/risk-config` | Optional enabled, threshold, block/warn level changes |
| `POST /admin/risk-keywords`, `/admin/risk-keywords/delete` | Literal keyword add/remove; level is high/medium/low |
| `POST /admin/users/{id}/risk-events/clear` | Clear counts; lift only an automatic risk restriction |
| `GET /admin/tasks`, `/admin/tasks/{id}` | Read-only task reporting across capabilities |
| `GET /admin/image-tasks`, `/admin/image-tasks/{id}` | Image-only lists and safe detail |
| `PATCH /admin/image-tasks/{id}/retry`, `/{id}/delete` | Safe image retry or soft deletion |
| `GET /admin/audit-logs`, `/admin/costs`, `/admin/text-calls` | Filtered/paged reporting |
| `GET/POST /admin/messages` | List or publish all/role/targeted messages |
| `GET/POST /admin/prompts`, `POST /admin/prompts/{id}` | Versioned prompt library; optional version precondition |
| `GET /admin/image-configs`, `POST /admin/image-configs/{id}` | Validated image-type configuration; optional version precondition |
| `GET /messages`, `/messages/unread-count` | Authenticated audience-scoped history and unread count |
| `PATCH /messages/{id}/read`, `/{id}/unread` | Idempotent owner-scoped read state |
| `DELETE /messages/{id}/read` | Alias for marking unread |

List endpoints return `rows`, `total`, `page`, `page_size`, `total_pages`. Pages
start at one, sizes are 1-100, and the maximum page is 1,000,000. `pageSize` is
accepted as an alias; contradictory aliases, repeated query keys, malformed
dates and invalid bounds are rejected. Counts and page reads share a snapshot.
Stable ordering includes the ID as a tie-breaker. Search is case-insensitive
literal substring matching, so `%` and `_` do not become SQL wildcards.

Common filters: `keyword`, `user_keyword`, `user_id`, `status`, `provider`, `model`,
`feature`, `media_type`, `image_type`, `from`, `to`. Specialized filters include
user `role`, `min_credits`, `max_credits`, `sort`; ledger `biz_type`; risk
`risk_level`, `source`, `action`, `cleared`; audit `action`, `target_type`.
Date-only values use Asia/Shanghai calendar boundaries; `to` includes that day.
RFC3339 timestamps are also accepted, with an exclusive `to`. Trends default to
seven calendar days ending at `to` or today and allow at most 366 days.

For the existing Java clients, `/messages` and `/admin/prompts` return arrays
when no page parameter is supplied, capped at 100 with `X-Total-Count`. Supplying
page parameters returns the full page envelope. Image config responses retain
the existing array shape and string `required_fields` contract.

Message audiences are `all`, `user`, `admin`, `super_admin`, or `targeted` with
`recipient_id`. Super admins can see admin-audience messages. Disabled, expired,
future-published, and other users' targeted messages cannot be read or marked
read. Personal edition has an authenticated empty history without querying
platform message tables; marking a nonexistent message returns 404.

## Safety and Accounting

- Manual credits reuse the existing `adjustCredits` transaction helper. Balance,
  ledger entry and audit event commit atomically. The same key, user, actor,
  amount and remark replays one entry; changed inputs conflict. Overdrafts and
  overflow are rejected. No billing/benefit route is owned here.
- Ordinary admins may reset passwords/status only for ordinary users. Super
  admins can manage other platform roles. A serialized active-super-admin check
  prevents concurrent requests from disabling the last one. Every status/password
  mutation revokes sessions. Automatic risk restrictions never disable admins.
- Risk defaults mirror the Java severity lists, threshold and keywords. ASCII
  keywords use word boundaries; explicit negative constraints are recognized.
  This is a deterministic keyword policy, not comprehensive semantic moderation.
  Only the configured keyword and classification are stored. Event keys are
  hashed and deduplicated, and project/task ownership is checked.
- Image retry requires a definitively failed task, no outstanding/uncertain
  provider or worker attempt, and a matching settled refund for any original
  credit charge. It creates a new task with a new charge/refund identity and
  preserves the original ledger. One original can create only one replacement;
  repeat requests return that replacement even after it completes. The response
  `task_id` is the new task; `original_task_id` identifies the failed task. Project
  and creative references are updated transactionally where applicable.
- Uncertain, queued, running, waiting-worker and polling tasks are not deleted or
  retried. Failed/cancelled tasks with unsettled credit refunds cannot be hidden.
  Soft deletion keeps accounting and provider evidence. Cost reporting
  includes deleted tasks; operational dashboard/task lists omit deleted tasks.
- Cost totals/averages include recorded CNY observations only and retain null
  for unknown totals. `unknown_cost_tasks`/`known_cost_tasks` expose coverage.
  Cost groups are paged independently of the overall filtered summary.
  `cost_policy` is null to avoid presenting the Java provider price table as
  measured charges; `accounting_policy` describes the actual policy.
- Dashboard/trend billing fields use real `recharge_orders` with
  `billing_metrics_available: true`. Only paid orders contribute to recharge and
  revenue (CNY yuan, amount_cents divided by 100); other currencies are counted
  separately, never silently added. Settlement dates use paid_at, with updated_at
  as a legacy fallback, grouped at Shanghai midnight. User filters apply to
  billing aggregates; task/provider filters do not change revenue attribution.
  User `recharge_count` is the count of that user's paid orders. Pending orders,
  paying-user counts and granted recharge credits are separately reported.
- Authorized task detail includes a whitelisted prompt/size/model/reference-ID
  snapshot. Text input exposes only user-role text; image/video scalar submitted
  inputs are allowed, but raw results, arbitrary options, upstream response
  metadata, upstream IDs, system/tool messages, headers and URLs are not.
  References are filtered by task ownership before signing. Each successful
  detail view writes a `task_detail_view` audit without the prompt content.
  Prompt-library content is available only to authorized administrators.
  Historical audit metadata is withheld because other writers can store unsafe
  values; safe action/target/time/request identifiers remain queryable. This is
  an intentional privacy difference from the Java raw-request/response views.

## Verification

Tests use `core/internal/testpg`, disposable schemas and only an explicit
`picpilot_test_*` database. The test config file is never printed. No provider
network calls, paid calls, production access, or commits are performed.

From `core`, configure the requested Go toolchain/cache environment and run:

```powershell
$env:GOCACHE='E:\picpilot\development-cache\go\build'
$env:GOMODCACHE='E:\picpilot\development-cache\go\modules'
$env:GOPATH='E:\picpilot\development-cache\go'
$env:GOMAXPROCS='2'
$env:PICPILOT_TEST_DATABASE_CONFIG_FILE='E:\picpilot\development-cache\postgres-go-migration\test-database.json'
& 'E:\picpilot\development-tools\go1.27.3\go\bin\go.exe' test ./store ./server ./risk -count=1
```

The new tests cover HTTP/store role checks, personal isolation, pagination and
literal searches, credit replay/concurrency/conflicts and rollback on audit
failure, concurrent last-admin protection, password session revocation, task
retry/refund/uncertain-state guards, message audience/read ownership, risk event
deduplication and restriction clearing, prompt/config versions, real-cost
coverage, text-call terminal idempotency, and metadata/input redaction.
