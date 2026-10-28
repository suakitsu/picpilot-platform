# Video Provider Adapters

`github.com/suakitsu/picpilot-platform/core/video` is a standalone, standard-library-only port of the PicPilot Java video gateway. It does not import the image/provider package, change module dependencies, choose routes, rotate keys, retry submissions, run a polling loop, persist tasks, store files, or charge/refund users. The parent owns those operations.

## Reference And Scope

The implementation was checked against the read-only Java sources in `src/main/java/com/openimage/video`: `VideoProviderGateway`, `VideoConfigService`, `VideoTaskService`, `VideoPromptPolicy`, `VideoWebhookController`, and `VideoResultStorageService`, plus their video tests. In particular, the tests port the documented APIMart create array and nested video arrays, APIMart callbacks, Seedance request differences, HFSY model/resolution mapping, and returned Agnes dimensions.

No live provider requests, secret/configuration-file reads, or production data accesses are needed by the package or its tests. All HTTP tests use local `httptest` servers. Example image/CDN URLs are inert fixtures; reference images are never fetched by this package.

## Public API

```go
func NewGateway(Config) (*Gateway, error)
func ResolveModel(Provider, string, string) (string, error)

func (*Gateway) Submit(context.Context, Request) (Submission, error)
func (*Gateway) Poll(context.Context, Submission) (PollResult, error)
func (*Gateway) ParseCallback(context.Context, []byte) (Callback, error)
func (*Gateway) FetchResult(context.Context, string, io.Writer) (FetchResult, error)
```

`Provider` is one of `video.Gemini`, `video.Seedance`, `video.HFSY`, or `video.Agnes`. Gemini and Seedance use APIMart but remain distinct model families. `ResolveModel` performs a deterministic mapping within an already selected provider; it is not routing or fallback. It accepts public codes or native IDs. The gateway additionally supports an explicitly configured custom Agnes model.

`Submission` contains `UpstreamTaskID`, `Provider`, and the native `Model`. Persist it and the parent-owned credential/key identity before further processing. Poll with a gateway using that same provider/account. The package deliberately does not maintain an in-memory registry of submissions.

`PollResult` contains `Status`, `Progress`, `ResultURL`, optional `*int` dimensions, decimal-string `Seconds`, a safe `Error`, and allowlisted `map[string]string` metadata. `Callback` contains `UpstreamTaskID` and `Result`. `FetchResult` contains `Bytes` and `ContentType`.

### Configuration

| Field | Behavior |
| --- | --- |
| `Provider` | Required; exactly one of the four families above |
| `APIKey` | Required nonempty printable ASCII token, no whitespace, maximum 8192 bytes; used only as the API bearer credential |
| `Client *http.Client` | Required, with an explicit non-nil `Transport`; see security contract below |
| `BaseURL` | Optional API base, including an optional path prefix; defaults below; no userinfo, query, or fragment |
| `PublicBaseURL` | Optional base for relative source image paths; no userinfo, query, or fragment |
| `WebhookURL` | Optional Gemini/Seedance webhook base, before APIMart appends `/callback`; transmitted verbatim, never returned |
| `AgnesModel` | Optional trusted admin override; 1-128 ASCII letters, digits, `.`, `_`, `:`, `/`, `-`; not accepted for other providers |
| `AllowHTTP` | False by default; opt in only for isolated local HTTP mocks, never to weaken production transport policy |
| `SubmitTimeout` | Default and maximum 45 seconds; a positive lower value is allowed |
| `PollTimeout` | Default and maximum 30 seconds |
| `FetchTimeout` | Default and maximum 5 minutes |
| `MaxResponseBytes` | Default 1 MiB, maximum 4 MiB; applies to API JSON, callback bytes, and marshaled request bodies |
| `MaxResultBytes` | Default and maximum 500 MiB; positive lower limits allowed |

Zero durations/limits select defaults. Invalid configuration is rejected without network I/O. Gateways copy the client, remove its cookie jar, and replace redirect handling with an unconditional stop; the caller's client is not mutated. Caller client timeouts can shorten these limits. Gateways are concurrency-safe if the injected transport is concurrency-safe; do not mutate a shared transport after use.

| Provider | Default Base | Create | Poll |
| --- | --- | --- | --- |
| Gemini / Seedance | `https://api.apimart.ai` | `POST /v1/videos/generations` | `GET /v1/tasks/{id}` |
| HFSY | `https://www.hfsyapi.cn` | `POST /v1/video/create` | `GET /v1/video/query?id={id}` |
| Agnes | `https://apihub.agnes-ai.com` | `POST /v1/videos` | `GET /agnesapi?video_id={id}` |

### Integration Example

This function is illustrative; tests never invoke a real provider. The caller supplies credentials and a policy-enforcing client, not environment-variable reads within this package.

```go
func submitSelectedVideo(
    ctx context.Context,
    secureClient *http.Client,
    credential string,
    imageURL string,
) (video.Submission, error) {
    gateway, err := video.NewGateway(video.Config{
        Provider: video.HFSY,
        APIKey:   credential,
        Client:   secureClient,
    })
    if err != nil {
        return video.Submission{}, err
    }
    return gateway.Submit(ctx, video.Request{
        Model:           "seedance-mini",
        Prompt:          "Show the product in one continuous shot",
        SourceURLs:      []string{imageURL},
        Resolution:      "480p",
        AspectRatio:     "9:16",
        DurationSeconds: 10,
    })
}
```

## Models And Allowed Inputs

All requests use typed fields, not an arbitrary provider-parameter map. Unknown model/resolution/ratio combinations and unsupported provider-specific options are rejected before a POST. `Prompt` must be valid UTF-8 and 4-4000 UTF-16 code units after trimming, matching Java length semantics. References must be nonblank HTTP(S) URLs with no userinfo or fragment; production defaults require HTTPS. Relative paths require `PublicBaseURL` and are appended to its path prefix. Protocol-relative URLs and non-HTTP schemes are rejected. The parent must provide already-uploaded, provider-accessible images.

Empty `Model` selects Gemini Omni, Seedance Fast, HFSY Fast, or Agnes V2, respectively. Empty ratio selects `9:16`; zero duration selects 10 seconds; empty resolution selects `720p`, except native `sd-2.0-fast-480` / `sd-2.0-480`, which imply `480p`. Resolution is case-insensitive; APIMart emits `4K`.

| Family / Public Code | Native APIMart Or Agnes Model | Resolutions | Ratios | References |
| --- | --- | --- | --- | --- |
| `gemini-omni` | `gemini-omni-flash-preview` | `720p` | `9:16`, `16:9` | 1-3, `.jpg`, `.jpeg`, `.png` URL paths |
| `seedance-mini` | `doubao-seedance-2.0-mini` | `480p`, `720p` | `9:16`, `16:9`, `1:1`, `3:4`, `4:3`, `adaptive` | 1-4 |
| `seedance-fast` | `doubao-seedance-2.0-fast` | `480p`, `720p` | Same Seedance ratios | 1-4 |
| `seedance-2` | `doubao-seedance-2.0` | `480p`, `720p`, `1080p`, `4K` | Same Seedance ratios | 1-4 |
| `agnes-v2` | `agnes-video-v2.0` or configured `AgnesModel` | `720p` | `9:16`, `16:9`, `1:1`, `3:4`, `4:3` | Exactly 1 |

Public catalog durations are 10 for Gemini/Agnes and 5, 8, 10, 15 for Seedance. The adapter also accepts the existing Java legacy-tier durations 3-10 for Gemini/Agnes/Seedance; the union for Seedance is 3-10 and 15, not arbitrary 11-14 seconds. The parent should apply its selected catalog/tier restrictions before calling the adapter. Gemini validates the requested duration but does not transmit it: upstream controls actual duration. These checks preserve local catalog and legacy behavior, not a claim about unverified upstream capabilities.

### Exact HFSY Mapping

HFSY accepts ratios `9:16`, `16:9`, `1:1`, durations 4-15 inclusive, and 1-4 references. It does not accept APIMart-specific audio or seed fields.

| Public Code | Resolution | Native Model | Wire `size` |
| --- | --- | --- | --- |
| `seedance-mini` | `480p` | `sd-2.0-mini` | `small` |
| `seedance-mini` | `720p` | `sd-2.0-mini` | `large` |
| `seedance-fast` | `480p` | `sd-2.0-fast-480` | `large` |
| `seedance-fast` | `720p` | `sd-2.0-fast` | `large` |
| `seedance-2` | `480p` | `sd-2.0-480` | `large` |
| `seedance-2` | `720p` | `sd-2.0` | `large` |

There is no `sd-2.0-mini-480` or separate mini-720 model. A native model mismatching an explicit resolution is rejected; it is never silently mapped to a different model.

### Complete Wire Field Sets

| Provider | Fields Sent |
| --- | --- |
| Gemini | `model`, compiled `prompt`, `image_urls`, `aspect_ratio`; optional configured `webhook` |
| Seedance | `model`, compiled `prompt`, `image_urls`, `duration`, `resolution`, `size` (aspect ratio), `generate_audio`; optional `seed` and configured `webhook` |
| HFSY | `model`, compiled `prompt`, `images`, `ratio`, `duration`, `size` (`small`/`large`), `watermark` |
| Agnes | `model`, continuity-enhanced `prompt`, fixed `negative_prompt`, `image`, `width`, `height`, `num_frames`, `frame_rate` |

`GenerateAudio *bool` is Seedance-only: nil means true, explicit false is preserved. `Seed *int64` is Seedance-only, 0-2147483647 inclusive, omitted when nil. `Watermark *bool` is HFSY-only: nil means false (the Java default), with explicit true/false supported. Even explicit false is rejected on unsupported providers; do not pass irrelevant options inherited from another provider. Gemini omits all Seedance-only fields. HFSY never sends `image_urls`, `resolution`, `generate_audio`, or `seed`.

For Agnes, `frame_rate` is always 24 and `num_frames` is `duration_seconds * 24 + 1` (241 at 10 seconds). Width/height are 720x1280, 1280x720, 720x720, 720x960, and 960x720 for `9:16`, `16:9`, `1:1`, `3:4`, and `4:3`. All parameters in the Java request are transmitted. Arbitrary width/height, FPS, frame-count, negative-prompt, audio, watermark, or seed overrides are not part of the current PicPilot Agnes contract and are not invented here. The fixed negative prompt includes identity/geometry/color/text changes, duplicate/missing parts, scene changes, temporal artifacts, deformation, blur, watermark, and subtitles.

### Prompt Compilation

The Java English product-continuity constraints and Chinese ordered-reference guide are ported. HFSY uses `@` followed by the Chinese image marker and index; APIMart uses the Chinese image label and index. Storyboard markers in English or Chinese permit only planned cuts/transitions; explicit single-shot markers take precedence. Agnes always uses its continuous-shot suffix and complete negative prompt. APIMart prompt output is capped at 4000 UTF-16 units, HFSY at 5000, truncating only the original prompt and never splitting a Unicode surrogate pair. No prompt rewriting service or script-generation calls are performed.

## Response And Callback Normalization

Create IDs support:

- APIMart: `data[0].task_id`, `data[0].id`, root `task_id`, `data.task_id`, root `id`, `data.id`.
- HFSY: root/data `task_id`, root/data `id`, `result.task_id`, `data.result.task_id`.
- Agnes: root/data `video_id`, root/data `id`.

IDs must be 1-256 characters: an ASCII alphanumeric first character followed by alphanumerics, `.`, `_`, `:`, or `-`. Credential-containing IDs are not returned. The package submits only one task, so unexpected multi-task arrays are ambiguous, not a successful single submission.

Poll status supports root/data `status` and `state`; APIMart prefers `data`, Agnes/HFSY prefer root. The normalized states are `submitted`, `generating`, `completed`, and `failed`. Java queue/pending/submit, success/complete/finish, and fail/error/cancel variants are recognized case-insensitively. `succeeded` and `done` also complete. Explicit failure takes precedence; `unsuccessful` fails, and `incomplete`/`not_completed` do not accidentally succeed. Unknown/missing status remains generating. Integer, fractional JSON-number (truncated), and percent-string progress are supported, clamped to 10-95 while running, with fallback 10; terminal states are 100. Parent task logic owns monotonic progress and valid state transitions.

Completed result URL precedence matches Java:

- APIMart: data/root `result.videos[0].url[0]`, data/root `result.videos[0].url`; then data `result.video_url`, `result.url`, `video_url`, `url`; then root `result.video_url`, `result.url`, `video_url`, `url`.
- HFSY: root/data `result_url`, root/data `result.url`, root/data `video_url`, root/data `url`.
- Agnes: root/data `metadata.url`, then root/data `url`.

A completed response without a usable URL is a protocol error, not a failed task and not permission to regenerate. Result URLs reject userinfo, fragments, invalid schemes, known API credentials, and query parameters named `api_key`, `apikey`, `authorization`, `prompt`, `negative_prompt`, `password`, `secret`, or `access_token`. Artifact URLs can contain necessary CDN signatures; treat them as sensitive capabilities, not log messages.

Agnes dimensions prefer root/data `metadata.size_mapping.width/height`, then direct width/height, then `size` formatted `WxH`. Returned dimensions may differ from requested dimensions and are not overwritten. HFSY accepts root/data width/height or result width/height. Agnes/APIMart seconds use root/data `seconds`; HFSY also accepts `duration`. Malformed/negative dimension or decimal metadata is omitted.

`ParseCallback` handles APIMart root, `data` object, and single-element `data` array payloads, including root `{id,status,result}` callbacks and both scalar/array nested video URLs. The parent must implement and authenticate the two Java callback routes before parsing:

- `POST /api/public/video-webhooks/apimart` with `X-Webhook-Secret`.
- `POST /api/public/video-webhooks/apimart/{secret}/callback` with the path token.

For the latter, configure `WebhookURL` as the public base plus `/api/public/video-webhooks/apimart/{escaped-secret}`, without `/callback`. Use constant-time secret comparison, reject disabled/missing/invalid secrets, limit the incoming body before allocation, correlate the extracted task ID with its persisted provider/account, and enforce idempotent state transitions. The adapter does not expose a public HTTP handler or authenticate callbacks itself.

### Safe Metadata

Only parsed numeric fields, valid dimensions/resolutions, and known status/cost-unit labels are retained. Raw upstream JSON, headers, prompts, source images, arbitrary messages, and credentials are never included in results/errors. All failure-message locations collapse to the fixed safe message `upstream video generation failed`; detailed upstream free text is deliberately not preserved.

APIMart retains numeric timing/expiry fields and converts costs with exact rational arithmetic, rounded to six decimals, using the Java reference constants: credits take precedence at `0.679110` CNY/credit, otherwise USD at `6.791100`. Metadata includes `actual_cost`, `upstream_cost`, `upstream_cost_unit`, `cost_conversion_rate`, `cost_source`, and, when present, `upstream_credits_cost`. For example 5.28 credits or USD 0.528 yields `3.585701`.

HFSY preserves numeric quota and recognized status labels and attaches the reference fixed-per-call metadata: mini 1.20, fast-480 2.00, fast-720 3.00, full-480 3.00, full-720 4.00, with `CNY_PER_CALL` and `hfsy_public_fixed_price`. Agnes preserves numeric reported `actual_cost`, `seconds`, size/mapping, and common timing fields. These are historical reference-normalization rules, not verified current prices and not charging/refund decisions. The parent must not settle a task merely because cost metadata exists, especially for nonterminal/failed HFSY responses.

## Failure Semantics

Use `errors.Is` with `ErrInvalidRequest`, `ErrRejected`, `ErrAmbiguous`, `ErrTransport`, `ErrProtocol`, or `ErrLimit`. `errors.As(err, *video.Error)` exposes `Kind`, safe `Operation`/`Code`, `HTTPStatus`, and capped `RetryAfter` (0-300 seconds). In actual Go code, use `var detail *video.Error; errors.As(err, &detail)`. Context cancellation/deadline sentinels remain discoverable via `errors.Is`; underlying URL/transport errors and upstream bodies are deliberately not wrapped.

| Situation | Classification / Parent Action |
| --- | --- |
| Local validation or already-cancelled context | `ErrInvalidRequest`; nothing was submitted |
| HTTP 4xx other than 408, or explicit 2xx non-admission without an ID | `ErrRejected`; no automatic retry/fallback is performed |
| Lost connection, timeout/cancellation after entering POST, HTTP 408, redirects, generic 5xx, malformed/truncated/oversized 2xx body, missing/invalid ID | `ErrAmbiguous`; quarantine/reconcile; never automatically resubmit, change provider, or assume refundable non-admission |
| Contradictory 2xx response with an ID and rejection/error fields | `ErrAmbiguous`, returning the valid `Submission` alongside the error for reconciliation |
| Agnes HTTP 503 with exact structured `code: video_queue_full` and no task ID | `ErrRejected` with that safe code; this is the Java explicit non-admission exception, not a broad 5xx retry rule |
| Poll transport/HTTP/protocol failure | Nonterminal error; keep the existing task ID/provider and let the parent decide when to poll again |
| Provider terminal generation failure | `PollResult{Status: Failed, Progress: 100}` with a fixed safe error |
| Fetch failure | Discard partial output; do not regenerate the video |

Every explicit invocation makes at most one application-level HTTP request. POST bodies have no `GetBody` replay function or idempotency/retry headers, and all redirects are disabled. There is no automatic retry, key rotation, Agnes/Gemini fallback, or route switching. A custom transport must not secretly retry paid POSTs. `RetryAfter` is only evidence for parent scheduling, never permission to replay an ambiguous submission. Parent persistence/idempotency must also prevent a caller from invoking `Submit` twice after a successful or uncertain attempt.

## Transport And Result Security Contract

The required injected `Client.Transport` is the boundary for shared network policy. The parent must enforce endpoint allowlists, HTTPS/TLS verification, DNS resolution and rebinding protection, private/link-local/loopback/metadata-address restrictions, proxy policy, finite dial/TLS/header timeouts, response-header limits, and context cancellation. The policy must cover both configured API origins and untrusted result/CDN URLs. Tests explicitly use isolated loopback servers. A plain URL-scheme check in this package is not an SSRF defense, and a transport pointer alone is not proof of a secure transport.

The shared boundary is now `provider.NewHTTPClient(provider.Options) (*http.Client, error)`. Pass its result through `video.Config.Client`; do not duplicate its DNS/IP/TLS policy. Options include `AllowLocalDevelopment`, `AllowedHosts`, `Resolver`, `TrustedRoots`, `DialTimeout`, `TLSHandshakeTimeout`, and `ResponseHeaderTimeout`. For video downloads set `MaxResponseBytes: 500 << 20` and `Timeout: 5*time.Minute`; the gateway separately caps API JSON at 1 MiB and requests at 45 seconds. Host allowlists must include approved API and result origins. Keep local-development bypasses test-only. Video still blocks all redirects, regardless of the factory's GET redirect allowance. The factory owns no credentials; callers close shared idle connections after all work is joined. `provider.SafeDownload` and `Gateway.DownloadImage` are image helpers, not substitutes for streaming `video.Gateway.FetchResult`.

Do not inject a transport that adds bearer credentials, cookies, or origin-bound headers to every request. The gateway itself adds its bearer credential only to its API-origin create/poll requests. Downloads use a newly constructed credential-free GET, no cookie jar, no redirect, a wall-clock context deadline, and a streaming size cap applied even without Content-Length (and after standard transport decompression). Oversized Content-Length is rejected before writing. Bytes written never exceed `MaxResultBytes`; any error requires discarding the partial writer output. The caller's writer must not block indefinitely. Only HTTP 200, nonempty bodies, and video MIME types are accepted; missing/octet-stream MIME defaults to `video/mp4`. Container/content verification and atomic publication remain the parent's storage responsibility.

`Config`, `Request`, and `*Gateway` have redacted formatting. Credentials, source URLs, prompts, webhook URLs, and clients are omitted from relevant JSON fields. Do not log request payloads in middleware or the injected transport. Secure handling of signed artifact URLs still belongs to the parent.

## Verification And Remaining Integration

The mock tests exercise every model mapping, complete provider field sets, audio/seed/watermark defaults and overrides, Agnes dimensions/custom models, validation without HTTP, prompt/reference policy, all Java create/result locations, callback shapes, normalized statuses/progress/costs, terminal failures, ambiguous/rejected submissions, dropped connections/timeouts, oversized payloads, redirect suppression, credential-free fetching, streaming limits, concurrent calls, and client-copy/no-POST-replay behavior. An executable external-package example exercises submit, poll, and result fetching through the exported API. No image-provider code or network-policy package is modified.

Run from `core` with the parent-provided toolchain and caches:

```powershell
$env:GOCACHE='E:\picpilot\development-cache\go\build'
$env:GOMODCACHE='E:\picpilot\development-cache\go\modules'
$env:GOPATH='E:\picpilot\development-cache\go'
$env:GOMAXPROCS='2'
$env:GOTOOLCHAIN='local'
$env:GOPROXY='off'
$env:GOSUMDB='off'
& 'E:\picpilot\development-tools\go1.27.3\go\bin\go.exe' test -mod=readonly -cover ./video
& 'E:\picpilot\development-tools\go1.27.3\go\bin\go.exe' vet -mod=readonly ./video
```

The Windows session could not start `go test -race`: cgo is disabled and no C compiler is available on PATH. Run race verification in the parent's cgo-enabled environment; ordinary mock tests, concurrency checks, and `go vet` do not replace the race detector.

No third-party dependencies are added; the adapter source uses Go 1.25-compatible standard-library APIs and works under the parent's newer toolchain/module minimum. Real upstream behavior is intentionally not verified. The application persistence/API layer below is separate from the standalone adapter. Raw-message redaction, unsupported-option rejection, scheme/redirect restrictions, and stricter malformed-response handling are intentional safety differences from Java, not claims of complete application migration.

## Video Persistence And HTTP API

The video-specific application files are `server/videos.go`, `server/execution_video.go`, `store/videos.go`, their `videos_test.go` files, and `store/migrations/0003_video.sql`. Shared router, job runner, attempts, credits, binary storage, module files, and other agents' migrations are not edited. The parent calls `s.registerVideoRoutes()` at initialization; it has the same idempotent guard as `registerProjectRoutes`, so fixture re-registration remains safe. All handlers are database/local-storage operations; none submits, polls, downloads a provider result, or starts a goroutine to execute a paid request.

| Method | Route | Contract |
| --- | --- | --- |
| GET | `/api/videos/config` | Authenticated legacy public model/variant/tier catalog |
| POST | `/api/videos/tasks` | Legacy multipart creation; JSON with `image_urls` is additive |
| GET | `/api/videos/tasks` | Latest 100 owned, nonhidden tasks |
| GET | `/api/videos/tasks/{id}` | Owned task with normalized public status and signed stored result |
| DELETE | `/api/videos/tasks/{id}` | Hide owned terminal task; preserves billing and idempotency |
| POST | `/api/videos/tasks/{id}/cancel` | Additive cancellation; only a queued task settles immediately |
| POST | `/api/videos/script-preview` | Immediate fallback plus optional queued text optimization; no inline provider call or credit charge |
| GET/PATCH | `/api/admin/video-config` | Platform administrator config, key states, pool metrics, recent tasks |
| GET | `/api/admin/video-tasks` | Admin `keyword`, `status`, `provider`, `page`, `page_size` filters |
| DELETE | `/api/admin/video-tasks/{id}` | Audited soft deletion of terminal task |
| POST | `/api/public/video-webhooks/apimart` | Constant-time `X-Webhook-Secret` verification |
| POST | `/api/public/video-webhooks/apimart/{secret}/callback` | Same verified callback contract using a path token |
| GET | `/api/videos/references/{id}/{filename}` | Signed owned image alias with a Gemini-compatible filename suffix |

### Creation And Configuration

Creation accepts `title`, `prompt`, `aspect_ratio`, `mode`, `model_code`, `duration_seconds`, `resolution`, `generate_audio`, `seed`, `original_prompt`, repeated `image_urls`, and repeated multipart `images`; `project_id` is an additive owned-project association. Unknown fields, including client-supplied prices or provider credentials, are rejected. `Idempotency-Key` is required: 16-128 ASCII letters/digits or `. _ : -`. Prompts use the adapter's 4-4000 UTF-16-unit bound. Uploaded images use the shared 25 MiB/50 MP normalization limit, maximum four files, and model-specific reference counts. Input order is linked URLs followed by uploads, matching Java. Gemini linked/owned input must be JPG/PNG. Other models also support WebP. Uploaded WebP is normalized by shared storage.

Catalog prices and variants mirror the Java reference: Gemini/Agnes at 720p/10s; Seedance mini/fast at 480p/720p and full at 480p/720p/1080p/4k, each at 5/8/10/15s. HFSY exact model/ratio matches are preferred for Seedance selections with its historical minimum-credit floor. Mini480 and mini720 both use `sd-2.0-mini`. Unsupported HFSY ratios/resolutions remain APIMart selections, never post-submission fallback. Legacy standard/quality durations are 3-10s; HFSY cannot accept the 3s legacy variant. Legacy standard may choose Agnes when Gemini is unavailable. Explicit Agnes allows one reference, Gemini three, Seedance four; legacy tiers allow three except Agnes's one.

Pricing is selected server-side and charged only by the foundation `Store.SubmitTask` transaction. Personal and demo tasks cost zero. `RequestIdentity` and a persisted fingerprint prevent input-changing retries while allowing exact retries after config/price changes or asset-ID allocation. Replaying a hidden/deleted task never submits it again. Unused uploaded reference rows/files are removed after rejected or duplicate submissions, with a persisted-request check protecting uncertain DB commits.

Creation calls the shared `screenUserInput(ctx,userID,projectID,"product_video",idempotencyKey,text)` before saving uploaded references or reserving credits. Both `prompt` (the submitted script) and a distinct nonblank `original_prompt` are caller-authored input and are screened together; supplying a harmless original description cannot bypass screening of an edited script. Server-generated instructions are not appended to the screening input. Exact existing-task replay returns the original task without another moderation event or charge. Blocked content uses the shared `422 CONTENT_BLOCKED` response; an account restricted during screening uses `403 ACCOUNT_RESTRICTED`. Moderation retries use stable action keys, not request trace IDs, and persist no full description. Personal uses the shared account check but does not load platform risk configuration.

All legacy admin fields are accepted: `enabled`, `paid_provider_requested`, `gemini_enabled`, `seedance_enabled`, `hfsy_enabled`, `agnes_route_enabled`; `standard_*` and `quality_*` enabled/duration/resolution/price; `agnes_route_percent`, `agnes_daily_limit`, `agnes_concurrency`, `agnes_circuit_failures`, `poll_seconds`, `timeout_minutes`, `agnes_model`; `apimart_api_key`, `hfsy_api_key`, `agnes_api_key` and corresponding `clear_*_api_key`; `agnes_keys` entries `{id,label,enabled,concurrency,api_key,clear_api_key}`; `model_configs` entries `{code,enabled,variants:[{resolution,duration_seconds,price,enabled}]}`. Unspecified/null fields retain settings; blank keys retain credentials; explicit empty key pools disable retired entries. Keys are encrypted per `scope/provider-id`, never returned. Key views expose only configured/error flags, 8-hex SHA-256 fingerprints, source and counters. Partial edits use compare-and-swap, provider versions/health update atomically, and legacy credential replacement is blocked while the provider has active/uncertain attempts. Retired pool rows remain for reconciliation, disabled for new reservations.

Preview accepts `{prompt,model_code,duration_seconds,resolution,aspect_ratio,generate_audio,reference_count}` and always returns `{optimized_prompt,mode,source,summary}`. `mode` is `single_shot` or `storyboard`; explicit no-cut instructions win, and Agnes remains single-shot. The fallback retains the entire normalized user description. Ordered reference labels, product identity, timing and audio guidance are appended only when the combined prompt fits 4000 UTF-16 units; user intent is never truncated to fit generated guidance.

Before composing any system prompt or submitting a text task, preview screens only the original `prompt` with feature `video_script_optimize`. `Idempotency-Key` is optional with the same 16-128-character syntax as creation. With no header, a namespaced SHA-256 of normalized preview input identifies the action; with a header, the namespaced header digest identifies it. Both moderation and task submission reuse this stable key. Explicitly supply a new key to request another optimization of identical input. Changed input under an existing explicit key conflicts rather than creating duplicate work.

With an enabled text provider in the proper platform/user scope, preview stores a zero-credit `jobs.Payload{Version:1, Text:...}` task of kind `video_script_optimize`. `RunTasks`, not `RunVideoTasks` or the handler, performs text execution. The immediate response includes a usable fallback plus `task_id`, `status`, and `safe_error`. Repeating the same preview request reads that same task and returns `source:"ai"` after successful, valid JSON output. The generic owned `GET /api/tasks/{id}` also exposes task status. Malformed/oversized model output, failed work and ambiguous submissions keep the fallback; replay never starts another task or POST. This is an asynchronous extension of Java's synchronous preview contract. Without an enabled scoped text provider, or in demo mode, preview stays local and creates no task.

Platform `Store.ActivePrompt(ctx,"video_script_optimize")` can replace the default script-writing system template. The output-format and user-intent preservation requirements remain appended to that system message; the exact original description is separately JSON-encoded in the user message with the selected video specification. Enabled-template content is captured in the durable text request, so later edits cannot change an already queued action. Disabled/missing templates use the default, and personal never inherits platform templates. Templates are not screened as user input and are not returned in preview responses. No text model is called by an HTTP handler.

### Durable Envelope And Executor API

`Task.Capability` is `video`, `Task.Kind` is `product_video`. `Task.Request` is `jobs.Payload{Version:1, Video:...}`, not the worker protocol. The video document explicitly stores prompt/reference IDs because marshaling `video.Request` directly intentionally omits prompts and URLs. It stores the public/native model, selected provider identity/scope/version, duration, size, audio/seed options, fingerprint and server price, but no credentials or image bytes. Owned asset URLs are freshly signed when compiling.

```go
// Server, no model calls or credential decryption:
CompileVideoTask(context.Context, store.Task) (video.Request, store.Provider, error)
CompileVideoAttempt(context.Context, store.Task, store.Attempt) (video.Request, error)
VideoWebhookURL() string // Executor-only, contains a secret; never log or expose.

// Store, existing global task/attempt states and credit ledger:
BeginVideoSubmission(context.Context, Task, Attempt) error
RecordVideoSubmission(context.Context, Task, string, video.Submission, time.Duration) (Task, error)
ClaimVideoPoll(context.Context, string, time.Duration) (VideoPollClaim, error)
VideoPollState(context.Context, Task) (VideoPollClaim, error)
VideoResultAsset(context.Context, Task) (Asset, error)
ApplyVideoPoll(context.Context, Task, video.PollResult, time.Duration) (Task, error)
ReleaseVideoPoll(context.Context, Task, time.Duration) error
CompleteVideoTask(context.Context, Task, string) (Task, error)
ReconcileVideoCallback(context.Context, video.Callback) (bool, error)
```

1. Claim queued video work with `ClaimTaskCapabilities(ctx, owner, ttl, []string{"video"})`. Compile to determine the pinned provider identity, then `ReserveAttempt`. On `ErrCircuitOpen`/`ErrProviderBusy`, release only unsubmitted work with a delay.
2. Use **`a.Provider`**, not the earlier provider listing. `CompileVideoAttempt` checks this captured snapshot against the pinned model/scope. Decrypt only at execution with vault AAD `scope/id`. Supply the shared secure HTTP client.
3. Call **`BeginVideoSubmission(ctx, lease, a)`** before the one POST. It atomically enters `submitting` and persists the encrypted provider snapshot in attempt metadata. This is the video-specific replacement for the generic `BeginSubmission`, necessary for safe polling after key rotation/restart.
4. Store any known ID with `RecordVideoSubmission`, even if the adapter also reports contradictory/ambiguous admission evidence. With no known ID, uncertain network/5xx outcomes use `MarkUncertain`, never another POST, refund, or route. Definitive local validation/rejection can fail/refund once. Use bounded recording contexts even if the HTTP context has ended.
5. `ClaimVideoPoll` returns the known `Submission`, fenced `Task`, optional staged `Pending` result, and **original `Provider` snapshot**. Never reload a newer provider credential to poll an older account. An empty snapshot indicates an older/incomplete integration and must not trigger a POST. Use `RenewTask` during long work. Poll transport/protocol/auth errors retain the ID and delay the next read.
6. `ApplyVideoPoll` refunds definitive failure atomically or stages completed evidence. Completion is not public until an owned, task-bound video asset is persisted and passed to `CompleteVideoTask`. Use `Store.RecordProviderOutcome(ctx, snapshot, code, authFailure)`; its version check prevents old-account outcomes from poisoning a rotated config.

Callbacks use an HMAC-SHA256 token derived from the server auth key and a video-specific domain separator. This does not reuse a session token. Both Java URL/header forms are supported; callback bodies are limited to 1 MiB, normalized without any network calls, and never logged or persisted raw. Unknown IDs enter a bounded 10,000-entry/48-hour inbox to handle acknowledgement races. Shared callback IDs matching multiple APIMart accounts fail closed; globally unique upstream IDs are required for the legacy unscoped callback route. Completed results are staged, not remotely exposed. First terminal evidence wins; duplicate failure/refund or success/recharge transitions are prohibited.

### Service Runtime

`func (s *Server) RunVideoTasks(ctx context.Context) error` is ready for the parent service lifecycle hook. It creates the client with `provider.NewHTTPClient`, runs two video-only submission workers through `taskrunner.Runner`, and runs two known-ID poll workers with exclusive PostgreSQL leases. `jobs` remains the pure internal payload package. Poll leases are renewed every 40 seconds with a two-minute TTL. Service cancellation joins all submission, polling, heartbeat, and download work before closing idle HTTP connections. Nothing is attached to an HTTP request's lifetime.

Reservation capacity/circuit errors defer the same unsubmitted task without reserving another provider. No retry/fallback POST is made after admission or ambiguity. Definitive rejection refunds once; uncertain admission without an ID is quarantined. A valid ID returned alongside an ambiguous envelope is persisted and polled. Poll transport/auth/protocol errors back off without refunding or losing the ID. Callback completion evidence is re-read under the lease after a concurrent poll, so only the stored winning result is downloaded.

Downloads use `FetchResult -> io.Pipe -> saveMedia`, retaining only a 512-byte signature prefix plus streaming buffers. The pipe and HTTP request are cancelled/joined on failure/shutdown. Only MP4/WebM signatures are accepted as generated video; the shared helper hashes, bounds, atomically stores, and inserts the owned `generated-video` asset. The completed task exposes a signed local asset URL, not a provider/CDN URL. A persisted asset left by a crash before final settlement is reused by the next fenced poll via `VideoResultAsset`, avoiding another download. Failed fetches retain staged completion evidence with a delay, not a busy loop.

**Personal requires no platform video setting row or administrator setup.** An enabled, owner-scoped HTTP video provider with a supported native model is enough for catalog selection, zero-credit creation, submission, and polling. The personal compiler/reservation/snapshot remain bound to that owner. A local HTTP UI origin is not passed as an unnecessary gateway base; callbacks are omitted in strict mode unless the public URL is HTTPS. Public reference image URLs therefore work with an HTTPS provider while the personal app is on localhost. Uploaded references still need a provider-reachable HTTPS media origin in production: URL-only upstream APIs cannot fetch an unexposed localhost asset. Enabling private-network transport is not a production workaround.

### Verified And Remaining Work

Isolated PostgreSQL tests use `core/internal/testpg.Open(t)` and the parent-provided test configuration file without printing it. Tests cover exclusive poll claims/reclaims, lease fencing, monotonic progress, snapshot rotation/circuit isolation, pre-acknowledgement callbacks, crash/uncertain quarantine, exact-once refunds, owned terminal hide/admin audit, configuration CAS, legacy response shapes, per-user scopes, personal zero billing, multipart retries/cleanup, request validation, audio/seed and HFSY watermark defaults. Runtime tests exercise all four provider families, credential-free result fetching from a separate mock CDN, signed stored-video serving, 5xx/missing-ID/403/contradictory-ID admission, poll/storage retry, persisted-artifact recovery, global capacity/circuit deferral, capability isolation, and shutdown during streaming. Personal HTTPS runtime tests run with an HTTP localhost UI and no platform settings. All HTTP fixtures, including Agnes, are `httptest`/`httptest.NewTLSServer`; no real upstream is contacted.

Screening/preview tests additionally cover both caller description fields, blocked multipart uploads before asset insertion, no credit reservation or text task on rejection, 422/403 propagation, stable moderation-event deduplication, warning replay, system-template exclusion from screening, disabled-template fallback, immutable queued template snapshots, personal isolation, preservation of 4000-unit ASCII/non-BMP input, generic-runner mock text execution, and invalid/ambiguous preview results without another submission.

The parent has mounted `registerVideoRoutes()` and starts/joins `RunVideoTasks` alongside image/text/worker services. The implementation consumes the parent's `saveMedia(ctx,userID,projectID,taskID,kind,filename,expectedMIME,reader) (store.Asset,error)`; no additional binary-storage signature is needed. It also consumes the shared screening/error, active-prompt, scoped text-provider and generic text-runner APIs without changing their files. The parent owns the `taskrunner.Runner` move. No changes were made here to shared `server.go`, `go.mod`, or migrations 0004 and later.

After the screening/template integration and runner move, `go test -mod=readonly ./server ./store -run Video -count=1` passes, as do the full `server`, `video`, `provider`, and `storage` suites. Vet passes for `server`, `store`, `video`, `provider`, and `storage`. The concurrent full-store run still reports out-of-scope failures in `TestPostgresCommerceFailureCancelRefundConcurrentAndRetry` (`commerce_test.go:59`, no rows) and `TestPostgresTextTelemetryFollowsWorkerWithoutDoubleCountingReceipt` (`telemetry_test.go:72`, `PAYLOAD_FIELD_NOT_ALLOWED`). Testing/vetting `taskrunner` is separately blocked by unqualified `Payload`, `ImageInput`, and `TextInput` references in its moved `runner_test.go:62-65`; the video runner lifecycle tests themselves pass. These shared files were not changed. Race detection remains unavailable without cgo/a C compiler.

Known parity differences: AI script preview is queued rather than synchronous, so clients should replay the stable preview request to obtain optimized output; legacy Agnes routing percentage/daily admission/threshold controls are stored but need policy integration (the shared provider circuit and per-provider concurrency are enforced). Atomic daily admission would benefit from a parent `ReserveAttemptTx(ctx, tx, lease, scope, providerID)` equivalent to the existing transactional submit helper. Timeout alone is not proof of failure and does not refund/repost known or unknown jobs; failed-to-late-success refund reversal from Java is intentionally disallowed. Demo video rendering is not implemented: free/demo queued tasks fail safely with `VIDEO_DEMO_EXECUTION_UNAVAILABLE`, never become paid requests after a mode change; existing known-ID work pauses while demo mode is enabled. Provider key fingerprints/counters are implemented, but daily-limit admission is not inferred from those display counters. Public status normalizes `running` to `submitting`, `polling` to `submitted`/`generating`, `uncertain` to `submission_unknown`, and `succeeded` to `completed`. Broad application parity is not claimed.
