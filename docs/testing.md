# Testing and Acceptance

The implementation checklist is the release gate: [implementation-status.md](implementation-status.md). A passing build alone is not feature parity. No automated command in this document needs a production database, model key, Worker Token, customer image or payment account.

## Go and PostgreSQL

From the repository root, the dedicated test Compose project builds the Go test image and starts a disposable PostgreSQL instance with no published ports:

```sh
docker compose -f compose.test.yml up --build --abort-on-container-exit --exit-code-from tests
docker compose -f compose.test.yml down
```

It uses a separate internal network, a database name beginning with `picpilot_test_`, temporary database storage, and mock gateways. The test-only database uses trust authentication inside that isolated network; the application Compose file never uses this setting. Do not merge `compose.test.yml` into production Compose configuration.

The suite includes race detection and real PostgreSQL transactions: duplicate business requests, atomic multi-instance claims, global concurrency, lease expiry/renewal, duplicate completion, stale callbacks, one-time credits/refunds, storage ownership and safe fallbacks. Some authentication tests are deliberately slower under `-race` because they use the real password hashing cost.

Without Docker, use Go 1.26 or newer and an independently provisioned empty PostgreSQL test database. Configure `PICPILOT_TEST_DATABASE_URL` privately, with a database name beginning `picpilot_test_`. Each test creates/drops only its own randomly named schema. Never use the application or original Java database.

```sh
cd core
go test -p=2 -count=1 ./...
go vet ./...
```

Without a test database, database tests explicitly skip. That is useful for pure unit tests but is not release acceptance. Protocol fixtures contain deterministic test-only values and must never be used as production credentials.

## Frontends

Run separately in `web/user` and, for the platform edition, `web/admin`:

```sh
npm ci
npm test -- --run
npm run typecheck
npm run lint
npm run build
npm audit
```

Use Node 24 for the verified toolchain. Browser tests must use mocked providers or the isolated mock acceptance installation; never record production tokens. Canvas source-build and browser acceptance are tracked separately, including upload, references, node editing, history, page reload, GIF assembly, asset export and video via mock adapters.

## Safe Acceptance Setup

Use distinct `COMPOSE_PROJECT_NAME`, ports, network subnet and an empty database/application volume. Set `PICPILOT_DEMO_MODE=true` only in that test installation. Synthetic output is explicitly labeled as mock, and none of it proves model quality or live merchant compatibility. Do not enable a real provider merely to make a test pass.

Required checks include:

- First-run wizard, owner creation, login/logout and password recovery/session revocation.
- Platform administrator role checks, personal edition blocking of operator features, and per-user provider encryption/ownership.
- Image/text tasks surviving navigation/reload, signed full-size image downloads, ordering of multiple references and no crop/upscale transformations.
- Linked Listing/A+ pricing matching the actual submitted tasks, suites, translation, chat and videos.
- Worker v1 authentication, empty 20-30 second claims, renew/grace windows, safe artifact uploads and stable idempotency keys.
- Billing and moderation with mock signatures, callbacks, duplicate requests and pagination.
- Both canvas interfaces on desktop/mobile, no service-worker scope leaks, no external credentials or executable plugin code with parent privileges.
- Stop/start, backup/restore into an empty destination, schema checksums and rollback using the matching version.

The `tests` and documentation fixture values are synthetic. Do not replace them with production values. Secret scans must cover staged files and commit history before any private push, and must be repeated before a later public release.
