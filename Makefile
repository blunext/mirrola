# Crawl the WordPress site into a static copy and preview it locally.
#
#   make crawl                 # crawl https://olamundo.pl into ~/priv/olamundo
#   make serve                 # preview ~/priv/olamundo on http://localhost:8081
#   make crawl URL=http://localhost:8000 DELAY=0   # crawl the local Docker WordPress
#   make crawl STRICT=1        # fail on dead links / output file collisions instead of warning
#   make publish               # commit everything in DIR (new files too) and push;
#                              # the push triggers the Cloudflare Pages deploy
#
# Typical flow: make crawl -> make serve (check it locally) -> make publish

URL         ?= https://olamundo.pl
DIR         ?= $(HOME)/priv/olamundo
DELAY       ?= 0
CONCURRENCY ?= 10
PORT        ?= 8081
STRICT      ?=
MSG         ?= Update site $(shell date '+%Y-%m-%d %H:%M')

BIN := mirrola

.PHONY: build test crawl serve publish

build:
	go build -o $(BIN) .

test:
	go test ./...

crawl: build
	./$(BIN) -url $(URL) -dir $(DIR) -delay $(DELAY) -concurrency $(CONCURRENCY) $(if $(STRICT),-strict)

serve:
	go run ./server -dir $(DIR) -port $(PORT)

# Publishes whatever is in DIR; it does not crawl, so run it only after a
# successful "make crawl" checked with "make serve". "git add -A" also picks up
# new files (new posts, new uploads), which "git commit -a" would silently skip.
# DIR's .gitignore must exclude .DS_Store and .idea/ (Pages publishes every
# file in the repo).
publish:
	cd $(DIR) && git add -A
	@cd $(DIR) && if git diff --cached --quiet; then \
		echo "Nothing changed, nothing to publish."; \
	else \
		git diff --cached --shortstat && \
		git commit -q -m "$(MSG)" && \
		git push && \
		echo "Pushed $$(git rev-parse --short HEAD) to $$(git rev-parse --abbrev-ref HEAD); Cloudflare Pages will deploy it."; \
	fi
