package crawler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
)

// --- Public API ---

// Init initializes the crawler with the given configuration.
// This function must be called before Run().
func Init(baseURL, outputDir string, rewriteURL, strict bool, ua string, timeout int, delay float64, queue, workers, depth int) error {
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
	cfg = NewConfigFromGlobals(baseURL, outputDir, rewriteURL)
	cfg.Strict = strict

	// Initialize regexps and HTTP client
	initRegexps()
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
						err := processURL(ctx, t.url, t.depth, tasks)
						switch {
						case err == nil:
						case isDeadLink(err) && !cfg.Strict && t.from != "":
							// A dead start URL is fatal (falls through to default):
							// otherwise Run would succeed with an empty mirror.
							line := fmt.Sprintf("%s (%v), linked from %s", t.url, err, t.from)
							fmt.Printf("[WARN] dead link: %s\n", line)
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
		fmt.Printf("[WARN] %d dead link(s) skipped (fix them in WordPress, or use -strict to fail):\n", len(deadLinks))
		for _, line := range deadLinks {
			fmt.Printf("  - %s\n", line)
		}
	}
	if fatalErr != nil {
		return fmt.Errorf("processing failed: %w", fatalErr)
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

// isDeadLink reports whether err means the URL doesn't exist on the site (a
// broken link in content), as opposed to the server refusing or failing.
func isDeadLink(err error) bool {
	var se *httpStatusError
	return errors.As(err, &se) && (se.Code == http.StatusNotFound || se.Code == http.StatusGone)
}

// --- Crawler logic ---

// enqueueLink normalizes and queues a URL for processing if not already visited.
// Respects max depth setting and filters out disallowed URL schemes (mailto, tel, etc.).
// from is the page or stylesheet that referenced link ("" for the start URL);
// it is only used to report where a dead link came from.
func enqueueLink(ctx context.Context, link, from string, depth int, tasks chan<- task) error {
	// Check max depth (0 = unlimited, 1 = current page only, etc.)
	if maxDepth > 0 && depth >= maxDepth {
		return nil // Skip URLs at or beyond max depth
	}

	u, err := url.Parse(link)
	if err != nil {
		return fmt.Errorf("failed to parse URL %s: %w", link, err)
	}
	if disallowedSchemes[strings.ToLower(u.Scheme)] {
		return nil
	}
	fixPath(u)
	u = normalize(u)
	link = u.String()

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
	t := task{url: link, from: from, depth: depth}
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
	resp, err := headOrGet(ctx, link)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &httpStatusError{Code: resp.StatusCode, Status: resp.Status}
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
		// Apply rate limiting ONLY for HTML pages, not assets
		if limiter != nil {
			if err := limiter.Wait(ctx); err != nil {
				return err
			}
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		assets, links, err := processHTML(ctx, link, body)
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
		newCSS, found := cfg.ProcessCSSFile(css, link, cfg.BaseURL)
		// CSS assets also inherit current depth
		for _, l := range found {
			if err := enqueueLink(ctx, l, link, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue CSS link %s: %v\n", l, err)
			}
		}
		out := cfg.GetOutputPath(link, ct)
		write, err := cfg.claimOutput(out, link)
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
		out := cfg.GetOutputPath(link, ct)
		write, err := cfg.claimOutput(out, link)
		if err != nil || !write {
			return err
		}
		if err := writeFile(out, resp.Body); err != nil {
			return err
		}
		fmt.Printf("[INFO] Saved asset: %s -> %s\n", link, out)
		return nil
	}
}
