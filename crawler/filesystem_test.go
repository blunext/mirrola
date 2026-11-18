package crawler

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
			name: "Polish characters in directory and file name",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/zdjęcia/łódź.jpg",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjecia/lodz.jpg",
		},
		{
			name: "Transliteration mode with Polish characters",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/zdjęcia/łódź.jpg",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjecia/lodz.jpg",
		},
		{
			name: "All Polish diacritics - lowercase",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/ąćęłńóśźż.txt",
			contentType: "text/plain",
			expected:    "/tmp/acelnoszz.txt",
		},
		{
			name: "All Polish diacritics - uppercase",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/ĄĆĘŁŃÓŚŹŻ.txt",
			contentType: "text/plain",
			expected:    "/tmp/ACELNOSZZ.txt",
		},
		{
			name: "Query string is not part of the file name",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/style.css?v=1.2",
			contentType: "text/css",
			expected:    "/tmp/style.css",
		},
		{
			name: "HTML page without extension",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/about",
			contentType: "text/html",
			expected:    "/tmp/about/index.html",
		},
		{
			name: "HTML page with trailing slash",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/products/",
			contentType: "text/html",
			expected:    "/tmp/products/index.html",
		},
		{
			name: "Root path",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/",
			contentType: "text/html",
			expected:    "/tmp/index.html",
		},
		{
			name: "Image with deep path",
			config: Config{
				OutputDir: "/var/www",
			},
			url:         "https://example.com/assets/images/logos/company.png",
			contentType: "image/png",
			expected:    "/var/www/assets/images/logos/company.png",
		},
		{
			name: "File without extension - adds extension from content type",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/image",
			contentType: "image/jpeg",
			expected:    "/tmp/image.jpg",
		},
		{
			name: "Polish characters + query params (query not in file name)",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/zdjęcie.jpg?v=1",
			contentType: "image/jpeg",
			expected:    "/tmp/zdjecie.jpg",
		},
		{
			name: "URL without scheme - treated as path",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "example.com/page",
			contentType: "text/html",
			expected:    "/tmp/example.com/page/index.html",
		},
		{
			name: "WebP image",
			config: Config{
				OutputDir: "/tmp",
			},
			url:         "https://example.com/photo.webp",
			contentType: "image/webp",
			expected:    "/tmp/photo.webp",
		},
		{
			name: "WOFF2 font",
			config: Config{
				OutputDir: "/tmp",
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
			name:     "Transliteration mode - Polish lowercase",
			config:   Config{},
			urlPath:  "/zdjęcia/łódź",
			expected: "/zdjecia/lodz",
		},
		{
			name:     "Transliteration mode - Polish uppercase",
			config:   Config{},
			urlPath:  "/ZDJĘCIA/ŁÓDŹ",
			expected: "/ZDJECIA/LODZ",
		},
		{
			name:     "All Polish characters",
			config:   Config{},
			urlPath:  "/ąćęłńóśźż/ĄĆĘŁŃÓŚŹŻ",
			expected: "/acelnoszz/ACELNOSZZ",
		},
		{
			name:     "Mixed with regular characters",
			config:   Config{},
			urlPath:  "/café-łódź-berlin",
			expected: "/cafe-lodz-berlin",
		},
		{
			name:     "Path with numbers and symbols",
			config:   Config{},
			urlPath:  "/artykuł-123_test",
			expected: "/artykul-123_test",
		},
		{
			name:     "Empty path",
			config:   Config{},
			urlPath:  "",
			expected: "",
		},
		{
			name:     "Root slash",
			config:   Config{},
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

// Helper function for tests
func parseAndNormalizeURL(rawURL string) (*url.URL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	return u, nil
}
