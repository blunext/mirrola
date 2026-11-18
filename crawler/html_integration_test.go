package crawler

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLProcessing_CompleteFlow(t *testing.T) {
	// Setup temp directory for output
	tempDir := t.TempDir()

	// Setup Config
	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	// Ensure global variables are set (legacy support if needed, though we migrated most)

	// Sample HTML content
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
	<base href="https://example.com/page/">
	<link rel="stylesheet" href="style.css">
	<script src="script.js"></script>
</head>
<body>
	<h1>Hello World</h1>
	<img src="image.jpg" alt="Test Image">
	<a href="about.html">About</a>
	<a href="https://google.com">External</a>
</body>
</html>
`

	// Process HTML
	ctx := context.Background()
	assets, links, err := processHTML(ctx, "https://example.com/page/index.html", []byte(htmlContent))

	// Verify no error
	require.NoError(t, err)

	// Verify assets found (style.css, script.js, image.jpg)
	// Note: processHTML resolves URLs based on base href
	assert.Contains(t, assets, "https://example.com/page/style.css")
	assert.Contains(t, assets, "https://example.com/page/script.js")
	assert.Contains(t, assets, "https://example.com/page/image.jpg")

	// Verify links found (about.html)
	assert.Contains(t, links, "https://example.com/page/about.html")

	// Verify file was saved
	expectedPath := filepath.Join(tempDir, "page/index.html")
	require.FileExists(t, expectedPath)

	// Read saved file and verify content rewriting
	savedContent, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	savedStr := string(savedContent)

	// Check that links are rewritten to absolute paths (relative to root)
	// Base is https://example.com/page/, assets are in same dir, so they should be /page/filename
	assert.Contains(t, savedStr, `href="/page/style.css"`)
	assert.Contains(t, savedStr, `src="/page/script.js"`)
	assert.Contains(t, savedStr, `src="/page/image.jpg"`)
	assert.Contains(t, savedStr, `href="/page/about.html"`)

	// External link should remain absolute
	assert.Contains(t, savedStr, `href="https://google.com"`)
}

func TestHTMLProcessing_WordPressCleanup(t *testing.T) {
	// Setup temp directory
	tempDir := t.TempDir()

	// Setup Config
	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	// Ensure global variables are set

	// HTML with WordPress junk
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
	<link rel="shortlink" href="https://example.com/?p=123">
	<link rel="pingback" href="https://example.com/xmlrpc.php">
	<style id="wp-emoji-styles-inline-css">
		img.wp-smiley, img.emoji { display: inline !important; }
	</style>
	<script src="https://example.com/wp-includes/js/wp-emoji-release.min.js"></script>
	<script src="https://example.com/wp-includes/js/comment-reply.min.js"></script>
</head>
<body>
	<h1>Content</h1>
	<!-- This is a comment -->
	<script>
		window._wpemojiSettings = {"baseUrl":"https:\/\/s.w.org\/images\/core\/emoji\/13.0.0\/72x72\/","ext":".png","svgUrl":"https:\/\/s.w.org\/images\/core\/emoji\/13.0.0\/svg\/","svgExt":".svg","source":{"concatemoji":"https:\/\/example.com\/wp-includes\/js\/wp-emoji-release.min.js?ver=5.7.2"}};
	</script>
</body>
</html>
`

	// Process HTML
	ctx := context.Background()
	_, _, err := processHTML(ctx, "https://example.com/post", []byte(htmlContent))
	require.NoError(t, err)

	// Read saved file
	expectedPath := filepath.Join(tempDir, "post/index.html")
	savedContent, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	savedStr := string(savedContent)

	// Verify junk is removed
	assert.NotContains(t, savedStr, "shortlink")
	assert.NotContains(t, savedStr, "pingback")
	assert.NotContains(t, savedStr, "wp-emoji-styles")
	assert.NotContains(t, savedStr, "wp-emoji-release.min.js")
	assert.NotContains(t, savedStr, "comment-reply.min.js")
	assert.NotContains(t, savedStr, "window._wpemojiSettings")
	assert.NotContains(t, savedStr, "This is a comment")

	// Verify content remains
	assert.Contains(t, savedStr, "<h1>Content</h1>")
}

// TestHTMLProcessing_Fragments: fragments must survive rewriting (footnotes
// like <a href="#_ftn1"> on olamundo.pl), fragment-only references must be
// left as written, and nothing fetched may carry a fragment.
func TestHTMLProcessing_Fragments(t *testing.T) {
	cfg = &Config{BaseURL: "https://example.com", OutputDir: t.TempDir()}
	cfg.InitRegexps()

	page := `<html><head><style>.a{clip-path:url(#clip);background:url('/bg.png#x')}</style></head><body>` +
		`<a href="#_ftn1" name="_ftnref1">[1]</a>` +
		`<a href="#" id="close-canvas-nav">x</a>` +
		`<a href="/post/#comments">c</a>` +
		`<a href="https://example.com/other/#top">o</a>` +
		`<a href="https://example.com">home</a>` +
		`<a href="https://example.com#top">home top</a>` +
		`<svg><use href="#icon"></use></svg>` +
		`<script id="slb_context">var x = {"u":"https:\/\/example.com\/g\/#s"};</script>` +
		`</body></html>`
	assets, links, err := processHTML(context.Background(), "https://example.com/post/", []byte(page))
	require.NoError(t, err)

	out, err := os.ReadFile(filepath.Join(cfg.OutputDir, "post", "index.html"))
	require.NoError(t, err)
	got := string(out)
	for _, want := range []string{
		`href="#_ftn1"`,
		`href="#" id="close-canvas-nav"`,
		`href="/post/#comments"`,
		`href="/other/#top"`,
		`href="/">home</a>`,
		`href="/#top"`,
		`<use href="#icon">`,
		`url(#clip)`,
		`url('/bg.png#x')`,
		`"u":"\/g\/#s"`,
	} {
		assert.Contains(t, got, want)
	}
	assert.NotContains(t, got, `href=""`)

	for _, u := range append(append([]string{}, assets...), links...) {
		assert.NotContains(t, u, "#", "fetched URLs carry no fragment: %s", u)
	}
	assert.Contains(t, links, "https://example.com/other/")
	assert.Contains(t, assets, "https://example.com/bg.png")
}

// TestHTMLProcessing_CanonicalIsAbsoluteOnPublicSite: <link rel="canonical">
// must be a full URL on the public site even when crawling another host (a
// local WordPress), while every other link stays root-relative.
func TestHTMLProcessing_CanonicalIsAbsoluteOnPublicSite(t *testing.T) {
	page := `<html><head>` +
		`<link rel="canonical" href="http://localhost:8000/zdjęcia-łódź/?p=1#top">` +
		`<link rel="stylesheet" href="http://localhost:8000/style.css">` +
		`<link rel="Canonical Alternate" href="http://localhost:8000/other/">` +
		`<link rel="canonical" href="https://elsewhere.example/x/">` +
		`</head><body><a rel="canonical" href="http://localhost:8000/post/">a</a></body></html>`

	cases := []struct{ siteURL, want string }{
		{"https://olamundo.pl", "https://olamundo.pl"},
		{"https://olamundo.pl/", "https://olamundo.pl"},
		{"", "http://localhost:8000"}, // default: the crawled host
	}
	for _, tc := range cases {
		t.Run("site="+tc.siteURL, func(t *testing.T) {
			cfg = &Config{BaseURL: "http://localhost:8000", SiteURL: tc.siteURL, OutputDir: t.TempDir()}
			cfg.InitRegexps()
			_, _, err := processHTML(context.Background(), "http://localhost:8000/post/", []byte(page))
			require.NoError(t, err)
			out, err := os.ReadFile(filepath.Join(cfg.OutputDir, "post", "index.html"))
			require.NoError(t, err)
			got := string(out)
			assert.Contains(t, got, `<link rel="canonical" href="`+tc.want+`/zdjecia-lodz/?p=1#top"/>`, "transliterated like the file, query and fragment kept")
			assert.Contains(t, got, `<link rel="Canonical Alternate" href="`+tc.want+`/other/"/>`, "rel is a token list, case-insensitive")
			assert.Contains(t, got, `href="https://elsewhere.example/x/"`, "a canonical on another host is left alone")
			assert.Contains(t, got, `<link rel="stylesheet" href="/style.css"/>`, "other links stay relative")
			// Attribute order is up to golang.org/x/net/html (it changed in v0.63).
			assert.Regexp(t, `<a (rel="canonical" href="/post/"|href="/post/" rel="canonical")>`, got, "only <link> canonicals are absolute")
		})
	}
}
