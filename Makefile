# Crawl the WordPress site into a static copy and preview it locally.
#
#   make crawl                 # crawl https://olamundo.pl into ~/priv/olamundo
#   make serve                 # preview ~/priv/olamundo on http://localhost:8081
#   make crawl URL=http://localhost:8000 DELAY=0   # crawl the local Docker WordPress
#   make crawl STRICT=1        # fail on dead links / output file collisions instead of warning
#   make crawl                 # also empties DIR first (see clean-site), so pages removed
#                              # from the site disappear from the mirror too. DIR must be
#                              # a git repo containing the marker file .mirrola-site
#   make publish               # commit everything in DIR (new files too) and push;
#                              # the push triggers the Cloudflare Pages deploy
#
# Typical flow: make crawl -> make serve (check it locally) -> make publish

URL         ?= https://olamundo.pl
# Public URL of the mirror (for <link rel=canonical>); stays the same when
# crawling a local WordPress with URL=http://localhost:8000.
SITE_URL    ?= https://olamundo.pl
DIR         ?= $(HOME)/priv/olamundo
DELAY       ?= 0
CONCURRENCY ?= 10
PORT        ?= 8081
STRICT      ?=
MSG         ?= Update site $(shell date '+%Y-%m-%d %H:%M')

BIN := mirrola
# Marks a directory as crawler output that clean-site may empty.
MARKER := .mirrola-site

.PHONY: build test clean-site crawl serve publish

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

crawl: build clean-site
	./$(BIN) -url "$(URL)" -site-url "$(SITE_URL)" -dir "$(DIR)" -delay $(DELAY) -concurrency $(CONCURRENCY) $(if $(STRICT),-strict)

serve:
	go run ./server -dir "$(DIR)" -port $(PORT)

# Publishes whatever is in DIR; it does not crawl, so run it only after a
# successful "make crawl" checked with "make serve". "git add -A" also picks up
# new files (new posts, new uploads), which "git commit -a" would silently skip.
# DIR's .gitignore must exclude .DS_Store and .idea/ (Pages publishes every
# file in the repo). It pushes whenever the branch is ahead of its upstream,
# also when there is nothing new to commit (e.g. after a manual commit or an
# earlier failed push).
publish:
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
		echo "Pushed $$(git rev-parse --short HEAD) to $$(git rev-parse --abbrev-ref HEAD); Cloudflare Pages will deploy it."; \
	fi
