package main

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

func getOutputPath(link string, contentType string) string {
	u, err := url.Parse(link)
	if err != nil {
		return filepath.Join(*outputDir, "index.html")
	}

	// Start with NFC-normalized path
	pathPart := norm.NFC.String(u.Path)

	// Apply transliteration if not using safe filenames
	if safeFilenames == nil || !*safeFilenames {
		if cleaned, err2 := removeDiacritics(pathPart); err2 == nil {
			pathPart = cleaned
		}
	}

	if pathPart == "" || pathPart == "/" {
		pathPart = "/index.html"
	} else {
		ext := filepath.Ext(pathPart)
		if ext == "" {
			// Decide based on content-type
			switch contentType {
			case "text/html", "application/xhtml+xml":
				if strings.HasSuffix(pathPart, "/") {
					pathPart += "index.html"
				} else {
					pathPart = pathPart + "/index.html"
				}
			case "text/css":
				pathPart += ".css"
			case "application/javascript", "text/javascript":
				pathPart += ".js"
			default:
				if ext2, _ := extForContentType(contentType); ext2 != "" {
					pathPart += ext2
				}
			}
		}
	}

	// Query baking
	if *rewriteURL && u.RawQuery != "" {
		if isStaticAssetExt(filepath.Ext(pathPart)) {
			u = rewriteAssetURL(u)
		} else {
			u = rewritePageURL(u)
		}
		pathPart = u.Path
		// Re-apply transliteration after query baking if needed
		if safeFilenames == nil || !*safeFilenames {
			if cleaned, err2 := removeDiacritics(pathPart); err2 == nil {
				pathPart = cleaned
			}
		}
	}

	return filepath.Join(*outputDir, pathPart)
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
