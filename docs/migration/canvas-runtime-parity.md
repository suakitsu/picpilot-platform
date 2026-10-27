# Canvas Runtime And Backend Delegation

This is an implementation inventory, not an acceptance claim. Paths and shapes
come from the read-only original source. `api-inventory.json` lists every Java
mapping separately from the 12 runtime entries in `canvas-runtime-inventory.json`.
The latter includes one conditional WebSocket route and one dynamic image path.

## P0 Nova Contracts

| Runtime contract | Shape and Go business dependency |
| --- | --- |
| `POST /api/nova/tasks` | `{taskId}`. Bridge `image.submit` creates 1-4 creative-image projects through `POST /api/projects/creative-images` (JSON or multipart), after parent confirmation and `GET /api/me`. Ordered references, intent `canvas_generation`, partial successes and non-duplicate submissions matter. |
| `GET /api/nova/tasks/{taskId}` | `{id,status,result:{images},warning?,error?}`. Bridge `image.status` calls `GET /api/projects/{id}`; maps Java `success` to Nova `completed`, running to `processing`; results carry `URL:` asset references. Preserve reload/session task mapping. |
| `POST /api/nova/tasks/{taskId}/ack` | `{acknowledged:true}` is a local receipt, not a billing/terminal callback. |
| `GET /api/nova/queue-status` | `NovaQueueStatus` fields are in the runtime JSON. Original bridge returns constant display counters; do not mistake those for multi-instance queue accounting. |
| `POST /api/nova/proxy/text` | Bridge `text.generate` invokes `POST /api/projects/canvas-text` with `{protocol,model,request_body,feature}` and receives `{response}` inside ApiResponse. Preserve raw compatible choices/tool calls. The browser optionally synthesizes SSE; that is NOT Worker v1 streaming. |
| `GET /api/nova/proxy/models` | `{data:[{id}]}` safe catalog. Original bridge uses fixed model IDs and intercepts upstream query parameters. Never pass through/log API keys or arbitrary provider URLs from the iframe. |

Source anchors: `canvas-lab/picpilot-canvas-bridge.js:408`,
`openImage-ts/src/components/canvas-lab/CanvasLab.tsx:384`, and
`third_party/nova-image-studio/frontend/src/lib/ccode-task-client.ts:209`.

The complete canvas workflow also needs `GET /api/videos/config`,
`POST /api/videos/script-preview`, multipart `POST /api/videos/tasks` with the
existing `Idempotency-Key`, `GET /api/videos/tasks/{id}`, and public video/image
reads. Bridge `video.generate` is not covered by an image-only backend.

## Retained Runtime Surface

- `GET /api/nova/config`: `{promptGalleryMode?:"1"|"2"|"3",promptGalleryPasswordEnabled?:boolean}`.
- `GET /api/nova/prompts`: `Array<{title:string,content:string,type:1|2}>`.
- `GET /api/nova/blacklist`: `{keywords:string[]}`.
- `POST /api/nova/prompt-gallery/verify`: `{password}` -> `{ok:boolean}`.
- Dynamic `GET /api/nova/images/{path}`: binary image/asset behavior or a compatible PicPilot public-file URL replacement.
- `/api/nova/ws`: native Nova task/queue subscription channel, explicitly disabled when embedded; HTTP polling must remain functional. Worker v1 uses neither SSE nor WebSocket.
- IndexedDB/localforage assets, node/edge/project persistence, undo/redo, crop/export, prompt gallery/import, Agent tool calls, reverse prompt, prompt optimization, GIF grid, references, history, text/image/video and full-resolution output all remain acceptance scope.

The gallery routes are NOT intercepted by the current bridge. They cannot be
discarded because the main image/text actions work. External prompt sources
and dynamic asset fetches are listed as unresolved/dynamic callers, not silently
claimed as Java endpoints.

## Clean Source Requirements

1. Nova source evidence: `third_party/nova-image-studio/UPSTREAM.md` records
   commit `7768f3f8d7f47e04c6d18572837a086c7a533161` (3.1.3). The checkout contains
   frontend source, frontend lockfile, license and local modifications, but no
   Nova backend source. Preserve the actual modified frontend, not just the pin.
2. The original Dockerfile runs `npm ci` and the frontend build with
   `NEXT_PUBLIC_PICPILOT_EMBEDDED=true`, injects both bridge/branding scripts,
   and overlays output onto an upstream image. This does not satisfy a Go-only
   business/runtime backend. Retained runtime routes need Go/static replacements.
3. Infinite Canvas is pinned in `scripts/fetch-canvas-lab-upstreams.sh` to
   `705fdd9b2f7bfe6fd58156fd5c1cdb1ad251c767` (documented v0.14.0). Its corresponding
   source directory is absent from this reference checkout. Existing
   `canvas-dist/infinite` output alone is not source completeness or a verified
   clean build. Parent must obtain pinned clean source in its authorized area,
   retain modifications/license/build inputs, and demonstrate a fresh build.
4. Export only source, package/lockfiles, declared assets, provenance, licenses
   and reproducible build scripts. Exclude dependency caches, generated bundles
   as substitutes for source, uploads, databases, user workspaces, credentials,
   environment files, runtime browser storage and private deployment state.
5. Verify both editions from a clean checkout with fresh temporary app state,
   pinned dependencies and mock providers. Compare source-built behavior on
   desktop/mobile, including iframe origin/window checks, loading/branding,
   upload/reference ordering, task reload recovery, assets and exports.
6. Retain upstream license notices and corresponding-source obligations as
   release-review gates. Repositories remain PRIVATE; this task does not
   authorize publishing upstream modifications or any application source.

No build, source download, deployment, database interaction or paid request was
performed by this inventory work. Commands in original deployment scripts were
read as source evidence, never executed.

## Full Backend Work Packages

| Package | Original controllers/contracts | Acceptance still required |
| --- | --- | --- |
| Accounts/storage | Auth, Public, PublicFile, CanvasWorkerInput | Full password/email/OAuth/avatar/session flows, ownership, HEIC/image normalization, signed reads, local/object storage and safe CORS. |
| Generation/history | Project, Assistant, CommerceSuite, ImageTranslation | Listing/A+/linked-A+/creative chat/version history, copy/facts/templates, translation, estimate/generate/retry/delete, assistant and all response shapes. |
| Canvas/runtime | Nova contracts above plus Project/Video | Both source-built experiences, state/assets/export, exact bridge shapes, tool calls and no direct provider credentials in iframe traffic. |
| Providers/queues | Existing HTTP image/text/video adapters plus Worker v1 | Personal encrypted provider isolation, platform routing, submission ambiguity, priority/fallback, global concurrency, restart recovery and no double task submission. |
| Video | Video, VideoWebhook, VideoAdmin, VideoTaskAdmin | Full variant configuration, idempotent submit/poll/webhook, storage, history/deletion, failure/refund and administrative inspection. |
| Billing/admin | Billing, Admin, AdminAiConfig, AdminTextCall, AdminCommerceSuite, Message | Payments/reconciliation/ledger/redeem/benefits/refunds, users/risk/audit/costs/configuration and messages; no personal billing/admin dependency. |
| Distribution | Both editions, shared core and source trees | Private repository pushes, independent clone/build/start, first-run, backups/restores/upgrades and three-language verified docs. |

All 155 Java method/content variants remain explicit checklist rows. Platform-only
does not mean removed from platform scope; personal adaptations need their own
policy and workflow tests.

## Parent Baseline Notes

At inspection the parent had four matching original routes: `POST /api/auth/login`,
`GET /api/me`, `PATCH /api/me`, `GET /api/public/ping`. They are marked
`handler_present_parity_unverified`, not complete. New setup/provider/health routes
are separately enumerated. Presence does not establish compatibility: the inspected
login decoder only accepted email/password while the original web caller also
sends remember_me/turnstile_token; ping omitted the original data.service field.
These are source-level follow-up items, not changes made by this task.

Worker provider configuration also needs the normative v1 bounds (text catalog,
max concurrency 1-32, local wait 1-300, timeout 1-300 after lease, lease 30-1800,
queue/fallback, disabled-by-default local IDs). Generic HTTP-provider limits must
not silently redefine Worker v1. Parent files may continue changing after this
snapshot; regenerate inventory and verify contracts before closing any gate.
