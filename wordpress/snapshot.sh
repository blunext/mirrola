#!/usr/bin/env bash
# Backs up the local WordPress (files + database) with restic. It only reads
# the WordPress side; restoring is a separate step.
#
#   ./snapshot.sh init                # create the repository (once)
#   ./snapshot.sh backup ["message"]  # dump the DB, snapshot public_html + dump, apply retention
#   ./snapshot.sh list                # list snapshots
#   ./snapshot.sh check [--read-data] # verify the repository (--read-data reads every file)
#
# The repository and its password come only from wordpress/.env
# (RESTIC_REPOSITORY, RESTIC_PASSWORD). Every RESTIC_* / AWS_* variable of the
# calling shell is dropped first, so other restic setups on this Mac are never
# touched, and the script refuses any repository other than the blog's.
# COMPOSE_* is dropped too, so docker compose always uses this directory's
# docker-compose.yml and .env. The message can also come from SNAPSHOT_MSG
# (that is how make passes it, so quotes and $ in it are safe).
set -euo pipefail
cd "$(dirname "$0")"

# The blog's repositories: the local one now, the R2 bucket olamundo-wp later.
LOCAL_REPO="$HOME/priv/olamundo-wp-restic"
HOST=olamundo-wp
DUMP=db/olamundo.sql
EXCLUDES=(cache wflogs upgrade upgrade-temp-backup)

die() { echo "BŁĄD: $*" >&2; exit 1; }

for var in $(compgen -e); do
  case $var in RESTIC_* | AWS_* | COMPOSE_*) unset "$var" ;; esac
done

[ -f .env ] || die "brak wordpress/.env"
# Read line by line instead of sourcing: .env also holds salts with shell
# special characters. Exported variables don't show up in ps arguments.
while IFS='=' read -r key value; do
  export "$key=${value%$'\r'}"
done < <(grep -E '^RESTIC_(REPOSITORY|PASSWORD)=' .env)
[ -n "${RESTIC_PASSWORD:-}" ] || die "brak RESTIC_PASSWORD w wordpress/.env"
case ${RESTIC_REPOSITORY:-} in
  "$LOCAL_REPO" | s3:*/olamundo-wp) ;;
  *) die "RESTIC_REPOSITORY w wordpress/.env to nie repozytorium bloga ($LOCAL_REPO albo s3:…/olamundo-wp)" ;;
esac

backup() {
  local msg=$1 running
  # Fails fast on a wrong password or a missing repository, before the dump.
  restic cat config >/dev/null || die "nie mogę otworzyć repozytorium $RESTIC_REPOSITORY (hasło? ./snapshot.sh init?)"
  # Captured first: with pipefail, "ps | grep -q" can fail when grep exits early.
  running=$(docker compose ps --status running --services 2>/dev/null) || die "docker compose nie działa"
  grep -qx db <<<"$running" || die "baza nie działa (cd wordpress && docker compose up -d)"

  echo "== Dump bazy -> wordpress/$DUMP"
  mkdir -p db
  # The dump is plain text with the whole database: it lives on disk only for
  # the backup (a failed one leaves it for a retry) and is removed afterwards.
  # --no-deps: db is already running, run must not touch it.
  docker compose run --rm --no-deps -T wpcli wp db export - \
    --single-transaction --skip-extended-insert --skip-dump-date </dev/null >db/partial.sql
  # A dump starts with a "-- " or "/*" comment; anything else (a PHP notice on
  # stdout) would end up in the backup.
  [ -s db/partial.sql ] || die "dump bazy jest pusty (db/partial.sql)"
  head -1 db/partial.sql | grep -qE '^(-- |/\*)' || die "dump bazy nie zaczyna się od nagłówka (db/partial.sql)"
  tail -1 db/partial.sql | grep -q '^-- Dump completed' || die "dump bazy jest niepełny (db/partial.sql)"
  mv db/partial.sql "$DUMP"
  ls -lh "$DUMP"

  echo "== Snapshot ($RESTIC_REPOSITORY)"
  local args=(--host "$HOST" --exclude .DS_Store)
  for dir in "${EXCLUDES[@]}"; do
    args+=(--exclude "$PWD/public_html/wp-content/$dir")
  done
  # restic has no snapshot description; the message becomes a tag (commas split tags).
  [ -z "$msg" ] || args+=(--tag "${msg//,/;}")
  restic backup "${args[@]}" public_html "$DUMP"

  echo "== Retencja"
  # --group-by host: the snapshot records absolute source paths (the tree
  # itself is relative: /public_html, /db), so after moving this repo the new
  # snapshots would otherwise start a separate retention series.
  restic forget --host "$HOST" --group-by host --prune --keep-last 10 --keep-weekly 8 --keep-monthly 12
  rm -f "$DUMP"
}

case ${1:-} in
  init) restic init ;;
  backup) backup "${2:-${SNAPSHOT_MSG:-}}" ;;
  list) restic snapshots --host "$HOST" ;;
  check)
    case ${2:-} in "" | --read-data) restic check ${2:+"$2"} ;; *) die "check przyjmuje tylko --read-data" ;; esac ;;
  *) die "użycie: $0 init | backup [\"opis\"] | list | check [--read-data]" ;;
esac
