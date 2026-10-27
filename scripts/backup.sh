#!/bin/sh
set -eu
umask 077
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
mkdir -p backups
running=$(docker compose ps --services --status running)
writers=$(printf '%s\n' "$running" | grep -E '^(gateway|user|admin|api)$' || true)
docker compose --profile maintenance build maintenance
restart() { if [ -n "$writers" ]; then docker compose start $writers || echo 'Application restart failed; inspect docker compose ps.' >&2; fi; }
trap restart 0
if [ -n "$writers" ]; then docker compose stop $writers; fi
docker compose --profile maintenance run --rm -e PICPILOT_BACKUP_QUIESCED=yes maintenance backup
