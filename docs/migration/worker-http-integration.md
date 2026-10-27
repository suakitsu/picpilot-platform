# Worker HTTP And Persistence Handoff

This implements the unchanged v1 wire contract on the new PostgreSQL baseline.
This scope does not implement the parent's business-task dispatch or credit settlement.
The parent has since added `execution_worker.go`, `store/dispatch.go` and migration
`0008_dispatch.sql`; its worker dispatch tests are included in the verification
below. Router mounting is still a parent-owned integration step.
See `workerprotocol-integration.md` for the completed pure-library stage and
`local-worker-protocol-v1.md` for the byte-identical authority.

## Files And Registration

- `core/store/migrations/0002_worker.sql`: hash-only tokens, token-bound worker
  instances, separate durable worker jobs, lease/terminal fingerprints, artifact
  receipts and owned-asset links. Extends generic provider lease limits to 1800
  and timeout minimum to 1 without changing HTTP-provider maximums.
- `core/store/worker_tokens.go`: token lifecycle, current-secret authentication,
  heartbeat binding and safe administrator status.
- `core/store/worker_jobs.go`: explicit normalized enqueue, atomic claims,
  recovery, unclaimed fallback and claim-time input materialization.
- `core/store/worker_callbacks.go`: renew/complete/fail, artifact receipts,
  input access and runner artifact lookup.
- `core/store/workers_test.go`: disposable-PostgreSQL and pure binding tests.
- `core/server/workers.go`, `workers_test.go`: raw worker HTTP adapters,
  administrator/owner token APIs, actual normalized file storage and HTTP tests.

The parent calls `s.registerWorkerRoutes()` from `routes()`. It is idempotent;
this task does not modify `server.go`. `workerHTTPS` calls the parent's
`s.secureRequest(r)`, which only trusts TLS or explicitly allowed immediate proxy
peers. Deployments must keep the backend private and overwrite forwarded headers.

## HTTP Surface

All worker routes use `Authorization: Bearer <worker-token>` and raw protocol
JSON, never application envelopes or user JWT authentication:

| Method | Path below `/api/internal/picpilot-worker/v1` |
| --- | --- |
| POST | `/heartbeat` |
| POST | `/claim` |
| POST | `/jobs/{jobId}/renew` |
| POST | `/jobs/{jobId}/artifacts` (strict multipart) |
| POST | `/jobs/{jobId}/complete` |
| POST | `/jobs/{jobId}/fail` |
| GET | `/inputs/{jobId}/{index}` |

Claim is one task per response, with cancellable 20-30 second polling and exactly
`{task,empty,serverTime,waitedMs}`; empty is HTTP 200 with `task:null`.
There is no `businessTaskId`, provider credential or raw token in the response.

The four original `/api/admin/ai-config` token/status routes use `s.auth/s.admin`
and application envelopes. Additive `/api/me/ai-config` equivalents use personal
edition plus owner-role checks and owner-scoped tokens/providers. Raw issuance is
allowed only through HTTPS create/regenerate and returned once. Reads return
metadata/prefixes, not token hashes or secrets. Regenerate/revoke invalidate old
raw secrets even for receipt replays.

## Parent Store APIs

```go
options := store.WorkerOptions{
    PublicBaseURL: cfg.PublicBaseURL,
    SigningKey: cfg.WorkerSigningKey,
    // GraceSeconds defaults to 15; InputTTLSeconds defaults to 300.
}
job, created, err := db.EnqueueWorkerJob(ctx, store.EnqueueWorkerJobInput{
    TaskID: businessTask.ID,
    ProviderScope: provider.OwnerScope,
    ProviderID: provider.ID,
    Capability: workerprotocol.Text,
    Protocol: workerprotocol.Responses,
    Model: provider.Model,
    Request: normalizedProtocolJSON,
    // InputAssetIDs: owned image-edit references, in source order.
    // TextInputs: optional structured image URL bindings described below.
}, options)
```

`Request` is a normalized provider-compatible payload, NOT `jobs.Payload` or a
business envelope. Validation rejects unsupported models/protocols, streaming,
secrets, base64 inputs and unknown top-level fields before insertion. Disabled
providers do not enqueue. The stored request and ordered owned references form
the enqueue fingerprint. `created=false` returns the existing durable job.
An image worker provider accepts both image-generation and image-edit jobs:
the normalized request and owned reference list select the job protocol, while
the model remains fixed to `gpt-image-2`. Text protocol matching stays exact.

| API | Integration responsibility |
| --- | --- |
| `LatestWorkerJobForTask(ctx, taskID)` | Resume before provider filtering, including disabled/deleted providers and results not yet settled after a restart. `ErrNotFound` means no worker dispatch exists. |
| `WorkerJobForTask(ctx, taskID, jobID)` | Internal runner snapshot; do not expose it over HTTP. |
| `ReconcileWorkerJob(ctx, taskID, jobID, localWaitSeconds)` | Returns `(WorkerJob, mayTryNextProvider bool, error)` after atomic cancellation/expiry/requeue/offline-fallback reconciliation. A replayed committed offline cancellation also returns true. |
| `RecoverWorkerJobs(ctx, scope)` | Bounded cancellation/expiry/requeue sweep; also runs before claim. Parent scheduler should repeat until caught up. |
| `FallbackWorkerJob(ctx, taskID, jobID, waitSeconds)` | Returns true only after committing unclaimed fallback cancellation. Do not start HTTP fallback before that commit. |
| `WorkerArtifactAsset(ctx, taskID, artifactID)` | Resolve a completed image's owned asset for existing business settlement. |
| `ClaimWorkerJob`, `RenewWorkerJob`, `CompleteWorkerJob`, `FailWorkerJob` | Store transitions used by HTTP; never settle/refund a business task. |
| `SaveWorkerArtifact`, `DiscardWorkerArtifactAsset` | Validate/commit one receipt; clean up a losing candidate only if it is unreferenced. |
| `WorkerInputAsset` | Verify live job/token/worker/signature and return owned asset metadata. HTTP streams `Files.Open`, never redirects credentials or fetches remote input. |
| `CreateWorkerToken`, `RegenerateWorkerToken`, `RevokeWorkerToken`, `AuthenticateWorkerToken`, `WorkerHeartbeat`, `WorkerAdminState` | Token/binding/status operations. |

On `succeeded`, the parent's runner owns the single business-task completion.
Text output is in `Job.ResultJSON.result`; image output contains `artifact_id`.
`failed` contains a sanitized error and allows the parent's next provider policy.
`expired` with queue policy is recovered, never run concurrently with HTTP.
The runner retains its separate renewable business lease while waiting. No
worker callback changes a business lease, task status, charge or refund.

Exact runner-facing signatures:

```go
func (*Store) EnqueueWorkerJob(context.Context, EnqueueWorkerJobInput, WorkerOptions) (WorkerJob, bool, error)
func (*Store) LatestWorkerJobForTask(context.Context, string) (WorkerJob, error)
func (*Store) ReconcileWorkerJob(context.Context, string, string, int) (WorkerJob, bool, error)
func (*Store) WorkerArtifactAsset(context.Context, string, string) (Asset, error)
```

Recommended `processHTTPModelTask` integration order:

1. Before filtering disabled providers, look up the latest worker job for `t.ID`.
   Reconcile it using its provider's local wait. Do not lose an outstanding job
   when the provider list changes. Do not serialize the internal snapshot.
2. Queued/leased means wait or release the business runner back to the existing
   durable scheduler; the next run resumes the same job. Keep the business lease
   renewed while waiting. Queue age is not text processing time. Do not reserve
   or submit an HTTP attempt while this state exists.
3. On worker success, read `Job.ResultJSON`. Image `artifact_id` is a worker
   receipt ID (`awa_...`), not the business asset ID. Resolve it through
   `WorkerArtifactAsset(ctx, t.ID, receiptID)`, then call the existing
   `CompleteTask(ctx, t, result)` with `result.artifact_id = asset.ID` and asset
   width/height/content_type/model. Do not call `saveImage` again.
4. Text success wraps the original provider-compatible result in `result`.
   Extract it with `workerprotocol.ExtractText`, and call `CompleteTask` with
   the existing business shape `{text,payload,model}`. Use the job's model.
5. Only `mayTryNextProvider=true` authorizes continuing the next-provider chain:
   genuine `failed`, committed unclaimed offline timeout, or the narrowly scoped
   unclaimed configuration cancellation described below. Re-enqueue of the same normalized request returns the same
   terminal row, not a fresh attempt. Retain that provider exclusion when
   continuing; do not turn busy/leased/expired into another HTTP submission.
   This bool is a concurrency/terminal-state gate, not a retry recommendation.
   Use `workerprotocol.Retryable(0, job.Job.ErrorCode)` for v1 failure policy;
   the strict fail request has no `retryable` field. Non-retryable invalid-input,
   model/auth/policy failures still require the parent's stopping policy.
6. Queue-policy expiry requeues the same job with a fresh lease on next claim.
   Fallback-policy expiry does not authorize automatic HTTP retry. Treat it as
   an expired execution according to business failure policy. Business
   cancellation and exhausted provider chains use existing `FailTask`/cancel
   settlement, never a worker-written ledger entry.

The database also forbids two unfinished worker jobs for one business task,
including across providers or changed normalized requests. Parent HTTP attempt
reservation remains the independent guard for HTTP retries and ambiguous submits.
Releasing pending work into `waiting_worker` for a later scheduler pass is
supported and avoids holding a runner goroutine. Claim/reconcile treat that
business status as active; `cancel_requested_at` makes it inactive even before
the business row reaches terminal cancellation. The parent's recovery must not
mistake such a job for an unsubmitted HTTP attempt or issue an early refund.

Provider `settings.worker_ids`, when a nonempty array, is an exact worker-ID
allowlist. Claims require both that match and the capability advertised in the
worker's latest heartbeat (as well as the claim capability). Empty/absent means
unrestricted; malformed/duplicate/invalid IDs fail closed. Status online-worker
counts apply the same allowlist. Changing the list does not invalidate a lease
already granted; revoke its token or cancel the business task to stop it.

Provider disable/removal/model/text-protocol changes do not cancel live leases
or discard their results. `ReconcileWorkerJob` cancels queued incompatible jobs
with internal reasons `WORKER_PROVIDER_DISABLED`, `WORKER_PROVIDER_CHANGED`, or
`WORKER_PROVIDER_REMOVED`. These are internal job reasons, not new v1 wire error
codes. Such cancellation permits the next provider only when `attempt == 0`
and the stored policy is `fallback`. Queue policy never silently becomes HTTP
fallback. Parent integration should honor the returned bool for cancelled jobs,
not recognize only `LOCAL_WORKER_OFFLINE`. Active leases always return false.

## Text Image Inputs

v1 allows structured image URLs in Responses/Chat payloads but explicitly forbids
binary `inputFiles` on text jobs. The wire rule is preserved.

For each owned image URL in a normalized text request, supply:

```go
TextInputs: []store.WorkerTextInput{
    {AssetID: assetID, JSONPointer: "/input/0/content/1/image_url"},
    // Chat: /messages/0/content/1/image_url/url
}
```

Only `type:input_image` / `type:image_url` structures are writable. The enqueue
adapter stores `picpilot-asset:<id>` instead of an expiring link. Claim replaces
those exact locations with fresh five-minute signed HTTPS asset URLs. References
are owner-checked; numbers and text are preserved using structured JSON parsing.
No binary/base64 image data is persisted. The parent must supply the bindings;
unbound external URLs are not automatically re-signed.

Text image URLs deliberately use the parent's bearer-free signed asset endpoint:
an upstream model cannot send the worker token. They remain valid until their
short expiry unless the underlying asset is deleted. The current asset endpoint
does not bind them to the worker job's cancellation state. Image-edit `inputFiles`
use the v1 token-protected input endpoint and reject cancellation immediately.

## Concurrency And File Boundaries

Each action rechecks the current token hash/revocation in its transaction.
Token row SHARE locks serialize acceptance against rotation/revocation. Worker
locks plus provider advisory locks serialize global capacity, with FIFO queued
selection and `FOR UPDATE SKIP LOCKED`. Job row locks serialize receipt/terminal
updates, expiry and cancellation. Task SHARE locks serialize acceptance against
business cancellation. Time is read from `clock_timestamp()` after locks.

Artifact normalization uses the parent's `saveImage`, outside held worker
transactions so concurrent uploads cannot exhaust the pool while waiting for a
second database connection. Acceptance is rechecked after storage. Concurrent
first uploads can create temporary candidates, but exactly one receipt commits;
normal failures and losing candidates are deleted only when unreferenced.
An uncertain commit never authorizes deleting a referenced artifact.

## Remaining Acceptance Gates

- Parent must register routes, dispatch normalized payloads, poll/recover jobs,
  renew its business lease and settle exactly once. This task does not silently
  add HTTP dispatch or fallback into the runner.
- Configure provider settings with v1-specific bounds: concurrency 1-32,
  local wait 1-300, text timeout 1-300 starting after lease, lease 30-1800,
  correct fixed local IDs/models, disabled defaults, no local provider secrets.
- Add background orphan reconciliation for process death between file/asset
  creation and receipt insertion. Normal failure/conflict cleanup is implemented;
  filesystem and PostgreSQL are not a shared transaction.
- Parent outer CORS/OPTIONS/panic handling must preserve raw protocol errors on
  worker paths. The worker middleware preserves raw errors for its own failures,
  but cannot intercept earlier application middleware outside its scope.
- Browser administration UI, production proxy deployment, multi-process load,
  Java-row migration/import and business/refund end-to-end acceptance remain
  separate gates. No paid upstream request or production token was used.

Tests use `internal/testpg.Open(t)` disposable schemas only, the configured test
database file without printing its contents, fixed test-only token labels, and
the specified E-drive Go caches. No module manifests or parent-owned files were
edited and no commits were made.

## Verification Snapshot

On 2026-10-26, using the specified Go 1.27.3 executable and E-drive caches:

```text
go test -mod=readonly -count=1 -run='Worker|FailureLocation|FailureReplay' ./workerprotocol ./store ./server
go vet -mod=readonly ./workerprotocol ./store ./server
```

Both passed with the isolated PostgreSQL test database enabled. The selected
suite includes the parent's dispatch/settlement integration tests, the full
worker HTTP surface, a real 20-second empty long poll, token rotation/revocation,
trusted-proxy rejection, cancellation, exact and changed-content replays,
concurrent claims/uploads, provider/worker capacity, mixed generations/edits,
worker-ID allowlists, provider disable/removal/model/protocol changes and expiry.
Claim/enqueue/reconcile/artifact concurrency cases also passed three repeated
runs. The full pure protocol suite passed at 85.0% statement coverage; strict
decoder fuzzing passed earlier in the pure-library stage.

A broader intermediate store-package run reported failures in video polling
tests outside this ownership; these were reported to the parent, not edited or
hidden. The current worker-focused suite and vet are green. Source route presence
and these tests do not mark all original Java/browser contracts parity-complete.
