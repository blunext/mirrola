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

# WP-CLI image (Alpine, PHP 8.4, MariaDB client). Runs as uid 33, which is
# www-data in the (Debian) WordPress image: files restored into the wp_files
# volume must be writable by Apache (uploads, plugin updates).
FROM wordpress:cli-php8.4
USER root
# openssh-client: git over SSH with the deploy key; socat: the localhost:8000
# forwarder for crawling (WordPress answers only to its own address).
RUN apk add --no-cache bash curl git openssh-client socat tzdata \
 && addgroup -g 33 olamundo && adduser -D -H -u 33 -G olamundo -h /tmp olamundo \
 && mkdir -p /data/public_html /cache /site \
 && chown -R 33:33 /data /cache /site
COPY --from=build /out/ /usr/local/bin/
COPY --from=restic /usr/bin/restic /usr/local/bin/restic
COPY wordpress/tools/olamundo.sh /usr/local/bin/olamundo
# GitHub's SSH host key (https://api.github.com/meta), so the first clone
# can't be intercepted.
COPY wordpress/tools/known_hosts /etc/olamundo/known_hosts
# restic's cache and the WordPress files live in named volumes (see
# docker-compose.yml); an empty volume takes the owner of these directories.
ENV RESTIC_CACHE_DIR=/cache
# Local time in restic's listings (snapshot times are stored in UTC anyway).
ENV TZ=Europe/Warsaw
# uid 33 ("olamundo" above; ssh refuses to run for a uid without a passwd
# entry) has its home in /tmp (WP-CLI keeps its cache there).
ENV HOME=/tmp
USER 33:33
WORKDIR /data
ENTRYPOINT ["/usr/local/bin/olamundo"]
