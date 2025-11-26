package main

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
		BaseURL:       "https://example.com",
		OutputDir:     tempDir,
		RewriteURL:    true,
		SafeFilenames: false,
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
