# Operations

These procedures are for the new Go installation, not the old Java production database. Use a repository revision matching the backup. The owner must approve any public deployment or repository publication separately.

## Stop and Start

```sh
docker compose stop
docker compose start
docker compose ps
```

`stop` preserves containers and volumes. `down` removes containers/networks but normally retains named volumes. **Never use `down -v` unless you deliberately want to erase this installation.** Never use global Docker prune commands as a PicPilot maintenance step.

## Backup

Prefer a quiet period with no active model jobs. The script stops this installation's API, UIs and gateway, creates a database dump plus application archive, verifies checksums, and restarts only the previously running application services. It does not stop PostgreSQL or other Docker projects. External/local workers may see a short connection interruption.

PowerShell:

```powershell
./scripts/backup.ps1
```

Linux/macOS:

```sh
sh scripts/backup.sh
```

If you use an explicit Compose project name, preserve it for the wrapper:

```powershell
$env:COMPOSE_PROJECT_NAME = 'your-installation-name'
./scripts/backup.ps1
```

```sh
COMPOSE_PROJECT_NAME=your-installation-name sh scripts/backup.sh
```

The resulting `backups/<UTC timestamp>-<edition>/` contains:

- `database.dump`: PostgreSQL custom-format dump, without ownership/ACL assumptions.
- `application.tar.gz`: files and `instance-secrets.json` from the application volume.
- `settings.env`: your private deployment environment.
- `manifest.txt` and `SHA256SUMS`: edition/schema metadata and integrity checks.

Treat the entire directory as sensitive. Store an encrypted copy off the server. A checksum detects corruption, not malicious replacement; only restore backups from a trusted source. Failed backups may leave incomplete directories and are never valid restore inputs. The tool does not automatically delete any historical backup.

Credentials supplied only by an external secret manager are not captured by copying `.env`; preserve their versions separately. Object-storage buckets, when configured instead of local storage, require their own private object backup and restore procedure. A database dump alone does not preserve images or decryptable credentials.

## Restore Into an Empty Installation

Do not overwrite a running installation. Use a new checkout or directory and a new Compose project name. Restore the same edition. Never restore a platform backup into personal or vice versa.

1. Obtain the exact application revision used for the backup, including matching migrations and frontend sources.
2. Put the trusted backup directory under the new checkout's `backups/` directory. Restore `settings.env` as the new `.env` using your file manager or the commands below. Do not run the application yet; it would initialize the empty data volume.
3. Set a new `COMPOSE_PROJECT_NAME` in `.env`. If the old installation is still present, use distinct ports, network subnet and proxy address. Correct public URLs before public traffic.

PowerShell (replace `BACKUP_NAME` with the directory name, not a path):

```powershell
Copy-Item -LiteralPath backups/BACKUP_NAME/settings.env -Destination .env
docker compose up -d db
docker compose --profile maintenance build maintenance
docker compose --profile maintenance run --rm maintenance verify BACKUP_NAME
docker compose --profile maintenance run --rm -e PICPILOT_RESTORE_CONFIRM=yes maintenance restore BACKUP_NAME
docker compose up -d --build
```

Linux/macOS:

```sh
umask 077
cp backups/BACKUP_NAME/settings.env .env
docker compose up -d db
docker compose --profile maintenance build maintenance
docker compose --profile maintenance run --rm maintenance verify BACKUP_NAME
docker compose --profile maintenance run --rm -e PICPILOT_RESTORE_CONFIRM=yes maintenance restore BACKUP_NAME
docker compose up -d --build
```

Check that `.env` did not already contain a different deployment before copying it. Keep it readable only by the operator. The restore tool requires an empty database and empty application volume and refuses to overwrite either. It validates checksums, edition and archive paths, rejects symlinks/devices, restores the database in one transaction, then restores application files and key permissions.

If a restore fails after database restoration, do not start the incomplete application. Keep the original backup and diagnose the new destination; retry using a new empty installation rather than deleting unknown data. Verify login, a historical image download, enabled provider configuration, credits and task history before reopening traffic. Do not submit paid model tasks as a basic restore check.

## Upgrade and Rollback

1. Stop accepting new work and let current tasks finish when practical.
2. Make and verify a backup with the procedure above. Record `git rev-parse HEAD` and keep that source revision.
3. Check out the reviewed release/commit, review its migration notes, then run `docker compose up -d --build`.
4. Inspect service health and perform the non-paid smoke checks. Database migrations run transactionally under a PostgreSQL advisory lock, and checksums must match.

Do not manually edit applied migration files or delete migration rows to force startup. The server rejects a database with unknown newer migrations. For rollback after a schema upgrade, restore the complete pre-upgrade database, files and keys into an empty installation with the previous source revision; do not run an older binary against a newer schema. Discarding only a container is not a database rollback.

## Owner Recovery

For an owner/super-administrator password reset when email is unavailable, run the explicit local command. It requires access to this deployment's private Docker/host environment. It changes only the named active owner account and revokes all existing sessions.

PowerShell:

```powershell
$secure = Read-Host 'New owner password (12-72 UTF-8 bytes)' -AsSecureString
$credential = New-Object Management.Automation.PSCredential('unused', $secure)
$credential.GetNetworkCredential().Password | docker compose exec -T api /picpilot --reset-owner-password-stdin --account owner@example.com
Remove-Variable credential, secure
```

Bash:

```bash
read -rs -p 'New owner password (12-72 UTF-8 bytes): ' password
printf '\n'
printf '%s\n' "$password" | docker compose exec -T api /picpilot --reset-owner-password-stdin --account owner@example.com
unset password
```

Replace the email with the actual owner. The password travels through stdin, never a command-line argument. Do not use a shell transcript, debug tracing, screenshot or shared terminal recording during recovery. Sign in again afterward.

## Logs and Data Boundaries

```sh
docker compose ps
docker compose logs --tail=100 api
```

Application errors use request/task identifiers and fixed error codes, not authorization values, complete prompts, image base64 or provider secrets. PostgreSQL production logging is configured not to include failing statements or bound parameters. Do not enable query/body tracing on a production instance without a separate privacy review.

Backups, uploads, databases, caches and `.env` are excluded from Git and build contexts. Keep all repository visibility private until the owner explicitly approves changing it.
