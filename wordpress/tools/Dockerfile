# Tools container: WP-CLI, restic, git and this repository's crawler and
# sitecheck. Built from the repository root (see docker-compose.yml):
#   docker compose build tools
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/mirrola . \
 && CGO_ENABLED=0 go build -o /out/sitecheck ./sitecheck \
 && CGO_ENABLED=0 go build -o /out/mirrola-server ./server

# restic from its official image (Alpine's package lags behind); bump the
# version here.
FROM restic/restic:0.20.0 AS restic

# WP-CLI image (Alpine, PHP 8.3, MariaDB client); runs as www-data (uid 82).
FROM wordpress:cli-php8.3
USER root
RUN apk add --no-cache bash git tzdata \
 && mkdir -p /data /cache \
 && chown www-data:www-data /data /cache
COPY --from=build /out/ /usr/local/bin/
COPY --from=restic /usr/bin/restic /usr/local/bin/restic
COPY wordpress/tools/olamundo.sh /usr/local/bin/olamundo
# restic's cache lives in a named volume (see docker-compose.yml).
ENV RESTIC_CACHE_DIR=/cache
# Local time in restic's listings (snapshot times are stored in UTC anyway).
ENV TZ=Europe/Warsaw
USER www-data
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/olamundo"]
