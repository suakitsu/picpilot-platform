# Native Go Nova Runtime

Status: implemented backend contracts with local mock-provider and isolated
PostgreSQL tests. This is not a claim of source-built browser acceptance or full
provider-protocol parity. No Node/Python business runtime, direct endpoint model
calls, independent billing ledger, or in-memory task registry is used.

## Parent Integration

1. `Server.New` now mounts `s.registerNovaRoutes()` through the central routes.
   Registration is idempotent. It does not mount or build static frontend files.
2. The central `s.RunBackground(ctx)` now invokes `s.RunNovaMaintenance(ctx)`;
   do not start an additional Nova loop when using that entry point. Maintenance
   retries interrupted cancellation intents every three seconds and is safe on
   multiple instances. It delegates cancellation to the existing business API;
   it does not execute models, independently settle credits, or expire completed
   Nova runs. Individual task polling also resumes pending cancels.
3. Apply embedded migration `0009_nova.sql` through the existing migrator.
4. Keep the parent runner, Worker reconciliation, risk service, and telemetry
   active. Nova uses `SubmitProjectTaskTx`, `CancelTask`, creative image prompt
   composition/reference resolution, `workflowTextSelection`,
   `prepareTextInput`, `directUserText`, and `screenUserInput`. Business task
   success/failure and the existing ledger are the only settlement authorities.
5. Serve the source-built canvas at `/canvas/nova/` with the API on the same
   origin. Static source/build/distribution and the iframe bridge belong to the
   parent/frontend agent, not this backend module.

## Authentication And Isolation

Every route below requires the normal PicPilot application session, including
gallery, models, status and the image alias. Use the application's Bearer token
or same-origin session cookie; cookie mutations retain the existing Origin/CSRF
check. Successful and failed Nova responses are raw JSON, not `ApiResponse`.

Personal mode reads only providers/settings under the current owner's user ID.
It does not inherit platform providers, credentials, prices, or gallery grants.
Platform mode reads platform provider configuration; task/asset/history queries
are still strictly scoped to the authenticated user. Catalog entries expose no
provider keys, base URLs, ciphertext, or provider settings.

Nonempty legacy `apiKey`/`baseUrl` body or models-query fields are rejected with
`IFRAME_CREDENTIALS_FORBIDDEN`. Empty fields may be omitted. `X-API-Key` is also
rejected. Never put application or provider credentials in URLs. The bridge must
remove the original frontend's model-probe credential query parameters.

Errors have `{error: string, code: string, message: string}`. Typical statuses:
400 malformed/unsupported request, 401 application auth, 403 ownership policy or
locked gallery, 404 unknown/foreign task or asset, 409 conflicting key/revision,
402 insufficient platform credits, 422/403 parent risk policy, 429 queue/rate
limit, 502 failed text task, 500 sanitized internal failure. Unknown and foreign
IDs are indistinguishable. Arbitrary provider error bodies are not returned.

## Retained Routes

| Method And Path | Raw Result |
| --- | --- |
| `GET /api/nova/config` | `{promptGalleryMode: "1"\|"2"\|"3", promptGalleryPasswordEnabled: boolean}` |
| `GET /api/nova/prompts` | `Array<{title: string, content: string, type: 1\|2}>`; locked/hidden returns 403 |
| `GET /api/nova/blacklist` | `{keywords: string[]}` |
| `POST /api/nova/prompt-gallery/verify` | `{password: string}` to `{ok: boolean}`; successful protected verification creates a one-hour, user/revision-scoped database grant |
| `GET /api/nova/proxy/models` | `{data: Array<{id: string, capability: "image"\|"text", protocol: string}>}` |
| `GET /api/nova/queue-status` | `QueueStatus`, below |
| `POST /api/nova/tasks` | `ImageRequest` to HTTP 202 `{taskId: string}` |
| `GET /api/nova/tasks/{id}` | `TaskView`, below |
| `POST /api/nova/tasks/{id}/ack` | `{ok: true, acknowledged: true, acknowledgedAt: RFC3339}` |
| `GET /api/nova/images/{id}/{index}` | Authenticated, owned binary image; index is zero-based in the successful output list |
| `POST /api/nova/proxy/text` | `TextRequest` to a settled compatible payload/SSE, or HTTP 202 durable polling receipt |

Additional Go contracts:

| Method And Path | Contract |
| --- | --- |
| `GET /api/nova/tasks?limit=50&before={cursor}` | `{tasks: TaskView[], nextCursor: string}`; descending durable owner history; limit 1..100; cursor is the previous page's last run ID |
| `POST /api/nova/tasks/{id}/cancel` | Idempotent cancellation request, returns current `TaskView` |
| `GET /api/nova/tasks/{id}/response?stream=true` | Settled raw text payload or synthesized SSE; pending is 202; works after reload/new session |
| `PUT /api/nova/settings` | Platform admin or personal owner only; body `{mode, prompts, keywords, revision, password?}`; returns `{revision, config}` |

The embedded frontend disables `/api/nova/ws`. No WebSocket service is provided;
task and queue polling remain authoritative. The new static adapter must keep
embedded mode enabled instead of opening an unauthenticated native socket.

## Typed Image Contract

```typescript
type ImageRequest = {
  mode: "text-to-image" | "image-to-image";
  prompt: string;                         // nonblank, <= 32,000 Unicode characters
  parallelCount: 1 | 2 | 3 | 4;
  protocol?: "openai";                    // normalized configured-provider route
  model?: "";                            // see shared contract gap below
  outputSize?: "auto" | "512" | "1K" | "2K" | "4K";
  aspectRatio?: "auto" | "1:1" | "1:4" | "1:8" | "2:3" | "3:2" |
    "3:4" | "4:1" | "4:3" | "4:5" | "5:4" | "8:1" | "9:16" | "16:9" | "21:9";
  customSize?: string;                    // bounded W x H, e.g. "2048x1536"
  gptImageQuality?: "auto" | "low" | "medium" | "high";
  gptImageBackground?: "auto" | "opaque" | "transparent";
  temperature?: number;                  // legacy OpenAI UI field, 0..2; not dispatched
  images?: Array<{data: string; mimeType: "image/png" | "image/jpeg" | "image/webp"}>;
  referenceAssetIds?: string[];           // owned asset IDs, resolved before inline images
};
```

Maximum JSON body is 12 MiB, maximum individual decoded reference is 8 MiB, and
there are at most ten total references. Bytes are checked against declared MIME
and passed through the shared safe image normalizer. Owned IDs preserve order;
the shared reference resolver deduplicates repeated asset IDs. Image-to-image
requires references; text-to-image rejects them instead of discarding them.
Arbitrary filesystem paths and external image URLs are not image-task inputs.

Size mapping retains the previous embedded bridge's nearest billed-size policy.
`auto`/`512` map into the 1K tier; supported aspect ratios select the closest
catalog ratio; custom dimensions select 1K/2K/4K by the 1800/2800 edge thresholds.
The dispatched dimensions and price come from the existing image price catalog.
New admissions include `X-Nova-Resolved-Size`; this is not a promise that the
upstream provider honors every pixel size. A 4K square maps to `2880x2880` as in
the retained catalog, not an invented uncatalogued price.

Each image is an ordinary creative project/task. All children, project links,
charges and the Nova mapping commit together. No partial admission/charging if
a child insert fails. Uploaded references are normalized before that transaction
and are never stored as base64 in `nova_runs` or business job JSON. Concurrent
upload replays may leave unlinked normalized reference assets for the parent's
orphan maintenance; they cannot create duplicate tasks or charges.

## Idempotency And Task State

Both image and text POST submissions require exactly one `Idempotency-Key`
header, 1..128 visible ASCII characters without whitespace. Generate it once per
logical user action and persist it across retries/reloads. Do not generate a new
key after a timeout, lost response, HTTP 202, or an uncertain provider outcome.

Keys are scoped to authenticated owner and operation kind. The identity is
canonical-JSON hashed; the identity body is not stored in Nova tables. Changed
payload with the same key returns 409. A matching replay returns the original
durable run, even if provider settings have subsequently changed. Text delivery
mode (`stream`/`Prefer`) is not part of business identity. Text wire protocol
aliases normalize before hashing. Business child keys are namespaced hashes.

```typescript
type TaskView = {
  id: string;
  kind: "image" | "text";
  mode?: string;
  status: "queued" | "processing" | "completed" | "failed" | "cancelled";
  result?: {images?: string[]; payload?: Record<string, unknown>};
  warning?: string;
  error?: string;
  cancellationRequested: boolean;
  createdAt: string;
  completedAt?: string;
  acknowledgedAt?: string;
};
```

Image strings retain `URL:` followed by the parent's short-lived signed asset
URL. Re-polling refreshes signed URLs after reload/expiry. The image alias checks
both run ownership and the generated asset's business task link, and streams
only through `Files.Open`, never arbitrary filesystem paths.

`waiting_worker`, `polling`, `uncertain`, and live `running` map to `processing`.
Do not interpret these as safe-to-resubmit failures. The aggregate becomes
terminal only after all children settle. Any successful children then yield
`completed`; mixed outcomes include a warning. All-cancelled yields `cancelled`.
Deleted/missing result assets cannot be returned as successes.

ACK records a durable receipt only. It does not cancel, expire/delete results,
refund, charge, or tell the business runner to settle. Cancellation records its
intent first, calls the existing business cancellation API for each child, and
retries interrupted dispatch through maintenance/polling. Queued tasks can
cancel/refund immediately; live Worker/HTTP submissions retain their real state
until the parent runner resolves them. Closing a text stream/request is NOT
cancellation. No automatic HTTP fallback is initiated by Nova.

## Text Proxy

```typescript
type TextRequest = {
  protocol: "openai-chat-completions" | "openai-responses";
  model?: string;
  stream: boolean;
  requestBody: Record<string, unknown>;
};
type PendingText = {
  taskId: string;
  status: "queued" | "processing";
  pollUrl: string;
  responseUrl: string;
};
```

Underscore protocol aliases are accepted. The native request body's `stream`,
if supplied, must match the outer flag and is removed before durable dispatch.
It never changes Worker Protocol v1. Messages/input, tools, tool choice,
reasoning, response-format/schema options and model constraints map to the
existing jobs text DTO. Chat allows at most twenty messages; roles are checked.
The body limit is 32 MiB, tool definitions are at most 64,000 bytes, individual
option JSON is bounded, and the normalized job passes shared creative bounds.

The shared vision normalizer stores owned `picpilot-asset:` placeholders, not
base64 or stale URLs. Only the existing runner materializes inline HTTP bytes
or fresh signed Worker inputs. Routing uses the configured platform/personal
text providers and preserves the selected model/protocol. No proxy base URL,
authorization header or model API request is generated by the Nova handler.

`Prefer: respond-async` or `Prefer: wait=0` returns a pending receipt immediately.
Default wait is 20 seconds; `Prefer: wait=N` permits 0..25 seconds. The response
always identifies the durable run through `X-Nova-Task-Id` and `Location` after
admission. A pending 202 is not a model response; fetch `responseUrl` after status
becomes terminal. Failed text responses are 502; cancelled responses are 409.

For completed jobs, non-stream mode returns the original structured compatible
payload, preserving choices, function/tool calls, usage and response output.
Stream mode synthesizes SSE from that settled payload: Chat completion deltas,
or Responses output-item/text/completion events, then `[DONE]`. This is a
compatibility transport, not live token streaming or a second provider request.

## Gallery And Queue Configuration

Mode 1 is always visible to authenticated users; mode 2 is private/unlockable;
mode 3 is hidden and denied server-side. A password applies in mode 2. Passwords
are hashed using the shared password service, never returned or stored in
plaintext. Settings writes use optimistic revisions (0 for first creation).
Omitting password preserves it; empty password removes it. Password length is
8..256 bytes when set. Changing any settings invalidates prior grants. Password
verification is limited to five attempts per authenticated user per minute per
server process; no new cross-instance authentication rate limiter is introduced.

Defaults embed the read-only upstream `backend/prompts.json` (22 entries) and
`backend/blacklist.json` (491 entries), semantically unchanged. Upstream pin:
`7768f3f8d7f47e04c6d18572837a086c7a533161`, Nova 3.1.3. These files are source data,
not runtime state; retain `third_party/nova-image-studio/LICENSE` and AGPL-3.0
provenance with distributions. Gallery content can be independently replaced in
the scoped database. The blacklist route is retained UI data, not an alternative
server risk policy or a claim that those keywords are endorsed by PicPilot.

```typescript
type QueueStatus = {
  concurrencyLimit: number;
  configuredConcurrency: number;
  processingCount: number;
  queuedCount: number;
  pendingCount: number;
  maxQueueSize: 64;
  remainingQueueSlots: number;
  displayConcurrency: number;
  displayQueued: number;
  acceptingNewTasks: boolean;
  retryAfterSeconds?: number;
  serverMessage?: string;
};
```

Counts are owner-scoped pending business child tasks, not in-memory group counts
or other users' activity. Admission atomically caps each owner at 64 unfinished
Nova outputs. Configured concurrency is the sum of enabled image/text provider
limits in that edition's scope; it is not a claim of currently available Worker
heartbeats or a reservation. Actual capacity is enforced by the parent runner.

## Remaining Contracts And Acceptance

- **Image selectors:** a nonempty `model` is persisted in `jobs.ImageInput` and
  restricts both HTTP and pull-worker dispatch to that configured model. An
  unknown or disabled model fails before charging with `IMAGE_ROUTING_MISMATCH`.
  Empty selection uses the configured provider priority chain. Existing Worker
  jobs keep their original model snapshot during reconciliation; model edits
  cannot redirect an active lease. Nova does not silently select another model.
- **Provider options/protocols:** Nova quality/background are preserved through
  existing jobs fields. Nonempty `gptImageStyle` is rejected, not silently
  forwarded. OpenAI's legacy temperature field is bounded but not dispatched,
  matching the retained OpenAI image behavior. Native Google/Grok image wire
  requests and Anthropic/Gemini text requests are not implemented by this Nova
  adapter; unsupported selections fail explicitly. Configured shared adapters
  remain independently owned by the parent. These are parity gaps, not removed
  requirements.
- **Frontend bridge:** remove provider keys/URLs; attach application auth; persist
  stable submission keys and returned task IDs; support 202 text polling and
  `cancelled`; retry polling without repeating charged actions; refresh expired
  signed image URLs through status; use the safe server model catalog. Do not
  import provider configuration from iframe-local storage.
- **Browser acceptance:** parent/Bernoulli must verify source-built canvas reload,
  gallery unlock, Agent tool calls, text/image references, cancel/partial results,
  exports, and same-origin signed assets on desktop/mobile. No browser acceptance
  is inferred from Go tests. WebSocket/live-token streaming remains unavailable.
- **Maintenance:** parent owns normalized reference-orphan cleanup, user/task
  retention/deletion policy, static distribution, and the shared task runner's
  Worker/HTTP recovery. Nova ACK must never be used as a cleanup deadline.

## Verification

Use the installed Go executable and the approved E-drive cache paths; do not
edit module files. Tests use `core/internal/testpg.Open(t)` against the isolated
test database selected by `PICPILOT_TEST_DATABASE_CONFIG_FILE`, never production
credentials. No configuration file contents are printed.

```text
go test -mod=readonly ./nova
go test -mod=readonly ./store ./server -run Nova -count=1
```

Coverage includes strict/deep/duplicate JSON rejection, reference and size bounds,
tool-call SSE, twelve concurrent identical submissions with one atomic charge
set, transaction rollback, queue cap, ownership, reload/new-server mapping,
ACK idempotency, cancellation recovery and live-lease refund safety, gallery
password hashing/revisions, personal isolation, real local mock HTTP image/text
execution, and durable vision placeholder materialization. Test labels and
credentials are fixture-only. No paid model calls or commits are performed.

Verified on 2026-10-26: Nova package and Nova-focused store/server integration
tests pass; `go vet ./nova ./store ./server` passes. The pure package reports
90.8% statement coverage; a five-second fuzz run completed 198,857 executions.
The broader store/server run was not green: separate commerce, translation and
Worker text-telemetry integration tests failed. Those modules were not modified
by this work, and their acceptance is not implied by the Nova-focused results.

Reverified on 2026-10-27 after parent integration: the Nova-focused package,
store and server tests and vet pass. Fixtures assert that `Server.New` mounted
Nova before exercising repeated-registration guards; restart recovery no longer
registers routes manually. A central `RunBackground` test simulates a persisted
cancel intent, observes recovery through database reads only, verifies queued
child cancellation/refunds with zero provider calls, and checks joined shutdown.
That lifecycle test also passes five consecutive runs. No full-suite acceptance
claim is made by this focused rerun.
