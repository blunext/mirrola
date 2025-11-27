package crawler

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// --- HTTP helpers ---

// doRequest performs an HTTP request with custom User-Agent
func doRequest(ctx context.Context, method, u string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", *userAgent)
	return client.Do(req)
}

// headOrGet fetches a URL using HEAD first (for efficiency), then GET.
// On 404, tries Unicode normalization fallback (NFC ↔ NFD) for Polish/international chars.
func headOrGet(ctx context.Context, u string) (*http.Response, error) {
	try := func(targetURL string) (*http.Response, error) {
		// Try HEAD first
		resp, err := doRequest(ctx, http.MethodHead, targetURL)
		if err != nil || resp.StatusCode >= 400 {
			// HEAD failed or error status, try GET directly
			if resp != nil {
				resp.Body.Close()
			}
			return doRequest(ctx, http.MethodGet, targetURL)
		}
		// HEAD succeeded, close it and do GET to get the body
		resp.Body.Close()
		return doRequest(ctx, http.MethodGet, targetURL)
	}

	resp, err := try(u)

	// If 404, try Unicode normalization fallback (handles servers that expect different normalization)
	// This is crucial for Polish chars: ó can be NFC (U+00F3) or NFD (U+006F U+0301)
	if err == nil && resp.StatusCode == 404 {
		resp.Body.Close() // Close the 404 response

		parsed, parseErr := url.Parse(u)
		if parseErr == nil {
			path := parsed.Path
			var altPath string

			// Check if we can flip normalization
			if norm.NFC.IsNormalString(path) {
				altPath = norm.NFD.String(path)
			} else {
				altPath = norm.NFC.String(path)
			}

			if altPath != path {
				parsed.Path = altPath
				parsed.RawPath = "" // Force re-encoding
				altURL := parsed.String()

				// fmt.Printf("[INFO] 404 for %s, trying fallback: %s\n", u, altURL)
				resp2, err2 := try(altURL)
				if err2 == nil && resp2.StatusCode < 400 {
					return resp2, nil // Found it!
				}
				if resp2 != nil {
					resp2.Body.Close()
				}
			}
		}
		// Fallback failed, re-request original URL to get valid response object
		return try(u)
	}

	return resp, err
}

// looksLikeHTML peeks at response body to detect HTML content when Content-Type header is missing
func looksLikeHTML(resp *http.Response) bool {
	buf := make([]byte, 256)
	n, _ := io.ReadFull(resp.Body, buf)
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf[:n]), resp.Body))
	b := strings.ToLower(string(buf[:n]))
	return strings.Contains(b, "<html") || strings.Contains(b, "<!doctype")
}
