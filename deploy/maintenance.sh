#!/bin/sh
set -eu
umask 077
export PGCONNECT_TIMEOUT=10
fail() { printf '%s\n' "$1" >&2; exit 1; }
edition=${PICPILOT_EDITION:?Missing build edition}
case "$edition" in platform|personal) ;; *) fail 'Invalid edition.';; esac

verify() {
    id=${1:-}
    case "$id" in ''|*[!a-zA-Z0-9_-]*) fail 'Use a backup directory name, not a path.';; esac
    source=/backup/$id
    [ -d "$source" ] && [ ! -L "$source" ] || fail 'Backup not found.'
    for name in database.dump application.tar.gz settings.env manifest.txt SHA256SUMS; do
        [ -f "$source/$name" ] && [ ! -L "$source/$name" ] || fail 'Backup is incomplete.'
    done
    [ "$(wc -l < "$source/SHA256SUMS")" -eq 4 ] || fail 'Invalid checksum manifest.'
    for name in database.dump application.tar.gz settings.env manifest.txt; do
        grep -Eq "^[0-9a-f]{64}  $name$" "$source/SHA256SUMS" || fail 'Invalid checksum manifest.'
    done
    (cd "$source" && sha256sum --check --status SHA256SUMS) || fail 'Backup checksum mismatch.'
    grep -qx "edition=$edition" "$source/manifest.txt" || fail 'Backup belongs to a different edition.'
    grep -qx 'format=picpilot-go-backup-v1' "$source/manifest.txt" || fail 'Unknown backup format.'
    pg_restore --list "$source/database.dump" >/dev/null || fail 'Invalid database archive.'
    tar -tzf "$source/application.tar.gz" | awk '
        /^\// || /(^|\/)\.\.(\/|$)/ { bad=1 }
        END { exit bad }
    ' || fail 'Unsafe application archive path.'
    tar -tvzf "$source/application.tar.gz" | awk 'substr($0,1,1)!="-" && substr($0,1,1)!="d" { bad=1 } END { exit bad }' || fail 'Application archive must not contain links or devices.'
}

case ${1:-help} in
backup)
    [ "${PICPILOT_BACKUP_QUIESCED:-}" = yes ] || fail 'Use scripts/backup.ps1 or scripts/backup.sh to stop application writers first.'
    actual=$(psql -Atqc 'SELECT edition FROM instance_state WHERE singleton=true')
    [ "$actual" = "$edition" ] || fail 'Database edition mismatch.'
    [ -f /source/instance-secrets.json ] || fail 'Instance secrets are missing; refusing an incomplete backup.'
    id="$(date -u +%Y%m%dT%H%M%SZ)-$edition"
    output=/backup/$id
    mkdir -m 700 "$output"
    pg_dump --format=custom --no-owner --no-acl --file="$output/database.dump"
    tar --numeric-owner -C /source -czf "$output/application.tar.gz" .
    cp /settings.env "$output/settings.env"
    version=$(psql -Atqc 'SELECT max(version) FROM schema_migrations')
    printf 'format=picpilot-go-backup-v1\nedition=%s\nschema=%s\ncreated_at=%s\n' "$edition" "$version" "$(date -u +%FT%TZ)" > "$output/manifest.txt"
    (cd "$output" && sha256sum database.dump application.tar.gz settings.env manifest.txt > SHA256SUMS)
    verify "$id"
    chown -R --reference=/backup "$output"
    printf 'Verified backup: backups/%s\nKeep it private; it includes credentials and user files.\n' "$id"
    ;;
verify)
    verify "${2:-}"
    printf '%s\n' 'Backup checksums, edition, database archive and application archive verified.'
    ;;
restore)
    [ "${PICPILOT_RESTORE_CONFIRM:-}" = yes ] || fail 'Set PICPILOT_RESTORE_CONFIRM=yes only for an empty destination.'
    verify "${2:-}"
    tables=$(psql -Atqc "SELECT count(*) FROM information_schema.tables WHERE table_schema NOT IN ('pg_catalog','information_schema')")
    [ "$tables" = 0 ] || fail 'Destination database is not empty; nothing was overwritten.'
    [ -z "$(find /source -mindepth 1 -maxdepth 1 -print -quit)" ] || fail 'Destination application volume is not empty; nothing was overwritten.'
    pg_restore --single-transaction --exit-on-error --no-owner --no-acl --dbname="$PGDATABASE" "$source/database.dump"
    tar --no-same-owner --no-overwrite-dir -C /source -xzf "$source/application.tar.gz"
    chown -R 65532:65532 /source
    chmod 700 /source
    chmod 600 /source/instance-secrets.json
    printf '%s\n' 'Database, files and instance keys restored. Start the matching application release and verify before opening public traffic.'
    ;;
*)
    printf '%s\n' 'Commands: backup | verify BACKUP_NAME | restore BACKUP_NAME' 'Use the documented stop/backup/restore procedure; never run against a live writing application.'
    ;;
esac
