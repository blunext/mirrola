package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

func writeSite(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for p, body := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(body), 0o644))
	}
	return root
}

func page(title, body string) string {
	return `<html><head><title>` + title + `</title><link rel="stylesheet" href="/s.css?ver=1"><script>var x=1</script></head><body>` + body + `</body></html>`
}

func TestVisibleText(t *testing.T) {
	doc, err := html.Parse(strings.NewReader(`<html><head><title>T</title><style>p{}</style><meta name="x" content="no"></head>` +
		`<body><h1>Head  line</h1><p>One <b>two</b><br>three</p><script>hidden()</script><noscript>no</noscript>` +
		`<img src="/a.jpg" alt="Photo"><svg><text>svg</text></svg></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, []string{"T", "Head line", "One two", "three", "[img: Photo]"}, visibleText(doc))
}

func TestServed(t *testing.T) {
	for link, want := range map[string]string{
		"/":                         "index.html",
		"/post/":                    "post/index.html",
		"/post":                     "post/index.html",
		"/post/#comments":           "post/index.html",
		"/img/a.jpg?ver=2#x":        "img/a.jpg",
		"/%C5%82od%C5%BA.jpg":       "łodź.jpg",
		"/a/../b/c.css":             "b/c.css",
		"/wp-content/x.css?ver=1.2": "wp-content/x.css",
	} {
		assert.Equal(t, want, served(link), link)
	}
}

func TestNormalizeDropsVersionsAndNonces(t *testing.T) {
	a := `<link href="/a.css?ver=6.1"><img src="/i.png?x=1&amp;ver=2"><script>var c={"nonce":"abcdef1234"};u="?_wpnonce=deadbeef01"</script>`
	b := `<link href="/a.css?ver=7.0"><img src="/i.png?x=1&amp;ver=3"><script>var c={"nonce":"0123456789"};u="?_wpnonce=feedface99"</script>`
	assert.Equal(t, string(normalize([]byte(a))), string(normalize([]byte(b))))
	assert.NotEqual(t, string(normalize([]byte(`<a href="/x/">`))), string(normalize([]byte(`<a href="/y/">`))))
	assert.Equal(t,
		string(normalize([]byte(`<a data-slb-asset="123"><script>$.extend(SLB.View.assets, {"123":{"id":5}})</script><!-- generated-on='18 November 2026 21:19' -->`))),
		string(normalize([]byte(`<a data-slb-asset="987"><script>$.extend(SLB.View.assets, {"987":{"id":5}})</script><!-- generated-on='19 November 2026 08:00' -->`))),
		"lightbox ids and sitemap timestamps are per request")
}

func TestCheck(t *testing.T) {
	old := writeSite(t, map[string]string{
		".git/config":     "x",
		".mirrola-site":   "",
		"_redirects":      "/feed/ / 301",
		"index.html":      page("Home", `<a href="/a/">a</a><a href="/b/">b</a><a href="/gone-before/">g</a>`),
		"a/index.html":    page("A", `<p>Old text</p><p>same</p>`),
		"b/index.html":    page("B", `<p>B</p>`),
		"m/index.html":    page("M", `<p>M</p>`),
		"s.css":           `body{background:url(/bg.png)}`,
		"bg.png":          "png",
		"old-only.jpg":    "jpg",
		"wp-content/p.js": "v1",
	})
	nw := writeSite(t, map[string]string{
		"index.html":      page("Home", `<a href="/a/">a</a><a href="/b/">b</a><a href="/gone-before/">g</a>`),
		"a/index.html":    page("A", `<p>New text</p><p>same</p>`),
		"c/index.html":    page("C", `<p>C</p><img src="/missing.jpg?ver=1" srcset="/missing-2x.jpg 2x">`),
		"m/index.html":    strings.Replace(page("M", `<p>M</p>`), "?ver=1", "?ver=9", 1) + `<!-- -->`,
		"s.css":           `body{background:url(/bg.png)} .x{background:url('/nope.png')}`,
		"bg.png":          "png",
		"wp-content/p.js": "v2",
		"leak.json":       `{"u":"http://LOCALHOST:8000/x"}`,
	})
	oldS, err := loadSite(old)
	require.NoError(t, err)
	newS, err := loadSite(nw)
	require.NoError(t, err)
	assert.NotContains(t, oldS.files, ".git/config", "dotfiles are not crawl output")
	assert.NotContains(t, oldS.files, "_redirects", "Pages config is not crawl output")

	r := check(oldS, newS, []string{"localhost:8000"})

	assert.Equal(t, []string{"b/index.html"}, r.removedPages)
	assert.Equal(t, []string{"c/index.html"}, r.addedPages)
	require.Len(t, r.textChanged, 1)
	assert.Equal(t, "a/index.html", r.textChanged[0].page)
	assert.Equal(t, []string{"Old text"}, r.textChanged[0].old)
	assert.Equal(t, []string{"New text"}, r.textChanged[0].new)
	assert.Equal(t, []string{"m/index.html"}, r.markupChanged, "?ver= alone is noise, the comment is a real markup change")
	assert.Equal(t, map[string][]string{".css": {"s.css"}, ".js": {"wp-content/p.js"}}, r.assetsChanged)
	assert.Equal(t, []string{"old-only.jpg"}, r.removedFiles)
	assert.Equal(t, []string{"leak.json"}, r.addedFiles)

	assert.Equal(t, map[string][]string{
		"/b/":             {"index.html"},
		"/missing.jpg":    {"c/index.html"},
		"/missing-2x.jpg": {"c/index.html"},
		"/nope.png":       {"s.css"},
	}, r.brokenNew, "/gone-before/ was already broken in the baseline")
	assert.Equal(t, 1, r.brokenOld)
	assert.Equal(t, map[string][]string{"localhost:8000": {"leak.json"}}, r.leaks, "case-insensitive")
	assert.True(t, r.failed())

	var out strings.Builder
	r.print(&out, 20)
	assert.Contains(t, out.String(), "[FAIL] pages removed: 1")
	assert.Contains(t, out.String(), "- Old text")
	assert.Contains(t, out.String(), "RESULT: FAIL")
}

func TestCheck_IdenticalSitesPass(t *testing.T) {
	files := map[string]string{"index.html": page("Home", `<a href="/a/">a</a>`), "a/index.html": page("A", `x`), "s.css": "a{}"}
	oldS, err := loadSite(writeSite(t, files))
	require.NoError(t, err)
	newS, err := loadSite(writeSite(t, files))
	require.NoError(t, err)
	r := check(oldS, newS, []string{"localhost:8000"})
	assert.False(t, r.failed())
	var out strings.Builder
	r.print(&out, 20)
	assert.Contains(t, out.String(), "RESULT: OK")
}

func TestMarkupPatterns(t *testing.T) {
	share := func(scheme, slug string) string {
		return page(slug, `<p>x</p><a href="`+scheme+`://www.facebook.com/sharer.php?u=https%3A%2F%2Folamundo.pl%2F`+slug+`%2F">fb</a><img data-slb-asset="`+slug+`123" src="/a.jpg">`)
	}
	oldS, err := loadSite(writeSite(t, map[string]string{"one/index.html": share("http", "one"), "two/index.html": share("http", "two"), "a.jpg": "j"}))
	require.NoError(t, err)
	newS, err := loadSite(writeSite(t, map[string]string{"one/index.html": share("https", "one"), "two/index.html": share("https", "two"), "a.jpg": "j"}))
	require.NoError(t, err)

	r := check(oldS, newS, nil)
	assert.False(t, r.failed(), "markup-only changes don't fail")
	assert.Equal(t, []string{"one/index.html", "two/index.html"}, r.markupChanged)
	require.Len(t, r.markupRemoved, 1)
	require.Len(t, r.markupAdded, 1)
	assert.Equal(t, pattern{`<a href="http://www.facebook.com/sharer.php?u=https%3A%2F%2Folamundo.pl{page}">fb`, 2}, r.markupRemoved[0], "page slug masked, so both pages count as one pattern")
	assert.Equal(t, 2, r.markupAdded[0].pages)

	var out strings.Builder
	r.print(&out, 20)
	assert.Contains(t, out.String(), "-     2  <a href=\"http://www.facebook.com")
}
