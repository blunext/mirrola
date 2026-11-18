# Local WordPress (copy of olamundo.pl)

Runs a copy of the production blog in Docker on http://localhost:8000
(phpMyAdmin on http://localhost:8080). Credentials are in `.env` (not in git).

## 1. Export from the host

```bash
ssh om
# Files: everything except caches. "./public_html" (not "./public_html/*") keeps dotfiles.
tar -czf wordpress.tar.gz --exclude='./public_html/wp-content/cache' ./public_html
# Database: utf8mb4 so Polish characters and emoji survive; no table locks on the live site.
grep DB_ public_html/wp-config.php       # DB name, user and password for the dump
mysqldump --single-transaction --default-character-set=utf8mb4 --no-tablespaces \
  -u olamundo_wp1 -p olamundo_wp1 > wordpress_db.sql
exit
```

```bash
cd wordpress
scp om:wordpress.tar.gz om:wordpress_db.sql .
```

## 2. Unpack the files

`public_html/wp-config.php` and `public_html/.htaccess` are the local, Docker-ready
versions tracked in git. The tarball has the production ones, so skip them:

```bash
tar -xzf wordpress.tar.gz --exclude='./public_html/wp-config.php' --exclude='./public_html/.htaccess'
git status public_html   # wp-config.php and .htaccess must not show up as modified
```

## 3. Import the database

Import the **original** dump, then rewrite URLs inside the database with WP-CLI
(step 4). Don't rewrite the `.sql` file with `sed` or `go run ./replace`: WordPress
keeps PHP-serialized strings (`s:19:"...";`) whose byte length must match the text.
`sed` breaks the length of every string it changes, and `replace/` miscounts strings
containing backslashes. On the 2024-12-30 dump it left 1239 serialized values that
PHP can't read. `wp search-replace` unserializes, replaces and serializes again
(same test: 0 unreadable values).

```bash
docker compose up -d db
# Waits for the healthcheck, then loads the dump; passwords stay inside the container.
until [ "$(docker inspect -f '{{.State.Health.Status}}' db)" = healthy ]; do sleep 2; done
docker exec -i db sh -c 'exec mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"' < wordpress_db.sql

docker compose up -d
```

To start over from a fresh dump: `docker compose down -v` (deletes the database volume), then repeat step 3.

## 4. Point the site at localhost

```bash
wp() { docker compose run --rm wpcli "$@"; }
for from in https://www.olamundo.pl http://www.olamundo.pl https://olamundo.pl http://olamundo.pl; do
  wp search-replace "$from" http://localhost:8000 --all-tables-with-prefix --skip-columns=guid
done
# JSON-escaped URLs (block attributes, plugin settings)
wp search-replace 'https:\/\/olamundo.pl' 'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid
wp search-replace 'http:\/\/olamundo.pl'  'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid
# Filesystem path of the host
wp search-replace /home/olamundo/domains/olamundo.pl/public_html /var/www/html --all-tables-with-prefix
wp option get siteurl && wp option get home   # both must be http://localhost:8000
wp cache flush
```

Add `--dry-run` to any `search-replace` to see the counts first. A warning
"Skipping an uninitialized class" means a plugin's object was left unchanged
(not corrupted).

## 5. Crawl it

From the repo root: `make crawl URL=http://localhost:8000`, then `make serve`.
If `siteurl`/`home` still point to production, the crawl stops with an off-site
redirect error.
