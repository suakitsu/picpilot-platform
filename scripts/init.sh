#!/bin/sh
set -eu
umask 077
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$root"
if [ -e .env ]; then
    printf '%s\n' '.env already exists; nothing was changed.'
    exit 0
fi
edition=$(tr -d '\r\n' < deploy/edition.txt)
case "$edition" in personal|platform) ;; *) echo 'Invalid edition.' >&2; exit 1;; esac
password=$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')
[ "${#password}" = 64 ] || { echo 'Secure random generation failed.' >&2; exit 1; }
set -C
sed -e "s/^POSTGRES_PASSWORD=$/POSTGRES_PASSWORD=$password/" \
    -e "s/^COMPOSE_PROJECT_NAME=picpilot-platform$/COMPOSE_PROJECT_NAME=picpilot-$edition/" \
    .env.example > .env
unset password
printf '%s\n' 'Created private .env with a random database password. No model provider is enabled.' 'Next: docker compose up -d --build'
