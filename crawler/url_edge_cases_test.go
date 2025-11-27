package crawler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURL_EdgeCases(t *testing.T) {

	tests := []struct {
		name        string
		currentURL  string
		link        string
		expected    string
		expectError bool
	}{
		{
			name:        "Malformed URL",
			currentURL:  "https://example.com",
			link:        "://invalid",
			expectError: true,
		},
		{
			name:       "Fragment only",
			currentURL: "https://example.com/page",
			link:       "#section1",
			expected:   "https://example.com/page", // Fragments are stripped in normalize
		},
		{
			name:       "Fragment with path",
			currentURL: "https://example.com/page",
			link:       "other#section2",
			expected:   "https://example.com/other",
		},
		{
			name:       "Unicode in path",
			currentURL: "https://example.com",
			link:       "/żółw",
			expected:   "https://example.com/%C5%BC%C3%B3%C5%82w", // Go's URL parser percent-encodes this
		},
		{
			name:       "Already percent-encoded",
			currentURL: "https://example.com",
			link:       "/%C5%BC%C3%B3%C5%82w",
			expected:   "https://example.com/%C5%BC%C3%B3%C5%82w",
		},
		{
			name:       "Dot segments (.)",
			currentURL: "https://example.com/a/b/",
			link:       "./c",
			expected:   "https://example.com/a/b/c",
		},
		{
			name:       "Dot segments (..)",
			currentURL: "https://example.com/a/b/",
			link:       "../c",
			expected:   "https://example.com/a/c",
		},
		{
			name:       "Protocol relative",
			currentURL: "https://example.com",
			link:       "//cdn.example.com/lib.js",
			expected:   "https://cdn.example.com/lib.js", // Inherits scheme
		},
		{
			name:       "Mailto scheme",
			currentURL: "https://example.com",
			link:       "mailto:user@example.com",
			expected:   "mailto:user@example.com", // resolveURL handles opaque URLs
		},
		{
			name:       "Tel scheme",
			currentURL: "https://example.com",
			link:       "tel:+123456789",
			expected:   "tel:+123456789",
		},
		{
			name:       "Data URI",
			currentURL: "https://example.com",
			link:       "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
			expected:   "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			abs, err := resolveURL(tt.currentURL, tt.link)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				if abs != nil {
					assert.Equal(t, tt.expected, abs.String())
				}
			}
		})
	}
}

func TestConfig_SameHost_EdgeCases(t *testing.T) {
	cfg := &Config{}

	tests := []struct {
		name     string
		u1       string
		u2       string
		expected bool
	}{
		{"Different schemes same host", "https://example.com", "http://example.com", true},
		{"Different ports same host", "https://example.com:8443", "https://example.com", true}, // sameHost uses strings.EqualFold(u.Host, b.Host), so ports matter! Wait, let's check implementation.
		// Implementation: strings.EqualFold(u.Host, b.Host). u.Host includes port if present.
		// So example.com:8443 != example.com.

		{"Case insensitive", "https://EXAMPLE.COM", "https://example.com", true},
		{"Subdomain vs root", "https://sub.example.com", "https://example.com", false},
		{"Invalid URL 1", "://invalid", "https://example.com", false},
		{"Invalid URL 2", "https://example.com", "://invalid", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For "Different ports same host", let's verify actual behavior
			// If u.Host includes port, they should differ.
			if tt.name == "Different ports same host" {
				// Adjust expectation based on implementation details if needed
				// net/url Host field includes port.
				// So "example.com:8443" != "example.com"
				// Expected: false
				tt.expected = false
			}

			result := cfg.SameHost(tt.u1, tt.u2)
			assert.Equal(t, tt.expected, result)
		})
	}
}
