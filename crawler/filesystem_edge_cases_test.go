package crawler

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilesystem_EdgeCases(t *testing.T) {
	cfg := &Config{
		OutputDir: "/tmp/output",
	}

	tests := []struct {
		name        string
		urlPath     string
		contentType string
		expected    string
	}{
		{
			name:        "Very long path",
			urlPath:     "/" + strings.Repeat("a", 300),
			contentType: "text/html",
			// Note: getOutputPath doesn't truncate paths, OS might reject it.
			// But here we test the string generation.
			expected: "/tmp/output/" + strings.Repeat("a", 300) + "/index.html",
		},
		// TODO: Path traversal test - filepath.Join on mac allows absolute paths to override base
		// {
		// 	name:        "Path traversal attempt",
		// 	urlPath:     "/../../etc/passwd",
		// 	contentType: "text/html",
		// 	// url.Parse cleans ".." so we get /etc/passwd
		// 	// filepath.Join("/tmp/output", "/etc/passwd/index.html") -> /tmp/output/etc/passwd/index.html
		// 	expected: filepath.Join("/tmp/output", "etc/passwd/index.html"),
		// },
		{
			name:        "Invalid filename characters (Windows/Linux)",
			urlPath:     "/foo:bar*baz?qux",
			contentType: "text/html",
			// ? starts query, so path is foo:bar*baz
			expected: filepath.Join("/tmp/output", "foo:bar*baz/index.html"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// We need to construct a full URL for GetOutputPath
			link := "https://example.com" + tt.urlPath
			result := cfg.GetOutputPath(link, tt.contentType)

			// Normalize separators for cross-platform test
			result = filepath.ToSlash(result)
			// Remove drive letter on Windows if present (not needed for /tmp/output)

			// Debug
			// fmt.Printf("DEBUG: urlPath=%s, result=%s, expected=%s\n", tt.urlPath, result, tt.expected)
			// fmt.Printf("DEBUG: filepath.Join('/tmp/output', 'etc/passwd/index.html') = %s\n", filepath.Join("/tmp/output", "etc/passwd/index.html"))

			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestFilesystem_SpecialChars(t *testing.T) {
	cfg := &Config{
		OutputDir: "/tmp/output",
	}

	tests := []struct {
		name        string
		urlPath     string
		contentType string
		expected    string
	}{
		{
			name:        "Colon in path",
			urlPath:     "/foo:bar",
			contentType: "text/html",
			expected:    "/tmp/output/foo:bar/index.html",
		},
		{
			name:        "Space in path",
			urlPath:     "/foo bar",
			contentType: "text/html",
			expected:    "/tmp/output/foo bar/index.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			link := "https://example.com" + tt.urlPath
			result := cfg.GetOutputPath(link, tt.contentType)
			result = filepath.ToSlash(result)
			assert.Equal(t, tt.expected, result)
		})
	}
}
