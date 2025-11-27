package crawler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetOutputPath_TrailingQuestionMark(t *testing.T) {
	// Setup Config
	cfg := &Config{
		BaseURL:       "https://example.com",
		OutputDir:     "./static",
		RewriteURL:    false,
		SafeFilenames: false,
	}
	cfg.InitRegexps()

	t.Run("URL with trailing ? (empty query)", func(t *testing.T) {
		// This mimics URLs like: https://antyweb.pl/_aw/fonts/font.eot?
		path := cfg.GetOutputPath("https://example.com/fonts/font.eot?", "font/eot")

		// Should remove the trailing ?
		assert.Contains(t, path, "font.eot")
		assert.NotContains(t, path, "?")
	})

	t.Run("URL with normal query params", func(t *testing.T) {
		// Normal query params should work as before
		path := cfg.GetOutputPath("https://example.com/script.js?v=123", "application/javascript")

		// Without rewrite, query params are ignored in filename
		assert.Contains(t, path, "script.js")
		assert.NotContains(t, path, "?")
		assert.NotContains(t, path, "v=123")
	})

	t.Run("URL without query", func(t *testing.T) {
		path := cfg.GetOutputPath("https://example.com/style.css", "text/css")
		assert.Contains(t, path, "style.css")
	})
}
