# The local WordPress (wordpress/, Docker) and the static copy of it that is
# olamundo.pl (Cloudflare Pages). Everything runs in Docker: the tools
# container (wordpress/tools) does snapshots, crawl and publishing, so the
# host needs only Docker.
#
# A session:
#   make start                 # put the latest R2 snapshot into the local WordPress (files
#                              # volume: only differences; DB: replaced) and start it.
#                              # Whatever wasn't snapshotted is lost.
#   (write in http://localhost:8000/wp-admin)
#   make stop [MSG="..."]      # asks whether to push the changes to R2 (make snapshot),
#                              # then stops WordPress and the DB
#   make snapshot [MSG="..."]  # push the current state to R2 (MSG becomes a snapshot tag)
#
# Publishing (the site repo blunext/olamundo lives in the "site" volume):
#   make crawl                 # crawl the local WordPress into the site repo (emptied first,
#                              # Pages config like _redirects kept). STRICT=1: fail on dead
#                              # links / output file collisions instead of warning.
#   make serve                 # preview the crawl on http://localhost:8081 (PORT=)
#   make publish [MSG="..."]   # commit the crawl and push the development branch: Pages
#                              # deploys the preview https://development.olamundo-3xg.pages.dev
#   make release               # put the pushed preview live on olamundo.pl: fast-forward
#                              # main to development and push it (Pages production branch)
#   make check                 # crawl the local WordPress into a throwaway dir and compare
#                              # it with the site repo at BASE (default origin/development,
#                              # fetched first): removed pages,
#                              # changed visible text, broken internal links. Report in
#                              # .check/report.txt. Run after every plugin/WP/PHP change.
#
# Typical flow: make crawl -> make serve (check it locally) -> make publish
#               -> check the preview -> make release
#
# Snapshots:
#   make snapshots             # list snapshots
#   make snapshot-check        # verify the restic repository (READ_DATA=1: read every pack)
#   make snapshot-init         # create the restic repository (new bucket only)

DELAY       ?= 0
CONCURRENCY ?= 10
PORT        ?= 8081
STRICT      ?=
BASE        ?= origin/development
READ_DATA   ?=
MSG         ?=

# The tools container (wordpress/tools): --build rebuilds it when the scripts
# or the crawler changed (a no-op otherwise).
TOOLS = cd wordpress && docker compose run --rm --build
# crawl and check need the running WordPress (the tools container would
# otherwise only start the DB).
WP_RUNNING = @docker inspect -f '{{.State.Running}}' wordpress 2>/dev/null | grep -q true \
	|| { echo "The local WordPress is not running (make start)."; exit 1; }

# Variables reach the container through the environment (docker compose run
# -e NAME), so quotes or $ in MSG are safe.
snapshot: export SNAPSHOT_MSG = $(MSG)
publish: export PUBLISH_MSG = $(MSG)
crawl check: export DELAY := $(DELAY)
crawl check: export CONCURRENCY := $(CONCURRENCY)
crawl: export STRICT := $(STRICT)
check: export BASE := $(BASE)

.PHONY: build test start stop snapshot snapshots snapshot-check snapshot-init \
	crawl serve publish release check

# Development of the crawler itself (needs Go on the host).
build:
	go build -o mirrola .

test:
	go test ./...

# Every session starts from the latest snapshot: WordPress is stopped while
# the tools container replaces its files (wp_files volume) and the DB, then
# started again. A failed restore leaves WordPress stopped.
start:
	cd wordpress && docker compose stop wordpress
	$(TOOLS) tools-restore start
	cd wordpress && docker compose up -d --wait wordpress
	@echo "WordPress: http://localhost:8000"

# Ends a session: asks whether to push the changes to R2 first (t/n, no
# default), then stops the whole stack (data stays in the volumes). A failed
# snapshot stops nothing, so it can be retried while WordPress still runs.
stop:
	@while :; do \
		printf 'Wypchnąć zmiany do R2? (t/n) '; read -r answer || exit 1; \
		case "$$answer" in \
			t) $(MAKE) --no-print-directory snapshot || exit 1; break ;; \
			n) echo "Bez snapshotu: niewypchnięte zmiany przepadną przy następnym make start."; break ;; \
		esac; \
	done
	cd wordpress && docker compose stop

# restic backups of the local WordPress to R2 (wordpress/tools/olamundo.sh).
snapshot:
	$(TOOLS) -e SNAPSHOT_MSG tools snapshot

snapshots:
	@$(TOOLS) tools snapshots

snapshot-check:
	$(TOOLS) tools repo-check $(if $(READ_DATA),--read-data)

snapshot-init:
	$(TOOLS) tools init

# The site repo is cloned (shallow) into the "site" volume on first use and
# pushed with this computer's deploy key (wordpress/deploy/). crawl resets it
# to origin/development first, so it always starts from what is published.
crawl:
	$(WP_RUNNING)
	$(TOOLS) -e DELAY -e CONCURRENCY -e STRICT tools crawl

# Only on this computer (127.0.0.1), not on the local network.
serve:
	$(TOOLS) -p 127.0.0.1:$(PORT):8081 tools serve

# Publishes what the last crawl left in the site repo to the preview branch;
# it does not crawl, and refuses unless the last crawl completed. "git add -A" also picks up new files (new posts,
# new uploads). It pushes whenever the branch is ahead of origin, also when
# there is nothing new to commit (e.g. after an earlier failed push).
publish:
	$(TOOLS) -e PUBLISH_MSG tools publish

# Fast-forwards main (olamundo.pl) to origin/development, i.e. what the
# preview shows; pushes without --force, refuses if main isn't an ancestor.
release:
	$(TOOLS) tools release

check:
	$(WP_RUNNING)
	@mkdir -p .check
	$(TOOLS) -e DELAY -e CONCURRENCY -e BASE tools site-check
