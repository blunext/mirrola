# Lokalny WordPress olamundo.pl

Źródłem prawdy jest ostatni snapshot w Cloudflare R2 (restic). Każda sesja zaczyna się od niego
i kończy nowym snapshotem. Na komputerze potrzebny jest tylko Docker; wszystko inne działa
w kontenerze narzędzi (`tools/`). Polecenia uruchamia się w katalogu głównym repo.

## Nowy komputer

1. Docker (ok. 3 GB wolnego miejsca na jego dysku: obrazy i dane WordPressa) i klon tego repo (`git clone https://github.com/blunext/mirrola.git`).
2. `wordpress/.env` według `.env.example` (skopiuj go i uzupełnij). Wartości (hasła bazy,
   salty, hasło resticu, token R2) dostajesz od właściciela; bez hasła resticu snapshotów
   nie da się odczytać.
   `PUBLISHER` to nazwa tego komputera (podpis commitów publikacji).
3. Klucz wdrożeniowy do repo strony (tylko do publikacji):
   ```bash
   cd wordpress && mkdir -p deploy && chmod 700 deploy
   ssh-keygen -t ed25519 -N '' -C 'olamundo deploy (<komputer>)' -f deploy/olamundo_deploy_key
   cat deploy/olamundo_deploy_key.pub
   ```
   Klucz publiczny: https://github.com/blunext/olamundo/settings/keys → Add deploy key,
   tytuł = nazwa komputera, zaznaczone „Allow write access”.
4. `make start` (pierwszy raz pobiera ok. 570 MB z R2).

## Sesja

```bash
make start      # ostatni snapshot z R2 → WordPress na http://localhost:8000
                # (pisanie w /wp-admin)
make stop       # pyta, czy wypchnąć zmiany do R2, potem zatrzymuje WordPressa
```

Co nie zostało wypchnięte do R2 (`make snapshot` albo „t” w `make stop`), przepada przy
następnym `make start`.

## Publikacja

```bash
make crawl      # crawl lokalnego WordPressa do repo strony (wolumen "site")
make serve      # podgląd na http://localhost:8081 (Ctrl-C kończy)
make publish    # podgląd na Pages: https://development.olamundo-3xg.pages.dev
make release    # olamundo.pl
```

Po każdej zmianie wtyczek, WordPressa albo PHP: `make check` (raport w `.check/report.txt`).

## WP-CLI

```bash
cd wordpress && docker compose run --rm --entrypoint wp tools --path=/data/public_html <polecenie>
```

Pliki WordPressa są tu tylko do odczytu; baza jest zapisywalna. Adresy w bazie zmienia się
przez `wp search-replace` (rozumie zserializowane dane PHP), nigdy `sed` na dumpie.

Pierwszy import ze starego hostingu (`sync-from-host.sh`, ten README w starszej wersji) jest
w historii gita (commit e01a86f i wcześniejsze).
