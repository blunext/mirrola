# Crawl the local WordPress into a static copy and preview it locally.
# olamundo.pl itself is the static copy on Cloudflare Pages (since 2026-11-23),
# so the local Docker WordPress (wordpress/) is the only source.
#
#   make crawl                 # crawl http://localhost:8000 into ~/priv/olamundo
#   make serve                 # preview ~/priv/olamundo on http://localhost:8081
#   make crawl URL=<url>       # crawl another WordPress instead
#   make crawl STRICT=1        # fail on dead links / output file collisions instead of warning
#   make crawl                 # also empties DIR first (see clean-site), so pages removed
#                              # from the site disappear from the mirror too. DIR must be
#                              # a git repo containing the marker file .mirrola-site
#   make publish               # commit everything in DIR (new files too) and push the
#                              # development branch: Pages deploys the preview
#                              # https://development.olamundo-3xg.pages.dev
#   make release               # put the pushed preview live on olamundo.pl: fast-forward
#                              # main to development and push it (Pages production branch)
#
#   make check                 # crawl the local WordPress into a throwaway dir (in Docker)
#                              # and compare it with DIR at BASE (default HEAD): removed
#                              # pages, changed visible text, broken internal links.
#                              # Report in .check/report.txt. Run after every plugin/WP change.
#
#   make snapshot              # back up the local WordPress (files + DB dump) with restic
#                              # (wordpress/snapshot.sh; repo and password in wordpress/.env)
#   make snapshot MSG="..."    # same, with the message as a snapshot tag
#   make snapshots             # list snapshots
#   make snapshot-check        # verify the restic repository (READ_DATA=1: read every file)
#
#   make sync FORCE=1          # replace the local WordPress (files + DB) with a copy
#                              # from the old host (wordpress/sync-from-host.sh, interactive).
#                              # Refuses without FORCE=1: the local copy is the source of
#                              # truth now, sync would undo local posts and the 2026-11-23
#                              # cleanup.
#   make sync-skip FORCE=1     # same, reusing wordpress.tar.gz / wordpress_db.sql
#
# Typical flow: make crawl -> make serve (check it locally) -> make publish
#               -> check the preview -> make release

URL         ?= http://localhost:8000
# Public URL of the mirror (canonical links, sitemaps): the crawl rewrites
# the source address (URL) to this one.
SITE_URL    ?= https://olamundo.pl
DIR         ?= $(HOME)/priv/olamundo
DELAY       ?= 0
CONCURRENCY ?= 10
PORT        ?= 8081
STRICT      ?=
BASE        ?= HEAD
# Pages branches in DIR: preview and production (olamundo.pl).
PREVIEW     ?= development
PRODUCTION  ?= main
FORCE       ?=
READ_DATA   ?=
MSG         ?= Update site $(shell date '+%Y-%m-%d %H:%M')

BIN := mirrola
# Marks a directory as crawler output that clean-site may empty.
MARKER := .mirrola-site

snapshot: export SNAPSHOT_MSG = $(if $(filter command line,$(origin MSG)),$(MSG))

.PHONY: build test clean-site crawl serve publish release snapshot snapshots snapshot-check sync sync-skip check

build:
	go build -o $(BIN) .

test:
	go test ./...

# Empties DIR before a crawl, so stale files (deleted posts, junk from older
# crawls) don't linger.
#
# It refuses unless DIR is a git repository that contains the marker file
# $(MARKER): "is a git repo" alone would also match this source repo or any
# other project. Mark a site directory once with:  touch DIR/$(MARKER)
#
# Keeps top-level hidden entries (.git, .gitignore, .idea, the marker) and
# Cloudflare Pages config files; deletes everything else. Anything not
# committed in DIR is lost. Tracked files come back with "git checkout -- ."
# in DIR; "git clean -fd" then removes whatever the crawl added (it would also
# remove uncommitted _headers/functions, so commit those first).
#
# Guard and delete are one shell command, so "make -i" can't skip the guard.
# The trailing slash makes find descend when DIR is a symlink.
clean-site:
	@test -n "$(DIR)" && test -d "$(DIR)/.git" && test -f "$(DIR)/$(MARKER)" \
		|| { echo "Refusing to clean '$(DIR)': it must be a git repository containing $(MARKER)."; exit 1; }; \
	find "$(DIR)/" -mindepth 1 -maxdepth 1 ! -name '.*' \
		! -name _headers ! -name _redirects ! -name _routes.json ! -name _worker.js ! -name functions \
		-exec rm -rf {} +

# Checks that the source answers before clean-site empties DIR: with the local
# WordPress stopped, a crawl would otherwise leave DIR empty.
crawl: build
	@curl -fsS -o /dev/null --max-time 30 "$(URL)/" \
		|| { echo "$(URL) does not answer; is the local WordPress running? (cd wordpress && docker compose up -d)"; exit 1; }
	@$(MAKE) --no-print-directory clean-site
	./$(BIN) -url "$(URL)" -site-url "$(SITE_URL)" -dir "$(DIR)" -delay $(DELAY) -concurrency $(CONCURRENCY) $(if $(STRICT),-strict)

serve:
	go run ./server -dir "$(DIR)" -port $(PORT)

# Publishes whatever is in DIR to the preview branch ($(PREVIEW)); it does not
# crawl, so run it only after a successful "make crawl" checked with "make
# serve". It refuses on any other branch, so an unchecked crawl never goes
# straight to production ("make release" does that). "git add -A" also picks up
# new files (new posts, new uploads), which "git commit -a" would silently skip.
# DIR's .gitignore must exclude .DS_Store and .idea/ (Pages publishes every
# file in the repo). It pushes whenever the branch is ahead of its upstream,
# also when there is nothing new to commit (e.g. after a manual commit or an
# earlier failed push).
publish:
	@branch=$$(cd "$(DIR)" && git rev-parse --abbrev-ref HEAD) && [ "$$branch" = "$(PREVIEW)" ] \
		|| { echo "Refusing to publish: $(DIR) is on '$$branch', not '$(PREVIEW)' (cd $(DIR) && git checkout $(PREVIEW))."; exit 1; }
	cd "$(DIR)" && git add -A
	@cd "$(DIR)" && \
	if git diff --cached --quiet; then \
		echo "Nothing new to commit."; \
	else \
		git --no-pager diff --cached --shortstat && git commit -q -m "$(MSG)"; \
	fi && \
	ahead=$$(git rev-list --count @{u}..HEAD 2>/dev/null || echo unknown) && \
	if [ "$$ahead" = "0" ]; then \
		echo "Nothing to push: $$(git rev-parse --abbrev-ref HEAD) is up to date with its upstream."; \
	else \
		git push && \
		echo "Pushed $$(git rev-parse --short HEAD) to $(PREVIEW); Pages deploys the preview. Check it, then: make release"; \
	fi

# Puts the preview live: fast-forwards $(PRODUCTION) (the Pages production
# branch, olamundo.pl) to what was pushed to $(PREVIEW). Refuses when the local
# $(PREVIEW) differs from origin (unpushed or unpulled commits: the preview
# would not match), and pushes without --force, so it only fast-forwards.
release:
	@cd "$(DIR)" && git fetch -q origin && \
	local=$$(git rev-parse $(PREVIEW)) && remote=$$(git rev-parse origin/$(PREVIEW)) && \
	if [ "$$local" != "$$remote" ]; then \
		echo "Refusing to release: local $(PREVIEW) differs from origin/$(PREVIEW); run make publish first."; exit 1; \
	fi && \
	if [ -z "$$(git rev-list origin/$(PRODUCTION)..origin/$(PREVIEW))" ]; then \
		echo "Nothing to release: $(PRODUCTION) already contains $(PREVIEW)."; exit 0; \
	fi && \
	echo "Releasing to $(PRODUCTION) (olamundo.pl):" && git --no-pager log --oneline origin/$(PRODUCTION)..origin/$(PREVIEW) && \
	git push origin "refs/remotes/origin/$(PREVIEW):refs/heads/$(PRODUCTION)" && \
	git fetch -q origin && \
	if [ "$$(git rev-parse --abbrev-ref HEAD)" != "$(PRODUCTION)" ] && git show-ref -q --verify refs/heads/$(PRODUCTION); then \
		git branch -f $(PRODUCTION) origin/$(PRODUCTION); \
	fi && \
	echo "Released $$(git rev-parse --short origin/$(PRODUCTION)); Pages deploys olamundo.pl."

# restic backups of the local WordPress (wordpress/snapshot.sh). MSG is passed
# only when given on the command line (its default is publish's commit
# message), and through the environment, so quotes or $ in it are safe.
snapshot:
	./wordpress/snapshot.sh backup

snapshots:
	@./wordpress/snapshot.sh list

snapshot-check:
	./wordpress/snapshot.sh check $(if $(READ_DATA),--read-data)

# Pulls WordPress from the old host into the local Docker setup (README steps
# 1-4). Since 2026-11-23 the local copy is the source of truth (olamundo.pl is
# on Pages and the host only lags behind), so sync would destroy local-only
# posts and uploads and bring back what the cleanup removed. It refuses
# without FORCE=1; the script then asks for confirmation as well.
SYNC_GUARD = @test -n "$(FORCE)" || { echo "Refusing: the local WordPress is the source of truth and sync would overwrite it with the old host's copy. Back it up first, then: make $@ FORCE=1"; exit 1; }

sync:
	$(SYNC_GUARD)
	./wordpress/sync-from-host.sh

sync-skip:
	$(SYNC_GUARD)
	./wordpress/sync-from-host.sh --skip-download

# Compares a fresh crawl of the local WordPress with the published mirror
# (DIR at BASE). Everything runs in a container: DIR and this repo are mounted
# read-only, the crawl goes to a throwaway directory inside it. Exit status 1:
# look at the [FAIL] sections before publishing.
check:
	@docker inspect -f '{{.State.Running}}' wordpress 2>/dev/null | grep -q true \
		|| { echo "The local WordPress is not running (cd wordpress && docker compose up -d)."; exit 1; }
	@mkdir -p .check
	docker run --rm --network wordpress_wordpress_network \
		-v "$(CURDIR)":/src:ro -v "$(DIR)":/site:ro -v "$(CURDIR)/.check":/report \
		-v mirrola-gomod:/go/pkg/mod -v mirrola-gobuild:/root/.cache/go-build \
		-e BASE="$(BASE)" -e SITE_URL="$(SITE_URL)" \
		golang:1.27 sh /src/scripts/check.sh
