# Provider HTTP adapters

Package: `github.com/suakitsu/picpilot-platform/core/provider`. The package uses
the standard library plus the parent's `golang.org/x/image/webp` dependency.
No module files or Java source were edited; no credentials or user data were read.

## Public API

- `NewGateway(Options) (*Gateway, error)` creates a concurrent, reusable gateway;
  call `CloseIdleConnections()` during shutdown. Do not mutate input values during calls.
- `Gateway.Do(ctx, Request) (Response, error)` dispatches an explicit `Protocol`.
- `GenerateImage`, `EditImage`, `GenerateText` are typed convenience methods.
- `NewHTTPClient(Options) (*http.Client, error)` exports the secure transport for
  `video.Config.Client` and other adapters, with no default credentials. Its streamed
  limits are `MaxRequestBytes`/`MaxResponseBytes`; call `client.CloseIdleConnections()`.
- `SafeDownload(ctx, url, Options)` fetches bounded raw bytes without credentials;
  reuse `Gateway.Download` for repeated calls. `Gateway.DownloadImage` additionally
  applies `MaxImageBytes`/`MaxPixels` and full JPEG/PNG/WebP decoding for uploaded URLs.
- `Provider` supplies `BaseURL`, in-memory `APIKey: Secret(key)`, `Model`,
  `ImageProfile`, `TextProtocol`, `DeepSeek`, and optional per-call `Timeout`.
- `ImageRequest` contains an already-compiled `Prompt`, pixel `Size` (default
  `1024x1024`), output options and ordered `[]Reference{Data: bytes}` or `{URL: url}`.
  `ImageResponse.Image` has original `Bytes`, decoded `MIMEType`, `Width`, `Height`;
  request/submission sizes and reference truncation are reported separately.
- `TextRequest` contains `[]Message` for chat or raw JSON `Input` for Responses.
  `Options` accepts protocol-specific JSON fields; model/stream overrides are rejected.
  `TextResponse.Payload` preserves tool calls, arguments, structured content, refusal,
  status, annotations and usage. `Text` is only a convenience projection, not an agent transcript.

## Wire Contracts

| Profile/protocol | Contract |
| --- | --- |
| `OpenAI` | `/v1/images/generations`; references automatically use multipart `/v1/images/edits`, one `image` or ordered `image[]`. Native GPT Image omits unsupported `response_format`; URL preference is rejected for these models. |
| `LegacyOpenAI`, `Hfsy` | Java JSON `reference_images`, `n=1`, `response_format`. URL references required. Exact gpt-image-2/pro family normalization; Hfsy upgrades at longest side >=1800, caps pro at four references and each file at 5 MiB. |
| `Yunfei` | Fixed `gpt-image-2`; edits use first reference as `image`, `size`, `response_format=b64_json`, no `n`. |
| `Rua` | Fixed model; `quality=auto`, `output_format=png`, `moderation=low`; edits use four repeated `image` parts. Java square-size mapping retained. |
| `XinZhiZhu` | Java square-size mapping and output fields; edits use five repeated `image` parts, `n=1`, `response_format=b64_json`. |
| `Aishuch` | Also appropriate for the Java apimart branch: `image_urls`, exact Java pixel mapping, lowercase `resolution`; observes `/v1/tasks/{escapedID}` without repeating POST. |
| `GeminiProxy` | Exact existing Java HTTP contract: Bearer auth, `/v1beta/models/{model}:generateContent`, text then up to four ordered URL `fileData` parts, `imageConfig.aspectRatio` and `resolution`. This is **not** Google's native contract. |
| `GoogleGemini` | Native generateContent: `x-goog-api-key`, validated inline image bytes, `responseModalities=[TEXT,IMAGE]`, `imageConfig.aspectRatio/imageSize`. Parses candidate inlineData and safety failures. No claim of Interactions/Imagen/Vertex compatibility. |
| `ChatCompletions` | `openai_chat_completions`, `/v1/chat/completions`, Bearer, `stream=false`; full endpoint and `/v1` bases supported. DeepSeek uses its unversioned endpoint and `thinking.type=disabled`. |
| `Responses` | `openai_responses`, `/v1/responses`, `input`, `stream=false`, `store=false` by default; function-call/output and reasoning history retained. Native JSON tool/schema fields are not converted into chat syntax. |

At most ten input references; excess is rejected. Lower legacy limits retain the
first references in order and set `ReferencesTruncated`. One output image is
requested/returned, as in Java. PNG/JPEG/WebP must fully decode after pixel and byte
checks; dimensions come from bytes, never requested size or upstream metadata.
No resize, crop, padding, recompression, or center-crop occurs. The Java one-pixel
WebP fixture with invalid compressed data is intentionally rejected.

Text keeps Java's 20-message, 24-part, 80,000-text-byte, four-inline-image and
28,000,000-image-character limits (byte counting is conservative for Unicode).
Remote text image URLs are rejected; Responses supports up to 100 history items.
Unknown top-level options fail closed; JSON schemas and tool definitions stay intact.
`incomplete` Responses output is returned with its status, never presented as completed.

## Safety And Scheduler Contract

`*Error` exposes `Kind`, `StatusCode`, and **exactly** these `Submission` constants:

- `NotSubmitted` (`not_submitted`): validation, reference fetch, DNS/connect/TLS,
  or cancellation before the POST was written. No generation request was sent.
- `Rejected` (`rejected`): POST received HTTP 4xx other than 408 (including 401/403/429).
- `Uncertain` (`uncertain`): possibly submitted, HTTP 408/5xx/redirect, broken response,
  invalid/oversized output, unresolved async task, or failed output download after submission.
  It also covers successful generation with unusable output. **Never automatically resubmit.**

`Error.Definitive()` is true only for `NotSubmitted`/`Rejected`; that is not a retry
recommendation. All post-submission JSON failures are conservatively uncertain.
Use `errors.As` for typed errors and `errors.Is` for context cancellation/deadlines.
There are no application retries, POST redirects, provider fallbacks, or replayable POST bodies.

Defaults: HTTPS only; private, loopback, link-local, metadata, multicast and special-use
addresses denied. DNS answers are all checked at dial time, then the validated IP
is dialed directly with normal hostname TLS verification. `AllowLocalDevelopment`
explicitly allows HTTP/private/loopback; link-local, multicast, special-use ranges
and `metadata.google.internal` remain blocked. Never enable this option in production.
`AllowedHosts` restricts exact hostnames for APIs and downloads; `TrustedRoots` uses
caller-supplied trust configuration without disabling certificate verification.
No environment proxies or cookie jars. Gateway downloads never carry API credentials,
even same-origin. The raw client preserves explicitly supplied per-request API auth
(including video polls); redirected GET/HEAD requests retain only `Accept`/`Range`.
POST redirects and idempotency-header transport retry opt-ins are disabled. Redirect
targets are revalidated and HTTPS downgrades rejected. Do not replace the policy
transport, add a cookie jar, or relax its redirect policy.
Use host allowlists and network egress rules in production: URLs passed to legacy
upstreams are fetched by those services too, outside this process's DNS controls.

Bounded defaults: request 64 MiB, JSON response 32 MiB, each image 20 MiB, 32 million
pixels, JSON nesting 64, response headers 64 KiB, three download redirects, 180s
whole-operation timeout. All are configurable except header/depth limits. Contexts
cover downloads, submission and polling; bounded image decoding is synchronous.
No adapter logging or persistence. Adapter/download errors never retain bodies/URLs/prompts/keys;
credential formatting/JSON is redacted, and known credential echoes in text output
are redacted. Never log payloads or manually convert `Secret` for logging.
The raw `http.Client` follows Go's standard `url.Error` wrapping, which includes URLs;
do not log its raw errors. Use the sanitized adapters or `SafeDownload` at service boundaries.

For `video.Config.Client`, configure `NewHTTPClient(Options{MaxResponseBytes: 500 << 20,
Timeout: 5 * time.Minute})` to accommodate video's maximum result budget. Video applies
its own smaller JSON/submit/poll limits. The provider tests exercise actual
`video.NewGateway` submit, poll and credential-free result fetch through this factory.

## Verification And Boundaries

Tests use `httptest` exclusively, synthetic images and mock-only keys. Run in `core`:

```powershell
$env:GOCACHE='E:\picpilot\development-cache\go\build'
$env:GOMODCACHE='E:\picpilot\development-cache\go\modules'
$env:GOPATH='E:\picpilot\development-cache\go'
$env:GOMAXPROCS='2'
& 'E:\picpilot\development-tools\go1.27.3\go\bin\go.exe' test -mod=readonly ./provider -count=1 -cover
& 'E:\picpilot\development-tools\go1.27.3\go\bin\go.exe' vet -mod=readonly ./provider
```

Remaining integration work: scheduler/provider ordering, worker leases, persistence,
prompt compilation/policy, MinIO URL rewriting, billing/credits, circuit breakers,
provider concurrency quotas and stream processing remain outside this package.
Rua's automatic edit-to-generation fallback and Java's curl transport are intentionally
not reproduced. Masks, animation, multi-image output, non-Aishuch async polling,
Google Interactions/Imagen/Vertex, audio/files and all provider-specific text tools
are not full-parity features. No live provider behavior is certified by mock tests.

Migration evidence: Java `AiImageClient`, `AiImageResponseAdapter`, `AiTextService`,
`AiRuntimeConfigService`, `AiWorkerJobService` protocol allowlists, and their model,
reference, worker-routing and response-adapter tests. Native extensions follow the
[OpenAI Images](https://developers.openai.com/api/reference/resources/images/methods/generate),
[Responses](https://developers.openai.com/api/reference/resources/responses/methods/create), and
[Gemini generateContent](https://ai.google.dev/api/generate-content?hl=en) contracts.
