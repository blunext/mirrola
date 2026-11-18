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

func TestSourceRewriter_Forms(t *testing.T) {
	r := newSourceRewriter("http://localhost:8000", "https://olamundo.pl")
	require.NotNil(t, r)
	cases := []struct{ in, want string }{
		{`"disqusUrl":"http://localhost:8000/wpis/"`, `"disqusUrl":"https://olamundo.pl/wpis/"`},
		{`sharer.php?u=http%3A%2F%2Flocalhost%3A8000%2Fwpis%2F`, `sharer.php?u=https%3A%2F%2Folamundo.pl%2Fwpis%2F`},
		{`sharer.php?u=http%3a%2f%2flocalhost%3a8000%2fwpis%2f`, `sharer.php?u=https%3a%2f%2folamundo.pl%2fwpis%2f`},
		{`{"url":"http:\/\/localhost:8000\/wpis\/"}`, `{"url":"https:\/\/olamundo.pl\/wpis\/"}`},
		{`"http://localhost:8000/wpis"`, `"https://olamundo.pl/wpis"`},
		{`src="//localhost:8000/x.js"`, `src="//olamundo.pl/x.js"`},
		{`<loc>http://localhost:8000/</loc>`, `<loc>https://olamundo.pl/</loc>`},
		{`no address here`, `no address here`},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, r.rewrite(tc.in, "page"), tc.in)
	}
	assert.Empty(t, r.leaks, "every known form must be rewritten")

	// A longer port or host is a different address: not rewritten, and
	// reported because it still names the source host.
	assert.Equal(t, "http://localhost:80001/", r.rewrite("http://localhost:80001/", "p2"))
	assert.Equal(t, map[string]bool{"p2": true}, r.leaks)
}

func TestSourceRewriter_OffWhenCrawlingPublicSite(t *testing.T) {
	assert.Nil(t, newSourceRewriter("https://olamundo.pl", "https://olamundo.pl"))
	assert.Nil(t, newSourceRewriter("https://olamundo.pl", ""))
	c := &Config{}
	assert.Equal(t, "http://localhost:8000/", c.ToPublicText("http://localhost:8000/", "x"))
}

// runCopy crawls an httptest site as a copy of the public site siteURL.
func runCopy(t *testing.T, page string) (dir, host string, err error) {
	t.Helper()
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		src := ts.URL
		enc := strings.NewReplacer(":", "%3A", "/", "%2F").Replace(src)
		fill := func(s string) string {
			return strings.NewReplacer("{SRC}", src, "{ENC}", enc, "{JSON}", strings.ReplaceAll(src, "/", `\/`),
				"{HOST}", strings.TrimPrefix(src, "http://")).Replace(s)
		}
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, fill(page))
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, fill(`/* {SRC}/ */ body{color:red}`))
		case "/sitemap.xml":
			w.Header().Set("Content-Type", "application/xml")
			fmt.Fprint(w, fill(`<urlset><url><loc>{SRC}/</loc></url></urlset>`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ts.Close)
	dir = t.TempDir()
	require.NoError(t, Init(ts.URL, "https://example.org", dir, false, "MirrolaTest/1.0", 5, 0, 100, 4, 0))
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return dir, strings.TrimPrefix(ts.URL, "http://"), Run(ctx)
}

func readOut(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

func TestRun_CopyOfSiteGetsPublicAddress(t *testing.T) {
	dir, host, err := runCopy(t, `<html><head><link rel="stylesheet" href="{SRC}/style.css"></head><body>
<a href="{SRC}/sitemap.xml">map</a>
<script>var embedVars = {"disqusUrl":"{SRC}/wpis/","postId":"1"};</script>
<script>var cfg = {"url":"{JSON}\/wpis\/"};</script>
<a href="https://www.facebook.com/sharer/sharer.php?u={ENC}%2Fwpis%2F">fb</a>
</body></html>`)
	require.NoError(t, err)

	page := readOut(t, filepath.Join(dir, "index.html"))
	assert.Contains(t, page, `href="/sitemap.xml"`, "links stay relative")
	assert.Contains(t, page, `"disqusUrl":"https://example.org/wpis/"`)
	assert.Contains(t, page, `{"url":"https:\/\/example.org\/wpis\/"}`)
	assert.Contains(t, page, `sharer.php?u=https%3A%2F%2Fexample.org%2Fwpis%2F`)
	assert.Contains(t, readOut(t, filepath.Join(dir, "style.css")), `/* https://example.org/ */`)
	assert.Contains(t, readOut(t, filepath.Join(dir, "sitemap.xml")), `<loc>https://example.org/</loc>`)

	hostname := strings.Split(host, ":")[0]
	for _, f := range []string{"index.html", "style.css", "sitemap.xml"} {
		assert.NotContains(t, readOut(t, filepath.Join(dir, f)), hostname, f)
	}
}

func TestRun_CopyOfSiteFailsOnUnknownForm(t *testing.T) {
	// Double URL-encoding is not a known form: the page is still saved, but
	// the crawl must fail rather than let a localhost address reach Pages.
	dir, _, err := runCopy(t, `<a href="https://x.example/?u=http%253A%252F%252F{HOST}%252F">x</a>`)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "still contain the source address")
	assert.FileExists(t, filepath.Join(dir, "index.html"))
}
