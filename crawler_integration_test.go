package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCrawler_BasicFlow(t *testing.T) {
	// Setup mock HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `
				<html>
					<head><link rel="stylesheet" href="/style.css"></head>
					<body>
						<a href="/page1">Page 1</a>
						<img src="/image.png">
					</body>
				</html>
			`)
		case "/page1":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><h1>Page 1</h1></body></html>`)
		case "/style.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, `body { color: red; }`)
		case "/image.png":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte("fake image data"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	// Setup temp directory
	tempDir := t.TempDir()

	// Setup Config
	cfg = &Config{
		BaseURL:       ts.URL,
		OutputDir:     tempDir,
		RewriteURL:    true,
		SafeFilenames: false,
	}
	cfg.InitRegexps()

	// Mock global variables
	client = ts.Client()
	outputDir = &tempDir
	rewriteURL = &cfg.RewriteURL
	safeFilenames = &cfg.SafeFilenames
	baseURLStr := ts.URL
	baseURL = &baseURLStr
	ua := "MirrolaTest/1.0"
	userAgent = &ua
	maxDepth = 2

	// Reset visited map
	visited.m = make(map[string]bool)

	// Setup crawler channels
	tasks := make(chan task, 100)
	var wg sync.WaitGroup      // Local wg for workers, distinct from tasksWg
	tasksWg = sync.WaitGroup{} // Reset global tasksWg

	// Start workers
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a few workers
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case taskItem := <-tasks:
					if err := processURL(ctx, taskItem.url, taskItem.depth, tasks); err != nil {
						t.Logf("Error processing %s: %v", taskItem.url, err)
					}
					tasksWg.Done()
				}
			}
		}()
	}

	// Enqueue start URL
	err := enqueueLink(ctx, ts.URL, 0, tasks)
	require.NoError(t, err)

	// Wait for completion
	// We need a way to wait for tasksWg.Wait() but also respect context timeout
	done := make(chan struct{})
	go func() {
		tasksWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-ctx.Done():
		t.Fatal("Timeout waiting for crawler to finish")
	}

	// Cancel context to stop workers
	cancel()
	wg.Wait()

	// Verify files created
	require.FileExists(t, filepath.Join(tempDir, "index.html"))
	require.FileExists(t, filepath.Join(tempDir, "page1/index.html"))
	require.FileExists(t, filepath.Join(tempDir, "style.css"))
	require.FileExists(t, filepath.Join(tempDir, "image.png"))

	// Verify content
	content, _ := os.ReadFile(filepath.Join(tempDir, "index.html"))
	assert.Contains(t, string(content), `href="/page1"`) // Current behavior: absolute path, no index.html
	assert.Contains(t, string(content), `href="/style.css"`)
	assert.Contains(t, string(content), `src="/image.png"`)
}
