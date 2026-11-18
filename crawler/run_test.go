package crawler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runSite crawls an httptest site end to end through Init + Run.
func runSite(t *testing.T, ctx context.Context, h http.Handler, strict bool, queue, workers int) (string, error) {
	t.Helper()
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	dir := t.TempDir()
	require.NoError(t, Init(ts.URL, dir, false, strict, "MirrolaTest/1.0", 5, 0, queue, workers, 0))

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Run(ctx) }()
	select {
	case err := <-done:
		return dir, err
	case <-time.After(25 * time.Second):
		t.Fatal("Run did not return (deadlock?)")
		return dir, nil
	}
}

func servePage(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprint(w, "<html><body>"+body+"</body></html>")
}

// site: home links to a good page and to an asset with the given status.
func siteWithAsset(status int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			servePage(w, `<a href="/ok/">ok</a><img src="/img.jpg">`)
		case "/ok/":
			servePage(w, `<p>ok</p>`)
		case "/img.jpg":
			if status == http.StatusOK {
				w.Header().Set("Content-Type", "image/jpeg")
				w.Write([]byte("jpg"))
				return
			}
			http.Error(w, http.StatusText(status), status)
		default:
			http.NotFound(w, r)
		}
	})
}

func TestRun_DeadLinkIsSkipped(t *testing.T) {
	dir, err := runSite(t, context.Background(), siteWithAsset(http.StatusNotFound), false, 100, 4)
	require.NoError(t, err, "a 404 must not abort the crawl")
	assert.FileExists(t, filepath.Join(dir, "index.html"))
	assert.FileExists(t, filepath.Join(dir, "ok", "index.html"))
	assert.NoFileExists(t, filepath.Join(dir, "img.jpg"))
}

func TestRun_GoneIsSkipped(t *testing.T) {
	_, err := runSite(t, context.Background(), siteWithAsset(http.StatusGone), false, 100, 4)
	require.NoError(t, err)
}

func TestRun_DeadStartURLFails(t *testing.T) {
	// A 404 on the start URL (wrong -url, misconfigured host) must not produce
	// a "successful" empty mirror that a deploy step would publish.
	dir, err := runSite(t, context.Background(), http.NotFoundHandler(), false, 100, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
	assert.NoFileExists(t, filepath.Join(dir, "index.html"))
}

func TestInit_RejectsZeroConcurrency(t *testing.T) {
	require.Error(t, Init("http://example.com", t.TempDir(), false, false, "ua", 5, 0, 10, 0, 0))
}

func TestRun_DeadLinkFailsInStrictMode(t *testing.T) {
	_, err := runSite(t, context.Background(), siteWithAsset(http.StatusNotFound), true, 100, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestRun_ServerErrorsAbort(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, err := runSite(t, context.Background(), siteWithAsset(status), false, 100, 4)
			require.Error(t, err, "HTTP %d means blocked/broken server, the mirror would be incomplete", status)
		})
	}
}

func TestRun_SmallQueueDoesNotDeadlock(t *testing.T) {
	// Deadlock needs every worker to be blocked enqueueing at once: several
	// hub pages, each producing many links, processed in parallel against a
	// tiny queue.
	const hubs, leaves = 4, 100
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b strings.Builder
		switch {
		case r.URL.Path == "/":
			for i := range hubs {
				fmt.Fprintf(&b, `<a href="/hub/%d/">hub</a>`, i)
			}
		case strings.HasPrefix(r.URL.Path, "/hub/") && strings.Count(r.URL.Path, "/") == 3: // /hub/N/
			for i := range leaves {
				fmt.Fprintf(&b, `<a href="%sleaf-%d/">leaf</a>`, r.URL.Path, i)
			}
		default:
			b.WriteString("<p>leaf</p>")
		}
		servePage(w, b.String())
	})
	dir, err := runSite(t, context.Background(), h, false, 1, 2)
	require.NoError(t, err)
	for i := range hubs {
		entries, err := os.ReadDir(filepath.Join(dir, "hub", fmt.Sprint(i)))
		require.NoError(t, err)
		assert.Len(t, entries, leaves+1, "hub %d: index.html + leaves", i) // index.html + leaf dirs
	}
}

func TestRun_CancelReturnsErrorAndRunsAgain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{}, 1)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			var b strings.Builder
			for i := range 50 {
				fmt.Fprintf(&b, `<a href="/slow/%d/">%d</a>`, i, i)
			}
			servePage(w, b.String())
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-r.Context().Done() // hang until the client gives up
	})
	go func() {
		<-started
		cancel()
	}()
	_, err := runSite(t, ctx, h, false, 100, 4)
	require.Error(t, err, "an interrupted crawl must not report success")
	assert.Contains(t, err.Error(), "interrupted")

	// Global state must be clean: a second crawl in the same process completes.
	_, err = runSite(t, context.Background(), siteWithAsset(http.StatusOK), false, 100, 4)
	require.NoError(t, err)
}
