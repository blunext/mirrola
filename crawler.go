package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// --- Crawler logic ---

// enqueueLink normalizes and queues a URL for processing if not already visited.
// Respects max depth setting and filters out disallowed URL schemes (mailto, tel, etc.).
func enqueueLink(ctx context.Context, link string, depth int, tasks chan<- task) error {
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
	select {
	case <-ctx.Done():
		tasksWg.Done()
		return ctx.Err()
	case tasks <- task{url: link, depth: depth}:
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
		return fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
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
			if err := enqueueLink(ctx, a, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue asset %s: %v\n", a, err)
			}
		}
		// Links increase depth
		for _, l := range links {
			if err := enqueueLink(ctx, l, depth+1, tasks); err != nil {
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
			if err := enqueueLink(ctx, l, depth, tasks); err != nil {
				fmt.Printf("[ERROR] Failed to enqueue CSS link %s: %v\n", l, err)
			}
		}
		out := cfg.GetOutputPath(link, ct)
		if err := writeFile(out, strings.NewReader(newCSS)); err != nil {
			return err
		}
		fmt.Printf("[INFO] Saved CSS: %s -> %s\n", link, out)
		return nil
	default:
		// binary or other asset
		out := cfg.GetOutputPath(link, ct)
		if err := writeFile(out, resp.Body); err != nil {
			return err
		}
		fmt.Printf("[INFO] Saved asset: %s -> %s\n", link, out)
		return nil
	}
}
