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

// fetch GETs a URL. On a 404 it retries once with the path's Unicode
// normalization flipped (NFC <-> NFD), because WordPress upload file names
// with Polish characters are stored in either form; if that fails too, the
// original 404 response is returned.
//
// There is deliberately no HEAD request first: one was added for "type
// detection" (b06d4d5), but since a86d318 a GET always followed it and its
// result was thrown away, so it only doubled the requests.
func fetch(ctx context.Context, u string) (*http.Response, error) {
	resp, err := doRequest(ctx, http.MethodGet, u)
	if err != nil || resp.StatusCode != http.StatusNotFound {
		return resp, err
	}

	parsed, perr := url.Parse(u)
	if perr != nil {
		return resp, nil
	}
	alt := norm.NFD.String(parsed.Path)
	if norm.NFD.IsNormalString(parsed.Path) {
		alt = norm.NFC.String(parsed.Path)
	}
	if alt == parsed.Path {
		return resp, nil // nothing to flip (plain ASCII)
	}
	parsed.Path = alt
	parsed.RawPath = "" // force re-encoding
	resp2, err2 := doRequest(ctx, http.MethodGet, parsed.String())
	if err2 == nil && resp2.StatusCode < 400 {
		resp.Body.Close()
		return resp2, nil
	}
	if resp2 != nil {
		resp2.Body.Close()
	}
	return resp, nil // keep the original 404
}

// looksLikeHTML peeks at response body to detect HTML content when Content-Type header is missing
func looksLikeHTML(resp *http.Response) bool {
	buf := make([]byte, 256)
	n, _ := io.ReadFull(resp.Body, buf)
	resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf[:n]), resp.Body))
	b := strings.ToLower(string(buf[:n]))
	return strings.Contains(b, "<html") || strings.Contains(b, "<!doctype")
}
