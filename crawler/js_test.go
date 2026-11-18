package crawler

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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
			},
			jsContent:     `var cdn = "https:\/\/cdn.example.com\/library.js";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var cdn = "https:\/\/cdn.example.com\/library.js";`, // external: left exactly as is
			expectedFound: []string{},
		},
		{
			name: "Unicode escapes \\u0026 (ampersand)",
			config: Config{
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
			},
			jsContent:     `var url = "https:\u002F\u002Fexample.com\u002Fimage.png";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var url = "\/image.png";`,
			expectedFound: []string{"https://example.com/image.png"},
		},
		{
			name: "Simple Lightbox specific pattern",
			config: Config{
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
			},
			jsContent:     `var img = "https:\/\/example.com\/zdjęcia\/łódź.jpg";`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `var img = "\/zdjecia\/lodz.jpg";`,
			expectedFound: []string{"https://example.com/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA.jpg"}, // URL gets percent-encoded
		},
		{
			name: "No URLs in JS",
			config: Config{
				BaseURL: "https://example.com",
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
				BaseURL: "http://example.com",
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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
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
				BaseURL: "https://example.com",
			},
			jsContent:     `// https://example.com/image.jpg`,
			currentURL:    "https://example.com/page",
			baseURL:       "https://example.com",
			expectedJS:    `// /image.jpg`, // unescaped URL stays unescaped
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

// TestConfig_ProcessInlineJS_OnlyURLsChange: escapes outside URLs must survive
// untouched. Decoding them would break the script: \u0022 would end a string,
// \u003c/script\u003e would end the <script> element.
func TestConfig_ProcessInlineJS_OnlyURLsChange(t *testing.T) {
	c := Config{BaseURL: "https://example.com"}
	c.InitRegexps()
	const page = "https://example.com/post/"

	cases := []struct {
		name, in, want string
		found          []string
	}{
		{
			name:  "escaped quote in title",
			in:    `{"source":"https:\/\/example.com\/a.jpg","title":"Say \u0022hi\u0022"}`,
			want:  `{"source":"\/a.jpg","title":"Say \u0022hi\u0022"}`,
			found: []string{"https://example.com/a.jpg"},
		},
		{
			name:  "escaped closing script tag",
			in:    `{"html":"\u003c/script\u003e","source":"https:\/\/example.com\/b.jpg"}`,
			want:  `{"html":"\u003c/script\u003e","source":"\/b.jpg"}`,
			found: []string{"https://example.com/b.jpg"},
		},
		{
			name:  "lightbox template and polish title (as on olamundo.pl)",
			in:    `{"layout":"<div><\/span><\/div>","name":"Domy\u015blny (jasny)","uri":"https:\/\/example.com\/wp-content\/plugins\/slb\/style.css"}`,
			want:  `{"layout":"<div><\/span><\/div>","name":"Domy\u015blny (jasny)","uri":"\/wp-content\/plugins\/slb\/style.css"}`,
			found: []string{"https://example.com/wp-content/plugins/slb/style.css"},
		},
		{
			name:  "literal backslash-u in text",
			in:    `{"t":"C:\\users","source":"https:\/\/example.com\/c.jpg"}`,
			want:  `{"t":"C:\\users","source":"\/c.jpg"}`,
			found: []string{"https://example.com/c.jpg"},
		},
		{
			name:  "NFD polish file name escaped by json_encode",
			in:    `{"source":"https:\/\/example.com\/uploads\/niedz\u0301wiedz\u0301-500x333.jpg"}`,
			want:  `{"source":"\/uploads\/niedzwiedz-500x333.jpg"}`,
			found: []string{"https://example.com/uploads/niedz%CC%81wiedz%CC%81-500x333.jpg"},
		},
		{
			name:  "URL followed by escaped quote stops before it",
			in:    `var h = "<a href=\"https:\/\/example.com\/d\/\">x<\/a>";`,
			want:  `var h = "<a href=\"\/d\/\">x<\/a>";`,
			found: []string{"https://example.com/d/"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, found := c.ProcessInlineJS(tc.in, page, c.BaseURL)
			assert.Equal(t, tc.want, got)
			assert.ElementsMatch(t, tc.found, found)
		})
	}
}
