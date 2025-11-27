package crawler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestHTTP_EdgeCases(t *testing.T) {
	// Setup mock server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/404":
			http.NotFound(w, r)
		case "/timeout":
			time.Sleep(200 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		case "/redirect":
			http.Redirect(w, r, "/target", http.StatusFound)
		case "/target":
			w.WriteHeader(http.StatusOK)
		case "/unicode/ó": // NFC
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Found NFC"))
		case "/unicode/o\u0301": // NFD
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Found NFD"))
		default:
			// For unicode fallback test, if we request one form and it's missing, return 404
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Setup Config and Client
	ua := "MirrolaTest/1.0"
	userAgent = &ua
	timeout := 1 // 1 second
	timeoutSec = &timeout
	delay := 0.0
	delayBetweenRequests = &delay

	initHTTPClient()
	// Override client timeout for faster tests
	client.Timeout = 100 * time.Millisecond

	ctx := context.Background()

	t.Run("404 Error", func(t *testing.T) {
		resp, err := headOrGet(ctx, ts.URL+"/404")
		// headOrGet returns response even on 404
		assert.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("Timeout", func(t *testing.T) {
		_, err := headOrGet(ctx, ts.URL+"/timeout")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "Client.Timeout")
	})

	t.Run("Redirect", func(t *testing.T) {
		// Reset timeout for redirect test
		client.Timeout = 1 * time.Second
		resp, err := headOrGet(ctx, ts.URL+"/redirect")
		assert.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		// Verify we landed on target (req.URL might show it)
		assert.Contains(t, resp.Request.URL.Path, "/target")
	})

	t.Run("Unicode Fallback NFC->NFD", func(t *testing.T) {
		// Server has NFD resource: /unicode/o\u0301
		// We request NFC: /unicode/ó
		// headOrGet should try NFC (404) -> fallback to NFD (200)

		// Note: httptest server might normalize paths?
		// Let's verify.

		u := ts.URL + "/unicode/\u00F3" // NFC
		resp, err := headOrGet(ctx, u)
		assert.NoError(t, err)
		if resp.StatusCode == 200 {
			// It worked!
			// body, _ := io.ReadAll(resp.Body)
			// If server matched NFC directly, body is "Found NFC".
			// If fallback happened, body is "Found NFD".
			// But wait, I added both endpoints to mock server.
			// To test fallback, I should ONLY have one on server.
		}
	})
}

func TestHTTP_UnicodeFallback(t *testing.T) {
	// Server ONLY has NFD resource
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check raw path to avoid auto-normalization by Go's mux if any
		if r.URL.Path == "/unicode/o\u0301" { // NFD
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Found NFD"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	ua := "MirrolaTest/1.0"
	userAgent = &ua
	timeout := 1
	timeoutSec = &timeout
	delay := 0.0
	delayBetweenRequests = &delay

	initHTTPClient()
	ctx := context.Background()

	// Request NFC
	u := ts.URL + "/unicode/\u00F3" // NFC
	resp, err := headOrGet(ctx, u)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "Found NFD", string(body))
}
