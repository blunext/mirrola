#!/usr/bin/env bash
# Runs inside the tools container (wordpress/tools/Dockerfile), started by make:
#   docker compose run --rm tools <command>
#
#   init                 create the restic repository (once)
#   snapshot ["message"] dump the DB, snapshot the WordPress files + dump to R2, apply retention
#   snapshots            list snapshots
#   check [--read-data]  verify the repository (--read-data reads every pack)
#
# The container gets wordpress/.env through compose's env_file (variables of
# the calling shell can't override it), so RESTIC_REPOSITORY, RESTIC_PASSWORD
# and the R2 token come only from there. The WordPress files are mounted
# read-only at /data/public_html; /data/db is a tmpfs, so the plain-text dump
# never reaches the disk.
set -euo pipefail

HOST=olamundo-wp
DATA=/data
DUMP=db/olamundo.sql
EXCLUDES=(cache wflogs upgrade upgrade-temp-backup)
# The blog's repository: the R2 bucket "olamundo"; the account id is 32 hex characters.
REPO_REGEX='^s3:https://[0-9a-f]{32}\.r2\.cloudflarestorage\.com/olamundo$'

die() { echo "BŁĄD: $*" >&2; exit 1; }

for var in RESTIC_PASSWORD AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY; do
  [ -n "${!var:-}" ] || die "brak $var w wordpress/.env"
done
[[ ${RESTIC_REPOSITORY:-} =~ $REPO_REGEX ]] \
  || die "RESTIC_REPOSITORY w wordpress/.env to nie repozytorium bloga (s3:https://<account-id>.r2.cloudflarestorage.com/olamundo)"

wp() { command wp --path="$DATA/public_html" "$@"; }

snapshot() {
  local msg=$1
  # Fails fast on a wrong password or a missing repository, before the dump.
  restic cat config >/dev/null || die "nie mogę otworzyć repozytorium $RESTIC_REPOSITORY (hasło? make snapshot-init?)"
  cd "$DATA"

  echo "== Dump bazy"
  wp db export - --single-transaction --skip-extended-insert --skip-dump-date >"$DUMP"
  # A dump starts with a "-- " or "/*" comment; anything else (a PHP notice on
  # stdout) would end up in the backup.
  [ -s "$DUMP" ] || die "dump bazy jest pusty"
  head -1 "$DUMP" | grep -qE '^(-- |/\*)' || die "dump bazy nie zaczyna się od nagłówka"
  tail -1 "$DUMP" | grep -q '^-- Dump completed' || die "dump bazy jest niepełny"
  ls -lh "$DUMP"

  echo "== Snapshot ($RESTIC_REPOSITORY)"
  local args=(--host "$HOST" --exclude .DS_Store)
  for dir in "${EXCLUDES[@]}"; do
    args+=(--exclude "$DATA/public_html/wp-content/$dir")
  done
  # restic has no snapshot description; the message becomes a tag (commas split tags).
  [ -z "$msg" ] || args+=(--tag "${msg//,/;}")
  # Relative paths: the snapshot tree is /public_html and /db/olamundo.sql.
  restic backup "${args[@]}" public_html "$DUMP"

  echo "== Retencja"
  # --group-by host: the snapshot records absolute source paths, which changed
  # when backups moved into this container (/data/...).
  restic forget --host "$HOST" --group-by host --prune --keep-last 10 --keep-weekly 8 --keep-monthly 12
}

case ${1:-} in
  init) restic init ;;
  snapshot) snapshot "${2:-${SNAPSHOT_MSG:-}}" ;;
  snapshots) restic snapshots --host "$HOST" ;;
  check)
    case ${2:-} in "" | --read-data) restic check ${2:+"$2"} ;; *) die "check przyjmuje tylko --read-data" ;; esac ;;
  *) die "użycie: olamundo init | snapshot [\"opis\"] | snapshots | check [--read-data]" ;;
esac
