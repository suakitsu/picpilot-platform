# Admin AI Configuration Compatibility

## Integration

The parent mounts `s.registerAdminAIRoutes()` in its shared bootstrap. The
registrar is idempotent and platform-only. `registerAdminRoutes()` and
`registerMessageRoutes()` are also idempotent for tests that register explicitly.
The shared server, generation runtime, provider CRUD, video routes, worker routes
and Local Worker Protocol v1 are not modified by this slice.

Migration `0012_aiconfig.sql` adds encrypted historical snapshots, not a second
provider table. The existing `providers` table is the only source of live image,
text and worker configuration. Only platform image/text rows can be changed here;
video rows and all other owner scopes are preserved. Personal edition does not
register or query these admin APIs and requires no admin/billing runtime hook.

## Routes

All routes use the existing auth/admin guards and store-level active-admin checks.
Full paths below include `/api`.

| Method and Path | Contract |
| --- | --- |
| `GET /api/admin/ai-config` | Legacy image/text singletons, provider arrays, rollback flag, timestamp, worker state, plus an etag |
| `PATCH /api/admin/ai-config` | Atomic replacement of supplied provider arrays; omitted groups remain unchanged |
| `POST /api/admin/ai-config/validate` | Local validation only; returns ok/valid/message and false network_checked/model_called |
| `POST /api/admin/ai-config/probe/{image,text}` | Explicit, bounded metadata connectivity check, never generation |
| `POST /api/admin/ai-config/rollback` | Bodyless request restores the state before the most recent save; optionally accept version and etag |
| `GET /api/admin/ai-config/versions` | Paged historical version/action/actor/time metadata, never snapshot contents |
| `GET /api/admin/ai-config/catalog` | Existing protocols, adapter profiles, configured model IDs, worker v1 models, and existing video catalog |
| `GET /api/admin/ai-config/runtime` | Read-only alias of the effective configuration view; does not probe |

The existing frontend calls the root, validate, probe and rollback routes.
Catalog, versions and runtime are additive introspection APIs. Worker listing and
token creation/regeneration/revocation remain exclusively in
`registerWorkerRoutes()` under their existing paths. No AdminCommerceSuite,
commerce, translation, billing or benefits routes are registered here.

The effective singleton is the first enabled provider in priority/ID order, or
the first configured provider if none are enabled. It is a configuration view,
not a promise of runtime health or availability. Arrays take precedence over
singletons when both are posted, matching the frontend. A singleton-only legacy
request patches that provider and preserves its peers. Missing provider IDs are
generated for new entries; existing IDs cannot change capability.

GET returns an `etag`; returning it on PATCH detects stale frontend edits. Each
request also compares its validation snapshot inside the write transaction,
including requests from older clients without an etag. Provider versions are
checked when provided. A conflicting save or busy provider returns 409; reload
before retrying. No-op saves create no version. Versions store the configuration
*before* the associated action; a rollback records the displaced state too, so
the legacy bodyless rollback can toggle back. History has no public raw export.

## Mapping and Credentials

- `id`, `name`, `enabled`, `base_url`, `model`, `priority`, execution mode,
  timeouts, wait/lease/concurrency and offline action map to existing Provider
  fields and use the existing provider validator. Model is a model ID, not a
  provider ID. Reserved local provider IDs stay pull_worker; omitted reserved
  providers are retained disabled, preserving worker/token identities.
- `provider` is the legacy adapter label. Known aliases map to existing profiles
  (hfsyapi, yunfei-1k/yunfei-hires, rua.chat, xin-zhi-zhu, aishuch); generic
  OpenAI-compatible labels map to openai. Legacy Gemini image model/labels infer
  gemini_generate_content with gemini_proxy, or google_gemini for Google's
  generativelanguage host. Explicit changed `profile` and `protocol` select
  existing adapters; unchanged echoed fields do not mask a legacy label/model
  edit. Gemini protocol/profile mismatches are rejected.
- `api_key` is encrypted with the existing Vault under platform/provider-ID.
  Blank keys preserve the existing credential; `clear_api_key` explicitly clears
  it. Pull workers have no upstream key or base URL. Changing HTTP origin with an
  inherited key requires re-entering or clearing the key, including unsaved probes.
- GET/PATCH/rollback responses always return an empty api_key, boolean
  api_key_set and a constant mask. No key suffix, ciphertext, authorization
  header, private settings object or decrypted history appears in responses or
  audit metadata. Revision snapshots are separately Vault-encrypted at rest.
- Arbitrary environment-variable references are deliberately unsupported.
  Nonempty `env_key` returns a validation error; provision the credential through
  the encrypted Provider/Vault path. It is never silently accepted or read from
  the process environment.
- `actual_cost`, `supported_sizes` and `notes` persist as compatibility metadata
  in the provider's existing image_options/text_options.admin_compat settings.
  Existing unrelated settings are preserved. actual_cost is a configured
  estimate, NOT observed cost or a credit charge. Supported sizes are descriptive
  metadata, NOT a replacement for provider runtime size validation. Dashboard
  actual costs still come only from recorded observations.

Saves and rollbacks lock canonical provider rows and reject modifications or
deletions with reserved/submitting/submitted/uncertain attempts or
queued/leased/expired worker jobs. Unchanged busy providers remain valid. The
provider changes, encrypted revision and safe audit event commit together.

## Image-Type Runtime Contract

The parent consumes `Store.RuntimeImageConfig(ctx, imageType)` before quoting or
creating tasks; this store hook has no side effects and is personal-edition safe.

- `enabled=false` means reject new generation for that configured image type.
- `default_model` and `fallback_model` are exact configured platform image-model
  names, never provider IDs. Empty means inherit the owning runtime's normal
  routing. Nonempty values must match an existing image provider (disabled
  providers may be staged). AI-config replacement/rollback cannot strand these
  references. Other provider CRUD APIs may change independently, so runtime must
  still enforce capability, enabled/health state and model availability.
- A fallback model is only eligible under the parent's existing definitive,
  not-submitted failure policy. It is NOT permission to resubmit an uncertain
  provider request. Keep the normal task/attempt accounting and idempotency.
- `credits_cost` is an integer per-image quote in the inclusive range 0..1,000,000.
  Zero is explicitly free, not inheritance. When using this type quote, do not
  add or multiply the size-tier charge again. If no type config exists, retain
  the parent's existing size-tier pricing. Snapshot the chosen quote into the
  task once, and leave charge/refund ownership with existing task transactions.
- `default_size` is a validated existing image-size value used only when the
  user's request omits size. Explicit request size still requires runtime
  validation. `required_fields` remains descriptive frontend text, not a parser,
  validator, secret reference or automatic prompt instruction.

These are mapping contracts; the parent owns model/fallback selection, quote
consumption, risk screening and durable text telemetry integration. This slice
does not modify those shared paths or migration `0013`.

## Probe Safety and Gaps

Only an explicit POST probe can perform network I/O. It sends authenticated GET
to the provider's metadata/models endpoint through the existing SSRF/TLS-aware
client, with a 10-second timeout, 256-KiB response bound and no redirects. It
discards the upstream body and returns fixed messages, HTTP status and latency.
HTTP success means only that metadata responded; model availability, image/text
generation and chargeability are NOT verified. Providers without a compatible
metadata endpoint return a truthful unsuccessful check. Pull-worker probes only
validate configuration and dispatch no jobs; worker liveness is listed separately.

No paid/generating probe endpoint is implemented. This deliberately differs from
the Java probe and the older frontend's generation-probe description. No keys are
looked up in arbitrary environment variables. These limitations are explicit,
not fake successful functional tests.

## Verification

Tests use `core/internal/testpg` and disposable schemas configured by
`PICPILOT_TEST_DATABASE_CONFIG_FILE`; the configuration contents are never printed.
HTTP upstreams are localhost httptest mocks only. Tests cover role and edition
isolation, repeated route registration, legacy full-document roundtrips and
bodyless rollback, credential redaction/retention/clearing, concurrency/version
conflicts, history pagination, active-attempt guards, scope isolation, model/price
validation, probe methods/redirects/body limits, settled billing date boundaries,
and audited task input whitelists with reference ownership.

From core, using the configured Go 1.27.3 toolchain and caches:

```text
go test ./store ./server ./risk -count=1
go vet ./store ./server ./risk
```
