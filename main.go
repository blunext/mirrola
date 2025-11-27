package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/blunext/mirrola/crawler"
)

// Mirrola - Static website crawler that downloads and rewrites pages for offline viewing.
// Supports concurrent workers, depth limits, rate limiting, and proper Unicode handling.
func main() {
	// Parse command-line flags
	baseURL := flag.String("url", "", "Base URL to start crawling (required)")
	outputDir := flag.String("dir", "./static", "Output directory")
	rewriteURL := flag.Bool("rewrite", false, "Bake query params into filenames")
	safeFilenames := flag.Bool("safe-filenames", false, "Use percent-encoded filenames (safer) instead of ASCII transliteration")
	userAgent := flag.String("ua", "StaticCrawler/1.0", "HTTP User-Agent")
	timeoutSec := flag.Int("timeout", 20, "HTTP timeout in seconds")
	delayBetweenRequests := flag.Float64("delay", 0, "Delay in seconds between requests (0 = no delay, e.g., 5 = wait 5 seconds)")
	queueSize := flag.Int("queue", 10000, "Task queue size")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Number of workers")
	maxDepth := flag.Int("max-depth", 0, "Maximum crawl depth (0 = unlimited, 1 = current page only, 2 = current + links, etc.)")
	flag.Parse()

	if *baseURL == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Initialize crawler with configuration
	if err := crawler.Init(*baseURL, *outputDir, *rewriteURL, *safeFilenames, *userAgent, *timeoutSec, *delayBetweenRequests, *queueSize, *concurrency, *maxDepth); err != nil {
		fmt.Printf("[ERROR] Failed to initialize crawler: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := crawler.Run(ctx); err != nil {
		fmt.Printf("[ERROR] Crawling failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Downloading completed.")
}
