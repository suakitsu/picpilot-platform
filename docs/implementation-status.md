# Implementation and acceptance status

Status: private migration validation candidate. The Java application remains unchanged; neither Go repository is public or approved for production traffic.

The objective is two independently deployable Go-backed editions with complete workflows, not a demo subset. The Java application remains unchanged.

## Acceptance gates

- [ ] Private GitHub repositories pushed and visibility verified (push is the final delivery step; public release is not authorized).
- [x] Shared Go core: PostgreSQL schema, versioned migration, configuration, authentication and authorization.
- [x] Personal encrypted per-user image/text/video provider settings and strict credential isolation.
- [x] Image/text/video adapters, mock gateways, routing and submission ambiguity handling.
- [x] Durable queues, atomic claims, restart recovery, idempotency and cancellation.
- [x] Local Worker Protocol v1 parity and multi-instance lease tests.
- [x] Upload/HEIC normalization, reference ownership, artifact storage and uncropped image handling.
- [x] Listing, A+, commerce suites, translation, creative chat and assistant workflows.
- [x] Nova workspace and infinite canvas built reproducibly from corresponding source; business backend is Go.
- [x] Platform administrator, users, credits, payments, refunds, risk, reports and messages.
- [x] Personal frontend has no platform billing/admin dependencies.
- [x] Clean-clone startup, first-run wizard, backups/restores and upgrades tested for both editions.
- [x] README.md, README.en.md and README.ja.md with independently verified commands.
- [x] Source/license/secret review, mock full-flow tests and desktop/mobile browser acceptance.
- [ ] Both repositories pushed privately; no public release without explicit approval.

## Structure

`core` is the authoritative versioned Go module. Both applications use a local module replacement to their checked-in core snapshot, avoiding private cross-repository downloads during quick start. Platform and personal entry points select immutable edition policies at startup. A later release synchronization check must prove that shared source is identical.

The first release uses PostgreSQL in both editions to retain transactional accounting and global queue correctness. Local file storage is the default; the validated S3-compatible backend is optional. HEIF is opt-in and uses the packaged native toolchain. External model, email, payment and OAuth integrations are disabled until explicitly configured.

## Verified Results

- Platform core: full `go test -p=2 ./... -count=1` and `go vet ./...` passed.
- Personal core: full `go test -p=2 ./... -count=1` and `go vet ./...` passed.
- Platform HTTP smoke: 23 checks passed. Personal HTTP smoke: 24 checks passed.
  Both covered first-run owner setup, login, provider ownership/encryption,
  upload, mock image/text generation, idempotency, signed media and logout.
- Disposable PostgreSQL startup applied all current migrations. Backup/restore
  into an empty destination, non-empty restore rejection, stop/start, and
  schema checksum checks passed in the isolated acceptance installation.
- Platform and personal user frontends passed typecheck, tests and production
  builds; platform admin passed typecheck, tests and production build.
  Nova passed 89 tests in each edition. Infinite Canvas typecheck/build passed
  in each edition.
- Native HEIF acceptance passed all eight Exif orientations, dimension limits,
  metadata stripping and temporary-file cleanup. Docker API, web and gateway
  images built successfully, and both Caddy configurations validated.
- Desktop and 390px mobile browser acceptance loaded Nova and Infinite Canvas
  through the PicPilot shell with no console errors or horizontal overflow.
- Staged-file secret scans and manual high-risk pattern scans passed for both
  repositories. No real model, payment, mail, OAuth, or production database
  was used during this migration validation.

The remaining unchecked item is deliberate: the final private Git push and
remote visibility verification. Public release still requires a separate,
explicit owner approval.
