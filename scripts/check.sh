#!/bin/sh
# Runs inside golang:1.27, started by "make check". Never run it on the host.
#   /src     this repository (read-only)
#   /site    DIR, the published mirror's git repo (read-only); the baseline is BASE (default HEAD)
#   /report  writable: crawl.log and report.txt end up here
# The local WordPress is reached on the compose network. It believes it lives at
# http://localhost:8000 (siteurl), so a small proxy on localhost:8000 forwards to
# wordpress:8000 keeping that Host header.
set -eu

BASE=${BASE:-HEAD}
SITE_URL=${SITE_URL:-https://olamundo.pl}

git config --global --add safe.directory '*'
cd /src
go build -o /usr/local/bin/mirrola .
go build -o /usr/local/bin/sitecheck ./sitecheck

mkdir -p /baseline
git -C /site archive "$BASE" | tar -x -C /baseline
echo "baseline: $(git -C /site log -1 --format='%h %ad %s' --date=short "$BASE")"

mkdir -p /tmp/proxy
cat > /tmp/proxy/main.go <<'GO'
package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

func main() {
	target, _ := url.Parse("http://wordpress:8000")
	p := httputil.NewSingleHostReverseProxy(target)
	direct := p.Director
	p.Director = func(r *http.Request) {
		host := r.Host
		direct(r)
		r.Host = host
	}
	panic(http.ListenAndServe("127.0.0.1:8000", p))
}
GO
(cd /tmp/proxy && go mod init proxy >/dev/null 2>&1 && go build -o /usr/local/bin/wpproxy .)
wpproxy &
i=0
until curl -fsS -o /dev/null http://localhost:8000/; do
  i=$((i+1)); [ $i -lt 30 ] || { echo "local WordPress does not answer through the proxy"; exit 2; }
  sleep 1
done

echo "crawling http://localhost:8000 (log: .check/crawl.log) ..."
if ! mirrola -url http://localhost:8000 -site-url "$SITE_URL" -dir /new -concurrency 10 > /report/crawl.log 2>&1; then
  echo "CRAWL FAILED:"; tail -n 20 /report/crawl.log; exit 2
fi
grep -E '^\[(WARN|ERROR)\]' /report/crawl.log | head -n 20 || true
echo

set +e
sitecheck -old /baseline -new /new -forbid localhost:8000 > /report/report.txt
status=$?
cat /report/report.txt
exit $status
