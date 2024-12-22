package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/net/html"
)

// Helper function to initialize rewriteUrl and restore it after the test
func withRewriteUrl(t *testing.T, value bool, testFunc func()) {
	// Backup the original rewriteUrl value
	var originalRewriteUrl bool
	if rewriteUrl != nil {
		originalRewriteUrl = *rewriteUrl
	} else {
		// If rewriteUrl is nil, initialize it with a default value
		originalRewriteUrl = true // Assuming default is true
	}
	// Ensure rewriteUrl points to a test variable
	testRewriteUrl := value
	rewriteUrl = &testRewriteUrl

	// Defer restoration of the original rewriteUrl
	defer func() {
		rewriteUrl = &originalRewriteUrl
	}()

	// Execute the test function
	testFunc()
}

// TestProcessInlineCSS tests the processInlineCSS function using table-driven tests.
func TestProcessInlineCSS(t *testing.T) {
	tests := []struct {
		name        string
		css         string
		currentURL  string
		baseURL     string
		rewriteUrl  bool
		expectedCSS string
		foundLinks  []string
	}{
		{
			name:        "No URLs in CSS",
			css:         "body { margin: 0; padding: 0; }",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  true,
			expectedCSS: "body { margin: 0; padding: 0; }",
			foundLinks:  []string{},
		},
		{
			name:        "Single URL in CSS",
			css:         "background: url('https://example.com/images/bg.png');",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  false,
			expectedCSS: "background: url('/images/bg.png');",
			foundLinks:  []string{"https://example.com/images/bg.png"},
		},
		{
			name:        "Multiple URLs in CSS",
			css:         "background: url('https://example.com/images/bg.png');\nbackground-image: url(\"https://example.com/images/bg2.jpg\");",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  false,
			expectedCSS: "background: url('/images/bg.png');\nbackground-image: url('/images/bg2.jpg');",
			foundLinks: []string{
				"https://example.com/images/bg.png",
				"https://example.com/images/bg2.jpg",
			},
		},
		{
			name:        "URL with Query Parameters",
			css:         "background: url('https://example.com/images/bg.png?version=1.2');",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  true,
			expectedCSS: "background: url('/images/bg_version_1.2.png');",
			foundLinks: []string{
				"https://example.com/images/bg.png?version=1.2",
			},
		},
		{
			name:        "External URL in CSS",
			css:         "background: url('https://external.com/images/bg.png');",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  false,
			expectedCSS: "background: url('https://external.com/images/bg.png');",
			foundLinks:  []string{},
		},
		{
			name:        "Malformed URL in CSS",
			css:         "background: url('ht!tp://[invalid-url]');",
			currentURL:  "https://example.com/page",
			baseURL:     "https://example.com",
			rewriteUrl:  false,
			expectedCSS: "background: url('ht!tp://[invalid-url]');",
			foundLinks:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize rewriteUrl for the test case
			withRewriteUrl(t, tt.rewriteUrl, func() {
				modifiedCSS, found := processInlineCSS(tt.css, tt.currentURL, tt.baseURL)

				assert.Equal(t, tt.expectedCSS, modifiedCSS, "CSS should be modified correctly")
				assert.ElementsMatch(t, tt.foundLinks, found, "Found links should match expected links")
			})
		})
	}
}

// TestProcessInlineStyle tests the processInlineStyle function using table-driven tests.
func TestProcessInlineStyle(t *testing.T) {
	tests := []struct {
		name       string
		style      string
		currentURL string
		baseURL    string
		rewriteUrl bool
		expected   string
		foundLinks []string
	}{
		{
			name:       "No URLs in style",
			style:      "margin: 0; padding: 0;",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: true,
			expected:   "margin: 0; padding: 0;",
			foundLinks: []string{},
		},
		{
			name:       "Single URL in style",
			style:      "background-image: url('https://example.com/images/bg.png');",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: false,
			expected:   "background-image: url('/images/bg.png');",
			foundLinks: []string{"https://example.com/images/bg.png"},
		},
		{
			name:       "Multiple URLs in style",
			style:      "background: url('https://example.com/images/bg.png'); color: red; background-image: url(\"https://example.com/images/bg2.jpg\");",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: false,
			expected:   "background: url('/images/bg.png'); color: red; background-image: url('/images/bg2.jpg');",
			foundLinks: []string{
				"https://example.com/images/bg.png",
				"https://example.com/images/bg2.jpg",
			},
		},
		{
			name:       "URL with Query Parameters",
			style:      "background: url('https://example.com/images/bg.png?version=1.2');",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: true,
			expected:   "background: url('/images/bg_version_1.2.png');",
			foundLinks: []string{
				"https://example.com/images/bg.png?version=1.2",
			},
		},
		{
			name:       "External URL in style",
			style:      "background: url('https://external.com/images/bg.png');",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: false,
			expected:   "background: url('https://external.com/images/bg.png');",
			foundLinks: []string{},
		},
		{
			name:       "Malformed URL in style",
			style:      "background: url('ht!tp://[invalid-url]');",
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: false,
			expected:   "background: url('ht!tp://[invalid-url]');",
			foundLinks: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize rewriteUrl for the test case
			withRewriteUrl(t, tt.rewriteUrl, func() {
				modifiedStyle, found := processInlineStyle(tt.style, tt.currentURL, tt.baseURL)

				assert.Equal(t, tt.expected, modifiedStyle, "Style should be modified correctly")
				assert.ElementsMatch(t, tt.foundLinks, found, "Found links should match expected links")
			})
		})
	}
}

// TestModifyLinks tests the modifyLinks function using table-driven tests.
func TestModifyLinks(t *testing.T) {
	tests := []struct {
		name          string
		htmlInput     string
		currentURL    string
		baseURL       string
		rewriteUrl    bool
		expectedHTML  string
		expectedLinks []string
	}{
		{
			name: "No links in HTML",
			htmlInput: `<html>
				<head><title>Test Page</title></head>
				<body><p>Hello World!</p></body>
			</html>`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			rewriteUrl:    true,
			expectedHTML:  `<html><head><title>Test Page</title></head><body><p>Hello World!</p></body></html>`,
			expectedLinks: []string{},
		},
		{
			name: "Single href link",
			htmlInput: `<html>
				<head><title>Test Page</title></head>
				<body><a href="https://example.com/about">About</a></body>
			</html>`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			rewriteUrl:    false,
			expectedHTML:  `<html><head><title>Test Page</title></head><body><a href="/about">About</a></body></html>`,
			expectedLinks: []string{"https://example.com/about"},
		},
		{
			name: "Multiple href and src links",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
					<link rel="stylesheet" href="https://example.com/css/style.css">
					<script src="https://example.com/js/script.js"></script>
				</head>
				<body>
					<img src="https://example.com/images/logo.png" alt="Logo">
					<a href="https://example.com/contact">Contact</a>
				</body>
			</html>`,
			currentURL:   "https://example.com/page",
			baseURL:      "https://example.com",
			rewriteUrl:   false,
			expectedHTML: `<html><head><title>Test Page</title><link rel="stylesheet" href="/css/style.css"/><script src="/js/script.js"></script></head><body><img src="/images/logo.png" alt="Logo"/><a href="/contact">Contact</a></body></html>`,
			expectedLinks: []string{
				"https://example.com/css/style.css",
				"https://example.com/js/script.js",
				"https://example.com/images/logo.png",
				"https://example.com/contact",
			},
		},
		{
			name: "Links with query parameters",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
					<link rel="stylesheet" href="https://example.com/css/style.css?ver=1.2">
				</head>
				<body>
					<a href="https://example.com/search?q=golang">Search</a>
				</body>
			</html>`,
			currentURL:   "https://example.com/page",
			baseURL:      "https://example.com",
			rewriteUrl:   true,
			expectedHTML: `<html><head><title>Test Page</title><link rel="stylesheet" href="/css/style_ver_1.2.css"/></head><body><a href="/search/index_q_golang.html">Search</a></body></html>`,
			expectedLinks: []string{
				"https://example.com/css/style.css?ver=1.2",
				"https://example.com/search?q=golang",
			},
		},
		{
			name: "External links should not be modified",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
					<link rel="stylesheet" href="https://external.com/css/style.css">
				</head>
				<body>
					<a href="https://external.com/contact">Contact</a>
				</body>
			</html>`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			rewriteUrl:    false,
			expectedHTML:  `<html><head><title>Test Page</title><link rel="stylesheet" href="https://external.com/css/style.css"/></head><body><a href="https://external.com/contact">Contact</a></body></html>`,
			expectedLinks: []string{},
		},
		{
			name: "Malformed URLs should remain unchanged",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
					<link rel="stylesheet" href="ht!tp://[invalid-url]">
				</head>
				<body>
					<a href="ht!tp://[invalid-url]">Broken Link</a>
				</body>
			</html>`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			rewriteUrl:    false,
			expectedHTML:  `<html><head><title>Test Page</title><link rel="stylesheet" href="ht!tp://[invalid-url]"/></head><body><a href="ht!tp://[invalid-url]">Broken Link</a></body></html>`,
			expectedLinks: []string{},
		},
		{
			name: "Style attribute processing",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
				</head>
				<body>
					<div style="background: url(&#39;https://example.com/images/bg.png&#39;);"></div>
				</body>
			</html>`,
			currentURL:   "https://example.com/page",
			baseURL:      "https://example.com",
			rewriteUrl:   false,
			expectedHTML: `<html><head><title>Test Page</title></head><body><div style="background: url(&#39;/images/bg.png&#39;);"></div></body></html>`,
			expectedLinks: []string{
				"https://example.com/images/bg.png",
			},
		},
		{
			name: "Style tag processing",
			htmlInput: `<html>
				<head>
					<title>Test Page</title>
					<style>
						body { background-image: url("https://example.com/images/bg.css.png"); }
					</style>
				</head>
				<body>
					<p>Hello World!</p>
				</body>
			</html>`,
			currentURL: "https://example.com/page",
			baseURL:    "https://example.com",
			rewriteUrl: false,
			expectedHTML: `<html><head><title>Test Page</title><style>
						body { background-image: url('/images/bg.css.png'); }
					</style></head><body><p>Hello World!</p></body></html>`,
			expectedLinks: []string{
				"https://example.com/images/bg.css.png",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize rewriteUrl for the test case
			withRewriteUrl(t, tt.rewriteUrl, func() {
				// Parse the input HTML
				doc, err := html.Parse(strings.NewReader(tt.htmlInput))
				assert.NoError(t, err, "HTML should parse without error")

				// Process the links
				foundLinks := modifyLinks(doc, tt.currentURL, tt.baseURL)

				// Render the modified HTML
				var buf strings.Builder
				err = html.Render(&buf, doc)
				assert.NoError(t, err, "HTML should render without error")

				// Normalize whitespace for comparison
				modifiedHTML := normalizeHTML(buf.String())
				expectedHTML := normalizeHTML(tt.expectedHTML)

				assert.Equal(t, expectedHTML, modifiedHTML, "HTML should be modified correctly")
				assert.ElementsMatch(t, tt.expectedLinks, foundLinks, "Found links should match expected links")
			})
		})
	}
}

// normalizeHTML is a helper function to normalize HTML strings by removing extra whitespace.
// This helps in comparing HTML structures without being affected by formatting differences.
func normalizeHTML(htmlStr string) string {
	// Remove leading and trailing whitespace from each line
	lines := strings.Split(htmlStr, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	// Join lines without any separator
	return strings.Join(lines, "")
}
