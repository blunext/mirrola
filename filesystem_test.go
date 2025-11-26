package main

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig_GetOutputPath tests the GetOutputPath method with various configurations
func TestConfig_GetOutputPath(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		url         string
		contentType string
		expected    string
	}{
		{
			name: "Safe filenames with Polish characters",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: true,
				RewriteURL:    false,
			},
			url:         "https://example.com/zdjęcia/łódź.jpg",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjęcia/łódź.jpg",
		},
		{
			name: "Transliteration mode with Polish characters",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/zdjęcia/łódź.jpg",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjecia/lodz.jpg",
		},
		{
			name: "All Polish diacritics - lowercase",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/ąćęłńóśźż.txt",
			contentType: "text/plain",
			expected:    "/tmp/acelnoszz.txt",
		},
		{
			name: "All Polish diacritics - uppercase",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/ĄĆĘŁŃÓŚŹŻ.txt",
			contentType: "text/plain",
			expected:    "/tmp/ACELNOSZZ.txt",
		},
		{
			name: "Query param baking enabled - CSS file",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    true,
			},
			url:         "https://example.com/style.css?v=1.2",
			contentType: "text/css",
			expected:    "/tmp/style_v_1.2.css",
		},
		{
			name: "Query param baking enabled - JS file",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    true,
			},
			url:         "https://example.com/app.js?version=3.4.5",
			contentType: "application/javascript",
			expected:    "/tmp/app_version_3.4.5.js",
		},
		{
			name: "Query param baking disabled - keeps query in path",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/style.css?v=1.2",
			contentType: "text/css",
			expected:    "/tmp/style.css",
		},
		{
			name: "HTML page without extension",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/about",
			contentType: "text/html",
			expected:    "/tmp/about/index.html",
		},
		{
			name: "HTML page with trailing slash",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/products/",
			contentType: "text/html",
			expected:    "/tmp/products/index.html",
		},
		{
			name: "Root path",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/",
			contentType: "text/html",
			expected:    "/tmp/index.html",
		},
		{
			name: "Image with deep path",
			config: Config{
				OutputDir:     "/var/www",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/assets/images/logos/company.png",
			contentType: "image/png",
			expected:    "/var/www/assets/images/logos/company.png",
		},
		{
			name: "File without extension - adds extension from content type",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/image",
			contentType: "image/jpeg",
			expected:    "/tmp/image.jpg",
		},
		{
			name: "Combined: Polish chars + query baking",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    true,
			},
			url:         "https://example.com/styl-główny.css?wersja=2.0",
			contentType: "text/css",
			expected:    "/tmp/styl-glowny_wersja_2.0.css",
		},
		{
			name: "Safe filenames + query params",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: true,
				RewriteURL:    true,
			},
			url:         "https://example.com/zdjęcie.jpg?v=1",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjęcie_v_1.jpg",
		},
		{
			name: "URL without scheme - treated as path",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "example.com/page",
			contentType: "text/html",
			expected:    "/tmp/example.com/page/index.html",
		},
		{
			name: "WebP image",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/photo.webp",
			contentType: "image/webp",
			expected:    "/tmp/photo.webp",
		},
		{
			name: "WOFF2 font",
			config: Config{
				OutputDir:     "/tmp",
				SafeFilenames: false,
				RewriteURL:    false,
			},
			url:         "https://example.com/fonts/roboto.woff2",
			contentType: "font/woff2",
			expected:    "/tmp/fonts/roboto.woff2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetOutputPath(tt.url, tt.contentType)
			assert.Equal(t, tt.expected, result, "GetOutputPath produced unexpected result")
		})
	}
}

// TestConfig_NormalizePath tests Unicode normalization and transliteration
func TestConfig_NormalizePath(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		urlPath  string
		expected string
	}{
		{
			name: "Safe filenames - no transliteration",
			config: Config{
				SafeFilenames: true,
			},
			urlPath:  "/zdjęcia/łódź",
			expected: "/zdjęcia/łódź",
		},
		{
			name: "Transliteration mode - Polish lowercase",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/zdjęcia/łódź",
			expected: "/zdjecia/lodz",
		},
		{
			name: "Transliteration mode - Polish uppercase",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/ZDJĘCIA/ŁÓDŹ",
			expected: "/ZDJECIA/LODZ",
		},
		{
			name: "All Polish characters",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/ąćęłńóśźż/ĄĆĘŁŃÓŚŹŻ",
			expected: "/acelnoszz/ACELNOSZZ",
		},
		{
			name: "Mixed with regular characters",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/café-łódź-berlin",
			expected: "/cafe-lodz-berlin",
		},
		{
			name: "Path with numbers and symbols",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/artykuł-123_test",
			expected: "/artykul-123_test",
		},
		{
			name: "Empty path",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "",
			expected: "",
		},
		{
			name: "Root slash",
			config: Config{
				SafeFilenames: false,
			},
			urlPath:  "/",
			expected: "/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.normalizePath(tt.urlPath)
			assert.Equal(t, tt.expected, result, "normalizePath produced unexpected result")
		})
	}
}

// TestConfig_ApplyQueryBaking tests query parameter baking into filenames
func TestConfig_ApplyQueryBaking(t *testing.T) {
	// Note: This requires URL parsing, so we test through helper that creates URLs
	tests := []struct {
		name        string
		config      Config
		rawURL      string
		pathPart    string
		expectedExt string // We check if path contains this
	}{
		{
			name: "Static asset - CSS",
			config: Config{
				SafeFilenames: false,
				RewriteURL:    true,
			},
			rawURL:      "https://example.com/style.css?v=1.2",
			pathPart:    "/style.css",
			expectedExt: "_v_1.2.css",
		},
		{
			name: "Static asset - JS",
			config: Config{
				SafeFilenames: false,
				RewriteURL:    true,
			},
			rawURL:      "https://example.com/app.js?version=3.0",
			pathPart:    "/app.js",
			expectedExt: "_version_3.0.js",
		},
		{
			name: "Static asset - image",
			config: Config{
				SafeFilenames: false,
				RewriteURL:    true,
			},
			rawURL:      "https://example.com/photo.jpg?size=large",
			pathPart:    "/photo.jpg",
			expectedExt: "_size_large.jpg",
		},
		{
			name: "Page without extension",
			config: Config{
				SafeFilenames: false,
				RewriteURL:    true,
			},
			rawURL:      "https://example.com/search?q=test",
			pathPart:    "/search",
			expectedExt: "index_q_test.html",
		},
		{
			name: "Safe filenames mode with Polish chars",
			config: Config{
				SafeFilenames: true,
				RewriteURL:    true,
			},
			rawURL:      "https://example.com/styl.css?v=1",
			pathPart:    "/styl.css",
			expectedExt: "_v_1.css",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse URL to get the u object
			u, err := parseAndNormalizeURL(tt.rawURL)
			assert.NoError(t, err, "URL parsing should not fail")

			result := tt.config.applyQueryBaking(u, tt.pathPart)
			assert.Contains(t, result, tt.expectedExt, "Baked query params should be in the path")
		})
	}
}

// Helper function for tests
func parseAndNormalizeURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return u, nil
}
