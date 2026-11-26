#!/usr/bin/env bash
# Runs inside the tools container (wordpress/tools/Dockerfile), started by make:
#   docker compose run --rm tools <command>
#
#   init                 create the restic repository (once)
#   start                put the latest snapshot into the wp_files volume and the DB
#                        (service tools-restore, WordPress stopped; run by make start)
#   snapshot ["message"] dump the DB, snapshot the WordPress files + dump to R2, apply retention
#   snapshots            list snapshots
#   repo-check [--read-data]  verify the restic repository (--read-data reads every pack)
#   crawl                crawl the local WordPress into the site repo (/site/olamundo)
#   serve                preview the site repo on :8081
#   publish              commit the crawl, push the preview branch
#   release              fast-forward the production branch to the pushed preview
#   site-check           crawl into a throwaway dir, compare with the site repo at $BASE (default origin/development)
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

# The site repo (Cloudflare Pages): preview and production branches.
SITE=/site/olamundo
SITE_REMOTE=git@github.com:blunext/olamundo.git
SITE_URL=https://olamundo.pl
PREVIEW=development
PRODUCTION=main
DEPLOY_KEY=/run/deploy/olamundo_deploy_key
# Written by a complete crawl, removed when it starts and after a push:
# publish refuses without it, so an emptied or half-written tree (crawl failed
# or was interrupted after the cleanup) is never published.
CRAWL_OK=$SITE/.git/olamundo-crawl-ok
# Kept by the crawl's cleanup: Pages config files (and top-level dotfiles).
PAGES_FILES=(_headers _redirects _routes.json _worker.js functions)

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
  # --ignore-ctime: make start restores every file's metadata (mode, times),
  # which bumps its ctime even when the file is unchanged; mtime and size are
  # restored exactly, so they are enough to skip unchanged files.
  local args=(--host "$HOST" --ignore-ctime --exclude .DS_Store --exclude "$DATA/$MARKER")
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

# git over SSH with this computer's deploy key, GitHub's pinned host key, and
# a commit identity naming the computer (PUBLISHER in .env), no real e-mail.
git_setup() {
  # Runs as uid 33: on Linux (or WSL2's own file system) a key only its owner
  # can read is invisible here; Docker Desktop's file sharing doesn't check.
  [ -r "$DEPLOY_KEY" ] || die "nie mogę odczytać klucza wordpress/deploy/olamundo_deploy_key (brak pliku albo uprawnień: kontener czyta go jako uid 33)"
  [ -n "${PUBLISHER:-}" ] || die "brak PUBLISHER w wordpress/.env (nazwa komputera, np. mac)"
  # ssh wants a private key only its user can read.
  install -m 600 "$DEPLOY_KEY" /tmp/deploy_key
  export GIT_SSH_COMMAND="ssh -i /tmp/deploy_key -o IdentitiesOnly=yes -o UserKnownHostsFile=/etc/olamundo/known_hosts -o StrictHostKeyChecking=yes"
  export GIT_AUTHOR_NAME="olamundo publish ($PUBLISHER)" GIT_AUTHOR_EMAIL=noreply@olamundo.pl
  export GIT_COMMITTER_NAME="$GIT_AUTHOR_NAME" GIT_COMMITTER_EMAIL="$GIT_AUTHOR_EMAIL"
  # The volume is owned by uid 33, which has no passwd entry.
  export GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=safe.directory GIT_CONFIG_VALUE_0='*'
}

# Clones the site repo into the volume on first use: shallow (the crawl
# regenerates everything, history isn't needed), both branches.
site_clone() {
  [ -d "$SITE/.git" ] && return
  echo "== Klon $SITE_REMOTE (płytki)"
  git clone -q --depth 1 --no-single-branch --branch "$PREVIEW" "$SITE_REMOTE" "$SITE"
}

site_guard() {
  [ -f "$SITE/.mirrola-site" ] || die "$SITE nie wygląda na repo strony (brak .mirrola-site)"
}

# WordPress answers only to its own address (siteurl http://localhost:8000):
# forward localhost:8000 in this container to the wordpress service. A TCP
# forward keeps the Host header as the crawler sends it.
wp_proxy() {
  getent hosts wordpress >/dev/null || die "kontener wordpress nie działa (make start)"
  socat TCP-LISTEN:8000,bind=127.0.0.1,fork,reuseaddr TCP:wordpress:8000 2>/dev/null &
  local i
  for i in $(seq 30); do
    curl -fsS -o /dev/null "$LOCAL_URL/" 2>/dev/null && return
    sleep 1
  done
  die "lokalny WordPress nie odpowiada (make start)"
}

crawl() {
  git_setup
  site_clone
  site_guard
  echo "== Repo strony = origin/$PREVIEW"
  git -C "$SITE" fetch -q --depth 1 origin
  git -C "$SITE" checkout -q "$PREVIEW"
  # The reset also drops a commit whose push failed: it is only a crawl
  # result, regenerated right below (only its MSG is lost). Refusing instead
  # would leave the repo stuck once origin moved (another computer, a file
  # edited on GitHub): a shallow fetch can't tell such a commit from a stale one.
  rm -f "$CRAWL_OK"
  git -C "$SITE" reset -q --hard "origin/$PREVIEW"
  # Emptied first, so pages removed from WordPress disappear from the mirror
  # too; top-level dotfiles (.git, .gitignore, the marker) and Pages config stay.
  local keep=()
  for f in "${PAGES_FILES[@]}"; do keep+=(! -name "$f"); done
  find "$SITE" -mindepth 1 -maxdepth 1 ! -name '.*' "${keep[@]}" -exec rm -rf {} +
  wp_proxy
  echo "== Crawl $LOCAL_URL"
  mirrola -url "$LOCAL_URL" -site-url "$SITE_URL" -dir "$SITE" \
    -delay "${DELAY:-0}" -concurrency "${CONCURRENCY:-10}" $([ "${STRICT:-}" = 1 ] && echo -strict)
  git -C "$SITE" add -A
  touch "$CRAWL_OK"
  echo "== Zmiany względem origin/$PREVIEW:"
  git -C "$SITE" --no-pager diff --cached --shortstat
  echo "Dalej: make serve (podgląd na tym komputerze), potem make publish"
}

serve() {
  site_guard
  mirrola-server -dir "$SITE" -port 8081
}

publish() {
  git_setup
  site_guard
  cd "$SITE"
  local branch
  branch=$(git rev-parse --abbrev-ref HEAD)
  [ "$branch" = "$PREVIEW" ] || die "repo strony jest na '$branch', nie na $PREVIEW"
  [ -f "$CRAWL_OK" ] || die "brak udanego crawla od ostatniej publikacji (przerwany albo nieudany?); najpierw make crawl"
  git add -A
  if git diff --cached --quiet; then
    echo "Nic nowego do commita."
  else
    git --no-pager diff --cached --shortstat
    git commit -q -m "${PUBLISH_MSG:-Update site $(date '+%Y-%m-%d %H:%M')}"
  fi
  if [ "$(git rev-list --count "origin/$PREVIEW..HEAD")" = 0 ]; then
    echo "Nic do wypchnięcia: $PREVIEW = origin/$PREVIEW."
    return
  fi
  git push -q origin "HEAD:refs/heads/$PREVIEW" \
    || die "push odrzucony (origin/$PREVIEW się zmienił, np. publikacja z innego komputera?); zrób jeszcze raz make crawl, potem make publish"
  rm -f "$CRAWL_OK"
  echo "Wypchnięto $(git rev-parse --short HEAD) na $PREVIEW; Pages buduje podgląd https://development.olamundo-3xg.pages.dev"
  echo "Sprawdź podgląd, potem: make release"
}

release() {
  git_setup
  site_guard
  cd "$SITE"
  git fetch -q --depth 1 origin
  # What goes live is origin/development, i.e. exactly what the preview shows;
  # a local crawl that wasn't published isn't part of it.
  [ "$(git rev-parse HEAD)" = "$(git rev-parse "origin/$PREVIEW")" ] \
    || echo "Uwaga: lokalne repo strony różni się od origin/$PREVIEW (nieopublikowany crawl?); wypuszczam origin/$PREVIEW, czyli to, co pokazuje podgląd."
  if [ "$(git rev-parse "origin/$PRODUCTION")" = "$(git rev-parse "origin/$PREVIEW")" ]; then
    echo "Nic do wypuszczenia: $PRODUCTION = $PREVIEW."
    return
  fi
  # git checks the fast-forward locally, and a shallow clone has no history to
  # see that main is an ancestor of development: deepen until it does, or
  # until the whole history is there (then main has diverged: refuse).
  local i
  for i in $(seq 30); do
    git merge-base --is-ancestor "origin/$PRODUCTION" "origin/$PREVIEW" && break
    [ "$(git rev-parse --is-shallow-repository)" = true ] \
      || die "$PRODUCTION nie jest przodkiem $PREVIEW (rozjechane gałęzie); nic nie wypuszczono"
    # No refspec: deepens both branches, so a diverged main ends the loop
    # (repository no longer shallow) instead of deepening one side forever.
    git fetch -q --deepen=20 origin
  done
  git merge-base --is-ancestor "origin/$PRODUCTION" "origin/$PREVIEW" \
    || die "nie udało się potwierdzić, że $PRODUCTION jest przodkiem $PREVIEW; nic nie wypuszczono"
  echo "Wypuszczam na $PRODUCTION (olamundo.pl):"
  git --no-pager log --oneline "origin/$PRODUCTION..origin/$PREVIEW"
  # No --force: GitHub refuses anything but a fast-forward.
  git push -q origin "refs/remotes/origin/$PREVIEW:refs/heads/$PRODUCTION"
  git fetch -q --depth 1 origin
  echo "Wypuszczono $(git rev-parse --short "origin/$PRODUCTION"); Pages buduje olamundo.pl."
}

# Compares a fresh crawl with the site repo at $BASE (default
# origin/development, fetched first: what the preview shows; the clone is
# shallow, so older commits aren't there). Exit status 1: look at the [FAIL]
# sections before publishing.
site_check() {
  git_setup
  site_clone
  site_guard
  git -C "$SITE" fetch -q --depth 1 origin
  local base=${BASE:-origin/$PREVIEW}
  rm -f /report/report.txt /report/crawl.log   # never leave a previous run's result behind
  mkdir -p /tmp/baseline /tmp/new
  git -C "$SITE" archive "$base" | tar -x -C /tmp/baseline
  echo "baseline: $(git -C "$SITE" log -1 --format='%h %ad %s' --date=short "$base")"
  wp_proxy
  echo "crawling $LOCAL_URL (log: .check/crawl.log) ..."
  if ! mirrola -url "$LOCAL_URL" -site-url "$SITE_URL" -dir /tmp/new \
      -delay "${DELAY:-0}" -concurrency "${CONCURRENCY:-10}" >/report/crawl.log 2>&1; then
    echo "CRAWL FAILED:"; tail -n 20 /report/crawl.log; exit 2
  fi
  grep -E '^\[(WARN|ERROR)\]' /report/crawl.log | head -n 20 || true
  echo
  local status=0
  sitecheck -old /tmp/baseline -new /tmp/new -forbid localhost:8000 >/report/report.txt || status=$?
  cat /report/report.txt
  exit "$status"
}

case ${1:-} in
  init) restic init ;;
  start) start ;;
  snapshot) snapshot "${2:-${SNAPSHOT_MSG:-}}" ;;
  snapshots) restic snapshots --host "$HOST" ;;
  repo-check)
    case ${2:-} in "" | --read-data) restic check ${2:+"$2"} ;; *) die "repo-check przyjmuje tylko --read-data" ;; esac ;;
  crawl) crawl ;;
  serve) serve ;;
  publish) publish ;;
  release) release ;;
  site-check) site_check ;;
  *) die "użycie: olamundo init | start | snapshot [\"opis\"] | snapshots | repo-check [--read-data] | crawl | serve | publish | release | site-check" ;;
esac
