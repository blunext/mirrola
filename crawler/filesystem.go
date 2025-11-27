package crawler

import (
	"bytes"
	"io"
	"mime"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// --- Asset I/O ---

// addMissingExtension adds appropriate file extension based on content type if missing
func addMissingExtension(pathPart, contentType string) string {
	// Handle root/empty paths
	if pathPart == "" || pathPart == "/" {
		return "/index.html"
	}

	ext := filepath.Ext(pathPart)
	if ext != "" {
		return pathPart // Extension already present
	}

	// Add extension based on content-type
	switch contentType {
	case "text/html", "application/xhtml+xml":
		if strings.HasSuffix(pathPart, "/") {
			return pathPart + "index.html"
		}
		return pathPart + "/index.html"
	case "text/css":
		return pathPart + ".css"
	case "application/javascript", "text/javascript":
		return pathPart + ".js"
	default:
		if ext2, _ := extForContentType(contentType); ext2 != "" {
			return pathPart + ext2
		}
	}

	return pathPart
}

// --- Config methods (for incremental refactoring) ---

// GetOutputPath generates the local filesystem path for a given URL (Config-based)
func (c *Config) GetOutputPath(link string, contentType string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(c.OutputDir, "index.html")
	}

	// Normalize path and apply transliteration if needed
	pathPart := c.normalizePath(u.Path)

	// Add extension if missing based on content type
	pathPart = addMissingExtension(pathPart, contentType)

	// Bake query parameters into filename if enabled
	if c.RewriteURL && u.RawQuery != "" {
		pathPart = c.applyQueryBaking(u, pathPart)
	}

	// Ensure path is relative to OutputDir (prevent path traversal)
	pathPart = strings.TrimLeft(pathPart, "/")

	return filepath.Join(c.OutputDir, pathPart)
}

// normalizePath applies NFC normalization and optionally transliterates diacritics (Config-based)
func (c *Config) normalizePath(urlPath string) string {
	// Remove trailing ? (empty query marker) and other invalid filesystem chars
	urlPath = strings.TrimSuffix(urlPath, "?")

	// Start with NFC-normalized path
	pathPart := norm.NFC.String(urlPath)

	if c.SafeFilenames {
		// Use percent-encoding for safe filenames
		pathPart = (&url.URL{Path: pathPart}).EscapedPath()
	} else {
		// Apply transliteration if not using safe filenames
		if cleaned, err := removeDiacritics(pathPart); err == nil {
			pathPart = cleaned
		}
	}

	return pathPart
}

// applyQueryBaking bakes query parameters into the filename (Config-based)
func (c *Config) applyQueryBaking(u *url.URL, pathPart string) string {
	// Rewrite URL to bake query params
	if isStaticAssetExt(filepath.Ext(pathPart)) {
		u = rewriteAssetURL(u)
	} else {
		u = rewritePageURL(u)
	}
	pathPart = u.Path

	// Re-apply normalization after query baking to match original mode
	if c.SafeFilenames {
		// Use percent-encoding for safe filenames
		pathPart = (&url.URL{Path: pathPart}).EscapedPath()
	} else {
		// Re-apply transliteration if needed
		if cleaned, err := removeDiacritics(pathPart); err == nil {
			pathPart = cleaned
		}
	}

	return pathPart
}

func extForContentType(ct string) (string, bool) {
	switch ct {
	case "image/jpeg":
		return ".jpg", true
	case "image/png":
		return ".png", true
	case "image/gif":
		return ".gif", true
	case "image/webp":
		return ".webp", true
	case "image/svg+xml":
		return ".svg", true
	case "font/woff":
		return ".woff", true
	case "font/woff2":
		return ".woff2", true
	case "application/json":
		return ".json", true
	case "application/pdf":
		return ".pdf", true
	}
	if exts, _ := mime.ExtensionsByType(ct); len(exts) > 0 {
		return exts[0], true
	}
	return "", false
}

func writeFile(path string, r io.Reader) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, r); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func saveHTML(outputFile string, doc *html.Node) error {
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return err
	}
	return writeFile(outputFile, &buf)
}

func isStaticAssetExt(ext string) bool {
	return staticAssetExts[strings.ToLower(ext)]
}

func removeDiacritics(s string) (string, error) {
	// First, handle Polish special characters that don't decompose with NFD
	replacer := strings.NewReplacer(
		"ł", "l", "Ł", "L",
		"ą", "a", "Ą", "A",
		"ć", "c", "Ć", "C",
		"ę", "e", "Ę", "E",
		"ń", "n", "Ń", "N",
		"ś", "s", "Ś", "S",
		"ź", "z", "Ź", "Z",
		"ż", "z", "Ż", "Z",
	)
	s = replacer.Replace(s)

	// Then apply NFD normalization for composed diacritics (ó, á, etc.)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, err := transform.String(t, s)
	return result, err
}
