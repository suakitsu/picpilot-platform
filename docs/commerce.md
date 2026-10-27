# Commerce Suites And Image Translation

The Go implementation follows the read-only Java CommerceSuiteController,
ImageTranslationController, AdminCommerceSuiteController and the user/admin
API-INVENTORY contracts. It uses existing projects/assets, `CreativeWrite`, task
leases, provider attempts, Local Worker dispatch, and the standard task runner.
There is no second task engine and no browser-owned background work.

## Integration Contracts

Main owns the following shared-file changes:

1. Register `s.registerCommerceRoutes()` and `s.registerTranslationRoutes()`.
   Commerce registration includes its admin routes; all registration is idempotent.
2. At the start of store `refundTask(ctx, tx, t)`:
   `if handled, err := refundCommerceTask(ctx, tx, t); handled || err != nil { return err }`.
   Without this delegation, batch tasks claim successfully but FailTask/CancelTask
   return `no rows in result set`: the legacy path looks for a per-task
   generation_charge, while the actual debit is attached to the batch project.
3. At the end of server `projectTaskView(ctx, t)`, before returning `v`:
   `decorateCommerceTaskView(t, v)`. This is required because the original UI polls
   the generic project endpoint. It restores source metadata and maps succeeded
   to legacy success, cancelled to failed, and nonterminal recovery states to running.
4. In `processHTTPModelTask`, after loading providers:
   `providers = commerceImageProviders(t, providers)`. After reserving an attempt,
   recheck `commerceImageProviderAllowed(t, a.Provider)`; reject a changed routing
   constraint before submission. The existing Local Worker enqueue transaction
   already verifies the selected provider model under its configuration lock.
   These pure helpers apply configured default/fallback models in order; tasks
   without constraints are unchanged.
5. At the start of store `adminTaskRefunded(ctx, tx, t)`:
   `if handled, err := commerceTaskRefunded(ctx, tx, t); handled || err != nil { return err }`.
   In `ReplaceCreativeTaskTx`, before hiding the old task, call
   `linkCommerceRetryTx(ctx, tx, user, oldID, newID)` and propagate its error.
   These keep generic/admin retries compatible with aggregate batch charging.
6. At the start of `projectsRetryTask`, before decoding its body:
   `if s.routeCommerceProjectRetry(w, r) { return }`. This retains feature gating,
   current catalog pricing, source snapshots and idempotency on generic retry URLs.

No additional runner or minute reconciliation loop is required. The shared
`screenUserInput` hook is used before charging/submission, with raw user fields
only and a stable operation key. Provider/system/template text is not classified
as user-authored input. Personal risk policy stays in the shared service.

## API

All routes use existing authentication, envelopes and errors. Both snake_case
and Java camelCase DTO fields are accepted, including nested product/catalog data.

- `/api/commerce-suite`: GET catalog/platforms/templates, POST estimate/generate,
  POST projects (generate alias), GET/DELETE projects/{projectId}, GET/DELETE
  tasks/{taskId}, POST tasks/{taskId}/retry and tasks/{taskId}/cancel.
- `/api/image-translation`: GET languages, POST generate, and the same project/task
  read/retry/cancel/delete routes. Legacy multipart accepts target_language,
  resolution and images. JSON additionally accepts owned reference_image_ids or
  reference_asset_ids and a text-only translation style.
- `/api/admin/commerce-suite`: GET/POST catalog, GET platforms/templates,
  PUT platforms/{code}, PUT templates/{id}. Personal does not mount these routes.

Commerce multipart accepts product_info, selected_slots and overlay_options as
JSON fields plus platform/resolution/reference_url and reference/references files.
Reference IDs are owner-checked. Commerce URLs use the existing SSRF-safe reference
resolver. Translation accepts only uploaded images or owned asset IDs, never remote
URLs. Limits: ten references, 25 MiB per file, 28 MiB combined, 30 MiB multipart.
No credentials, image bytes or transient source URLs are stored in task documents.

An explicit Idempotency-Key must be 8..128 characters. Otherwise a deterministic
content hash identifies the operation. Upload hashes include bytes and filename,
not random asset IDs. Replays return the same project and current linked outputs;
a new explicit key starts a new batch. A different request with the same key is 409.
Unlinked uploads from a failed/replayed request enter the existing deletion queue.

Delete refuses active projects and uses the existing failed-image deletion rules.
Cancellation uses the common engine: queued work refunds immediately; an in-flight
or uncertain submission must be reconciled before a refund or retry is safe.

## Plans And Runtime Configuration

The legacy Java suite renders a deterministic catalog plan, not an additional paid
text-model request. Go persists that plan in generated_copy.plan and the final
per-slot prompt in each durable image task before returning queued outputs.
All eight platform presets, sixteen slot kinds and fifty-two default templates
are provided. TikTok uses a seven-image product gallery with no forced poster text;
prices are confined to confirmed main/promo slots. Runtime product/framing/text
rules also apply to older customized templates. Overlay text is rendered by the
image provider; there is no backend text overlay, forced center crop or upscale.

Admin catalog updates are validated and serialized with existing admin audit
transactions. Enabled presets/templates are honored; duplicate selections are
deduplicated and unsupported slots ignored as in Java. Unsupported provider params
are rejected rather than saved and ignored. Supported params: quality,
output_format, background, moderation, and n=1.

`ActivePrompt` scenes commerce_suite and image_translation append enabled trusted
workflow guidance. Dedicated `RuntimeImageConfig` rows gate each feature, enforce
required_fields (comma-separated or JSON array of dotted input paths), set default
resolution tier from default_size, and constrain default_model/fallback_model.
Explicit user resolution wins; catalog/source aspect ratio wins over a generic
feature size so translation references are never reshaped.

Catalog cost_rule defaults to 1K=6, 2K=24, 4K=30. Runtime feature credits_cost is
the base six-credit unit multiplier: ceil(catalog tier cost * feature cost / 6).
Thus a feature base of 12 doubles the tier price, zero makes it free, and the default
6 leaves catalog pricing unchanged. Estimate and generation use exactly the same
rules. Each task snapshots its accepted cost and prompt; later admin edits affect
new batches/retries only. Personal ignores all platform prices/admin templates,
charges zero, writes no ledger, and routes only through the owner's providers.

## Accounting And Resilience

A batch commits its project, source links, all task rows, allocation records and
one aggregate ledger debit atomically. Insufficient credit or a late invalid
reference rolls everything back. Existing common task submission creates each task
at zero charge inside that transaction, then its allocated cost is restored before
commit. `commerce_batches` and `commerce_batch_items` are business allocations only.

Failed/cancelled members verify the original aggregate debit and refund their own
allocation with the shared unique generation_refund task key. Siblings and callback
duplicates cannot double-refund. A retry is a new standard generation_charge with
one replacement link; the old task is hidden from output counts but retains ledger
history. Successful or uncertain provider work is never represented as a refunded
failure just because the browser navigated away.

Each translation task contains exactly one source asset and its filename/index,
dimensions, target language, style, and original source ID in prompt_snapshot.
Fourteen languages are supported. Translation requests preserve non-text pixels,
layout, brands, codes, amounts and unreadable text; they never invent text. The size
mapping requests a compatible model aspect/tier only. Actual provider output
dimensions are retained by the existing asset pipeline.

## Verification

Use the configured Go executable/caches and isolated PostgreSQL test harness.
No production connection, real payment/model/email request, or deployment is needed.
Focused suites are `go test ./commerce ./store ./server -run 'Commerce|Translation|CatalogDefaults|ProductVisibleCopy'`.
Tests exercise catalog compatibility, transaction rollback, concurrent idempotency,
refund/retry allocation, owner isolation, source aspect preservation, runtime/admin
configuration, raw-input risk screening, Personal, and durable Mock execution.
