#!/usr/bin/env bash
# Runs inside the tools container (wordpress/tools/Dockerfile), started by make:
#   docker compose run --rm tools <command>
#
#   init                 create the restic repository (once)
#   start                put the latest snapshot into the wp_files volume and the DB
#                        (service tools-restore, WordPress stopped; run by make start)
#   snapshot ["message"] dump the DB, snapshot the WordPress files + dump to R2, apply retention
#   snapshots            list snapshots
#   check [--read-data]  verify the repository (--read-data reads every pack)
#
# The container gets wordpress/.env through compose's env_file (variables of
# the calling shell can't override it), so RESTIC_REPOSITORY, RESTIC_PASSWORD
# and the R2 token come only from there. The WordPress files (the wp_files
# volume) are at /data/public_html, read-only except in tools-restore;
# /data/db is a tmpfs, so the plain-text dump never reaches the disk.
set -euo pipefail

HOST=olamundo-wp
DATA=/data
# The address the local WordPress runs at; a snapshot made elsewhere (another
# address in siteurl) is refused until address rewriting exists.
LOCAL_URL=http://localhost:8000
DUMP=db/olamundo.sql
# Written by start after a successful restore. snapshot refuses without it: a
# volume the WordPress image filled with a stock core (after a plain
# "docker compose up" on an empty volume) must never become the latest snapshot.
MARKER=public_html/.olamundo-restored
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

# A dump starts with a "-- " or "/*" comment and ends with "-- Dump completed";
# anything else (a PHP notice on stdout, a cut-off download) is refused.
check_dump() {
  [ -s "$1" ] || die "dump bazy jest pusty"
  head -1 "$1" | grep -qE '^(-- |/\*)' || die "dump bazy nie zaczyna się od nagłówka"
  tail -1 "$1" | grep -q '^-- Dump completed' || die "dump bazy jest niepełny"
}

start() {
  restic cat config >/dev/null || die "nie mogę otworzyć repozytorium $RESTIC_REPOSITORY"
  [ -w "$DATA/public_html" ] || die "pliki WordPressa są tylko do odczytu (start działa w usłudze tools-restore)"
  local id
  # The newest snapshot of this host, whatever its source paths (they changed
  # once, when backups moved into this container).
  id=$(restic snapshots --host "$HOST" --json | php -r '
    $s = json_decode(stream_get_contents(STDIN), true) ?: [];
    // Times carry different UTC offsets; compare them as timestamps.
    $t = fn($x) => strtotime(preg_replace("/\\.\\d+/", "", $x["time"]));
    usort($s, fn($a, $b) => $t($a) <=> $t($b));
    echo $s ? end($s)["id"] : "";')
  [ -n "$id" ] || die "brak snapshotów w $RESTIC_REPOSITORY"
  echo "== Snapshot ${id:0:8}"

  # The DB first, into the tmpfs: checked before anything is replaced.
  cd "$DATA"
  restic dump "$id" /db/olamundo.sql >"$DUMP"
  check_dump "$DUMP"
  # One row per INSERT (--skip-extended-insert), so siteurl is a line of its own.
  local siteurl
  siteurl=$({ grep -oE "^INSERT INTO \`[a-z0-9_]*options\` VALUES \([0-9]+,'siteurl','[^']*'" "$DUMP" || true; } | sed -E "s/.*,'siteurl','//; s/'$//")
  [ -n "$siteurl" ] || die "w dumpie snapshotu nie ma siteurl; nic nie zmieniono"
  [ "$siteurl" = "$LOCAL_URL" ] \
    || die "snapshot ma adres '$siteurl', a nie $LOCAL_URL (zamiana adresów jeszcze nie istnieje); nic nie zmieniono"

  # --delete would empty the volume if the snapshot had no WordPress files.
  # Captured first: with pipefail, "restic ls | grep -q" can fail when grep exits early.
  local listing
  listing=$(restic ls "$id" /public_html/wp-includes/version.php)
  grep -q version.php <<<"$listing" \
    || die "snapshot ${id:0:8} nie zawiera plików WordPressa; nic nie zmieniono"

  echo "== Pliki (tylko różnice; pliki spoza snapshotu są usuwane)"
  rm -f "$MARKER"
  restic restore "$id:/public_html" --target "$DATA/public_html" --overwrite if-changed --delete
  # wp-config.php and .htaccess always come from git (wordpress/config).
  cp /config/wp-config.php /config/.htaccess "$DATA/public_html/"

  echo "== Baza"
  wp db reset --yes
  wp db import "$DUMP"
  wp core is-installed || die "po imporcie WordPress nie jest zainstalowany"
  echo "${id}" >"$MARKER"
  echo "== Gotowe: snapshot ${id:0:8}"
}

snapshot() {
  local msg=$1
  # Fails fast on a wrong password or a missing repository, before the dump.
  restic cat config >/dev/null || die "nie mogę otworzyć repozytorium $RESTIC_REPOSITORY (hasło? make snapshot-init?)"
  cd "$DATA"

  # An empty volume (before the first make start) or an empty DB must never
  # become the latest snapshot: the next start would restore nothing.
  [ -f public_html/wp-includes/version.php ] || die "brak plików WordPressa (najpierw make start)"
  [ -f "$MARKER" ] || die "pliki WordPressa nie pochodzą z make start (np. po samym docker compose up); najpierw make start"
  wp core is-installed || die "baza WordPressa jest pusta (najpierw make start)"

  echo "== Dump bazy"
  wp db export - --single-transaction --skip-extended-insert --skip-dump-date >"$DUMP"
  check_dump "$DUMP"
  ls -lh "$DUMP"

  echo "== Snapshot ($RESTIC_REPOSITORY)"
  local args=(--host "$HOST" --exclude .DS_Store --exclude "$DATA/$MARKER")
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
  start) start ;;
  snapshot) snapshot "${2:-${SNAPSHOT_MSG:-}}" ;;
  snapshots) restic snapshots --host "$HOST" ;;
  check)
    case ${2:-} in "" | --read-data) restic check ${2:+"$2"} ;; *) die "check przyjmuje tylko --read-data" ;; esac ;;
  *) die "użycie: olamundo init | start | snapshot [\"opis\"] | snapshots | check [--read-data]" ;;
esac
