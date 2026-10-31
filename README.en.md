# PicPilot Platform

[简体中文](README.md) | [English](README.en.md) | [日本語](README.ja.md)

An open-source multi-user AI platform for image generation, video generation, e-commerce creation, reference images, creative canvas, provider routing, credits, payments and operations. Go owns business logic, jobs and storage; React/Next.js renders the interface.

> **Status: public migration acceptance build.** Use a new database for the first deployment. Do not connect the old Java production database.

## Editions

| Repository | Use case |
| --- | --- |
| `suakitsu/picpilot-platform` | Multi-user operations, admin console, credits, payments and moderation |
| `suakitsu/picpilot-personal` | Personal self-hosting with your own providers; no platform billing or admin site |

Both Go repositories contain complete source and a `core` snapshot. One clone is enough to build.

## Quick start

Install Git, Docker Engine/Docker Desktop and Docker Compose v2. The first build is most comfortable with 4 CPU cores, 8 GB RAM and 10 GB free disk.

```sh
git clone https://github.com/suakitsu/picpilot-platform.git
cd picpilot-platform
```

Windows PowerShell:

```powershell
./scripts/init.ps1
docker compose up -d --build
docker compose ps
docker compose exec api /picpilot --setup-token
```

Linux / macOS: replace the first command with `sh scripts/init.sh` and keep the rest.

1. Open `http://localhost:8080` and use the one-time setup token printed in the terminal to create the first administrator.
2. Open `http://localhost:8081` and sign in to the admin console.
3. Add a provider with its real protocol, model, URL and key; validate it before enabling it.

Fresh installs have no default account or password. Registration, payments and Local Worker providers start disabled. Keep `instance-secrets.json`; deleting it makes saved provider keys unreadable.

## Providers and local gateways

Configured provider base URLs support both `http://` and `https://`, including local gateways such as `http://127.0.0.1:8888/v1`. From a container, Docker Desktop can reach a host gateway through `host.docker.internal`. User-submitted external image references still use the strict safe-download policy.

The provider must match the upstream protocol. A vendor name alone does not make an arbitrary API compatible. Configuration checks do not call paid models.

## Screenshots

![Platform operations dashboard](docs/screenshots/platform-admin-dashboard-desktop.png)

![Platform user workspace](docs/screenshots/platform-user-workspace-desktop.png)

![Platform AI creation page](docs/screenshots/platform-user-creative-desktop.png)

![Platform history page](docs/screenshots/platform-user-history-desktop.png)

## Deployment and maintenance

- By default, only `127.0.0.1:8080/8081` is published; PostgreSQL has no host port.
- For public access, configure HTTPS at the gateway or reverse proxy and keep public URLs consistent.
- Back up `.env`, the database, uploads and `instance-secrets.json`; never commit them.
- Use `docker compose stop` for routine shutdown. `docker compose down -v` deletes persistent volumes.

Read more:

- [Configuration, HTTPS and Local Worker](docs/configuration.md)
- [Backups, restores, upgrades and rollback](docs/operations.md)
- [Testing and safety boundary](docs/testing.md)
- [Implementation and acceptance status](docs/implementation-status.md)

## License

PicPilot-owned code is available under [MIT](LICENSE). Bundled Nova data and both canvas projects retain AGPL-3.0; the combined application is distributed under AGPL-3.0. See [LICENSING.md](LICENSING.md) for scope and third-party notices.
