package crawler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig_ProcessCSSFile tests processing CSS files with URL rewriting
func TestConfig_ProcessCSSFile(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		css           string
		cssURL        string
		baseURL       string
		expectedCSS   string
		expectedFound []string
	}{
		{
			name: "Single url() with single quotes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/images/bg.png');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/images/bg.png');",
			expectedFound: []string{"https://example.com/images/bg.png"},
		},
		{
			name: "Single url() with double quotes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           `background: url("https://example.com/images/bg.png");`,
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/images/bg.png');",
			expectedFound: []string{"https://example.com/images/bg.png"},
		},
		{
			name: "url() without quotes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url(https://example.com/images/bg.png);",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/images/bg.png');",
			expectedFound: []string{"https://example.com/images/bg.png"},
		},
		{
			name: "Multiple URLs in one file",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css: `.header { background: url('https://example.com/header.png'); }
.footer { background: url('https://example.com/footer.png'); }`,
			cssURL:  "https://example.com/style.css",
			baseURL: "https://example.com",
			expectedCSS: `.header { background: url('/header.png'); }
.footer { background: url('/footer.png'); }`,
			expectedFound: []string{
				"https://example.com/header.png",
				"https://example.com/footer.png",
			},
		},
		{
			name: "Data URIs should be skipped",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA');",
			expectedFound: []string{},
		},
		{
			name: "External URLs should be skipped",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://external.com/image.png');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('https://external.com/image.png');",
			expectedFound: []string{},
		},
		{
			name: "Relative URLs should be resolved",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('../images/logo.png');",
			cssURL:        "https://example.com/css/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/images/logo.png');",
			expectedFound: []string{"https://example.com/images/logo.png"},
		},
		{
			name: "Query parameters with rewrite enabled",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: true,
			},
			css:           "background: url('https://example.com/bg.png?v=1.2');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/bg_v_1.2.png');",
			expectedFound: []string{"https://example.com/bg.png?v=1.2"},
		},
		{
			name: "Query parameters with rewrite disabled",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/bg.png?v=1.2');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/bg.png?v=1.2');",
			expectedFound: []string{"https://example.com/bg.png?v=1.2"},
		},
		{
			name: "Polish characters in URL",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/zdjęcia/łódź.jpg');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/zdjęcia/łódź.jpg');",
			expectedFound: []string{"https://example.com/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA.jpg"}, // URL gets percent-encoded
		},
		{
			name: "Multiple url() on same line",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/bg1.png'), url('https://example.com/bg2.png');",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/bg1.png'), url('/bg2.png');",
			expectedFound: []string{"https://example.com/bg1.png", "https://example.com/bg2.png"},
		},
		{
			name: "URL with whitespace",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url(  'https://example.com/image.png'  );",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/image.png');",
			expectedFound: []string{"https://example.com/image.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultCSS, found := tt.config.ProcessCSSFile(tt.css, tt.cssURL, tt.baseURL)

			assert.Equal(t, tt.expectedCSS, resultCSS, "CSS should be rewritten correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}

// TestConfig_ProcessInlineCSS tests processing inline CSS in HTML
func TestConfig_ProcessInlineCSS(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		css           string
		currentURL    string
		baseURL       string
		expectedCSS   string
		expectedFound []string
	}{
		{
			name: "Inline style in HTML element",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background-image: url('https://example.com/bg.png')",
			currentURL:    "https://example.com/page.html",
			baseURL:       "https://example.com",
			expectedCSS:   "background-image: url('/bg.png')",
			expectedFound: []string{"https://example.com/bg.png"},
		},
		{
			name: "Multiple background images",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/bg1.jpg'), url('https://example.com/bg2.jpg')",
			currentURL:    "https://example.com/page.html",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/bg1.jpg'), url('/bg2.jpg')",
			expectedFound: []string{"https://example.com/bg1.jpg", "https://example.com/bg2.jpg"},
		},
		{
			name: "Font face with url()",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "src: url('https://example.com/fonts/font.woff2')",
			currentURL:    "https://example.com/page.html",
			baseURL:       "https://example.com",
			expectedCSS:   "src: url('/fonts/font.woff2')",
			expectedFound: []string{"https://example.com/fonts/font.woff2"},
		},
		{
			name: "Mix of local and external URLs",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/local.png'), url('https://cdn.example.com/external.png')",
			currentURL:    "https://example.com/page.html",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('/local.png'), url('https://cdn.example.com/external.png')",
			expectedFound: []string{"https://example.com/local.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultCSS, found := tt.config.ProcessInlineCSS(tt.css, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedCSS, resultCSS, "Inline CSS should be rewritten correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}

// TestConfig_ProcessInlineStyle tests processing style attributes
func TestConfig_ProcessInlineStyle(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		style         string
		currentURL    string
		baseURL       string
		expectedStyle string
		expectedFound []string
	}{
		{
			name: "Simple background-image",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "background-image: url('https://example.com/img.png')",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "background-image: url('/img.png')",
			expectedFound: []string{"https://example.com/img.png"},
		},
		{
			name: "Background shorthand",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "background: url('https://example.com/bg.jpg') no-repeat center",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "background: url('/bg.jpg') no-repeat center",
			expectedFound: []string{"https://example.com/bg.jpg"},
		},
		{
			name: "Border image",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "border-image: url('https://example.com/border.png')",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "border-image: url('/border.png')",
			expectedFound: []string{"https://example.com/border.png"},
		},
		{
			name: "Complex style with multiple properties",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "color: red; background: url('https://example.com/bg.png'); padding: 10px",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "color: red; background: url('/bg.png'); padding: 10px",
			expectedFound: []string{"https://example.com/bg.png"},
		},
		{
			name: "Data URI should be skipped",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "background: url('data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA')",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "background: url('data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAUA')",
			expectedFound: []string{},
		},
		{
			name: "Data URI mixed with regular URL",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			style:         "background: url('data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'), url('https://example.com/bg.png')",
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedStyle: "background: url('data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7'), url('/bg.png')",
			expectedFound: []string{"https://example.com/bg.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultStyle, found := tt.config.ProcessInlineStyle(tt.style, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedStyle, resultStyle, "Style should be rewritten correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}

// TestConfig_ProcessCSS_EdgeCases tests edge cases in CSS processing
func TestConfig_ProcessCSS_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		config        Config
		css           string
		cssURL        string
		baseURL       string
		expectedCSS   string
		expectedFound []string
	}{
		{
			name: "Empty CSS",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "",
			expectedFound: []string{},
		},
		{
			name: "CSS with no URLs",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           ".class { color: red; padding: 10px; }",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   ".class { color: red; padding: 10px; }",
			expectedFound: []string{},
		},
		{
			name: "Malformed url() - missing closing paren",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "background: url('https://example.com/img.png'",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "background: url('https://example.com/img.png'",
			expectedFound: []string{},
		},
		{
			name: "URL-like string not in url()",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			css:           "/* https://example.com/comment.png */",
			cssURL:        "https://example.com/style.css",
			baseURL:       "https://example.com",
			expectedCSS:   "/* https://example.com/comment.png */",
			expectedFound: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultCSS, found := tt.config.ProcessCSSFile(tt.css, tt.cssURL, tt.baseURL)

			assert.Equal(t, tt.expectedCSS, resultCSS, "CSS should be handled correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}
