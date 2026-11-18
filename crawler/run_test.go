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
	require.NoError(t, Init(ts.URL, dir, strict, "MirrolaTest/1.0", 5, 0, queue, workers, 0))

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
	require.Error(t, Init("http://example.com", t.TempDir(), false, "ua", 5, 0, 10, 0, 0))
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

// TestRun_RelativeLinksResolveAfterRedirect: WordPress redirects /post to
// /post/. A relative img.jpg on that page is /post/img.jpg, not /img.jpg.
func TestRun_RelativeLinksResolveAfterRedirect(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			servePage(w, `<a href="/post">post</a>`)
		case "/post":
			http.Redirect(w, r, "/post/", http.StatusMovedPermanently)
		case "/post/":
			servePage(w, `<img src="img.jpg">`)
		case "/post/img.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("jpg"))
		default:
			http.NotFound(w, r)
		}
	})
	dir, err := runSite(t, context.Background(), h, false, 100, 4)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(dir, "post", "img.jpg"))
	page, err := os.ReadFile(filepath.Join(dir, "post", "index.html"))
	require.NoError(t, err)
	assert.Contains(t, string(page), `src="/post/img.jpg"`)
	assert.Empty(t, cfg.Collisions(), "/post and /post/ are one resource")
}

// offsite is a second host for redirect tests.
func offsite(t *testing.T) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.jpg" {
			w.Header().Set("Content-Type", "image/jpeg")
			w.Write([]byte("cdn"))
			return
		}
		servePage(w, `<p>production</p>`)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// TestRun_StartURLRedirectingOffsiteFails: a local WordPress whose siteurl
// still points to production redirects everything there. The crawl must fail
// instead of silently mirroring production.
func TestRun_StartURLRedirectingOffsiteFails(t *testing.T) {
	prod := offsite(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, prod.URL+r.URL.Path, http.StatusMovedPermanently)
	})
	dir, err := runSite(t, context.Background(), h, false, 100, 4)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another host")
	assert.Contains(t, err.Error(), "siteurl")
	assert.NoFileExists(t, filepath.Join(dir, "index.html"))
}

func TestRun_PageRedirectingOffsiteIsSkipped(t *testing.T) {
	prod := offsite(t)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			servePage(w, `<a href="/go/">go</a><img src="/img.jpg">`)
		case "/go/":
			http.Redirect(w, r, prod.URL+"/", http.StatusFound)
		case "/img.jpg": // asset served from a CDN
			http.Redirect(w, r, prod.URL+"/img.jpg", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	})
	dir, err := runSite(t, context.Background(), h, false, 100, 4)
	require.NoError(t, err)
	assert.NoFileExists(t, filepath.Join(dir, "go", "index.html"), "another host's page must not be published as ours")
	got, err := os.ReadFile(filepath.Join(dir, "img.jpg"))
	require.NoError(t, err, "an asset redirected to a CDN is still saved")
	assert.Equal(t, "cdn", string(got))

	_, err = runSite(t, context.Background(), h, true, 100, 4)
	require.Error(t, err, "-strict fails on it")
}
