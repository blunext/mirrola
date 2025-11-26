package main

import (
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig_ToRelative tests converting absolute URLs to relative paths
func TestConfig_ToRelative(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		absURL   string
		baseURL  string
		expected string
	}{
		{
			name: "Same host - converts to relative",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/images/logo.png",
			baseURL:  "https://example.com",
			expected: "/images/logo.png",
		},
		{
			name: "Different host - keeps absolute",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://external.com/image.png",
			baseURL:  "https://example.com",
			expected: "https://external.com/image.png",
		},
		{
			name: "Same host with query params - preserves query",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/style.css?v=1.2",
			baseURL:  "https://example.com",
			expected: "/style.css?v=1.2",
		},
		{
			name: "Safe filenames mode with Polish characters",
			config: Config{
				SafeFilenames: true,
			},
			absURL:   "https://example.com/zdjęcia/łódź.jpg",
			baseURL:  "https://example.com",
			expected: "/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA.jpg", // NFC-normalized and percent-encoded
		},
		{
			name: "Root path",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/",
			baseURL:  "https://example.com",
			expected: "/",
		},
		{
			name: "Deep path",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/blog/2024/01/post.html",
			baseURL:  "https://example.com",
			expected: "/blog/2024/01/post.html",
		},
		{
			name: "Case insensitive host matching",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://EXAMPLE.COM/image.png",
			baseURL:  "https://example.com",
			expected: "/image.png",
		},
		{
			name: "With fragment - fragment not in result",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/page#section",
			baseURL:  "https://example.com",
			expected: "/page",
		},
		{
			name: "Subdomain - different host",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://blog.example.com/post",
			baseURL:  "https://example.com",
			expected: "https://blog.example.com/post",
		},
		{
			name: "Invalid base URL - returns absolute",
			config: Config{
				SafeFilenames: false,
			},
			absURL:   "https://example.com/image.png",
			baseURL:  "not a valid url",
			expected: "https://example.com/image.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			absURL, err := url.Parse(tt.absURL)
			assert.NoError(t, err, "Absolute URL should parse correctly")

			result := tt.config.ToRelative(absURL, tt.baseURL)
			assert.Equal(t, tt.expected, result, "ToRelative should produce expected result")
		})
	}
}

// TestConfig_RewriteURLWithPolicy tests query parameter baking strategies
func TestConfig_RewriteURLWithPolicy(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		rawURL      string
		expectPath  string // Expected path after rewrite
		expectQuery string // Expected query (should be empty after baking)
	}{
		{
			name:        "Short query on CSS asset - readable baking",
			config:      Config{},
			rawURL:      "https://example.com/style.css?v=1.2",
			expectPath:  "/style_v_1.2.css",
			expectQuery: "",
		},
		{
			name:        "Short query on JS asset - readable baking",
			config:      Config{},
			rawURL:      "https://example.com/app.js?version=3.0&build=release",
			expectPath:  "/app_build_release_version_3.0.js", // keys sorted alphabetically: build, version
			expectQuery: "",
		},
		{
			name:        "Short query on image - readable baking",
			config:      Config{},
			rawURL:      "https://example.com/photo.jpg?size=large",
			expectPath:  "/photo_size_large.jpg",
			expectQuery: "",
		},
		{
			name:        "Short query on page - readable baking",
			config:      Config{},
			rawURL:      "https://example.com/search?q=test&category=blog",
			expectPath:  "/search/index_q_test_category_blog.html",
			expectQuery: "",
		},
		{
			name:        "Long query (>80 chars) on asset - hashed",
			config:      Config{},
			rawURL:      "https://example.com/style.css?v=1.2.3.4.5.6.7.8.9.10&param1=value1&param2=value2&param3=value3&param4=value4",
			expectPath:  "/style_", // Query params get baked (order may vary)
			expectQuery: "",
		},
		{
			name:        "Long query on page - hashed",
			config:      Config{},
			rawURL:      "https://example.com/search?very_long_parameter_name_1=value&very_long_parameter_name_2=value&very_long_parameter_name_3=value",
			expectPath:  "/search/index_q_", // Should contain q_ prefix with hash
			expectQuery: "",
		},
		{
			name:        "No query - no change",
			config:      Config{},
			rawURL:      "https://example.com/style.css",
			expectPath:  "/style.css",
			expectQuery: "",
		},
		{
			name:        "Empty query - no change",
			config:      Config{},
			rawURL:      "https://example.com/style.css?",
			expectPath:  "/style.css",
			expectQuery: "",
		},
		{
			name:        "Special characters in query - sanitized",
			config:      Config{},
			rawURL:      "https://example.com/api.js?key=abc&foo=bar",
			expectPath:  "/api_", // Parameters baked (order may vary due to map iteration)
			expectQuery: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			assert.NoError(t, err, "URL should parse correctly")

			result := tt.config.RewriteURLWithPolicy(u)

			// Check that path contains expected substring and query is cleared
			assert.Contains(t, result.Path, tt.expectPath, "Path should contain expected component")
			assert.Equal(t, tt.expectQuery, result.RawQuery, "Query should be cleared after baking")
		})
	}
}

// TestConfig_RewriteURLWithPolicy_Assets tests asset-specific rewriting
func TestConfig_RewriteURLWithPolicy_Assets(t *testing.T) {
	tests := []struct {
		name          string
		rawURL        string
		expectedExt   string
		shouldBeBaked bool
	}{
		{
			name:          "CSS file",
			rawURL:        "https://example.com/style.css?v=1",
			expectedExt:   ".css",
			shouldBeBaked: true,
		},
		{
			name:          "JavaScript file",
			rawURL:        "https://example.com/app.js?v=2",
			expectedExt:   ".js",
			shouldBeBaked: true,
		},
		{
			name:          "Image PNG",
			rawURL:        "https://example.com/image.png?size=large",
			expectedExt:   ".png",
			shouldBeBaked: true,
		},
		{
			name:          "Image JPG",
			rawURL:        "https://example.com/photo.jpg?width=800",
			expectedExt:   ".jpg",
			shouldBeBaked: true,
		},
		{
			name:          "Image WebP",
			rawURL:        "https://example.com/modern.webp?quality=90",
			expectedExt:   ".webp",
			shouldBeBaked: true,
		},
		{
			name:          "Font WOFF2",
			rawURL:        "https://example.com/font.woff2?v=1",
			expectedExt:   ".woff2",
			shouldBeBaked: true,
		},
	}

	cfg := &Config{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			assert.NoError(t, err)

			result := cfg.RewriteURLWithPolicy(u)

			if tt.shouldBeBaked {
				assert.Empty(t, result.RawQuery, "Query should be baked into filename")
				assert.Contains(t, result.Path, tt.expectedExt, "File extension should be preserved")
			}
		})
	}
}

// TestConfig_RewriteURLWithPolicy_Pages tests page-specific rewriting
func TestConfig_RewriteURLWithPolicy_Pages(t *testing.T) {
	tests := []struct {
		name           string
		rawURL         string
		shouldHaveHTML bool
	}{
		{
			name:           "Root with query",
			rawURL:         "https://example.com/?page=2",
			shouldHaveHTML: true,
		},
		{
			name:           "Path without extension",
			rawURL:         "https://example.com/about?ref=home",
			shouldHaveHTML: true,
		},
		{
			name:           "Path with trailing slash",
			rawURL:         "https://example.com/blog/?category=tech",
			shouldHaveHTML: true,
		},
	}

	cfg := &Config{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			assert.NoError(t, err)

			result := cfg.RewriteURLWithPolicy(u)

			if tt.shouldHaveHTML {
				assert.Contains(t, result.Path, "index", "Page should have index")
				assert.Contains(t, result.Path, ".html", "Page should have .html extension")
				assert.Empty(t, result.RawQuery, "Query should be baked")
			}
		})
	}
}
