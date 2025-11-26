package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestConfig_ProcessSrcSet tests processing srcset attributes
func TestConfig_ProcessSrcSet(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		srcset         string
		currentURL     string
		baseURL        string
		expectedSrcset string
		expectedFound  []string
	}{
		{
			name: "Single image URL",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/image.jpg",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/image.jpg",
			expectedFound:  []string{"https://example.com/image.jpg"},
		},
		{
			name: "Image with 1x descriptor",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/image.jpg 1x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/image.jpg 1x",
			expectedFound:  []string{"https://example.com/image.jpg"},
		},
		{
			name: "Multiple images with density descriptors",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/image-1x.jpg 1x, https://example.com/image-2x.jpg 2x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/image-1x.jpg 1x, /image-2x.jpg 2x",
			expectedFound: []string{
				"https://example.com/image-1x.jpg",
				"https://example.com/image-2x.jpg",
			},
		},
		{
			name: "Multiple images with width descriptors",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/small.jpg 480w, https://example.com/large.jpg 1024w",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/small.jpg 480w, /large.jpg 1024w",
			expectedFound: []string{
				"https://example.com/small.jpg",
				"https://example.com/large.jpg",
			},
		},
		{
			name: "Three images with different sizes",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/img-480.jpg 480w, https://example.com/img-800.jpg 800w, https://example.com/img-1200.jpg 1200w",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/img-480.jpg 480w, /img-800.jpg 800w, /img-1200.jpg 1200w",
			expectedFound: []string{
				"https://example.com/img-480.jpg",
				"https://example.com/img-800.jpg",
				"https://example.com/img-1200.jpg",
			},
		},
		{
			name: "External URLs should be skipped",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://cdn.example.com/external.jpg 1x, https://example.com/local.jpg 2x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "https://cdn.example.com/external.jpg 1x, /local.jpg 2x",
			expectedFound:  []string{"https://example.com/local.jpg"},
		},
		{
			name: "Relative URLs should be resolved",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "../images/photo.jpg 1x, ../images/photo@2x.jpg 2x",
			currentURL:     "https://example.com/pages/blog.html",
			baseURL:        "https://example.com",
			expectedSrcset: "/images/photo.jpg 1x, /images/photo@2x.jpg 2x",
			expectedFound: []string{
				"https://example.com/images/photo.jpg",
				"https://example.com/images/photo@2x.jpg",
			},
		},
		{
			name: "Query parameters with rewrite enabled",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: true,
			},
			srcset:         "https://example.com/img.jpg?v=1 1x, https://example.com/img.jpg?v=2 2x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/img_v_1.jpg 1x, /img_v_2.jpg 2x",
			expectedFound: []string{
				"https://example.com/img.jpg?v=1",
				"https://example.com/img.jpg?v=2",
			},
		},
		{
			name: "Query parameters with rewrite disabled",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/img.jpg?size=small 480w, https://example.com/img.jpg?size=large 1024w",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/img.jpg?size=small 480w, /img.jpg?size=large 1024w",
			expectedFound: []string{
				"https://example.com/img.jpg?size=small",
				"https://example.com/img.jpg?size=large",
			},
		},
		{
			name: "Polish characters in URL",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/zdjęcia/łódź-480.jpg 480w, https://example.com/zdjęcia/łódź-800.jpg 800w",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/zdjęcia/łódź-480.jpg 480w, /zdjęcia/łódź-800.jpg 800w",
			expectedFound: []string{
				"https://example.com/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA-480.jpg",
				"https://example.com/zdj%C4%99cia/%C5%82%C3%B3d%C5%BA-800.jpg",
			},
		},
		{
			name: "WebP images with descriptors",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/photo.webp 1x, https://example.com/photo-2x.webp 2x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/photo.webp 1x, /photo-2x.webp 2x",
			expectedFound: []string{
				"https://example.com/photo.webp",
				"https://example.com/photo-2x.webp",
			},
		},
		{
			name: "Image without descriptor",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/image.jpg",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/image.jpg",
			expectedFound:  []string{"https://example.com/image.jpg"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultSrcset, found := tt.config.ProcessSrcSet(tt.srcset, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedSrcset, resultSrcset, "Srcset should be rewritten correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}

// TestConfig_ProcessSrcSet_EdgeCases tests edge cases in srcset processing
func TestConfig_ProcessSrcSet_EdgeCases(t *testing.T) {
	tests := []struct {
		name           string
		config         Config
		srcset         string
		currentURL     string
		baseURL        string
		expectedSrcset string
		expectedFound  []string
	}{
		{
			name: "Empty srcset",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "",
			expectedFound:  []string{},
		},
		{
			name: "Whitespace only",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "   ",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "",
			expectedFound:  []string{},
		},
		{
			name: "Trailing comma",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/img1.jpg 1x, https://example.com/img2.jpg 2x,",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/img1.jpg 1x, /img2.jpg 2x",
			expectedFound: []string{
				"https://example.com/img1.jpg",
				"https://example.com/img2.jpg",
			},
		},
		{
			name: "Extra whitespace between entries",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "https://example.com/img1.jpg  1x  ,   https://example.com/img2.jpg  2x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/img1.jpg 1x, /img2.jpg 2x",
			expectedFound: []string{
				"https://example.com/img1.jpg",
				"https://example.com/img2.jpg",
			},
		},
		{
			name: "Relative path without scheme - resolved",
			config: Config{
				BaseURL:    "https://example.com",
				RewriteURL: false,
			},
			srcset:         "not-a-valid-url 1x",
			currentURL:     "https://example.com/page",
			baseURL:        "https://example.com",
			expectedSrcset: "/not-a-valid-url 1x", // Treated as relative path
			expectedFound:  []string{"https://example.com/not-a-valid-url"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.InitRegexps()

			resultSrcset, found := tt.config.ProcessSrcSet(tt.srcset, tt.currentURL, tt.baseURL)

			assert.Equal(t, tt.expectedSrcset, resultSrcset, "Srcset should be handled correctly")
			assert.ElementsMatch(t, tt.expectedFound, found, "Found URLs should match")
		})
	}
}
