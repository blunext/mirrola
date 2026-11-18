package crawler

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
			name:     "Same host - converts to relative",
			config:   Config{},
			absURL:   "https://example.com/images/logo.png",
			baseURL:  "https://example.com",
			expected: "/images/logo.png",
		},
		{
			name:     "Different host - keeps absolute",
			config:   Config{},
			absURL:   "https://external.com/image.png",
			baseURL:  "https://example.com",
			expected: "https://external.com/image.png",
		},
		{
			name:     "Same host with query params - preserves query",
			config:   Config{},
			absURL:   "https://example.com/style.css?v=1.2",
			baseURL:  "https://example.com",
			expected: "/style.css?v=1.2",
		},
		{
			name:     "Polish characters are transliterated like the file name",
			config:   Config{},
			absURL:   "https://example.com/zdjęcia/łódź.jpg",
			baseURL:  "https://example.com",
			expected: "/zdjecia/lodz.jpg",
		},
		{
			name:     "Root path",
			config:   Config{},
			absURL:   "https://example.com/",
			baseURL:  "https://example.com",
			expected: "/",
		},
		{
			name:     "Deep path",
			config:   Config{},
			absURL:   "https://example.com/blog/2024/01/post.html",
			baseURL:  "https://example.com",
			expected: "/blog/2024/01/post.html",
		},
		{
			name:     "Case insensitive host matching",
			config:   Config{},
			absURL:   "https://EXAMPLE.COM/image.png",
			baseURL:  "https://example.com",
			expected: "/image.png",
		},
		{
			name:     "With fragment - fragment kept in link",
			config:   Config{},
			absURL:   "https://example.com/page#section",
			baseURL:  "https://example.com",
			expected: "/page#section",
		},
		{
			name:     "Subdomain - different host",
			config:   Config{},
			absURL:   "https://blog.example.com/post",
			baseURL:  "https://example.com",
			expected: "https://blog.example.com/post",
		},
		{
			name:     "Invalid base URL - returns absolute",
			config:   Config{},
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
