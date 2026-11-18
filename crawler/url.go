package crawler

import (
	"net/url"
	"path"
	"strings"
)

// --- URL helpers ---

// normalize canonicalizes a URL by lowercasing scheme/host, removing default ports,
// cleaning path segments, and clearing fragments for consistent comparison
func normalize(u *url.URL) *url.URL {
	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	// Remove default ports (80 for HTTP, 443 for HTTPS)
	if (u.Scheme == "http" && strings.HasSuffix(u.Host, ":80")) || (u.Scheme == "https" && strings.HasSuffix(u.Host, ":443")) {
		u.Host = strings.Split(u.Host, ":")[0]
	}

	// Clean dot segments and ensure leading slash for absolute paths
	if u.Path != "" {
		cleaned := path.Clean(u.Path)
		// Preserve leading slash (path.Clean might remove it for relative paths)
		if strings.HasPrefix(u.Path, "/") && !strings.HasPrefix(cleaned, "/") {
			cleaned = "/" + cleaned
		}
		// Preserve trailing slash (path.Clean removes it)
		if strings.HasSuffix(u.Path, "/") && !strings.HasSuffix(cleaned, "/") {
			cleaned += "/"
		}
		u.Path = cleaned
	}

	u.RawPath = ""       // Clear to force proper re-encoding
	u.ForceQuery = false // Remove trailing '?' if query is empty (e.g. font.eot? -> font.eot)
	return u
}

// sameHost checks if two URLs belong to the same host (case-insensitive)

// resolveURL converts a relative or absolute link to an absolute URL
// based on the current page URL, and unifies the scheme for same-host links
func resolveURL(currentURL, link string) (*url.URL, error) {
	base, err := url.Parse(currentURL)
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(link)
	if err != nil {
		return nil, err
	}
	abs := base.ResolveReference(u)

	// Ensure same-host links use the same scheme as base
	if strings.EqualFold(abs.Host, base.Host) {
		abs.Scheme = base.Scheme
	}
	return normalize(abs), nil
}

// fixPath clears RawPath to force URL encoding based on Path field
func fixPath(u *url.URL) {
	u.RawPath = ""
}

// --- Config methods (for incremental refactoring) ---

// ToRelative converts an absolute URL to a relative path for use in static HTML (Config-based)
func (c *Config) ToRelative(abs *url.URL, base string) string {
	b, err := url.Parse(base)
	if err != nil {
		return abs.String()
	}
	if !strings.EqualFold(abs.Host, b.Host) {
		return abs.String() // Keep external URLs absolute
	}

	// Use the same path normalization as GetOutputPath, so the link always
	// points at the file that was actually written (transliterated).
	pathStr := c.normalizePath(abs.Path)

	if abs.RawQuery != "" {
		return pathStr + "?" + abs.RawQuery
	}
	return pathStr
}

// SameHost checks if two URLs have the same host (case-insensitive)
func (c *Config) SameHost(u1, u2 string) bool {
	u, err := url.Parse(u1)
	if err != nil {
		return false
	}
	b, err := url.Parse(u2)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, b.Host)
}
