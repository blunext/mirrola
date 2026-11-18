package crawler

import (
	"io"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

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
	}

	var ext2 string
	switch contentType {
	case "text/css":
		ext2 = ".css"
	case "application/javascript", "text/javascript":
		ext2 = ".js"
	default:
		ext2, _ = extForContentType(contentType)
	}
	if ext2 != "" {
		// "/feed/" served as RSS becomes feed/index.xml, not a hidden feed/.xml
		if strings.HasSuffix(pathPart, "/") {
			return pathPart + "index" + ext2
		}
		return pathPart + ext2
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

	// Confine the path to OutputDir. This must run after transliteration:
	// "..́" survives URL cleaning but becomes ".." once the accent is
	// stripped. Cleaning
	// a rooted path cannot climb above "/" (browsers resolve dot segments in
	// links the same way, so links still match).
	pathPart = strings.TrimLeft(path.Clean("/"+pathPart), "/")

	return filepath.Join(c.OutputDir, pathPart)
}

// normalizePath maps a URL path to the on-disk path: NFC normalization plus
// transliteration of diacritics (łódź → lodz). Used for both file names and
// links, so the two always match.
func (c *Config) normalizePath(urlPath string) string {
	// Remove trailing ? (empty query marker) and other invalid filesystem chars
	urlPath = strings.TrimSuffix(urlPath, "?")
	return transliterate(norm.NFC.String(urlPath))
}

// transliterate removes diacritics, falling back to the input on error.
func transliterate(p string) string {
	if cleaned, err := removeDiacritics(p); err == nil {
		return cleaned
	}
	return p
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
	// Unique temp name in the target dir: concurrent writers never share it,
	// and the final rename stays atomic (same filesystem).
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(tmp)
		}
	}()
	// CreateTemp uses 0600; static files must be world-readable when served.
	if err = f.Chmod(0o644); err != nil {
		return err
	}
	if _, err = io.Copy(f, r); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	err = os.Rename(tmp, path)
	return err
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
