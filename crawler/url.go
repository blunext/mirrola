package crawler

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"
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

// toRelative converts an absolute URL to a relative path for use in static HTML.
// Handles Unicode normalization (NFC) when safe filenames are enabled to ensure
// links match the actual files on disk across different filesystems.

// fixPath clears RawPath to force URL encoding based on Path field
func fixPath(u *url.URL) {
	u.RawPath = ""
}

// --- Query rewrite policy (with hashing for long queries) ---

// rewriteURLWithPolicy decides how to bake query parameters into filenames.
// For long queries (>80 chars), uses SHA1 hash. For short queries, uses readable format.
// Distinguishes between pages (no extension) and assets (with extension).

// sanitizeQueryPart replaces special chars in query params for safe filenames
func sanitizeQueryPart(s string) string {
	s = strings.ReplaceAll(s, "=", "_")
	s = strings.ReplaceAll(s, "?", "_")
	s = strings.ReplaceAll(s, "&", "_")
	return s
}

// rewriteAssetURL bakes query params into asset filename (e.g., style.css?v=1 → style_v_1.css)
func rewriteAssetURL(u *url.URL) *url.URL {
	return rewriteAssetURLWithSuffix(u, querySuffix(u))
}

// querySuffix converts query parameters to a sanitized filename suffix
func querySuffix(u *url.URL) string {
	q := u.Query()
	if len(q) == 0 {
		return ""
	}

	// Sort keys for deterministic output
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(q))
	for _, k := range keys {
		vals := q[k]
		for _, v := range vals {
			parts = append(parts, sanitizeQueryPart(k)+"_"+sanitizeQueryPart(v))
		}
	}
	return strings.Join(parts, "_")
}

// rewriteAssetURLWithSuffix bakes a suffix into an asset's filename before extension
func rewriteAssetURLWithSuffix(u *url.URL, suffix string) *url.URL {
	if u.RawQuery == "" {
		return u
	}
	ext := filepath.Ext(u.Path)
	if ext == "" {
		u.RawQuery = ""
		return u
	}
	base := strings.TrimSuffix(u.Path, ext)
	if suffix != "" {
		u.Path = base + "_" + suffix + ext
	} else {
		u.Path = base + ext
	}
	u.RawQuery = ""
	return u
}

// rewritePageURL bakes query params into page filename
func rewritePageURL(u *url.URL) *url.URL {
	return rewritePageURLWithSuffix(u, sanitizeQueryPart(u.RawQuery))
}

// rewritePageURLWithSuffix bakes a suffix into a page's filename.
// Converts paths without extensions to /path/index.html format.
func rewritePageURLWithSuffix(u *url.URL, suffix string) *url.URL {
	if u.RawQuery == "" {
		return u
	}
	ext := filepath.Ext(u.Path)
	if ext == "" {
		// Pages without extension become directories with index.html
		if u.Path == "" || u.Path == "/" {
			u.Path = "/index.html"
			ext = ".html"
		} else {
			if !strings.HasSuffix(u.Path, "/") {
				u.Path += "/"
			}
			u.Path += "index.html"
			ext = ".html"
		}
	}
	d := filepath.Dir(u.Path)
	b := filepath.Base(u.Path)
	name := strings.TrimSuffix(b, ext)
	if suffix != "" {
		b = name + "_" + suffix + ext
	} else {
		b = name + ext
	}
	u.Path = filepath.Join(d, b)
	u.RawQuery = ""
	return u
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

	var pathStr string
	if c.SafeFilenames {
		// Force NFC normalization to match GetOutputPath behavior
		pathStr = (&url.URL{Path: norm.NFC.String(abs.Path)}).EscapedPath()
	} else {
		pathStr = abs.Path
	}

	if abs.RawQuery != "" {
		return pathStr + "?" + abs.RawQuery
	}
	return pathStr
}

// RewriteURLWithPolicy decides how to bake query parameters into filenames (Config-based)
func (c *Config) RewriteURLWithPolicy(u *url.URL) *url.URL {
	if len(u.RawQuery) > 80 {
		// Hash long queries to avoid filesystem path length limits
		sum := sha1.Sum([]byte(u.RawQuery))
		sfx := hex.EncodeToString(sum[:8]) // 16-char hex
		if filepath.Ext(u.Path) == "" {
			return rewritePageURLWithSuffix(u, "q_"+sfx)
		}
		return rewriteAssetURLWithSuffix(u, "q_"+sfx)
	}

	// Short queries get human-readable filenames
	if filepath.Ext(u.Path) == "" {
		return rewritePageURL(u)
	}
	return rewriteAssetURL(u)
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
