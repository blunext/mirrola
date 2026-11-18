# Crawl the WordPress site into a static copy and preview it locally.
#
#   make crawl                 # crawl https://olamundo.pl into ~/priv/olamundo
#   make serve                 # preview ~/priv/olamundo on http://localhost:8081
#   make crawl URL=http://localhost:8000 DELAY=0   # crawl the local Docker WordPress
#   make crawl STRICT=1        # fail on output file collisions instead of warning

URL         ?= https://olamundo.pl
DIR         ?= $(HOME)/priv/olamundo
DELAY       ?= 1
CONCURRENCY ?= 4
PORT        ?= 8081
STRICT      ?=

BIN := mirrola

.PHONY: build test crawl serve

build:
	go build -o $(BIN) .

test:
	go test ./...

crawl: build
	./$(BIN) -url $(URL) -dir $(DIR) -delay $(DELAY) -concurrency $(CONCURRENCY) $(if $(STRICT),-strict)

serve:
	go run ./server -dir $(DIR) -port $(PORT)
