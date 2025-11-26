package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLProcessing_SourceElements(t *testing.T) {
	// Setup temp directory
	tempDir := t.TempDir()

	// Setup Config
	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	// Ensure global variables are set
	outputDir = &tempDir
	baseURLStr := "https://example.com"
	baseURL = &baseURLStr
	rewrite := false
	rewriteURL = &rewrite

	// HTML with <picture> and <source> elements
	htmlContent := `
<!DOCTYPE html>
<html>
<head>
	<title>Responsive Images</title>
</head>
<body>
	<picture>
		<source srcset="/images/mobile.jpg 480w, /images/mobile-2x.jpg 960w" media="(max-width: 600px)">
		<source srcset="/images/desktop.jpg 1024w, /images/desktop-2x.jpg 2048w" media="(min-width: 601px)">
		<img src="/images/fallback.jpg" alt="Fallback">
	</picture>
	
	<video>
		<source src="/videos/movie.mp4" type="video/mp4">
		<source src="/videos/movie.webm" type="video/webm">
	</video>
</body>
</html>
`

	// Process HTML
	ctx := context.Background()
	assets, _, err := processHTML(ctx, "https://example.com/page", []byte(htmlContent))
	require.NoError(t, err)

	// Verify all source URLs are extracted
	assert.Contains(t, assets, "https://example.com/images/mobile.jpg")
	assert.Contains(t, assets, "https://example.com/images/mobile-2x.jpg")
	assert.Contains(t, assets, "https://example.com/images/desktop.jpg")
	assert.Contains(t, assets, "https://example.com/images/desktop-2x.jpg")
	assert.Contains(t, assets, "https://example.com/images/fallback.jpg")
	assert.Contains(t, assets, "https://example.com/videos/movie.mp4")
	assert.Contains(t, assets, "https://example.com/videos/movie.webm")
}

func TestHTMLProcessing_ImgSrcset(t *testing.T) {
	tempDir := t.TempDir()

	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	outputDir = &tempDir
	baseURLStr := "https://example.com"
	baseURL = &baseURLStr

	// HTML with img srcset
	htmlContent := `
<!DOCTYPE html>
<html>
<body>
	<img src="/small.jpg" 
	     srcset="/small.jpg 300w, /medium.jpg 600w, /large.jpg 1200w"
	     alt="Test">
</body>
</html>
`

	ctx := context.Background()
	assets, _, err := processHTML(ctx, "https://example.com/", []byte(htmlContent))
	require.NoError(t, err)

	// Verify srcset URLs are extracted
	assert.Contains(t, assets, "https://example.com/small.jpg")
	assert.Contains(t, assets, "https://example.com/medium.jpg")
	assert.Contains(t, assets, "https://example.com/large.jpg")
}

func TestHTMLProcessing_AudioSource(t *testing.T) {
	tempDir := t.TempDir()

	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	outputDir = &tempDir
	baseURLStr := "https://example.com"
	baseURL = &baseURLStr

	htmlContent := `
<html>
<body>
	<audio controls>
		<source src="/audio/song.mp3" type="audio/mpeg">
		<source src="/audio/song.ogg" type="audio/ogg">
	</audio>
</body>
</html>
`

	ctx := context.Background()
	assets, _, err := processHTML(ctx, "https://example.com/", []byte(htmlContent))
	require.NoError(t, err)

	assert.Contains(t, assets, "https://example.com/audio/song.mp3")
	assert.Contains(t, assets, "https://example.com/audio/song.ogg")
}
