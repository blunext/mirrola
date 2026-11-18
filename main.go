package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/blunext/mirrola/crawler"
)

// Mirrola - Static website crawler that downloads and rewrites pages for offline viewing.
// Supports concurrent workers, depth limits, rate limiting, and proper Unicode handling.
func main() {
	// Parse command-line flags
	baseURL := flag.String("url", "", "Base URL to start crawling (required)")
	siteURL := flag.String("site-url", "", "Public URL the mirror will be served at, used for absolute links like <link rel=canonical> (default: -url)")
	outputDir := flag.String("dir", "./static", "Output directory")
	strict := flag.Bool("strict", false, "Fail on dead links (404/410) and on two URLs mapping to the same output file (default: warn and continue)")
	userAgent := flag.String("ua", "StaticCrawler/1.0", "HTTP User-Agent")
	timeoutSec := flag.Int("timeout", 20, "HTTP timeout in seconds")
	delayBetweenRequests := flag.Float64("delay", 0, "Seconds between page requests, e.g. 1 (0 = no delay); static files are not delayed")
	queueSize := flag.Int("queue", 10000, "Task queue size")
	concurrency := flag.Int("concurrency", runtime.NumCPU(), "Number of workers")
	maxDepth := flag.Int("max-depth", 0, "Maximum crawl depth (0 = unlimited, 1 = current page only, 2 = current + links, etc.)")
	flag.Parse()

	if *baseURL == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Initialize crawler with configuration
	if err := crawler.Init(*baseURL, *siteURL, *outputDir, *strict, *userAgent, *timeoutSec, *delayBetweenRequests, *queueSize, *concurrency, *maxDepth); err != nil {
		fmt.Printf("[ERROR] Failed to initialize crawler: %v\n", err)
		os.Exit(1)
	}

	// Ctrl-C cancels the crawl: in-flight downloads are abandoned (no partial
	// files are left, writes go through a temp file) and Run reports an error.
	// After the first signal the default handler is restored, so a second
	// Ctrl-C kills the process if shutdown ever stalls.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := crawler.Run(ctx); err != nil {
		fmt.Printf("[ERROR] Crawling failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Downloading completed.")
}
