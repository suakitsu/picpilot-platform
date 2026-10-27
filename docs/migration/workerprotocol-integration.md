# Worker Protocol Integration

## Completed Pure Library

`core/workerprotocol` is standard-library-only, with no HTTP router, database,
network client, filesystem implementation, wall clock, random generator or
billing side effects. The authority document is copied byte-for-byte to
`local-worker-protocol-v1.md` (SHA-256
`4c4bc9f674452dcee04e62d5f7a0a30389d51ef3125d79e1e0ac04514edca97b`).
`testdata/schema.json` is mechanically extracted from its normative JSON block;
`original-fixture.json` is the original test-only fixture. `wire-fixtures.json`
adds valid/invalid endpoint and payload examples. No production token exists in
these files.

| Exported API | Purpose |
| --- | --- |
| `Decode[T]`, `ValidateWire`, `JSONSchema`, DTO `Validate` methods | Exact camelCase envelope fields, required/null/unknown/duplicate checks, payload allowlists and cross-field semantics. Raw `json.Unmarshal` alone is NOT strict. |
| Heartbeat/claim/lease/complete/fail/terminal/artifact/input/error DTOs | Exact v1 wire names. Claim always has `{task,empty,serverTime,waitedMs}` and explicit `task:null`; no business task field or legacy lease token name. |
| `ValidatePayload`, `ValidateInputFiles`, `ValidatePublicHTTPSURL`, `ExtractText` | Capability/protocol/model catalog, non-streaming, n=1, limits, secret/base64 checks at arbitrary nested levels, signed metadata and accepted text result families. |
| `ValidateMultipartFields`, `VerifyArtifactSource` | Exact multipart field names and actual uploaded byte checksum/limit. File bytes are not a JSON DTO. |
| `BindWorker`, `VerifyWorkerOwnership`, `Capacity.Available` | Pure binding/ownership and capacity decisions. Opaque token IDs only; authenticate current token separately. |
| `Acquire`, `Renew`, `VerifyLease`, `HardDeadline`, `IsActive` | Explicit-time lease operations; renewAfter at two thirds, lease and grace deadlines, hash-only snapshots, fresh identity on a new attempt. |
| `Expire`, `Requeue`, `Cancel`, `ReconcileBusinessTask`, `OfflineFallback` | Queue-only recovery and unclaimed fallback; cancellation and stale callback rejection. No parallel fallback from a leased job. |
| `ProcessingDeadline`, `AllowsNextProvider` | Post-claim processing timeout and genuine worker failure policy. |
| `Complete`, `Fail`, `RecordArtifact` | Copy-on-write terminal and artifact decisions; exact key/content replay, immutable receipts, opposite terminal conflict, source versus normalized checksums and attempt ownership. |
| `CanonicalHash`, `CompleteFingerprint`, `FailFingerprint` | Stable nested object order, preserved array order and number precision; result/artifact/metadata included. |
| `LeaseDigest`, `InputSignature`, `VerifyInputAccess` | Java-compatible lease digest and HMAC input signature formula; explicit ownership/time/signature checks. No secret generation or network access. |
| `NewError`, `Retryable`, `MustStop`, `ShouldRetryTransport`, `SanitizeErrorMessage` | Safe error envelope, status/retry classification and short sanitized execution failure text. |

## Required Adapter Sequence

1. Enforce TLS and independent current Worker-token authentication before every
   operation, including idempotent replay. Do not accept arbitrary forwarded
   HTTPS headers. Keep administrator token issuance behind user/admin auth.
2. Decode strictly; multipart fields are validated separately. Verify worker-ID
   binding to the token's persistent ID; never substitute a user JWT.
3. Lock authoritative state, reconcile inactive/deleted business tasks before
   callback access, and enforce provider/worker concurrency in the same claim
   transaction. Claim FIFO per provider with atomic row selection. Pure
   `Capacity.Available` is not a lock or concurrency guarantee.
4. Supply a fresh high-entropy raw lease only for claim output; store the hash.
   Build signed inputs for that job/worker/attempt from owned source files. Keep
   business IDs out of the claimed task. Signed input URLs must not outlive the
   intended window and no credentials may cross an origin redirect.
5. Use one explicit UTC instant per transition. At `now == graceUntil` the lease
   is expired. After success/failure an exact receipt replay may succeed later;
   it is not a new terminal transition. Same-identity artifact receipts may
   similarly replay after the window, but never after cancellation or on a new
   attempt. New results/uploads/renewals require an active lease/grace window.
6. For artifacts, validate/decode magic bytes, source byte limit and 50 MP,
   normalize orientation/metadata, and calculate SHA256 of the exact normalized
   stored bytes. `RecordArtifact` validates metadata and source bytes but cannot
   prove the caller actually decoded/normalized an image. Handle storage cleanup
   on transaction conflicts and orphaned uploads.
7. Persist changed job state, idempotency key/hash and artifact ownership
   atomically. Return a replay without settling twice. Existing business task
   processing alone owns final success/refund/credit settlement.

Internal Job/Worker/ArtifactRecord snapshots are not wire responses. Do not
serialize them or log their contents. DTOs containing raw lease IDs are also not
log-safe. Error strings are fixed safe text; worker failure messages are sanitized
before persistence.

## Compatibility Decisions

- The unchanged authority is normative. The original Java test policy also
  treats `OUTPUT_DIMENSION_MISMATCH` and `OUTPUT_ASPECT_RATIO_MISMATCH` as
  retryable; both are retained and tested without editing the spec.
- The Java legacy terminal replay path accepts missing stored fingerprints.
  The pure library intentionally fails closed there: v1 requires exact content.
  Drain/migrate legacy rows explicitly; do not turn this into an authentication
  or idempotency bypass.
- Java contains depth-limited secret scanning and permissive coercion/truncation
  paths. Strict wire validation rejects missing/wrong-type/duplicate keys and
  scans nested secrets without those holes. Artifact fingerprints use the full
  supplied artifact ID, without truncation collisions. Internal canonical hashes
  are not wire fields; existing persisted Java hashes need migration compatibility
  verification for uncommon numeric/string encodings before replaying old rows.
- The upload replay path checks actual supplied bytes, not only a claimed
  checksum. Changed bytes cannot replay a receipt just by repeating its header.
- Failure receipt hashes cover the validated original message, while persisted
  display text is independently sanitized. Distinct messages must not collapse
  into one receipt because they both redact to the same text. Messages containing
  filesystem paths or URLs have their entire detail omitted, including paths
  with spaces. This is an internal-hash hardening difference from Java's lossy
  sanitized-message fingerprint; legacy receipt import needs an explicit plan.
- URL validation is literal/public-host validation, not DNS resolution or an
  HTTP fetcher. Parent storage/network code must enforce egress and redirects.
- Numeric schema arithmetic is bounded beyond all representable DTO integers
  to avoid large-exponent resource amplification. Unconstrained JSON payload
  values are preserved with `json.Number`, never rounded through float64.

## Verification

Local package tests cover the original controller/job/error-policy scenarios,
exact deadline boundaries, mismatched ownership, queue recovery and stale leases,
late completion, cancellation of every callback, canonical changed payloads,
artifact metadata/checksum/replay, signed inputs, public-host rejection,
secret/base64 rejection, failure redaction and copy-on-write deterministic calls.
The authority hash/schema, original fixture, and valid/invalid JSON fixtures are
checked. A bounded strict-decoder fuzz run and `go vet` are also run locally.

Use the installed `E:/picpilot/development-tools/go1.27.3/go/bin/go.exe` from
`core`, with `GOCACHE=E:/picpilot/development-cache/go/build`,
`GOMODCACHE=E:/picpilot/development-cache/go/modules`,
`GOPATH=E:/picpilot/development-cache/go`, `GOMAXPROCS=2`,
`GOTMPDIR=E:/picpilot/development-cache/go/tmp`, `GOTOOLCHAIN=local` and
`GOWORK=off`:

```text
go test -mod=readonly -count=1 -cover ./workerprotocol
go vet -mod=readonly ./workerprotocol
go test -mod=readonly -run=^$ -fuzz=FuzzStrictDecode -fuzztime=10s -parallel=2 ./workerprotocol
```

No module manifests were edited. Database, HTTP, token revocation, image decoding,
multi-instance concurrency and billing integration were not established by the
pure-library tests. A C compiler is not present on PATH, so race instrumentation
is not claimed. These remain adapter/integration acceptance gates, not omissions
from the full platform/personal migration scope.
