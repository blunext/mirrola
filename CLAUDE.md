# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project goals

Mirrola turns a self-managed WordPress blog (production domain `olamundo.pl`, hosted at a shared-hosting provider) into a static site. There are two goals:

1. **Static mirror → Cloudflare Pages.** Crawl the live site, rewrite every same-host link to a root-relative path, strip WordPress/Cloudflare cruft, and write a self-contained tree that can be deployed to Cloudflare Pages. This is what the Go crawler does.
2. **Portable WordPress → crawl → publish.** Pull WordPress (files + DB) off the hosting provider and run it in Docker, either locally or on a public on-demand Docker host. New posts get written there, then the crawler mirrors that instance and the output goes to Pages. Only the local docker-compose part exists so far. On-demand hosting and the Pages deploy step are not in the repo yet.

## Working rules

- **Anything that deletes or may overwrite files runs in Docker, never on the host.** That covers `make clean-site` / `make crawl` with a real `DIR`, `find … rm`, and crawls into a real directory. Use `golang:1.27` (has go, make, git, find), mount this repo and `~/priv/olamundo` read-only (`:ro`), copy them inside, and put the test scenario in a script file. On the host, only `go build`, `go vet` and `make -n`. `~/priv/olamundo` is the live site repo, and the untracked SQL dumps in `wordpress/` cannot be restored by git.
- **Commit only when the owner asks; never push.** Leave changes uncommitted and say so. A push to `~/priv/olamundo` deploys the site to Cloudflare Pages.
- **Publishing stays manual:** `make crawl` → `make serve` (the owner checks locally) → `make publish`. Don't chain or automate these steps.
- **Proving a fix:** a test that fails on the old code (run it in a `git worktree` of `HEAD`) plus a full crawl of the local copy (`go run ./server -dir ~/priv/olamundo`), comparing the old and new binary's output.
- **The owner writes in Polish; reply in Polish.**

## Commands

```bash
make crawl                                    # empty ~/priv/olamundo, then crawl https://olamundo.pl into it (DELAY=0, CONCURRENCY=10)
make serve                                    # preview ~/priv/olamundo on :8081 (8080 is phpMyAdmin)
make publish                                  # git add -A + commit + push in ~/priv/olamundo (no crawl; run after checking with make serve); the push deploys to Cloudflare Pages
make crawl URL=http://localhost:8000 DELAY=0  # crawl the local Docker WordPress; DIR=, STRICT=1 also work
make sync                                     # replace the local WordPress (files + DB) with a fresh copy from the host (wordpress/sync-from-host.sh, interactive; wipes local-only posts)
make sync-skip                                # same, reusing the already downloaded wordpress.tar.gz / wordpress_db.sql
make check                                    # crawl the local WordPress in Docker into a throwaway dir and compare it with ~/priv/olamundo at BASE (default HEAD); report in .check/report.txt; run after every plugin/WP/PHP change
# make crawl runs clean-site first: it deletes everything in DIR except top-level .* and Pages config (_headers, _redirects, …).
# It refuses unless DIR is a git repo containing the marker file .mirrola-site (touch DIR/.mirrola-site once). Never point DIR at a source repo.

go build -o mirrola .                         # build crawler CLI
go run . -url https://olamundo.pl -dir ./static [-strict] [-delay 2] [-max-depth N] [-concurrency N]
go run ./server -dir ./static [-port N]       # preview static output (default :8080) (dir → index.html, trailing-slash redirect)

go test ./...                                 # all tests live in crawler/
go test ./crawler -run TestCrawler_BasicFlow  # single test
go test ./crawler -coverprofile=crawler/cover.out && go tool cover -html=crawler/cover.out
```

## Crawler architecture (`crawler/`)

- **Entry:** `main.go` parses flags and calls `crawler.Init(...)` and then `crawler.Run(ctx)`.
- **State is split between package globals and `*Config`.** This is a deliberate refactor that is only half done. `config.go` holds globals (`client`, `limiter`, `visited`, `tasksWg`, regexps, and `cfg`). `types.go` holds the `Config` struct. Newer code is written as `Config` methods (`ProcessCSSFile`, `ProcessInlineJS`, `ProcessSrcSet`, `GetOutputPath`, `ToRelative`, `SameHost`, …), and older code is free functions reading globals. Regexps get compiled in both places (`initRegexps` and `Config.InitRegexps`). Tests that touch free functions must set the globals up themselves. Continue migrating toward `Config` rather than adding new globals.
- **Work loop (`crawler.go`):** a buffered `tasks` channel feeds N workers. `enqueueLink` normalizes the URL (`fixPath` + `normalize`), dedups it through `visited`, enforces `max-depth`, and calls `tasksWg.Add`. When `tasksWg` drains, a goroutine cancels the context and closes the channel. If the queue is full, `enqueueLink` hands the send to a goroutine instead of blocking: workers are the only producers, so blocking could deadlock the pool.
- **Redirects (`processURL`):** `http.Client` follows them silently. Content is saved under the requested URL's path (what pages link to), but relative links resolve against the final URL (`resp.Request.URL`, via `processHTMLAt`) and collisions are checked on the final URL. An HTML page redirecting to another host is not saved (`offsiteRedirectError`); for the start URL this is fatal, which catches a local WordPress whose `siteurl`/`home` still point to production. Assets redirected off-site (CDN) are saved.
- **Errors (`Run`):** dead links (HTTP 404/410, `isDeadLink`) and off-site page redirects (`isSkippable`) are logged with the referring page (`task.from`) and listed at the end; they don't stop the crawl unless `-strict`, or unless it's the start URL. Every other failure (403/429 from a WAF, 5xx, network, write errors) is fatal: the first one (`sync.Once`) cancels the crawl and `Run` returns an error. Cancelling the parent context (Ctrl-C, via `signal.NotifyContext` in `main.go`) also returns an error. Rule: an incomplete mirror must never exit 0, because a deploy step would publish it. After workers stop, `Run` drains the queue so `tasksWg` returns to zero and a later `Run` starts clean (`Init` also resets `visited` and `limiter`).
- **Depth semantics:** assets (img/css/js/fonts found in HTML or CSS) inherit the page's depth. Only `<a href>` links increase depth.
- **Dispatch by Content-Type (`processURL`):**
  - HTML goes to `processHTML` (`html.go`), which calls `filterDocument` and then `rewriteLinks`.
  - CSS goes to `ProcessCSSFile` (rewrites `url(...)`).
  - Everything else is streamed straight to disk.
  - `-delay` throttles page requests before they are sent (one shared limiter). Whether a URL is a page is guessed from the URL (`looksLikePage`: trailing `/`, no extension, `.html`/`.htm`/`.php`); static files are never delayed.
- **`filterDocument`** removes the configured `unwantedTags` (shortlink, pingback, EditURI, oEmbed, wp-json), Cloudflare challenge scripts, comment-reply, WP emoji JS and CSS, and all HTML comments. Extend `unwantedTags` / `shouldRemoveScript` / `shouldRemoveStyle` to strip more.
- **Link rewriting:**
  - Same-host URLs become root-relative paths (`ToRelative`). External hosts stay absolute. Exception: `<link rel="canonical">` gets the full URL on the public site (`ToPublicURL`, `-site-url`, Makefile `SITE_URL`, default `-url`), so *.pages.dev previews and a local-WordPress crawl still point search engines at https://olamundo.pl/….
  - Fragments are kept in the written link (`resolveURL` preserves them; `urlKey` drops them for fetching/enqueueing). Fragment-only references (`#_ftn1`, `#`, `url(#clip)`) are left exactly as written (`isFragmentOnly`).
  - Rewriting covers `href`, `src`, `srcset`, inline `style`, `<style>`, `<source>`, and inline `<script>`.
  - **Crawling a copy (`-url` host ≠ `-site-url` host, e.g. the local WordPress):** after link rewriting, `ToPublicText` (`publicurl.go`) replaces the source address with the public one in every saved HTML page, CSS file and text asset (JS, JSON, XML/sitemaps). It handles every form the source address is written in: plain, `\/`-escaped, URL-encoded (`http%3A%2F%2Flocalhost%3A8000`), `\u002F`-escaped and protocol-relative. That catches what isn't a link: Disqus `disqusUrl`, Facebook/LinkedIn share links, sitemaps, which leaked `localhost` in 462 pages before. If a saved file still contains the source hostname afterwards (an unknown form, e.g. double encoding), it is logged and `Run` fails at the end, so the mirror can't be published. When crawling the public site itself this is a no-op.
  - Inline JS handling targets JSON-escaped `https:\/\/…` URLs (Simple Lightbox plugin) and re-escapes slashes after rewriting.
- **Output paths (`filesystem.go` `GetOutputPath`):**
  - Extensionless HTML URLs become `path/index.html`.
  - A missing extension is inferred from the Content-Type.
  - Paths are NFC-normalized and Polish diacritics transliterated (`removeDiacritics`: `łódź` → `lodz`), so files and links are plain ASCII and NFC/NFD spellings collapse to one file. (A percent-encoded `-safe-filenames` mode existed but was removed: static hosts decode the URL, so literal `%XX` file names never matched.)
  - The query string is never part of the file name (static hosts ignore it); links keep it. A `-rewrite` mode that baked queries into file names existed but was removed: it computed file names and links with different rules (404s), and the site has no pages that differ only by query.
  - **Invariant:** the link from `ToRelative` must resolve to the file written by `GetOutputPath`. Both go through `normalizePath`; `TestLinkMatchesWrittenFile` (`consistency_test.go`) checks it the way a static host resolves links (decode path, drop query, dir → `index.html`).
  - **Hidden paths are never written** (`checkNotHidden` in `claimOutput`): any path component starting with `.` is refused (`hiddenPathError`, skipped like a dead link, fatal with `-strict`). The output dir is a git repo, so a crawled `/.git/config` or `/.gitignore` would otherwise overwrite it; `make clean-site` keeps dotfiles too.
  - **Collisions (`collisions.go`):** `claimOutput` records which URL produced each output path. The same resource in NFC/NFD form is skipped silently; a different URL on the same path (e.g. `łódź.jpg` vs `lodz.jpg`, or `?ver=1` vs `?ver=2`) logs `[WARN]` and keeps the first file, or aborts with `-strict`. A `Config` built without `NewConfigFromGlobals` has no registry and doesn't track.
- **HTTP (`http.go` `fetch`):** one GET per URL (no HEAD: it used to precede every GET and its result was never used). On a 404 it retries once with the path's Unicode normalization flipped (NFC↔NFD), because Polish characters in WordPress upload filenames are inconsistent. `writeFile` writes to a temp file, then renames it, and cleans up on failure.

## Regression check (`make check`, `sitecheck/`, `scripts/check.sh`)

- Runs in `golang:1.27` on the compose network: builds the crawler and `sitecheck`, extracts the baseline with `git archive BASE` from `~/priv/olamundo` (mounted read-only), crawls the local WordPress through a tiny proxy on `localhost:8000` → `wordpress:8000` that keeps the Host header (WordPress redirects any other host), then runs `sitecheck -old baseline -new crawl -forbid localhost:8000`.
- `sitecheck` fails (exit 1) on removed pages, changed *visible* text (title, body text, image alt), broken internal links that weren't broken in the baseline (resolved like a static host), and forbidden strings. Markup-only changes are reported as tag patterns with page counts (numbers and the page's own path masked, `?ver=` and nonces ignored): that is how the missing lightbox layout showed up. Asset and file changes are informational.
- Workflow for any WordPress change (plugin removal/update, core, PHP image): change one thing, `make check`, read the report.

## WordPress side (`wordpress/`)

- `docker-compose.yml` runs MariaDB 10.6, `wordpress:6.7.1-php8.3-apache` on `:8000` (Apache listens on 8000 *inside* the container too, `wordpress/apache/*.conf`: WordPress requests its own address `http://localhost:8000` from inside the container, Simple Lightbox fetches its `layout.html` and WP-Cron spawns itself this way; with Apache on 80 these failed silently and 31 gallery pages lost the lightbox layout), and phpMyAdmin on `:8080`, with credentials from `wordpress/.env` (gitignored). `./public_html` is bind-mounted as `/var/www/html`.
  - Only `wp-config.php` and `.htaccess` are tracked from `public_html`. The rest comes from the hosting tarball.
  - **No secrets in git** (this repo is public). `wp-config.php` reads DB credentials and the 8 keys/salts with `olamundo_env()` (`getenv`, dies with "Missing environment variable X" when unset). Values live only in `wordpress/.env`; `.env.example` lists the names. Compose passes them to `wordpress` and `wpcli` through the `x-wp-env` anchor with `${VAR:?}`, so a missing value stops compose. Local salts are random, not production's (until 2026-11-18 the committed wp-config.php held the production salts; they were rotated on the host).
- `wordpress/README.md` covers fetching files and the DB dump from the host (`ssh om`, tar, mysqldump, scp) and importing the dump into the `db` container.
- **URLs in the database are rewritten with WP-CLI `wp search-replace` after importing the original dump**, not in the `.sql` file. Use `docker compose run --rm wpcli …` (service `wpcli`, profile `tools`, not started by `up`). The steps are in `wordpress/README.md`. WordPress stores PHP-serialized strings (`s:<len>:"…";`), and WP-CLI unserializes, replaces and serializes them again.
  - Tested 2026-11-18 on the 2024-12-30 dump in throwaway containers: 0 of 1898 serialized values unreadable after `search-replace`. The only warning was a skipped `GoogleSitemapGeneratorStatus` object, a plugin class not loaded. What still contains `olamundo.pl` is `guid` (skipped on purpose) and Wordfence `wp_wf*` tables.
  - **`replace/` is kept but broken; don't use it on a dump.** It measures string lengths on the SQL-escaped text (backslashes counted twice) and rewrites every length, even where the text didn't change. On the same dump it left 1239 serialized values unreadable by PHP (1142 in `wp_postmeta`). `wordpress/wordpress.sql` is its output and is corrupted. `sed` on a dump breaks every serialized string it changes.
- SQL dumps (`wordpress/*.sql*`) and the built `mirrola` binary are untracked artifacts. Don't commit them.
- To mirror the local instance: `make crawl URL=http://localhost:8000` (DIR stays `~/priv/olamundo`, `SITE_URL` stays `https://olamundo.pl`). Links are root-relative, so the output does not depend on the source host. If the instance's `siteurl`/`home` still point to production, the crawl fails with an off-site redirect error.
