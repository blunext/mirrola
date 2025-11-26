package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig_ProcessInlineJS tests processing inline JavaScript for Simple Lightbox
func TestConfig_ProcessInlineJS(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		jsContent     string
		currentURL    string
		baseURL       string
		expectedJS    string
		expectedFound []string
	}{
		{
			name: "Simple HTTP URL with escaped slashes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var img = "https:\/\/example.com\/images\/photo.jpg";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var img = "\/images\/photo.jpg";`,
			expectedFound: []string{"https://example.com/images/photo.jpg"},
		},
		{
			name: "HTTPS URL with escaped slashes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var url = "https:\/\/example.com\/assets\/script.js";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "\/assets\/script.js";`,
			expectedFound: []string{"https://example.com/assets/script.js"},
		},
		{
			name: "Multiple URLs in one script",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent: `var img1 = "https:\/\/example.com\/img1.jpg";
var img2 = "https:\/\/example.com\/img2.jpg";`,
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			expectedJS: `var img1 = "\/img1.jpg";
var img2 = "\/img2.jpg";`,
			expectedFound: []string{
				"https://example.com/img1.jpg",
				"https://example.com/img2.jpg",
			},
		},
		{
			name: "External URL - should not be modified",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var cdn = "https:\/\/cdn.example.com\/library.js";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var cdn = "https://cdn.example.com/library.js";`, // Slashes unescaped but URL not modified
			expectedFound: []string{},
		},
		{
			name: "Unicode escapes \\u0026 (ampersand)",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var url = "https:\/\/example.com\/page?foo=1\u0026bar=2";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "\/page?foo=1&bar=2";`,
			expectedFound: []string{"https://example.com/page?foo=1&bar=2"},
		},
		{
			name: "Unicode escapes \\u002F (forward slash)",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var url = "https:\u002F\u002Fexample.com\u002Fimage.png";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "\/image.png";`,
			expectedFound: []string{"https://example.com/image.png"},
		},
		{
			name: "Query parameters with rewrite enabled",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: true,
			},
			jsContent:     `var img = "https:\/\/example.com\/photo.jpg?v=1.0";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var img = "\/photo_v_1.0.jpg";`,
			expectedFound: []string{"https://example.com/photo.jpg?v=1.0"},
		},
		{
			name: "Simple Lightbox specific pattern",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent: `(function(){
var slb_url = "https:\/\/example.com\/wp-content\/uploads\/image.jpg";
})();`,
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			expectedJS: `(function(){
var slb_url = "\/wp-content\/uploads\/image.jpg";
})();`,
			expectedFound: []string{"https://example.com/wp-content/uploads/image.jpg"},
		},
		{
			name: "Mixed escaped and unescaped slashes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var url = "https:\/\/example.com/images/photo.jpg";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "\/images\/photo.jpg";`,
			expectedFound: []string{"https://example.com/images/photo.jpg"},
		},
		{
			name: "Polish characters in URL",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var img = "https:\/\/example.com\/zdjęcia\/łódź.jpg";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var img = "\/zdjęcia\/łódź.jpg";`,
			expectedFound: []string{"https://example.com/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA.jpg"}, // URL gets percent-encoded
		},
		{
			name: "No URLs in JS",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var x = 5; console.log("Hello World");`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var x = 5; console.log("Hello World");`,
			expectedFound: []string{},
		},
		{
			name: "HTTP (not HTTPS) URL",
			config: Config{
				BaseURL:    "http://example.com",
				RewriteURL: false,
			},
			jsContent:     `var img = "http:\/\/example.com\/image.png";`,
			currentURL:    "http://example.com/page",
			baseURL:       "http://example.com",
			expectedJS:    `var img = "\/image.png";`,
			expectedFound: []string{"http://example.com/image.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultJS, found := tt.config.ProcessInlineJS(tt.jsContent, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedJS, resultJS, "JavaScript should be rewritten correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}

// TestConfig_DecodeUnicodeEscapes tests Unicode escape sequence decoding
func TestConfig_DecodeUnicodeEscapes(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Ampersand \\u0026",
			input:    "foo\u0026bar",
			expected: "foo&bar",
		},
		{
			name:     "Forward slash \\u002F",
			input:    "path\u002Fto\u002Ffile",
			expected: "path/to/file",
		},
		{
			name:     "Polish ł \\u0142",
			input:    "\u0142ód\u017A",
			expected: "łódź",
		},
		{
			name:     "Multiple escapes",
			input:    "\u0068\u0065\u006C\u006C\u006F", // "hello"
			expected: "hello",
		},
		{
			name:     "No escapes",
			input:    "plain text",
			expected: "plain text",
		},
		{
			name:     "Mixed escaped and unescaped",
			input:    "hello\u0026world",
			expected: "hello&world",
		},
		{
			name:     "Invalid escape sequence - kept as-is",
			input:    `\uZZZZ`,
			expected: `\uZZZZ`,
		},
		{
			name:     "Quote mark \\u0022",
			input:    "Hello\u0022World\u0022",
			expected: `Hello"World"`,
		},
	}

	cfg := &Config{}
	cfg.InitRegexps()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cfg.decodeUnicodeEscapes(tt.input)
			assert.Equal(t, tt.expected, result, "Unicode escapes should be decoded correctly")
		})
	}
}

// TestConfig_ProcessInlineJS_EdgeCases tests edge cases
func TestConfig_ProcessInlineJS_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		jsContent     string
		currentURL    string
		baseURL       string
		expectedJS    string
		expectedFound []string
	}{
		{
			name: "Empty JavaScript",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     "",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    "",
			expectedFound: []string{},
		},
		{
			name: "Malformed URL - missing protocol",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `var url = "example.com/image.jpg";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "example.com/image.jpg";`,
			expectedFound: []string{},
		},
		{
			name: "URL in comment - should not be processed",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			jsContent:     `// https://example.com/image.jpg`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `// \/image.jpg`,
			expectedFound: []string{"https://example.com/image.jpg"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultJS, found := tt.config.ProcessInlineJS(tt.jsContent, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedJS, resultJS, "JavaScript should be handled correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}
