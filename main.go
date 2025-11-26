package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
)

// Mirrola - Static website crawler that downloads and rewrites pages for offline viewing.
// Supports concurrent workers, depth limits, rate limiting, and proper Unicode handling.
func main() {
	// Parse command-line flags
	baseURL := flag.String("url", "", "Base URL to start crawling (required)")
	outputDir := flag.String("dir", "./static", "Output directory")
	rewriteURL := flag.Bool("rewrite", false, "Bake query params into filenames")
	safeFilenames := flag.Bool("safe-filenames", false, "Use percent-encoded filenames (safer) instead of ASCII transliteration")
	userAgent = flag.String("ua", "StaticCrawler/1.0", "HTTP User-Agent")
	timeoutSec = flag.Int("timeout", 20, "HTTP timeout in seconds")
	requestsPerSecond = flag.Uint("rate", 0, "Max requests per second (0 = unlimited)")
	flag.IntVar(&queueSize, "queue", 10000, "Task queue size")
	flag.IntVar(&concurrency, "concurrency", runtime.NumCPU(), "Number of workers")
	flag.IntVar(&maxDepth, "max-depth", 0, "Maximum crawl depth (0 = unlimited, 1 = current page only, 2 = current + links, etc.)")
	flag.Parse()

	if *baseURL == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Initialize global Config from flags
	cfg = NewConfigFromGlobals(*baseURL, *outputDir, *rewriteURL, *safeFilenames)

	initRegexps()
	initHTTPClient()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	fmt.Printf("Starting download for %s, workers: %d\n", cfg.BaseURL, concurrency)
	if maxDepth > 0 {
		fmt.Printf("Max depth: %d\n", maxDepth)
	}
	if *requestsPerSecond > 0 {
		fmt.Printf("Rate limit: %d requests/second\n", *requestsPerSecond)
	}
	tasks := make(chan task, queueSize)

	if err := enqueueLink(ctx, cfg.BaseURL, 0, tasks); err != nil {
		fmt.Println("enqueue error:", err)
		os.Exit(1)
	}

	// Track first error encountered (fail-fast on critical errors)
	var processError atomic.Value

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
						if err := processURL(ctx, t.url, t.depth, tasks); err != nil {
							fmt.Printf("[ERROR] %s: %v\n", t.url, err)
							if processError.Load() == nil {
								processError.Store(err)
								cancel()
							}
						}
					}()
				}
			}
		}()
	}

	workersWg.Wait()
	if err, ok := processError.Load().(error); ok && err != nil {
		fmt.Printf("[ERROR] Processing failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Downloading completed.")
}
