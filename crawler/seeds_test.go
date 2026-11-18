package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSite: a WordPress-like site whose only path to /orphan/ is the sitemap.
// {SRC} is replaced with the server's own address.
func seedSite(robots bool, notFoundStatus int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src := "http://" + r.Host
		xml := func(body string) {
			w.Header().Set("Content-Type", "application/xml; charset=UTF-8")
			fmt.Fprint(w, strings.ReplaceAll(body, "{SRC}", src))
		}
		switch r.URL.Path {
		case "/":
			servePage(w, `<a href="/about/">about</a>`)
		case "/about/", "/orphan/", "/a&b/":
			servePage(w, `<p>page</p>`)
		case "/sitemap.html":
			servePage(w, `<a href="/orphan/">orphan</a>`)
		case "/robots.txt":
			if !robots {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, strings.ReplaceAll("User-agent: *\nDisallow: /wp-admin/\n\nSitemap: {SRC}/sitemap.xml\nsitemap:{SRC}/sitemap.html\nSitemap: https://elsewhere.example/sitemap.xml\n", "{SRC}", src))
		case "/sitemap.xml":
			xml(`<?xml version="1.0" encoding="UTF-8"?><?xml-stylesheet type='text/xsl' href='{SRC}/wp-content/sitemap.xsl'?>` +
				`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><sitemap><loc>{SRC}/post-sitemap.xml</loc></sitemap></sitemapindex>`)
		case "/post-sitemap.xml":
			xml(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">` +
				`<url><loc>{SRC}/orphan/</loc></url><url><loc> <![CDATA[{SRC}/about/]]> </loc></url>` +
				`<url><loc>{SRC}/a&amp;b/</loc></url><url><loc>https://elsewhere.example/x/</loc></url></urlset>`)
		case "/404.png":
			w.Header().Set("Content-Type", "image/png")
			fmt.Fprint(w, "png")
		case "/wp-content/sitemap.xsl":
			w.Header().Set("Content-Type", "text/xsl")
			fmt.Fprint(w, `<xsl:stylesheet version="1.0"/>`)
		case "/data.xml":
			xml(`<rss><channel><link>{SRC}/never-fetched/</link><loc>{SRC}/never-fetched/</loc></channel></rss>`)
		default:
			w.Header().Set("Content-Type", "text/html; charset=UTF-8")
			w.WriteHeader(notFoundStatus)
			fmt.Fprint(w, `<html><head><title>Not found</title></head><body><h1>Nothing here</h1><a href="/">home</a><img src="/404.png"></body></html>`)
		}
	})
}

func crawlSeeds(t *testing.T, h http.Handler, siteURL string, strict bool) (string, string, error) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	require.NoError(t, Init(ts.URL, siteURL, dir, strict, "MirrolaTest/1.0", 5, 0, 100, 4, 0))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return dir, ts.URL, Run(ctx)
}

func read(t *testing.T, dir, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	require.NoError(t, err, rel)
	return string(b)
}

func TestSeeds_RobotsSitemapsAnd404(t *testing.T) {
	dir, src, err := crawlSeeds(t, seedSite(true, http.StatusNotFound), "", true)
	require.NoError(t, err, "strict: the 404 probe and an off-site sitemap are not dead links")

	assert.Contains(t, read(t, dir, "robots.txt"), "Sitemap: "+src+"/sitemap.xml", "saved as served when crawling the public site")
	assert.Contains(t, read(t, dir, "sitemap.xml"), "<loc>"+src+"/post-sitemap.xml</loc>", "sitemaps keep absolute URLs")
	assert.FileExists(t, filepath.Join(dir, "post-sitemap.xml"), "sitemap index is followed")
	assert.FileExists(t, filepath.Join(dir, "wp-content", "sitemap.xsl"), "the sitemap's stylesheet is saved")
	assert.FileExists(t, filepath.Join(dir, "sitemap.html"), "HTML sitemap listed in robots.txt")
	assert.FileExists(t, filepath.Join(dir, "orphan", "index.html"), "a page only the sitemap links to is mirrored")
	assert.FileExists(t, filepath.Join(dir, "a&b", "index.html"), "&amp; in <loc> is unescaped")

	notFound := read(t, dir, "404.html")
	assert.Contains(t, notFound, "Nothing here")
	assert.Contains(t, notFound, `href="/"`, "links in 404.html are rewritten like any page")
	assert.NoDirExists(t, filepath.Join(dir, strings.Trim(notFoundPath, "/")), "the probe path itself is not saved")
	assert.FileExists(t, filepath.Join(dir, "404.png"), "404.html's assets are fetched")
}

func TestSeeds_CopyOfSiteGetsPublicSitemapURLs(t *testing.T) {
	dir, src, err := crawlSeeds(t, seedSite(true, http.StatusNotFound), "https://example.org", false)
	require.NoError(t, err)
	host := strings.TrimPrefix(src, "http://")

	robots := read(t, dir, "robots.txt")
	assert.Contains(t, robots, "Sitemap: https://example.org/sitemap.xml")
	assert.NotContains(t, robots, host)
	assert.Contains(t, read(t, dir, "post-sitemap.xml"), "<loc>https://example.org/orphan/</loc>")
	assert.Contains(t, read(t, dir, "sitemap.xml"), "href='https://example.org/wp-content/sitemap.xsl'")
}

func TestSeeds_SiteWithoutRobotsOr404Page(t *testing.T) {
	// No robots.txt is fine even with -strict, and a server answering 200 for
	// everything gets no 404.html (and the probe page is not saved either).
	dir, _, err := crawlSeeds(t, seedSite(false, http.StatusOK), "", true)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "robots.txt"))
	assert.NoFileExists(t, filepath.Join(dir, "404.html"))
	assert.NoDirExists(t, filepath.Join(dir, strings.Trim(notFoundPath, "/")))
	assert.NoFileExists(t, filepath.Join(dir, "orphan", "index.html"), "no robots.txt, no sitemap")
}

func TestSeeds_OnlyAtSiteRoot(t *testing.T) {
	var hits []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		servePage(w, `<p>x</p>`)
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	require.NoError(t, Init(ts.URL+"/blog/", "", t.TempDir(), false, "MirrolaTest/1.0", 5, 0, 100, 1, 0))
	require.NoError(t, Run(context.Background()))
	assert.Equal(t, []string{"/blog/"}, hits, "robots.txt and 404.html belong to the host, not to /blog/")
}

func TestDiscover_IgnoresNonSitemapXML(t *testing.T) {
	cfg = &Config{BaseURL: "http://localhost:8000"}
	links, assets := discover("http://localhost:8000/data.xml", "application/xml",
		[]byte(`<rss><channel><loc>http://localhost:8000/x/</loc></channel></rss>`))
	assert.Empty(t, links)
	assert.Empty(t, assets)
	links, _ = discover("http://localhost:8000/notes.txt", "text/plain", []byte("Sitemap: http://localhost:8000/s.xml"))
	assert.Empty(t, links, "Sitemap: lines count only in /robots.txt")
}
