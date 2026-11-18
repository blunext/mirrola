# Lokalny WordPress (kopia olamundo.pl)

Strona: http://localhost:8000, phpMyAdmin: http://localhost:8080. Hasła w `.env`.

Kroki 1–4 robi automatycznie `make sync` w katalogu repo (`./sync-from-host.sh`).
`make sync-skip` (`--skip-download`) używa już pobranych plików.
Zastępuje lokalną bazę i pliki kopią z serwera.

## 1. Eksport z serwera

```bash
ssh om
grep DB_ ~/domains/olamundo.pl/public_html/wp-config.php
mysqldump --single-transaction --default-character-set=utf8mb4 --no-tablespaces \
  -u olamundo_wp1 -p olamundo_wp1 > wordpress_db.sql
tar -czf wordpress.tar.gz -C ~/domains/olamundo.pl --exclude='./public_html/wp-content/cache' --exclude='./public_html/wp-content/wflogs' ./public_html
tar -tzf wordpress.tar.gz | head -3      # ./public_html/...
ls -lh wordpress.tar.gz
exit
```

```bash
cd wordpress
scp om:wordpress_db.sql om:wordpress.tar.gz .
```

## 2. Rozpakowanie

Bez `wp-config.php` i `.htaccess`, bo lokalne wersje są w gicie. Lokalny `wp-config.php` łączy się z kontenerem `db` i zapisuje błędy PHP do `wp-content/debug.log` zamiast na stronę.

```bash
tar -xzf wordpress.tar.gz --exclude='./public_html/wp-config.php' --exclude='./public_html/.htaccess'
git status public_html   # nic; zmieniony wp-config.php lub .htaccess: git checkout -- public_html
```

## 3. Import bazy

Importujemy oryginalny dump. Adresy zmieniamy dopiero w bazie (krok 4), nie w pliku:
`sed` i `go run ./replace` psują zserializowane dane PHP.

```bash
docker compose up -d db
until [ "$(docker inspect -f '{{.State.Health.Status}}' db)" = healthy ]; do sleep 2; done
docker exec -i db sh -c 'exec mysql -u"$MYSQL_USER" -p"$MYSQL_PASSWORD" "$MYSQL_DATABASE"' < wordpress_db.sql
docker compose up -d
```

Od nowa: `docker compose down -v` (usuwa bazę) i ponownie krok 3.

## 4. Adresy na localhost

```bash
wp() { docker compose run --rm wpcli wp "$@"; }
wp search-replace https://www.olamundo.pl http://localhost:8000 --all-tables-with-prefix --skip-columns=guid
wp search-replace http://www.olamundo.pl  http://localhost:8000 --all-tables-with-prefix --skip-columns=guid
wp search-replace https://olamundo.pl     http://localhost:8000 --all-tables-with-prefix --skip-columns=guid
wp search-replace http://olamundo.pl      http://localhost:8000 --all-tables-with-prefix --skip-columns=guid
wp search-replace 'https:\/\/olamundo.pl' 'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid
wp search-replace 'http:\/\/olamundo.pl'  'http:\/\/localhost:8000' --all-tables-with-prefix --skip-columns=guid
wp search-replace /home/olamundo/domains/olamundo.pl/public_html /var/www/html --all-tables-with-prefix
wp option get siteurl && wp option get home   # http://localhost:8000
wp cache flush
```

`--dry-run` pokazuje liczbę zmian bez zapisu. Ostrzeżenie „Skipping an uninitialized class” jest niegroźne.

## 5. Crawl

W katalogu głównym repo: `make crawl URL=http://localhost:8000`, potem `make serve`.
