# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project goals

Mirrola turns a self-managed WordPress blog (production domain `olamundo.pl`, hosted at a shared-hosting provider) into a static site. There are two goals:

1. **Static mirror → Cloudflare Pages.** Crawl the live site, rewrite every same-host link to a root-relative path, strip WordPress/Cloudflare cruft, and write a self-contained tree that can be deployed to Cloudflare Pages. This is what the Go crawler does.
2. **Portable WordPress → crawl → publish.** Pull WordPress (files + DB) off the hosting provider and run it in Docker, either locally or on a public on-demand Docker host. New posts get written there, then the crawler mirrors that instance and the output goes to Pages. Only the local docker-compose part exists so far. On-demand hosting and the Pages deploy step are not in the repo yet.

## Commands

```bash
make crawl                                    # crawl https://olamundo.pl into ~/priv/olamundo (DELAY=0, CONCURRENCY=10; full site ~10 min)
make serve                                    # preview ~/priv/olamundo on :8081 (8080 is phpMyAdmin)
make crawl URL=http://localhost:8000 DELAY=0  # crawl the local Docker WordPress; DIR=, STRICT=1 also work

go build -o mirrola .                         # build crawler CLI
go run . -url https://olamundo.pl -dir ./static [-rewrite] [-strict] [-delay 2] [-max-depth N] [-concurrency N]
go run ./server -dir ./static [-port N]       # preview static output (default :8080) (dir → index.html, trailing-slash redirect)
go run ./replace -input <dump.sql> -output <out.sql>   # rewrite prod URLs/paths in a SQL dump for local Docker

go test ./...                                 # all tests live in crawler/
go test ./crawler -run TestCrawler_BasicFlow  # single test
go test ./crawler -coverprofile=crawler/cover.out && go tool cover -html=crawler/cover.out
```

## Crawler architecture (`crawler/`)

- **Entry:** `main.go` parses flags and calls `crawler.Init(...)` and then `crawler.Run(ctx)`.
- **State is split between package globals and `*Config`.** This is a deliberate refactor that is only half done. `config.go` holds globals (`client`, `limiter`, `visited`, `tasksWg`, regexps, and `cfg`). `types.go` holds the `Config` struct. Newer code is written as `Config` methods (`ProcessCSSFile`, `ProcessInlineJS`, `ProcessSrcSet`, `GetOutputPath`, `ToRelative`, `SameHost`, …), and older code is free functions reading globals. Regexps get compiled in both places (`initRegexps` and `Config.InitRegexps`). Tests that touch free functions must set the globals up themselves. Continue migrating toward `Config` rather than adding new globals.
- **Work loop (`crawler.go`):** a buffered `tasks` channel feeds N workers. `enqueueLink` normalizes the URL (`fixPath` + `normalize`), dedups it through `visited`, enforces `max-depth`, and calls `tasksWg.Add`. When `tasksWg` drains, a goroutine cancels the context and closes the channel. If the queue is full, `enqueueLink` hands the send to a goroutine instead of blocking: workers are the only producers, so blocking could deadlock the pool.
- **Errors (`Run`):** dead links (HTTP 404/410, `isDeadLink`) are logged with the referring page (`task.from`) and listed at the end; they don't stop the crawl unless `-strict`. Every other failure (403/429 from a WAF, 5xx, network, write errors) is fatal: the first one (`sync.Once`) cancels the crawl and `Run` returns an error. Cancelling the parent context (Ctrl-C, via `signal.NotifyContext` in `main.go`) also returns an error. Rule: an incomplete mirror must never exit 0, because a deploy step would publish it. After workers stop, `Run` drains the queue so `tasksWg` returns to zero and a later `Run` starts clean (`Init` also resets `visited` and `limiter`).
- **Depth semantics:** assets (img/css/js/fonts found in HTML or CSS) inherit the page's depth. Only `<a href>` links increase depth.
- **Dispatch by Content-Type (`processURL`):**
  - HTML goes to `processHTML` (`html.go`), which calls `filterDocument` and then `rewriteLinks`.
  - CSS goes to `ProcessCSSFile` (rewrites `url(...)`).
  - Everything else is streamed straight to disk.
  - `-delay` rate-limits HTML pages only, never assets.
- **`filterDocument`** removes the configured `unwantedTags` (shortlink, pingback, EditURI, oEmbed, wp-json), Cloudflare challenge scripts, comment-reply, WP emoji JS and CSS, and all HTML comments. Extend `unwantedTags` / `shouldRemoveScript` / `shouldRemoveStyle` to strip more.
- **Link rewriting:**
  - Same-host URLs become root-relative paths (`ToRelative`). External hosts stay absolute.
  - Rewriting covers `href`, `src`, `srcset`, inline `style`, `<style>`, `<source>`, and inline `<script>`.
  - Inline JS handling targets JSON-escaped `https:\/\/…` URLs (Simple Lightbox plugin) and re-escapes slashes after rewriting.
- **Output paths (`filesystem.go` `GetOutputPath`):**
  - Extensionless HTML URLs become `path/index.html`.
  - A missing extension is inferred from the Content-Type.
  - Paths are NFC-normalized and Polish diacritics transliterated (`removeDiacritics`: `łódź` → `lodz`), so files and links are plain ASCII and NFC/NFD spellings collapse to one file. (A percent-encoded `-safe-filenames` mode existed but was removed: static hosts decode the URL, so literal `%XX` file names never matched.)
  - With `-rewrite`, query strings get baked into the filename (`name_query.ext`). Queries over 80 chars become `q_<sha1>`.
  - **Invariant:** the link from `ToRelative` must resolve to the file written by `GetOutputPath`. Both go through `normalizePath`; `TestLinkMatchesWrittenFile` (`consistency_test.go`) checks it the way a static host resolves links (decode path, drop query, dir → `index.html`).
  - **Collisions (`collisions.go`):** `claimOutput` records which URL produced each output path. The same resource in NFC/NFD form is skipped silently; a different URL on the same path (e.g. `łódź.jpg` vs `lodz.jpg`, or `?ver=1` vs `?ver=2` without `-rewrite`) logs `[WARN]` and keeps the first file, or aborts with `-strict`. A `Config` built without `NewConfigFromGlobals` has no registry and doesn't track.
- **HTTP (`http.go`):** HEAD, then GET. On a 404 it retries with the path's Unicode normalization flipped (NFC↔NFD), because Polish characters in WordPress upload filenames are inconsistent. `writeFile` writes to a temp file, then renames it, and cleans up on failure.

## WordPress side (`wordpress/`)

- `docker-compose.yml` runs MariaDB 10.5, `wordpress:6.7.1-php8.3-apache` on `:8000`, and phpMyAdmin on `:8080`, with credentials from `wordpress/.env` (gitignored). `./public_html` is bind-mounted as `/var/www/html`.
  - Only `wp-config.php` and `.htaccess` are tracked from `public_html`. The rest comes from the hosting tarball.
  - `wp-config.php` has hardcoded values. It doesn't use `getenv_docker`.
- `wordpress/README.md` covers fetching files and the DB dump from the host (`ssh om`, tar, mysqldump, scp) and importing the dump into the `db` container.
- **Use `replace/` rather than plain `sed` for URL rewriting in the dump.** WordPress stores PHP-serialized strings (`s:<len>:"…";`), and `replace` recomputes their byte lengths. The README's `sed` lines would corrupt them.
  - Rules in `replace/main.go` map `olamundo.pl` (http/https/www/upper-case/JSON-escaped) to `http://localhost:8000`, map the host filesystem path to `/var/www/html`, and map DB name `olamundo_wp1` to `wordpress`.
  - Default `-input`/`-output` are absolute paths on the author's machine.
- SQL dumps (`wordpress/*.sql*`) and the built `mirrola` binary are untracked artifacts. Don't commit them.
- To mirror the local instance, point the crawler at it, e.g. `go run . -url http://localhost:8000 -dir ./static`. The output is root-relative, so it doesn't depend on the source host.
