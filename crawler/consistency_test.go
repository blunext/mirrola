package crawler

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/text/unicode/norm"
)

// servedFile simulates how a static host (Cloudflare Pages, server/main.go)
// resolves a root-relative link from the HTML to a file: the browser sends the
// link percent-encoded, the server decodes the path, ignores the query and
// serves index.html for directory-like paths.
func servedFile(outputDir, link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	p := u.Path
	if strings.HasSuffix(p, "/") {
		p += "index.html"
	} else if path.Ext(p) == "" {
		p += "/index.html"
	}
	return filepath.Join(outputDir, strings.TrimLeft(p, "/"))
}

// TestLinkMatchesWrittenFile guards the invariant that ToRelative and
// GetOutputPath agree: the link written into HTML/CSS must resolve to the very
// file the crawler saved for that URL. This was broken for Polish characters:
// files were transliterated (lodz.jpg) while links kept diacritics (łódź.jpg).
func TestLinkMatchesWrittenFile(t *testing.T) {
	const base = "https://example.com"
	c := &Config{BaseURL: base, OutputDir: "/out"}

	nfd := func(s string) string { return norm.NFD.String(s) }
	cases := []struct {
		name, link, contentType string
	}{
		{"plain asset", "/wp-content/uploads/2020/04/photo.jpg", "image/jpeg"},
		{"polish NFC", "/wp-content/uploads/2020/04/niedźwiedź.jpg", "image/jpeg"},
		{"polish NFD (as stored on olamundo.pl)", nfd("/wp-content/uploads/2020/04/niedźwiedź.jpg"), "image/jpeg"},
		{"polish NFD percent-encoded", "/wp-content/uploads/2020/04/niedz%CC%81wiedz%CC%81-500x333.jpg", "image/jpeg"},
		{"ł does not decompose", "/zdjęcia/Łódź-ŁÓDŹ.png", "image/png"},
		{"all polish letters", "/ąćęłńóśźż-ĄĆĘŁŃÓŚŹŻ.jpg", "image/jpeg"},
		{"en dash kept as is", "/wp-content/uploads/2014/12/caravaggio-–-wieczerza.jpg", "image/jpeg"},
		{"space in name", "/uploads/my photo.jpg", "image/jpeg"},
		{"css with version query", "/wp-content/themes/t/style.css?ver=6.7", "text/css"},
		{"page with trailing slash", "/2020/04/wpis-o-niedźwiedziu/", "text/html"},
		{"page without trailing slash", "/o-mnie", "text/html"},
		{"root", "/", "text/html"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Same pipeline as the HTML/CSS processors: resolve, fixPath, ToRelative.
			abs, err := resolveURL(base+"/some/page/", tc.link)
			require.NoError(t, err)
			written := c.GetOutputPath(abs.String(), tc.contentType)

			fixPath(abs)
			rel := c.ToRelative(abs, base)

			assert.Equal(t, written, servedFile(c.OutputDir, rel),
				"link %q must resolve to the written file", rel)
		})
	}
}

func TestClaimOutput(t *testing.T) {
	newCfg := func(strict bool) *Config {
		return &Config{Strict: strict, outputs: newOutputRegistry()}
	}
	nfcURL := "https://example.com/uploads/niedźwiedź.jpg"
	nfdURL := "https://example.com/uploads/niedz%CC%81wiedz%CC%81.jpg"
	otherURL := "https://example.com/uploads/niedzwiedz.jpg"

	t.Run("first claim writes", func(t *testing.T) {
		c := newCfg(false)
		write, err := c.claimOutput("/out/a.jpg", nfcURL)
		require.NoError(t, err)
		assert.True(t, write)
	})

	t.Run("NFC and NFD spelling of the same URL is not a collision", func(t *testing.T) {
		c := newCfg(true)
		_, _ = c.claimOutput("/out/a.jpg", nfcURL)
		write, err := c.claimOutput("/out/a.jpg", nfdURL)
		require.NoError(t, err)
		assert.False(t, write, "already written, skip duplicate")
		assert.Empty(t, c.Collisions())
	})

	t.Run("host with and without trailing slash is not a collision", func(t *testing.T) {
		c := newCfg(true)
		_, _ = c.claimOutput("/out/index.html", "https://example.com")
		write, err := c.claimOutput("/out/index.html", "https://example.com/")
		require.NoError(t, err)
		assert.False(t, write)
		assert.Empty(t, c.Collisions())
	})

	t.Run("different URLs on the same path warn and keep the first", func(t *testing.T) {
		c := newCfg(false)
		_, _ = c.claimOutput("/out/a.jpg", nfcURL)
		write, err := c.claimOutput("/out/a.jpg", otherURL)
		require.NoError(t, err)
		assert.False(t, write)
		require.Len(t, c.Collisions(), 1)
		assert.Contains(t, c.Collisions()[0], "/out/a.jpg")
	})

	t.Run("query-only difference is a collision (query is not in the file name)", func(t *testing.T) {
		c := newCfg(false)
		_, _ = c.claimOutput("/out/style.css", "https://example.com/style.css?ver=1")
		write, _ := c.claimOutput("/out/style.css", "https://example.com/style.css?ver=2")
		assert.False(t, write)
		assert.Len(t, c.Collisions(), 1)
	})

	t.Run("strict mode returns an error", func(t *testing.T) {
		c := newCfg(true)
		_, _ = c.claimOutput("/out/a.jpg", nfcURL)
		_, err := c.claimOutput("/out/a.jpg", otherURL)
		var collision *errOutputCollision
		require.ErrorAs(t, err, &collision)
		assert.Equal(t, "/out/a.jpg", collision.Path)
	})

	t.Run("no registry disables tracking", func(t *testing.T) {
		c := &Config{}
		for range 2 {
			write, err := c.claimOutput("/out/a.jpg", nfcURL)
			require.NoError(t, err)
			assert.True(t, write)
		}
	})
}

func TestClaimOutput_Concurrent(t *testing.T) {
	c := &Config{outputs: newOutputRegistry()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	writers := 0
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// half NFC, half NFD: all the same resource
			link := "https://example.com/łódź.jpg"
			if i%2 == 1 {
				link = "https://example.com/" + norm.NFD.String("łódź.jpg")
			}
			if write, _ := c.claimOutput("/out/lodz.jpg", link); write {
				mu.Lock()
				writers++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, 1, writers, "exactly one goroutine may write the file")
	assert.Empty(t, c.Collisions())
}

func TestWriteFile_ConcurrentSamePath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "sub", "file.txt")

	contents := make([]string, 20)
	for i := range contents {
		contents[i] = strings.Repeat(fmt.Sprintf("writer-%02d;", i), 5000)
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(contents))
	for _, content := range contents {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- writeFile(target, strings.NewReader(content))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, contents, string(got), "file must hold one complete write, not interleaved data")

	info, err := os.Stat(target)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm())

	entries, err := os.ReadDir(filepath.Dir(target))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp files may be left behind")
}

// TestGetOutputPath_StaysInOutputDir: paths that only turn into ".." after
// transliteration ("..\u0301"), or carry "/../" in the query, must not escape
// OutputDir.
func TestGetOutputPath_StaysInOutputDir(t *testing.T) {
	links := []string{
		"https://example.com/a/../../etc/x.jpg",
		"https://example.com/a/%2E%2E/%2E%2E/etc/x.jpg",
		"https://example.com/a/..%CC%81/..%CC%81/..%CC%81/etc/x.jpg",
		"https://example.com/style.css?x=/../../../../tmp/evil",
		"https://example.com/page?x=/../../../../tmp/evil",
	}
	c := &Config{OutputDir: "/out"}
	for _, l := range links {
		abs, err := resolveURL("https://example.com/", l)
		require.NoError(t, err)
		for _, ct := range []string{"image/jpeg", "text/css", "text/html"} {
			got := c.GetOutputPath(abs.String(), ct)
			rel, err := filepath.Rel(c.OutputDir, got)
			require.NoError(t, err)
			assert.False(t, rel == ".." || strings.HasPrefix(rel, "../"),
				"%s (%s) escaped OutputDir: %s", l, ct, got)
		}
	}
}
