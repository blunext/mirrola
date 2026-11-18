package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
)

// --- Public API ---

// Init initializes the crawler with the given configuration.
// This function must be called before Run().
func Init(baseURL, siteURL, outputDir string, strict bool, ua string, timeout int, delay float64, queue, workers, depth int) error {
	if workers < 1 {
		return fmt.Errorf("concurrency must be at least 1, got %d", workers)
	}
	// Store flag values in global pointers
	userAgent = &ua
	timeoutSec = &timeout
	delayBetweenRequests = &delay

	// Store worker pool settings
	concurrency = workers
	queueSize = queue
	maxDepth = depth

	// Reset per-run state so Init+Run can be called more than once (tests).
	visited.Lock()
	visited.m = make(map[string]bool)
	visited.Unlock()

	// Initialize global Config from flags
	cfg = NewConfigFromGlobals(baseURL, outputDir)
	cfg.Strict = strict
	cfg.SiteURL = siteURL
	cfg.source = newSourceRewriter(baseURL, siteURL)

	// Initialize HTTP client (Config.InitRegexps runs in NewConfigFromGlobals)
	initHTTPClient()

	return nil
}

// Run starts the crawler and blocks until all pages are downloaded or an error occurs.
//
// Dead links (HTTP 404/410) are logged and listed in a summary but don't stop
// the crawl, unless cfg.Strict is set. Any other failure (5xx, 403/429 from a
// WAF, network or write errors) aborts the crawl and Run returns an error, so a
// deploy script never publishes an incomplete mirror. Cancelling the parent
// context (Ctrl-C) also returns an error.
func Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	fmt.Printf("Starting download for %s, workers: %d\n", cfg.BaseURL, concurrency)
	if maxDepth > 0 {
		fmt.Printf("Max depth: %d\n", maxDepth)
	}
	if *delayBetweenRequests > 0 {
		fmt.Printf("Delay between requests: %.2f seconds\n", *delayBetweenRequests)
	}

	tasks := make(chan task, queueSize)

	if err := enqueueLink(ctx, cfg.BaseURL, "", 0, tasks); err != nil {
		return fmt.Errorf("enqueue error: %w", err)
	}
	if err := enqueueSeeds(ctx, tasks); err != nil {
		return fmt.Errorf("enqueue error: %w", err)
	}

	// First fatal error wins; it cancels the crawl.
	var (
		fatalOnce sync.Once
		fatalErr  error
	)
	var deadMu sync.Mutex
	var deadLinks []string

	// Goroutine to close task chan when all tasks are done
	go func() {
		tasksWg.Wait()
		cancel()
		close(tasks)
	}()

	// Worker pool: concurrent goroutines processing tasks from channel
	var workersWg sync.WaitGroup
	for i := 0; i < concurrency; i++ {
		workersWg.Add(1)
		go func() {
			defer workersWg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case t, ok := <-tasks:
					if !ok {
						return
					}
					func() {
						defer tasksWg.Done()
						var err error
						if t.kind == taskNotFound {
							err = processNotFound(ctx, t, tasks)
						} else {
							err = processURL(ctx, t.url, t.depth, tasks)
						}
						switch {
						case err == nil:
						case t.kind == taskOptional && isDeadLink(err):
							fmt.Printf("[INFO] %s not found on the site, skipped\n", t.url)
						case isSkippable(err) && !cfg.Strict && t.from != "":
							// A dead or off-site start URL is fatal (falls through to
							// default): otherwise Run would succeed with an empty mirror.
							line := fmt.Sprintf("%s (%v), linked from %s", t.url, err, t.from)
							fmt.Printf("[WARN] skipped: %s\n", line)
							deadMu.Lock()
							deadLinks = append(deadLinks, line)
							deadMu.Unlock()
						case ctx.Err() != nil:
							// Already shutting down; errors here are fallout of the cancel.
						default:
							fmt.Printf("[ERROR] %s: %v\n", t.url, err)
							fatalOnce.Do(func() {
								fatalErr = fmt.Errorf("%s: %w", t.url, err)
								cancel()
							})
						}
					}()
				}
			}
		}()
	}

	workersWg.Wait()
	// After a cancel, tasks still queued (or being handed off by enqueueLink)
	// were never processed. Account for them so tasksWg reaches zero, the
	// closer goroutine above exits and closes the channel, and a later Run
	// starts clean. On normal completion the channel is already closed.
	for range tasks {
		tasksWg.Done()
	}

	if c := cfg.Collisions(); len(c) > 0 {
		fmt.Printf("[WARN] %d output collision(s); only the first URL was saved for each:\n", len(c))
		for _, line := range c {
			fmt.Printf("  - %s\n", line)
		}
	}
	if len(deadLinks) > 0 {
		sort.Strings(deadLinks)
		fmt.Printf("[WARN] %d link(s) skipped, dead or redirecting to another host (fix them in WordPress, or use -strict to fail):\n", len(deadLinks))
		for _, line := range deadLinks {
			fmt.Printf("  - %s\n", line)
		}
	}
	if fatalErr != nil {
		return fmt.Errorf("processing failed: %w", fatalErr)
	}
	if leaks := cfg.SourceLeaks(); len(leaks) > 0 {
		fmt.Printf("[ERROR] %d file(s) still contain the source address %s after rewriting it to %s:\n", len(leaks), cfg.source.sourceLabel, cfg.SiteURL)
		for _, u := range leaks {
			fmt.Printf("  - %s\n", u)
		}
		return fmt.Errorf("%d file(s) still contain the source address %s; don't publish this mirror", len(leaks), cfg.source.sourceLabel)
	}
	if err := parent.Err(); err != nil {
		return fmt.Errorf("crawl interrupted, output is incomplete: %w", err)
	}
	return nil
}

// httpStatusError is returned by processURL for a non-200 response.
type httpStatusError struct {
	Code   int
	Status string
}

func (e *httpStatusError) Error() string { return "HTTP " + e.Status }

// looksLikePage guesses from the URL alone whether it is a page rather than a
// static file: a directory-style path, no extension, or a server-side script.
func looksLikePage(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return true
	}
	if u.Path == "" || strings.HasSuffix(u.Path, "/") {
		return true
	}
	switch strings.ToLower(path.Ext(u.Path)) {
	case "", ".html", ".htm", ".php":
		return true
	}
	return false
}

// offsiteRedirectError is returned by processURL for an HTML page whose
// request was redirected to another host.
type offsiteRedirectError struct {
	From, To string
}

func (e *offsiteRedirectError) Error() string {
	return fmt.Sprintf("redirects to another host: %s (if this is a local WordPress copy, its siteurl/home options still point there)", e.To)
}

// isDeadLink reports whether err means the URL doesn't exist on the site (a
// broken link in content), as opposed to the server refusing or failing.
func isDeadLink(err error) bool {
	var se *httpStatusError
	return errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusGone)
}

// isSkippable reports whether the URL can be left out of the mirror without
// aborting the crawl: a dead link, or a page that redirects off-site.
func isSkippable(err error) bool {
	var oe *offsiteRedirectError
	var he *hiddenPathError
	return isDeadLink(err) || errors.As(err, &oe) || errors.As(err, &he)
}

// --- Crawler logic ---

// enqueueLink normalizes and queues a URL for processing if not already visited.
// Respects max depth setting and filters out disallowed URL schemes (mailto, tel, etc.).
// from is the page or stylesheet that referenced link ("" for the start URL);
// it is only used to report where a dead link came from.
func enqueueLink(ctx context.Context, link, from string, depth int, tasks chan<- task) error {
	return enqueue(ctx, task{url: link, from: from, depth: depth}, tasks)
}

// enqueue is enqueueLink for a task that may carry a kind (seeds.go).
func enqueue(ctx context.Context, t task, tasks chan<- task) error {
	// Check max depth (0 = unlimited, 1 = current page only, etc.)
	if maxDepth > 0 && t.depth >= maxDepth {
		return nil // Skip URLs at or beyond max depth
	}

	u, err := url.Parse(t.url)
	if err != nil {
		return fmt.Errorf("failed to parse URL %s: %w", t.url, err)
	}
	if disallowedSchemes[strings.ToLower(u.Scheme)] {
		return nil
	}
	fixPath(u)
	u = normalize(u)
	link := u.String()
	t.url = link

	// Thread-safe deduplication check
	visited.Lock()
	if visited.m[link] {
		visited.Unlock()
		return nil
	}
	visited.m[link] = true
	visited.Unlock()

	// Increment WaitGroup before enqueueing to track active tasks
	tasksWg.Add(1)
	select {
	case <-ctx.Done():
		tasksWg.Done()
		return ctx.Err()
	case tasks <- t:
		return nil
	default:
		// Queue full. Enqueueing is done by the workers themselves, so blocking
		// here could deadlock the pool once every worker waits on a full
		// queue. Hand the send off instead; tasksWg keeps the channel open
		// until it lands.
		go func() {
			select {
			case tasks <- t:
			case <-ctx.Done():
				tasksWg.Done()
			}
		}()
		return nil
	}
}

// processURL fetches and processes a URL based on its Content-Type.
// Routes HTML to HTML processor, CSS to CSS processor, and everything else as binary assets.
func processURL(ctx context.Context, link string, depth int, tasks chan<- task) error {
	// -delay throttles requests for pages (WordPress renders them in PHP;
	// static files are cheap). It has to wait before the request: waiting
	// after it, as before, let every worker hit the server at once. The type
	// is unknown until the response, so decide from the URL.
	if limiter != nil && looksLikePage(link) {
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
	}

	resp, err := fetch(ctx, link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{Code: resp.StatusCode, Status: resp.Status}
	}

	// http.Client follows redirects silently; final is where the content
	// actually came from. Content is still saved under link's path (that is
	// what pages link to), but relative URLs inside it resolve against final,
	// and the collision check sees final (so /post and its redirect target
	// /post/ count as one resource).
	final := link
	if resp.Request != nil && resp.Request.URL != nil {
		final = resp.Request.URL.String()
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}

	// Detect HTML even when Content-Type is missing or wrong
	isHTML := ct == "text/html" || ct == "application/xhtml+xml"
	if !isHTML && ct == "" {
		if looksLikeHTML(resp) {
			isHTML = true
		}
	}

	switch {
	case isHTML:
		// A page that redirects to another host is not part of this site.
		// Saving it would publish someone else's page under our path, or, for
		// a local WordPress whose siteurl still points to production, silently
		// mirror production instead of the local copy.
		if !cfg.SameHost(final, link) {
			return &offsiteRedirectError{From: link, To: final}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		assets, links, err := processHTMLAt(ctx, link, final, body)
		if err != nil {
			return err
		}
		// Assets inherit current depth (they are part of the page)
		for _, a := range assets {
			if err := enqueueLink(ctx, a, link, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue asset %s: %v\n", a, err)
			}
		}
		// Links increase depth
		for _, l := range links {
			if err := enqueueLink(ctx, l, link, depth+1, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue link %s: %v\n", l, err)
			}
		}
		return nil
	case ct == "text/css":
		// treat as CSS asset with rewriting
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		css := string(b)
		newCSS, found := cfg.ProcessCSSFile(css, final, cfg.BaseURL)
		newCSS = cfg.ToPublicText(newCSS, link)
		// CSS assets also inherit current depth
		for _, l := range found {
			if err := enqueueLink(ctx, l, link, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue CSS link %s: %v\n", l, err)
			}
		}
		out := cfg.GetOutputPath(link, ct)
		write, err := cfg.claimOutput(out, final)
		if err != nil || !write {
			return err
		}
		if err := writeFile(out, strings.NewReader(newCSS)); err != nil {
			return err
		}
		fmt.Printf("[INFO] Saved CSS: %s -> %s\n", link, out)
		return nil
	default:
		// binary or other asset
		// Assets redirected elsewhere (e.g. to a CDN) are fine to save locally.
		out := cfg.GetOutputPath(link, ct)
		write, err := cfg.claimOutput(out, final)
		if err != nil || !write {
			return err
		}
		var body io.Reader = resp.Body
		rewrite := cfg.source != nil && isText(ct)
		if rewrite || isRobots(link) || ct == "text/xml" || ct == "application/xml" {
			b, err := io.ReadAll(resp.Body)
			if err != nil {
				return err
			}
			// robots.txt lists sitemaps, sitemaps list pages (seeds.go).
			links, assets := discover(link, ct, b)
			for _, a := range assets {
				if err := enqueueLink(ctx, a, link, depth, tasks); err != nil {
					fmt.Printf("[ERROR] Failed to enqueue asset %s: %v\n", a, err)
				}
			}
			for _, l := range links {
				if err := enqueueLink(ctx, l, link, depth+1, tasks); err != nil {
					fmt.Printf("[ERROR] Failed to enqueue link %s: %v\n", l, err)
				}
			}
			text := string(b)
			if rewrite {
				// Scripts, feeds and sitemaps can carry the source address too.
				text = cfg.ToPublicText(text, link)
			}
			body = strings.NewReader(text)
		}
		if err := writeFile(out, body); err != nil {
			return err
		}
		fmt.Printf("[INFO] Saved asset: %s -> %s\n", link, out)
		return nil
	}
}
