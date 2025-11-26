package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTMLProcessing_RemoveStyleEdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	cfg = &Config{
		BaseURL:   "https://example.com",
		OutputDir: tempDir,
	}
	cfg.InitRegexps()

	tests := []struct {
		name         string
		html         string
		shouldFind   string // String that should be in output
		shouldntFind string // String that should NOT be in output
	}{
		{
			name: "Remove wp-emoji-styles by ID",
			html: `<html><head>
				<style id="wp-emoji-styles-inline-css">
					img.wp-smiley, img.emoji { display: inline !important; }
				</style>
				<style>body { color: red; }</style>
			</head></html>`,
			shouldFind:   "body { color: red; }",
			shouldntFind: "wp-smiley",
		},
		{
			name: "Remove emoji CSS by content",
			html: `<html><head>
				<style>
					img.wp-smiley, img.emoji { border: none; }
				</style>
				<style>h1 { font-size: 2em; }</style>
			</head></html>`,
			shouldFind:   "h1 { font-size: 2em; }",
			shouldntFind: "img.wp-smiley",
		},
		{
			name: "Keep normal styles",
			html: `<html><head>
				<style>.container { max-width: 1200px; }</style>
			</head></html>`,
			shouldFind:   "container",
			shouldntFind: "",
		},
		{
			name: "Multiple wp-emoji styles",
			html: `<html><head>
				<style id="wp-emoji-styles-css">img.emoji { height: 1em; }</style>
				<style id="wp-emoji-styles-inline-css">img.wp-smiley { width: 1em; }</style>
				<style>.normal { color: blue; }</style>
			</head></html>`,
			shouldFind:   "normal",
			shouldntFind: "emoji",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			_, _, err := processHTML(ctx, "https://example.com/", []byte(tt.html))
			require.NoError(t, err)

			// Read saved file
			savedPath := filepath.Join(tempDir, "index.html")
			content, err := os.ReadFile(savedPath)
			require.NoError(t, err)

			contentStr := string(content)
			if tt.shouldFind != "" {
				assert.Contains(t, contentStr, tt.shouldFind)
			}
			if tt.shouldntFind != "" {
				assert.NotContains(t, contentStr, tt.shouldntFind)
			}
		})
	}
}
