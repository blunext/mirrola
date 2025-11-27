package crawler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtForContentType(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		wantExt     string
		wantOk      bool
	}{
		// Built-in mappings
		{"JPEG", "image/jpeg", ".jpg", true},
		{"PNG", "image/png", ".png", true},
		{"GIF", "image/gif", ".gif", true},
		{"WebP", "image/webp", ".webp", true},
		{"SVG", "image/svg+xml", ".svg", true},
		{"WOFF", "font/woff", ".woff", true},
		{"WOFF2", "font/woff2", ".woff2", true},
		{"JSON", "application/json", ".json", true},
		{"PDF", "application/pdf", ".pdf", true},

		// Fallback to mime.ExtensionsByType (returns first extension, may vary by system)
		{"Plain text", "text/plain", "", true}, // May be .txt, .conf, .text, etc.
		{"HTML", "text/html", "", true},        // May be .html or .htm
		{"CSS", "text/css", ".css", true},
		{"XML", "application/xml", "", true}, // May be .xml, .xsl, etc.

		// Note: application/javascript may not have mime mapping on all systems
		{"JavaScript", "application/javascript", "", false},

		// Unknown content type
		{"Unknown", "application/x-unknown", "", false},
		{"Empty", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext, ok := extForContentType(tt.contentType)
			assert.Equal(t, tt.wantOk, ok)
			// Only check exact ext for built-in mappings (not mime fallback)
			if tt.wantOk && tt.wantExt != "" {
				assert.Equal(t, tt.wantExt, ext)
			}
			// For fallback cases, just verify we got something if expected
			if tt.wantOk && tt.wantExt == "" {
				assert.NotEmpty(t, ext, "Should return non-empty extension")
			}
		})
	}
}

func TestAddMissingExtension_WithContentType(t *testing.T) {
	tests := []struct {
		name        string
		pathPart    string
		contentType string
		expected    string
	}{
		{"Image without ext", "/photo", "image/jpeg", "/photo.jpg"},
		{"Image with ext", "/photo.jpg", "image/jpeg", "/photo.jpg"},
		{"CSS without ext", "/style", "text/css", "/style.css"},
		{"JS without ext", "/app", "application/javascript", "/app.js"},
		{"HTML without ext", "/page", "text/html", "/page/index.html"},
		{"HTML root", "/", "text/html", "/index.html"},
		{"HTML empty", "", "text/html", "/index.html"},
		{"Unknown content type", "/file", "unknown/type", "/file"},
		{"WebP", "/image", "image/webp", "/image.webp"},
		{"WOFF2 font", "/font", "font/woff2", "/font.woff2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := addMissingExtension(tt.pathPart, tt.contentType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
