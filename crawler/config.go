package crawler

import (
	"net/http"
	"regexp"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// --- Config & globals ---

// Crawler configuration and runtime settings
var (
	// Worker pool settings
	concurrency int // number of concurrent workers
	queueSize   int // task queue buffer size
	maxDepth    int // maximum crawl depth (0 = unlimited)

	// Command-line flags (pointers set by flag.Parse)
	userAgent            *string  // HTTP User-Agent header
	timeoutSec           *int     // HTTP request timeout
	delayBetweenRequests *float64 // delay in seconds between requests (0 = no delay)

	// HTTP infrastructure
	client  *http.Client  // shared HTTP client
	limiter *rate.Limiter // rate limiter for requests

	// Visited URLs tracking (thread-safe)
	visited = struct {
		sync.Mutex
		m map[string]bool
	}{m: make(map[string]bool)}

	// Compiled regexps for URL extraction
	reCSSURL   *regexp.Regexp // matches url(...) in CSS
	reJSAbsURL *regexp.Regexp // matches absolute URLs in JS
	unicodeEsc *regexp.Regexp // matches \uXXXX escape sequences

	// WaitGroup for tracking active tasks
	tasksWg sync.WaitGroup

	// Global Config instance (initialized in main)
	cfg *Config
)

// task represents a URL to crawl with its depth level
type task struct {
	url   string
	depth int
}

// unwantedTag defines HTML tags to remove (mostly WordPress-specific)
type unwantedTag struct {
	Tag   string
	Attrs map[string]string
}

// Tags to remove from HTML (WordPress metadata, pingbacks, etc.)
var unwantedTags = []unwantedTag{
	{Tag: "link", Attrs: map[string]string{"rel": "shortlink"}},
	{Tag: "link", Attrs: map[string]string{"rel": "pingback"}},
	{Tag: "link", Attrs: map[string]string{"rel": "EditURI"}},
	{Tag: "link", Attrs: map[string]string{"rel": "https://api.w.org/"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "title": "JSON", "type": "application/json"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "application/json+oembed"}},
	{Tag: "link", Attrs: map[string]string{"rel": "alternate", "type": "text/xml+oembed"}},
}

// URL schemes that should never be crawled
var disallowedSchemes = map[string]bool{"mailto": true, "tel": true, "javascript": true, "data": true}

// File extensions considered static assets (for query param baking logic)
var staticAssetExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
	".svg": true, ".ico": true, ".css": true, ".js": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true, ".otf": true, ".pdf": true,
}

// initRegexps compiles all regular expressions used for URL extraction
func initRegexps() {
	reCSSURL = regexp.MustCompile(`url\(\s*['"']?\s*([^'")]+?)\s*['"']?\s*\)`) // url('...') in CSS
	reJSAbsURL = regexp.MustCompile(`(https?://[^\s"']+)`)                     // https://... in JS
	unicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)                       // \uXXXX escapes
}

// initHTTPClient creates the shared HTTP client with timeout and connection pooling
func initHTTPClient() {
	client = &http.Client{
		Timeout: time.Duration(*timeoutSec) * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	// Initialize rate limiter if delay is enabled
	if *delayBetweenRequests > 0 {
		// Convert delay to rate: delay of 5 seconds = 0.2 requests/second
		requestsPerSecond := 1.0 / *delayBetweenRequests
		limiter = rate.NewLimiter(rate.Limit(requestsPerSecond), 1)
	}
}
