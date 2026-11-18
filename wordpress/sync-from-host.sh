#!/usr/bin/env bash
# Replaces the local WordPress (files + database) with a fresh copy from the
# host and points it at http://localhost:8000. Steps 1-4 of README.md.
# Interactive: asks for confirmation and for the database password.
#
#   ./sync-from-host.sh                  # export on the host, download, import
#   ./sync-from-host.sh --skip-download  # reuse wordpress.tar.gz and wordpress_db.sql
set -euo pipefail
cd "$(dirname "$0")"

HOST=om
# shellcheck disable=SC2088  # ~ expands on the host, not here
REMOTE_DIR='~/domains/olamundo.pl'
DB=olamundo_wp1
LOCAL=http://localhost:8000

die() { echo "BŁĄD: $*" >&2; exit 1; }
step() { echo; echo "== $*"; }

skip_download=false
case "${1:-}" in
  "") ;;
  --skip-download) skip_download=true ;;
  *) die "nieznana opcja: $1" ;;
esac

[ -f .env ] || die "brak wordpress/.env"
docker info >/dev/null 2>&1 || die "Docker nie działa"
git diff --quiet -- public_html || die "niezacommitowane zmiany w public_html/wp-config.php lub .htaccess"

echo "Skrypt zastąpi lokalną bazę i pliki public_html kopią z serwera."
echo "Wszystko, co dodano tylko lokalnie (wpisy, obrazki), przepadnie."
read -r -p "Wpisz 'tak', aby kontynuować: " answer || die "przerwano"
[ "$answer" = tak ] || die "przerwano"

if ! $skip_download; then
  step "1. Eksport na serwerze ($HOST)"
  # wflogs: Wordfence rewrites it constantly ("file changed as we read it").
  # shellcheck disable=SC2029  # $REMOTE_DIR is meant to be filled in here
  ssh "$HOST" "tar -czf wordpress.tar.gz -C $REMOTE_DIR --exclude='./public_html/wp-content/cache' --exclude='./public_html/wp-content/wflogs' ./public_html"
  echo "Hasło do bazy $DB:"
  ssh -t "$HOST" "mysqldump --single-transaction --default-character-set=utf8mb4 --no-tablespaces -u $DB -p $DB > wordpress_db.sql"
  scp "$HOST:wordpress.tar.gz" "$HOST:wordpress_db.sql" .
fi

step "Sprawdzenie pobranych plików"
[ -f wordpress.tar.gz ] && [ -f wordpress_db.sql ] || die "brak wordpress.tar.gz lub wordpress_db.sql"
tail -1 wordpress_db.sql | grep -q '^-- Dump completed' || die "wordpress_db.sql jest niepełny"
tar -tzf wordpress.tar.gz ./public_html/wp-includes/version.php >/dev/null 2>&1 \
  || die "wordpress.tar.gz jest uszkodzony albo nie zawiera ./public_html/... (symlink zamiast katalogu?)"
ls -lh wordpress.tar.gz wordpress_db.sql

step "2. Rozpakowanie"
# Stopped so it doesn't write files or run cron while files and DB are replaced.
docker compose stop wordpress
# Removes only ignored files under public_html (the previous copy); the
# tracked wp-config.php and .htaccess stay.
git clean -fdXq public_html
tar -xzf wordpress.tar.gz --exclude='./public_html/wp-config.php' --exclude='./public_html/.htaccess'
git diff --quiet -- public_html || die "wp-config.php lub .htaccess różni się od wersji w gicie"
grep "^\$wp_version" public_html/wp-includes/version.php || true

step "3. Import bazy"
docker compose up -d db
# A real query over TCP: on a fresh volume the healthcheck can pass while
# MariaDB's temporary init server (socket only) is still running.
ready=false
for _ in $(seq 90); do
  if docker exec db sh -c 'mysql -h127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" -e "SELECT 1"' >/dev/null 2>&1; then
    ready=true; break
  fi
  sleep 2
done
$ready || die "baza nie odpowiada (docker compose logs db)"
docker exec db sh -c 'exec mysql -h127.0.0.1 -uroot -p"$MYSQL_ROOT_PASSWORD" -e "DROP DATABASE IF EXISTS \`$MYSQL_DATABASE\`; CREATE DATABASE \`$MYSQL_DATABASE\` CHARACTER SET utf8mb4"'
docker exec -i db sh -c 'exec mysql -h127.0.0.1 -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"' < wordpress_db.sql

step "4. Adresy na localhost"
wp() { docker compose run --rm -T wpcli wp "$@" </dev/null; }
for from in https://www.olamundo.pl http://www.olamundo.pl https://olamundo.pl http://olamundo.pl; do
  wp search-replace "$from" "$LOCAL" --all-tables-with-prefix --skip-columns=guid --report-changed-only
done
wp search-replace 'https:\/\/olamundo.pl' 'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid --report-changed-only
wp search-replace 'http:\/\/olamundo.pl' 'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid --report-changed-only
wp search-replace /home/olamundo/domains/olamundo.pl/public_html /var/www/html --all-tables-with-prefix --report-changed-only
[ "$(wp option get siteurl | tr -d '\r')" = "$LOCAL" ] || die "siteurl nie wskazuje na $LOCAL"
[ "$(wp option get home | tr -d '\r')" = "$LOCAL" ] || die "home nie wskazuje na $LOCAL"
wp cache flush

step "Start WordPressa"
docker compose up -d

step "Gotowe"
echo "Sprawdź $LOCAL, potem w katalogu repo: make crawl URL=$LOCAL"
