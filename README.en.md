# PicPilot Platform Go

[简体中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

The operator-managed PicPilot edition, with a Go business backend, user workspace, administrator console, creative studio and infinite canvas. Accounts, durable tasks, credits, payments, risk controls and model providers share one backend.

**Private migration acceptance build; production readiness has not been declared.** See the [acceptance checklist](docs/implementation-status.md). Never point it at the old production database. The Go edition has its own schema and does not automatically import Java data.

## Editions

| Repository | Purpose |
| --- | --- |
| Original openImage Java repository | Existing platform, preserved separately |
| `suakitsu/picpilot-platform-go` | Multi-user platform; operators configure models, credits and payments |
| `suakitsu/picpilot-personal-go` | Self-hosted personal edition; configure your own providers without platform billing or an administrator console |

Each Go repository includes its own shared-core snapshot and frontend sources. One clone is sufficient; a second private repository is not a build dependency. React/Next.js still renders the frontend. Next handles rendering and transport only; Go owns business logic, task execution and storage.

## Quick Start

Install Git, a running Docker Engine or Docker Desktop, and Docker Compose v2. The first build needs access to image registries and npm/Go dependency sources. Allow at least 4 CPU cores, 8 GB available memory and 10 GB free disk. Builds are concurrency-limited; no reboot is performed.

```sh
git clone https://github.com/suakitsu/picpilot-platform-go.git
cd picpilot-platform-go
```

While this repository is private, use your authorized GitHub account. Never embed access tokens in the clone URL.

Windows PowerShell:

```powershell
./scripts/init.ps1
docker compose up -d --build
docker compose ps
docker compose exec api /picpilot --setup-token
```

Linux / macOS:

```sh
sh scripts/init.sh
docker compose up -d --build
docker compose ps
docker compose exec api /picpilot --setup-token
```

1. Open the [user site](http://localhost:8080) and create the initial administrator using the setup token printed by the final command. Keep that token private.
2. Open the [administrator console](http://localhost:8081) and sign in with that account.
3. Add your image, text or video provider. Select the actual compatible protocol and model, enter its key, then enable it explicitly. Configuration checks do not call a paid model.
4. Registration, payment channels and Local Worker providers start disabled. Configure them before opening the service to other users.

There are no default credentials. The initializer creates a random database password. On first start, Go generates separate encryption, session and signing keys in the application volume. **Keep `instance-secrets.json`; deleting it makes saved provider credentials unreadable.**

Only `127.0.0.1:8080/8081` is published by default. PostgreSQL has no host port. For port conflicts, edit both ports and their public URLs in `.env` before starting. Never use `docker compose down -v` as a shutdown command: it deletes persistent data.

## Scope

- Image, text and video use persistent jobs, polling, ownership checks and duplicate-submission protection. Ordinary HTTP providers and Local Worker providers remain separate execution modes.
- Listing, A+, commerce suites, translation, creative images/chat, video and both canvases are verified against the acceptance checklist.
- Platform operations include users, credits, payment history, moderation, prompts, tasks and usage records. A clean installation contains no customer or historical production data.
- Local Worker only makes outbound HTTPS long-poll requests. It does not need a public port or upload its local gateway credentials. [Protocol v1](docs/local-worker-protocol-v1.md) is authoritative.
- Provider compatibility depends on the actual protocol and model. Vendor labels alone do not guarantee compatibility. Default tests never invoke paid models or live payment services.

## Operations

- [Configuration, HTTPS and Local Worker](docs/configuration.md)
- [Backups, restores, upgrades and rollback](docs/operations.md)
- [Testing and safety](docs/testing.md)
- [Implementation and acceptance status](docs/implementation-status.md)

```sh
docker compose logs --tail=100 api
docker compose stop
docker compose start
```

For a lost owner password without email configured, use [local owner recovery](docs/operations.md#owner-recovery). Do not bypass hashing or session revocation with manual plaintext database updates.

## Source and License

[AGPL-3.0](LICENSE). Corresponding modified canvas sources, licenses and provenance are retained under `third_party/*/UPSTREAM.md`. Branding changes do not remove license obligations. Both new Go repositories remain private until the owner explicitly approves publication.

Environment files, keys, databases, uploads, backups, dependency caches and generated builds do not belong in Git.
