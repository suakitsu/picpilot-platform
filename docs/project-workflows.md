# Project and Assistant Workflows

## Integration

Call `registerProjectRoutes()` and `registerAssistantRoutes()` from `Server.routes`.
Both registrations are idempotent. The existing shared product-image upload route
remains untouched. Run `RunCreativeWorkflows(ctx)` beside `RunTasks(ctx)` with the
same shutdown context. It reconciles completed listing copy and creative planning
tasks; cancellation returns `ctx.Err()`. Project/chat reads also reconcile locally.
The current shared `RunBackground` already starts both runners; do not start duplicates.
Only the shared executor calls models. All submissions use `jobs.Payload{Version:1}`.

Migration `0004_creative.sql` adds project reference links, request idempotency,
chats, versions, messages and durable planning turns. Existing projects/tasks/assets
and the credit ledger remain authoritative. `Store.CreativeWrite` wraps each business
operation; `SubmitProjectTaskTx` preserves `SubmitTask` hashing and ledger semantics
inside that transaction. A parity test covers both task entry points.

## Frontend Contract

Responses retain the shared `{success,code,message,data,request_id}` envelope.
`success:true` acknowledges a request; it does not mean a model task succeeded.
Use a fresh 8-128-character `Idempotency-Key` for each deliberate generation and reuse
it for retries of the same HTTP request. Legacy callers without a key get a stable
request-derived key; an identical request will replay until given a new key.

| Operation | Pending response / polling |
| --- | --- |
| `POST /api/projects/{id}/generate-copy` | `project_id`, `task_id`, `status:copy_queued`, `generated_copy`; poll project detail or the text-task endpoint. Completion is `copy_generated`; invalid/failed output is `copy_failed`. |
| Optimize, domestic selling-point suggestions, canvas text | `task_id`, actual task `status`, `safe_error`; poll `GET /api/projects/text-tasks/{taskId}`. On success the legacy result fields appear. |
| `POST /api/assistant/chat` | Same queued fields; poll `GET /api/assistant/tasks/{taskId}` for `reply`, `intent`, action fields, confidence and actual model. |
| `POST /api/projects/creative-chats/generate` | Initially `chat`, `task_id`/`planning_task_id`, `status:queued`, `project:null`, `final_prompt:""`, `version_no:0`. Poll `GET /api/projects/creative-chats/{chatId}`. `pending_tasks` tracks planning; messages expose failures; versions appear only after a valid image plan commits. Replaying a completed submission returns the image task ID/status when applicable. |
| Listing images/package, linked A+, single creative/A+ images | Return persisted queued image tasks/project detail immediately. Poll project detail, project image tasks or generic task detail. |

Creative submit's `data.chat` is the full `{chat,messages,versions,pending_tasks}` view;
the new chat ID is `data.chat.chat.id`. Chat polling returns that full view directly
under `data`. Follow each version's `task_id` for image completion. Keep the stable
`planning_task_id` separately: a replay's `task_id` can advance from planner to image.

Pending task states include `queued`, `running`, `waiting_worker`, `polling` and
`uncertain`. Stop automatic resubmission on `uncertain`; it may already be paid.
`failed`/`cancelled` are terminal. Invalid structured text returns a sanitized 502
on result retrieval, never a fabricated reply. Creative planning failures appear
on the assistant message with `status:failed` and `safe_error`.
Canvas completion retains the complete upstream `payload` and top-level structured
fields, including tool-only replies. It is not reduced to a text string.
Generic project polling delegates commerce/translation task decoration to the existing
shared hook. Retry those project types through their source workflow endpoints, not
`/api/projects/{id}/images/{taskId}/retry`, which returns 409 `WORKFLOW_RETRY_REQUIRED`
before charging rather than dropping business payload fields. Store replacement
bookkeeping invokes `linkCommerceRetryTx` to preserve batch membership for shared retries.

## Business Behavior

- Project create/detail/list, facts, uploaded/linked images, configurable listing slots,
  package, linked A+, image history/delete/retry and all legacy assistant routes exist.
  CamelCase DTO aliases and JSON/multipart creative submissions are accepted.
- The project-level `include_aplus` survives copy generation. An omitted package flag
  inherits it; explicit false disables the package's A+ component. Enabled listing jobs,
  linked A+ project/job, references and all charges commit together or not at all.
- Listing generation requires valid generated copy and a main product image. Confirmed
  facts override supplied facts; main images override inferred text color hints.
- Creative chat planning is a real queued text task. Conversation/clarification costs
  no image credits. Valid image plans create a project, image task, message pair and
  monotonically numbered version atomically; at most ten active versions are allowed.
- References preserve composer order and owner checks. History selections must belong
  to the same chat. Revisions with no new selection inherit the latest result first,
  then prior references, capped at ten. Explicit `reference_policy:none` disables use.
- Chat versions aggregate into one project-history entry. Deleting a chat-backed project
  deletes that chat's history and related projects. Active/uncertain work must first be
  resolved or cancelled. Only failed image tasks can be individually hidden. Manual
  retry creates a new charged task, preserving the prior attempt and refund audit.
- JPEG/PNG/WebP references use the shared image normalizer and decoded dimensions.
  Exact requested canvases, including legacy A+ sizes such as `970x600`, reach the
  provider unchanged; no nearest-size substitution or center crop is performed.

## Safety and Boundaries

Owned text image references persist as `picpilot-asset:<id>` only; external/data/signed
images pass through `prepareTextInput`. Keys, inline image data and signed URLs are
not persisted in task requests. Text has at most four images; image tasks at most ten.
External reference ingestion uses the shared SSRF-safe client, its explicit local-dev
opt-in, bounded reads and image pixel ceilings. Responses bound every string to 32000
characters; schemas, tools and structured content are validated without dropping tools.

Explicit text model/protocol selectors require any matching enabled provider and are
stored in `jobs.TextInput.Model/Protocol`; unselected workflows do not pin the first
provider. The shared executor owns per-attempt `jobs.AdaptTextInput` and safe fallback.
Assistant actions are constrained to known local routes and never submit image work.
Clean installs return empty histories and disabled assistant status, with 503 for
unconfigured capabilities; there are no seeded operators, credentials or domain names.
Project/chat list endpoints currently return the most recent 500 entries.

Original generation input passes `screenUserInput` before task submission or credit
reservation, using a stable operation-scoped request key. This includes listing copy,
listing/package/linked A+, single creative/A+, creative planning, retries, optimize,
selling-point suggestions, assistant user messages, and canvas user text in either
Messages or Responses Input. Screening excludes operator/system prompts, assistant
history, tool results, image URLs and generated planner/copy text. Blocked submissions
return 422 `CONTENT_BLOCKED`; account restrictions return 403 `ACCOUNT_RESTRICTED`.
Repeated blocked requests with the same key count once. Canvas/single/retry/planner
screen before external reference ingestion. No internal reconciliation reclassifies
model-generated text as user input.

Raw project asset queries, reference linking and chat-asset eligibility exclude
`worker-candidate` before any link/display-type override. Accepted `worker-output`
assets become visible through the same paths. Shared asset ownership checks still apply.

## Runtime Configuration

`Store.ActivePrompt` supplies the newest enabled scene. Templates supplement the
built-in factual/safety instructions and retain explicit user text and reference roles.
No user message/tool item is rewritten to inject templates. Private image template
content/checklists stay out of project prompt snapshots, including manual retries.

| Workflow | Scene and ordered fallback |
| --- | --- |
| Listing copy | `copy/listing`, then `copy/title` |
| Listing images | `image/brief` |
| Creative image | `image/creative`, then `image/single` |
| A+ image | `image/aplus`, then `image/single` |
| Creative planner | `prompt/creative-chat`, then `prompt/optimize` |
| Prompt optimization | `prompt/optimize` |
| Selling-point suggestions | `copy/domestic-selling-points` |
| Assistant | `assistant/global` |
| Canvas text | `canvas/text` (separate prepended system item) |

The first five scene families and prompt optimization preserve the Java scene lookup
conventions. The last three are explicit Go extension scenes, not claimed legacy
template compatibility. `template_id`, when present, must match the active template
selected for the image kind; otherwise 409 `TEMPLATE_NOT_CONFIGURED`. Combined prompt
fields above 32000 characters fail with 409 `PROMPT_TEMPLATE_TOO_LARGE`, never truncation.

`RuntimeImageConfig(kind)` applies to the seven known listing types, `creative` and
`aplus`. Missing rows use built-in defaults/shared tier pricing. Exact choices:

| Saved field | Behavior |
| --- | --- |
| `enabled` | Disabled listing slots are omitted; all-disabled listing generation returns 409 `IMAGE_TYPES_DISABLED` (copy may still run with empty briefs). Disabled single image kinds return 409 `IMAGE_KIND_DISABLED`. |
| `image_name`, `sort_order` | Used for task names, copy image briefs and stable listing submission order. |
| `default_size` | Exact canvas when no explicit/inferred user size or prior chat/project size exists. Explicit sizes win; no crop/substitution. New project A+ defaults are stored at creation; later package requests preserve that choice unless overridden. |
| `credits_cost` | Used at the configured default size. A different explicit/inherited size uses shared `image_pricing` tier cost, not the lower configured default price. Personal/demo remain zero. Missing A+ config retains legacy canvas and 1K-tier price. |
| `required_fields` | Free-text verification checklist in private image instructions. It is not parsed as a schema or treated as verified facts. |
| `default_model`, `fallback_model` | Nonempty values fail explicitly with 409 `IMAGE_CONFIG_MODEL_UNSUPPORTED`. `jobs.ImageInput` and the executor currently have no corresponding routing selectors; silently dropping these values would be incorrect. |

Copy briefs snapshot enabled kinds, sizes and prices at copy submission; actual image
submission resolves current configuration again and updates the persisted briefs.
Linked A+ and enabled listing charges remain one transaction. Creative conversation
can continue while image generation is disabled: configuration is checked again when
an image plan is applied, and errors become terminal `safe_error` on the planning turn
without an image charge. Retry retains its prior exact canvas but uses current pricing,
enabled gate and active template. Database/configuration errors fail closed.

## Remaining Gaps

Image model routing is the explicit shared-executor dependency described above.
Only the seven listing kinds plus creative/A+ are recognized here; arbitrary new admin
kind rows do not create new business routes. Templates are text guidance, not a template
language, and model output still requires review. Listing draft/compliance instructions run
as one validated text task rather than the old two-call/local-fallback sequence.
Demo text execution needs kind-specific JSON for `listing_copy`, `creative_plan`,
`assistant_chat` and `domestic_selling_points`; generic mock prose is correctly rejected.
Failed/replayed upload ingestion can leave unlinked owned assets for later retention
cleanup; it cannot create an extra charged generation.

Tests use `httptest` and disposable PostgreSQL schemas via `internal/testpg.Open`,
including 45-credit rollback versus the full 48-credit package, concurrent replay,
task-hash parity, owner/chat isolation, structured vision/tool persistence, exact A+
size, planner continuation, version limits, output limits and ambiguous-task retry refusal.
Policy regressions cover every original-input entrypoint, risk-key deduplication,
no blocked-task/asset/charge side effects, internal prompt/output exclusion, private
retry snapshots, structured Responses/tool preservation, configured size/price/order,
all-disabled listing types, template limits and unsupported image-routing settings.

### Verification Snapshot (2026-10-27)

Installed Go 1.27.3, `GOMAXPROCS=2`, the requested E-drive Go caches and guarded
isolated PostgreSQL test config were used. No paid provider requests were made.
Focused project/creative/canvas/assistant/policy and worker-candidate tests pass.
`go test ./creative ./store ./server -count=1 -timeout=180s` with only the following
two known shared-refund cases excluded passes (0.04s / 41.03s / 115.47s respectively):

- `TestPostgresCommerceFailureCancelRefundConcurrentAndRetry`
- `TestPostgresTranslationRetryRefundAndDelete`

Both excluded tests independently fail on all three reruns with `no rows in result
set`. The shared `refundTask` still looks only for per-task `generation_charge`,
whereas these business batches use aggregate charges. Main must wire the existing
`refundCommerceTask` hook into shared refund handling; those shared files were not
modified here. This is not a claim that the unfiltered full-core/race suite is green.
