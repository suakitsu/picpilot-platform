# PicPilot Local Worker Protocol v1

Status: stable v1  
Authority: this file is the only authoritative Local Worker protocol document.  
Base path: `/api/internal/picpilot-worker/v1`

## 1. Purpose and compatibility

PicPilot Local Worker is an optional local-first execution layer for image and
text AI calls:

| Provider ID | Capability | Execution mode | Default model |
| --- | --- | --- | --- |
| `local-cliproxy-image` | `image` | `pull_worker` | `gpt-image-2` |
| `local-cliproxy-text` | `text` | `pull_worker` | `gpt-5.5` |

The Local Worker only makes outbound HTTPS requests to PicPilot. It opens no
public port and uses no SSE or WebSocket connection.

The Local Worker calls its own local CLIProxy/OpenAI-compatible endpoint. The
PicPilot server never requests, receives, stores, or logs the local
CLIProxyAPI Key, OpenAI OAuth token, refresh token, browser session, local base
URL, or other workstation configuration.

This feature is additive:

- Both local Providers are created disabled.
- Existing Providers remain `execution_mode=http`.
- When local Providers are disabled, image/text routing behaves exactly as
  before and no `ai_worker_jobs` row is created.
- When enabled with a higher priority, Local Worker is attempted first.
- An unclaimed job with `offline_action=fallback` is cancelled after
  `local_wait_seconds`, then the existing HTTP Provider chain continues.
- An unclaimed job with `offline_action=queue` remains queued.
- After a job is leased, PicPilot does not start an HTTP fallback in parallel.
- `local_wait_seconds` only controls how long the server waits for a Worker to
  claim a job. For synchronous text calls, `timeout_seconds` starts after the
  job is leased and controls how long the request waits for the Worker result.
- Worker callbacks do not directly deduct or refund credits. Existing business
  task settlement remains the single source of billing truth.
- Video and all unrelated business flows are outside this protocol.

## 2. Transport and authentication

All requests MUST use HTTPS:

```http
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json
```

`artifacts` uses `multipart/form-data`.

HTTP requests are rejected with `426 HTTPS_REQUIRED`. The reverse proxy MUST
strip client-supplied forwarding headers and set its own HTTPS forwarding
information.

### 2.1 Worker Token lifecycle

Production Worker Tokens are generated only in the authenticated administrator
interface:

```text
GET  /api/admin/ai-config/workers
POST /api/admin/ai-config/worker-tokens
POST /api/admin/ai-config/worker-tokens/{tokenId}/regenerate
POST /api/admin/ai-config/worker-tokens/{tokenId}/revoke
```

Create body:

```json
{
  "name": "Studio workstation"
}
```

The raw token is returned once by create/regenerate. The database stores only a
peppered SHA-256 hash and a short display prefix. Regeneration immediately
invalidates the previous token. Revocation makes authentication fail
immediately with `401 WORKER_TOKEN_INVALID`.

The token is independent of user JWTs and administrator JWTs. Never place a
production Worker Token in source code, logs, documentation, Git, test
fixtures, screenshots, or chat messages.

### 2.2 `workerId` binding

`workerId` MUST match `[A-Za-z0-9._:-]{3,120}`. The first successful heartbeat
binds it to the authenticated Worker Token ID. Another Token cannot reuse that
ID and receives `409 WORKER_ID_IN_USE`. A revoked/regenerated Token cannot
continue using an old raw secret.

## 3. Model, task, and payload whitelist

Allowed capability/protocol pairs:

| Capability | Protocol |
| --- | --- |
| `text` | `openai_responses` |
| `text` | `openai_chat_completions` |
| `image` | `openai_images_generations` |
| `image` | `openai_images_edits` |

Image model:

```text
gpt-image-2
```

Text model catalog:

```text
gpt-5.6-sol
gpt-5.5
gpt-5.6-terra
gpt-5.6-luna
```

The claim-level `model` and `request.model` always match. Unknown models,
protocol/capability mismatches, streaming requests, embedded base64 inputs,
credentials, local URLs, proxy configuration, and unknown top-level payload
fields are rejected before a job is queued.

General limits:

- Serialized request: at most 256 KiB.
- Serialized input-file metadata: at most 256 KiB.
- Image prompt: 1 to 32,000 characters.
- Image output count: exactly `n=1`.
- Image edit inputs: 1 to 10 signed HTTPS inputs.
- Image generation inputs: none.
- Text inputs: no binary input files.
- `stream`: always `false`.

Allowed top-level request fields:

| Protocol | Fields |
| --- | --- |
| `openai_responses` | `model`, `input`, `instructions`, `temperature`, `top_p`, `max_output_tokens`, `reasoning`, `text`, `tools`, `tool_choice`, `metadata`, `store`, `stream` |
| `openai_chat_completions` | `model`, `messages`, `temperature`, `top_p`, `max_tokens`, `max_completion_tokens`, `frequency_penalty`, `presence_penalty`, `response_format`, `stop`, `seed`, `stream`, `reasoning_effort`, `thinking`, `tools`, `tool_choice` |
| `openai_images_generations` | `model`, `prompt`, `n`, `size`, `quality`, `output_format`, `response_format`, `background`, `moderation`, `output_compression`, `user` |
| `openai_images_edits` | `model`, `prompt`, `n`, `size`, `quality`, `output_format`, `response_format`, `background`, `moderation`, `output_compression`, `input_fidelity`, `user` |

Forbidden keys at any nesting level include authorization, API keys, access or
refresh tokens, OAuth values, client secrets, cookies, base URLs, and proxy
URLs.

## 4. JSON Schemas

All camelCase names below are wire-format names. The schemas use JSON Schema
2020-12. `additionalProperties: false` is normative for endpoint envelopes and
protocol payload top-level objects.

```json
{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://suakitsu.com/schemas/picpilot-local-worker-v1",
  "$defs": {
    "workerId": {
      "type": "string",
      "pattern": "^[A-Za-z0-9._:-]{3,120}$"
    },
    "leaseId": {
      "type": "string",
      "minLength": 16,
      "maxLength": 128
    },
    "idempotencyKey": {
      "type": "string",
      "pattern": "^[A-Za-z0-9._:-]{8,128}$"
    },
    "sha256": {
      "type": "string",
      "pattern": "^[a-fA-F0-9]{64}$"
    },
    "capability": {
      "enum": ["image", "text"]
    },
    "protocol": {
      "enum": [
        "openai_responses",
        "openai_chat_completions",
        "openai_images_generations",
        "openai_images_edits"
      ]
    },
    "textModel": {
      "enum": [
        "gpt-5.6-sol",
        "gpt-5.5",
        "gpt-5.6-terra",
        "gpt-5.6-luna"
      ]
    },
    "errorResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["error"],
      "properties": {
        "error": {
          "type": "object",
          "additionalProperties": false,
          "required": ["code", "message", "retryable"],
          "properties": {
            "code": {"type": "string"},
            "message": {"type": "string"},
            "retryable": {"type": "boolean"}
          }
        }
      }
    },
    "heartbeatRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["workerId", "capabilities", "maxConcurrency"],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "capabilities": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {"$ref": "#/$defs/capability"}
        },
        "maxConcurrency": {
          "type": "integer",
          "minimum": 1,
          "maximum": 32
        }
      }
    },
    "heartbeatResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "workerId",
        "serverTime",
        "activeTasks",
        "heartbeatIntervalSeconds"
      ],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "serverTime": {"type": "string", "format": "date-time"},
        "activeTasks": {"type": "integer", "minimum": 0},
        "heartbeatIntervalSeconds": {"type": "integer", "minimum": 5}
      }
    },
    "claimRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["workerId", "capabilities", "maxTasks", "waitSeconds"],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "capabilities": {
          "type": "array",
          "minItems": 1,
          "uniqueItems": true,
          "items": {"$ref": "#/$defs/capability"}
        },
        "maxTasks": {"const": 1},
        "waitSeconds": {
          "type": "integer",
          "minimum": 20,
          "maximum": 30
        }
      }
    },
    "responsesPayload": {
      "type": "object",
      "additionalProperties": false,
      "required": ["model", "input", "stream"],
      "properties": {
        "model": {"$ref": "#/$defs/textModel"},
        "input": {},
        "instructions": {},
        "temperature": {},
        "top_p": {},
        "max_output_tokens": {},
        "reasoning": {},
        "text": {},
        "tools": {},
        "tool_choice": {},
        "metadata": {},
        "store": {},
        "stream": {"const": false}
      }
    },
    "chatCompletionsPayload": {
      "type": "object",
      "additionalProperties": false,
      "required": ["model", "messages", "stream"],
      "properties": {
        "model": {"$ref": "#/$defs/textModel"},
        "messages": {"type": "array", "minItems": 1},
        "temperature": {},
        "top_p": {},
        "max_tokens": {},
        "max_completion_tokens": {},
        "frequency_penalty": {},
        "presence_penalty": {},
        "response_format": {},
        "stop": {},
        "seed": {},
        "stream": {"const": false},
        "reasoning_effort": {},
        "thinking": {},
        "tools": {},
        "tool_choice": {}
      }
    },
    "imageGenerationPayload": {
      "type": "object",
      "additionalProperties": false,
      "required": ["model", "prompt", "n"],
      "properties": {
        "model": {"const": "gpt-image-2"},
        "prompt": {"type": "string", "minLength": 1, "maxLength": 32000},
        "n": {"const": 1},
        "size": {},
        "quality": {},
        "output_format": {},
        "response_format": {},
        "background": {},
        "moderation": {},
        "output_compression": {},
        "user": {}
      }
    },
    "imageEditPayload": {
      "type": "object",
      "additionalProperties": false,
      "required": ["model", "prompt", "n"],
      "properties": {
        "model": {"const": "gpt-image-2"},
        "prompt": {"type": "string", "minLength": 1, "maxLength": 32000},
        "n": {"const": 1},
        "size": {},
        "quality": {},
        "output_format": {},
        "response_format": {},
        "background": {},
        "moderation": {},
        "output_compression": {},
        "input_fidelity": {},
        "user": {}
      }
    },
    "inputFile": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "index",
        "url",
        "filename",
        "contentType",
        "fileSize",
        "urlExpiresAt",
        "urlChecksum",
        "urlSignatureAlgorithm"
      ],
      "properties": {
        "index": {"type": "integer", "minimum": 0},
        "url": {"type": "string", "format": "uri", "pattern": "^https://"},
        "filename": {"type": "string"},
        "contentType": {"type": "string"},
        "fileSize": {"type": "integer", "minimum": 0},
        "urlExpiresAt": {"type": "string", "format": "date-time"},
        "urlChecksum": {"$ref": "#/$defs/sha256"},
        "urlSignatureAlgorithm": {"const": "HMAC-SHA256"}
      }
    },
    "claimedTask": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "jobId",
        "leaseId",
        "leaseUntil",
        "renewAfter",
        "graceUntil",
        "capability",
        "protocol",
        "model",
        "request",
        "inputFiles",
        "attempt"
      ],
      "properties": {
        "jobId": {"type": "string"},
        "leaseId": {"$ref": "#/$defs/leaseId"},
        "leaseUntil": {"type": "string", "format": "date-time"},
        "renewAfter": {"type": "string", "format": "date-time"},
        "graceUntil": {"type": "string", "format": "date-time"},
        "capability": {"$ref": "#/$defs/capability"},
        "protocol": {"$ref": "#/$defs/protocol"},
        "model": {
          "enum": [
            "gpt-image-2",
            "gpt-5.6-sol",
            "gpt-5.5",
            "gpt-5.6-terra",
            "gpt-5.6-luna"
          ]
        },
        "request": {
          "anyOf": [
            {"$ref": "#/$defs/responsesPayload"},
            {"$ref": "#/$defs/chatCompletionsPayload"},
            {"$ref": "#/$defs/imageGenerationPayload"},
            {"$ref": "#/$defs/imageEditPayload"}
          ]
        },
        "inputFiles": {
          "type": "array",
          "maxItems": 10,
          "items": {"$ref": "#/$defs/inputFile"}
        },
        "attempt": {"type": "integer", "minimum": 1}
      }
    },
    "claimResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["task", "empty", "serverTime", "waitedMs"],
      "properties": {
        "task": {
          "oneOf": [
            {"$ref": "#/$defs/claimedTask"},
            {"type": "null"}
          ]
        },
        "empty": {"type": "boolean"},
        "serverTime": {"type": "string", "format": "date-time"},
        "waitedMs": {"type": "integer", "minimum": 0}
      }
    },
    "leaseRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["workerId", "leaseId"],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "leaseId": {"$ref": "#/$defs/leaseId"}
      }
    },
    "leaseResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["jobId", "leaseUntil", "renewAfter", "graceUntil"],
      "properties": {
        "jobId": {"type": "string"},
        "leaseUntil": {"type": "string", "format": "date-time"},
        "renewAfter": {"type": "string", "format": "date-time"},
        "graceUntil": {"type": "string", "format": "date-time"}
      }
    },
    "artifactForm": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "workerId",
        "leaseId",
        "idempotencyKey",
        "sha256",
        "file"
      ],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "leaseId": {"$ref": "#/$defs/leaseId"},
        "idempotencyKey": {"$ref": "#/$defs/idempotencyKey"},
        "sha256": {"$ref": "#/$defs/sha256"},
        "file": {
          "type": "string",
          "contentEncoding": "binary",
          "contentMediaType": "image/png"
        }
      }
    },
    "artifactResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "artifactId",
        "idempotent",
        "contentType",
        "fileSize",
        "sha256",
        "sourceSha256",
        "width",
        "height",
        "format"
      ],
      "properties": {
        "artifactId": {"type": "string"},
        "idempotent": {"type": "boolean"},
        "contentType": {
          "enum": ["image/jpeg", "image/png", "image/webp"]
        },
        "fileSize": {"type": "integer", "minimum": 1},
        "sha256": {"$ref": "#/$defs/sha256"},
        "sourceSha256": {"$ref": "#/$defs/sha256"},
        "width": {"type": "integer", "minimum": 1},
        "height": {"type": "integer", "minimum": 1},
        "format": {"enum": ["jpeg", "png", "webp"]}
      }
    },
    "completeRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": ["workerId", "leaseId", "idempotencyKey"],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "leaseId": {"$ref": "#/$defs/leaseId"},
        "idempotencyKey": {"$ref": "#/$defs/idempotencyKey"},
        "artifactId": {"type": "string"},
        "result": {},
        "metadata": {"type": "object"}
      },
      "oneOf": [
        {"required": ["artifactId"]},
        {"required": ["result"]}
      ]
    },
    "terminalResponse": {
      "type": "object",
      "additionalProperties": false,
      "required": ["jobId", "status", "idempotent"],
      "properties": {
        "jobId": {"type": "string"},
        "status": {"enum": ["succeeded", "failed"]},
        "idempotent": {"type": "boolean"},
        "retryable": {"type": "boolean"}
      }
    },
    "failRequest": {
      "type": "object",
      "additionalProperties": false,
      "required": [
        "workerId",
        "leaseId",
        "idempotencyKey",
        "errorCode",
        "errorMessage"
      ],
      "properties": {
        "workerId": {"$ref": "#/$defs/workerId"},
        "leaseId": {"$ref": "#/$defs/leaseId"},
        "idempotencyKey": {"$ref": "#/$defs/idempotencyKey"},
        "errorCode": {
          "type": "string",
          "minLength": 1,
          "maxLength": 80
        },
        "errorMessage": {
          "type": "string",
          "maxLength": 500
        }
      }
    }
  }
}
```

## 5. Endpoints and examples

Examples use placeholders only. They do not contain a usable Token.

### 5.1 Heartbeat

```http
POST /api/internal/picpilot-worker/v1/heartbeat HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json

{
  "workerId": "studio-pc-01",
  "capabilities": ["image", "text"],
  "maxConcurrency": 1
}
```

Success, `200`:

```json
{
  "workerId": "studio-pc-01",
  "serverTime": "2026-07-31T12:00:00+08:00",
  "activeTasks": 0,
  "heartbeatIntervalSeconds": 20
}
```

Send heartbeat at the returned interval. Administrator online state becomes
offline after `OPENIMAGE_AI_WORKER_HEARTBEAT_OFFLINE_SECONDS`.

### 5.2 Claim with long polling

```http
POST /api/internal/picpilot-worker/v1/claim HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json

{
  "workerId": "studio-pc-01",
  "capabilities": ["image", "text"],
  "maxTasks": 1,
  "waitSeconds": 25
}
```

`waitSeconds` is constrained to 20-30 seconds. The server waits until a job is
available or the period ends; Workers MUST NOT replace this with one-second
polling.

Claimed, `200`:

```json
{
  "task": {
    "jobId": "awj_example",
    "leaseId": "awl_job_specific_secret",
    "leaseUntil": "2026-07-31T12:03:00+08:00",
    "renewAfter": "2026-07-31T12:02:00+08:00",
    "graceUntil": "2026-07-31T12:03:15+08:00",
    "capability": "image",
    "protocol": "openai_images_edits",
    "model": "gpt-image-2",
    "request": {
      "model": "gpt-image-2",
      "prompt": "Create a clean product image.",
      "size": "1024x1024",
      "n": 1
    },
    "inputFiles": [
      {
        "index": 0,
        "url": "https://suakitsu.com/api/internal/picpilot-worker/v1/inputs/awj_example/0?workerId=studio-pc-01&expires=1785470700&signature=<hex-hmac>",
        "filename": "reference-1.img",
        "contentType": "application/octet-stream",
        "fileSize": 0,
        "urlExpiresAt": "2026-07-31T12:05:00Z",
        "urlChecksum": "<64-character-hex-hmac>",
        "urlSignatureAlgorithm": "HMAC-SHA256"
      }
    ],
    "attempt": 1
  },
  "empty": false,
  "serverTime": "2026-07-31T12:00:00+08:00",
  "waitedMs": 18
}
```

No task after long polling, `200`:

```json
{
  "task": null,
  "empty": true,
  "serverTime": "2026-07-31T12:00:25+08:00",
  "waitedMs": 25000
}
```

There is no `204` no-task response in v1.

Claims are FIFO per Provider (`created_at ASC`) and atomic across threads and
backend instances. PostgreSQL advisory transaction locks, an in-transaction
active count, and `FOR UPDATE SKIP LOCKED` prevent duplicate leasing and enforce
global Provider concurrency.

### 5.3 Renew

```http
POST /api/internal/picpilot-worker/v1/jobs/awj_example/renew HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json

{
  "workerId": "studio-pc-01",
  "leaseId": "awl_job_specific_secret"
}
```

Success, `200`:

```json
{
  "jobId": "awj_example",
  "leaseUntil": "2026-07-31T12:06:00+08:00",
  "renewAfter": "2026-07-31T12:05:00+08:00",
  "graceUntil": "2026-07-31T12:06:15+08:00"
}
```

Renew at or shortly after `renewAfter`. A disconnected Worker may recover and
renew after `leaseUntil` only while the same lease remains before
`graceUntil`.

### 5.4 Download a signed image-edit input

The claim URL format is:

```text
https://{publicBase}/api/internal/picpilot-worker/v1/inputs/{jobId}/{index}
  ?workerId={urlEncodedWorkerId}
  &expires={unixSeconds}
  &signature={lowercaseHexHmacSha256}
```

The server computes `urlChecksum`/`signature` as HMAC-SHA256 over:

```text
jobId + "\n" +
index + "\n" +
workerId + "\n" +
expires + "\n" +
sourceHttpsUrl
```

The Worker cannot compute the HMAC because the signing secret remains on the
server. It verifies integrity by requiring `urlChecksum` to equal the
`signature` query value and by accepting only HTTPS. The server verifies the
HMAC, expiration, Token ownership, active business task, and active
lease/grace window.

PicPilot rejects input URLs containing user-info credentials or literal
localhost, private, link-local, carrier-grade NAT, cloud-metadata, reserved, or
local-network hosts before queueing (`INPUT_URL_NOT_PUBLIC`).

Request the URL with the Worker Bearer Token. The server currently returns
`307 Temporary Redirect` to the existing HTTPS asset. The Worker's HTTP client
MUST strip `Authorization` when following a cross-origin redirect. Signed URLs
and downloaded inputs must not be persisted beyond the job.

### 5.5 Upload an image artifact

The Worker must download/decode the local model output and upload actual image
bytes. Base64 is never stored in `request_json` or `result_json`.

```http
POST /api/internal/picpilot-worker/v1/jobs/awj_example/artifacts HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: multipart/form-data; boundary=...

workerId=studio-pc-01
leaseId=awl_job_specific_secret
idempotencyKey=artifact-awj-example-0001
sha256=<SHA-256 of the exact multipart file bytes>
file=<binary image>
```

Accepted output formats are JPEG, PNG, and WebP. PicPilot validates magic bytes,
decodes the image, enforces the configured byte limit and 50-megapixel limit,
normalizes metadata/orientation, and stores only a genuine normalized image.
The default artifact limit is 25 MiB; the effective request limit is also
bounded by the deployment multipart configuration.

Success, `200`:

```json
{
  "artifactId": "awa_example",
  "idempotent": false,
  "contentType": "image/png",
  "fileSize": 1048576,
  "sha256": "<SHA-256 of normalized stored bytes>",
  "sourceSha256": "<SHA-256 supplied for uploaded bytes>",
  "width": 2048,
  "height": 2048,
  "format": "png"
}
```

Repeating the same upload with the same `idempotencyKey`, Worker, lease identity,
and source SHA returns the same artifact with `idempotent: true`. Reusing the key
with different bytes returns `409 IDEMPOTENCY_CONFLICT`.

### 5.6 Complete

Image:

```http
POST /api/internal/picpilot-worker/v1/jobs/awj_example/complete HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json

{
  "workerId": "studio-pc-01",
  "leaseId": "awl_job_specific_secret",
  "idempotencyKey": "complete-awj-example-0001",
  "artifactId": "awa_example",
  "metadata": {
    "model": "gpt-image-2"
  }
}
```

Text:

```json
{
  "workerId": "studio-pc-01",
  "leaseId": "awl_job_specific_secret",
  "idempotencyKey": "complete-awj-text-0001",
  "result": {
    "choices": [
      {
        "message": {
          "content": "Generated product copy"
        }
      }
    ]
  },
  "metadata": {
    "model": "gpt-5.5"
  }
}
```

Accepted text result shapes:

- `result.text`
- `result.output_text`
- `result.choices[0].message.content`
- `result.output[].content[].text`

Success, `200`:

```json
{
  "jobId": "awj_example",
  "status": "succeeded",
  "idempotent": false
}
```

Exact duplicate, `200`:

```json
{
  "jobId": "awj_example",
  "status": "succeeded",
  "idempotent": true
}
```

`idempotencyKey` and a canonical request hash are stored atomically with the
terminal state. The same key and same payload replay safely. Any changed
artifact, result, or metadata returns `409 IDEMPOTENCY_CONFLICT`. A second
terminal action of another type returns `409 JOB_TERMINAL_CONFLICT`.

### 5.7 Fail

```http
POST /api/internal/picpilot-worker/v1/jobs/awj_example/fail HTTP/1.1
Host: suakitsu.com
Authorization: Bearer <worker-token-shown-once>
Content-Type: application/json

{
  "workerId": "studio-pc-01",
  "leaseId": "awl_job_specific_secret",
  "idempotencyKey": "fail-awj-example-0001",
  "errorCode": "UPSTREAM_TIMEOUT",
  "errorMessage": "Local model request timed out"
}
```

Success, `200`:

```json
{
  "jobId": "awj_example",
  "status": "failed",
  "idempotent": false,
  "retryable": true
}
```

An exact duplicate returns `idempotent: true`. Error text is limited to 500
characters and obvious Bearer/API-key values are redacted. Never submit a stack
trace, prompt, local path, credentials, OAuth data, or full local response body.

## 6. Lease, cancellation, retry, and idempotency semantics

State transitions:

```text
queued -> leased -> succeeded
                  -> failed
                  -> cancelled
                  -> expired
queued -> cancelled
expired -> queued  only when offline_action=queue
```

- `leaseId` is a high-entropy, job-specific secret. Only its hash is stored.
- `leaseUntil` is the normal renewal deadline.
- `renewAfter` is the recommended renewal time.
- `graceUntil` is the hard ownership deadline.
- `renew`, first artifact upload, `complete`, and `fail` are accepted during the
  normal lease or grace window for the same Worker and lease.
- After `graceUntil`, the attempt expires. Results from that lease are rejected.
- If an expired queue-policy job is leased again, the new attempt gets a new
  `leaseId`; the old Worker cannot overwrite it.
- Queue wait time occurs before leasing and is not lease time.
- A user-deleted/cancelled image business task cancels its queued/leased Worker
  job. Later input download, artifact, renew, complete, or fail returns
  `410 JOB_CANCELLED`.
- A repeated successful artifact/complete/fail request is safe only when its
  idempotency key and canonical content match.
- Terminal updates run under a database row lock, so callbacks cannot settle a
  task twice.
- Existing image task logic remains responsible for one-time success, refund,
  storage, and credit settlement.

Provider fallback:

- Merely being busy does not create a parallel HTTP call.
- `offline_action=fallback` applies only while the job is still unclaimed after
  `local_wait_seconds`.
- A genuine Worker failure permits the existing next HTTP Provider.
- `offline_action=queue` requeues an expired attempt.
- A claimed Local Worker job never races an HTTP fallback.

## 7. Error responses and retry classification

All protocol errors use:

```json
{
  "error": {
    "code": "LEASE_EXPIRED",
    "message": "Worker lease has expired",
    "retryable": false
  }
}
```

Common status codes:

| HTTP | Codes |
| --- | --- |
| `400` | `REQUEST_SCHEMA_INVALID`, `WORKER_ID_INVALID`, `CAPABILITIES_REQUIRED`, `IDEMPOTENCY_KEY_INVALID`, `ARTIFACT_EMPTY`, `ARTIFACT_INVALID`, `ARTIFACT_SHA256_INVALID`, `ARTIFACT_SHA256_MISMATCH`, `ARTIFACT_MIME_UNSUPPORTED`, `ARTIFACT_REQUIRED`, `RESULT_REQUIRED`, `RESULT_TOO_LARGE`, `RESULT_SECRET_NOT_ALLOWED`, `BASE64_RESULT_NOT_ALLOWED` |
| `401` | `WORKER_TOKEN_MISSING`, `WORKER_TOKEN_INVALID` |
| `403` | `WORKER_TOKEN_MISMATCH`, `WORKER_MISMATCH`, `LEASE_ID_INVALID`, `ARTIFACT_WORKER_MISMATCH`, `INPUT_SIGNATURE_INVALID` |
| `404` | `JOB_NOT_FOUND`, `INPUT_NOT_FOUND` |
| `409` | `WORKER_ID_IN_USE`, `JOB_NOT_LEASED`, `LEASE_EXPIRED`, `IDEMPOTENCY_CONFLICT`, `JOB_TERMINAL_CONFLICT` |
| `410` | `JOB_CANCELLED`, `INPUT_URL_EXPIRED` |
| `426` | `HTTPS_REQUIRED` |
| `500` | `WORKER_INTERNAL_ERROR` |

Transport/protocol errors are retryable only when `error.retryable=true`.
Currently retryable categories include:

- `WORKER_INTERNAL_ERROR`
- `WORKER_WAIT_INTERRUPTED`
- `WORKER_HEARTBEAT_REQUIRED`
- `LOCAL_WORKER_OFFLINE`
- `ARTIFACT_READ_FAILED`
- HTTP `408`, `429`, and `5xx`

Worker-reported execution errors classified retryable:

- `NETWORK_ERROR` and `NETWORK_*`
- `UPSTREAM_TIMEOUT`
- `UPSTREAM_RATE_LIMITED`
- `UPSTREAM_UNAVAILABLE`
- `UPSTREAM_5*`
- `WORKER_RUNTIME_ERROR`

All other Worker-reported error codes are non-retryable by default, including
invalid payload, unsupported model, content-policy rejection, and upstream
authentication/configuration errors. The Worker must not retry a task after
`JOB_CANCELLED`, `LEASE_EXPIRED`, `WORKER_MISMATCH`,
`LEASE_ID_INVALID`, or `JOB_TERMINAL_CONFLICT`.

### 7.1 Required error examples

Invalid/revoked Token, `401`:

```json
{
  "error": {
    "code": "WORKER_TOKEN_INVALID",
    "message": "Worker token is invalid or revoked",
    "retryable": false
  }
}
```

Cancelled job, `410`:

```json
{
  "error": {
    "code": "JOB_CANCELLED",
    "message": "Worker job was cancelled",
    "retryable": false
  }
}
```

Expired lease, `409`:

```json
{
  "error": {
    "code": "LEASE_EXPIRED",
    "message": "Worker lease has expired",
    "retryable": false
  }
}
```

Artifact checksum failure, `400`:

```json
{
  "error": {
    "code": "ARTIFACT_SHA256_MISMATCH",
    "message": "Artifact SHA-256 does not match the uploaded bytes",
    "retryable": false
  }
}
```

Duplicate complete is not an error; it returns the `200` idempotent response in
section 5.6.

## 8. Administrator configuration and status

Provider fields:

| Field | Rule |
| --- | --- |
| `execution_mode` | Local IDs are fixed to `pull_worker`; existing Providers remain `http` |
| `priority` | Lower number runs first; local defaults to `-100` |
| `model` | Image fixed to `gpt-image-2`; text selected from the v1 catalog |
| `enabled` | Defaults to `false` |
| `local_wait_seconds` | 1-300 seconds |
| `timeout_seconds` | 1-300 seconds after a text job is leased; local text defaults to 90 seconds |
| `offline_action` | `queue` or `fallback` |
| `lease_seconds` | 30-1800 seconds |
| `max_concurrency` | 1-32 global active leases for the Provider |

Pull Worker Providers require no `base_url`, API key, or environment-key field.
The administrator status endpoint reports Token metadata, Worker online state,
last heartbeat, active/current job IDs, capability, and concurrency. It never
returns a raw Token.

## 9. Database and server configuration

Flyway migrations:

- `V40__ai_pull_worker_provider.sql`: Token, instance, job, and artifact tables.
- `V41__ai_worker_protocol_v1_hardening.sql`: lease grace, terminal
  idempotency hashes, artifact ownership, source SHA, and uniqueness.

Required production secrets/settings:

```dotenv
OPENIMAGE_AI_WORKER_PUBLIC_BASE_URL=https://suakitsu.com
OPENIMAGE_AI_WORKER_TOKEN_PEPPER=<independent-random-secret-at-least-32-bytes>
OPENIMAGE_AI_WORKER_SIGNING_SECRET=<different-random-secret-at-least-32-bytes>
```

Optional:

```dotenv
OPENIMAGE_AI_WORKER_HEARTBEAT_OFFLINE_SECONDS=60
OPENIMAGE_AI_WORKER_INPUT_URL_TTL_SECONDS=300
OPENIMAGE_AI_WORKER_MAX_ARTIFACT_BYTES=26214400
OPENIMAGE_AI_WORKER_LEASE_GRACE_SECONDS=15
```

The public base URL must be the externally reachable backend HTTPS origin. Do
not reuse the Token pepper as the signing secret in production.

## 10. Fixed automation fixture

The only fixed protocol fixture is:

```text
openImage/src/test/resources/fixtures/local-worker-protocol-v1.json
```

Its Token starts with `ppw_test_` and explicitly states that it is not a
production Token. It is valid only as static test data and is not inserted into
the production database. Protocol tests use mocks and never call a real model.

## 11. Minimal Worker algorithm

```text
load Worker Token from the local secret store
heartbeat(workerId, capabilities, maxConcurrency)

while running:
    envelope = claim(waitSeconds=25, maxTasks=1)
    if envelope.empty:
        continue

    job = envelope.task
    schedule renew near job.renewAfter

    if job.protocol is an image edit:
        download signed input URLs without forwarding Authorization cross-origin

    call the local OpenAI-compatible endpoint using job.protocol/model/request

    if image:
        decode/download output bytes
        sha = SHA-256(output bytes)
        artifact = upload multipart with a stable idempotencyKey and sha
        complete with artifact.artifactId and another stable idempotencyKey
    else:
        complete with normalized text result and a stable idempotencyKey

    on sanitized local/upstream failure:
        fail with a stable idempotencyKey, short errorCode, and short errorMessage

    on transport uncertainty:
        retry the exact same request with the exact same idempotencyKey

    on JOB_CANCELLED or LEASE_EXPIRED:
        stop local execution and discard late output
```

The Worker must keep `leaseId` in memory only. It must not send its local
CLIProxyAPI Key, OAuth state, local server URL, or local configuration to any
PicPilot endpoint.

## 12. Deployment and rollback

Deployment:

1. Back up PostgreSQL.
2. Deploy the backend and let Flyway apply V40 and V41.
3. Configure the public HTTPS origin and two independent random secrets.
4. Deploy the administrator frontend.
5. Keep both Local Worker Providers disabled.
6. Generate a Worker Token once in the administrator page.
7. Start the local Worker and confirm heartbeat/empty long polling.
8. Enable one Local Worker Provider with `offline_action=fallback`.
9. Validate with mock/local execution only; health checks must not call a paid
   model.

Rollback:

1. Disable `local-cliproxy-image` and `local-cliproxy-text`.
2. Revoke Worker Tokens.
3. Allow active leases to complete or reach `graceUntil`.
4. Deploy the previous application build.
5. Keep V40/V41 tables and columns for audit/forward compatibility. Remove them
   only in a separately reviewed migration after confirming no retained Worker
   jobs or artifacts are needed.
