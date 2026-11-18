package crawler

import (
	"regexp"
	"time"
)

// Config holds all crawler configuration options.
// This struct coexists with global variables during incremental refactoring.
type Config struct {
	// URLs and paths
	BaseURL   string
	OutputDir string

	// Processing options
	RewriteURL bool
	Strict     bool // abort the crawl when two URLs map to the same output file

	// outputs tracks which URL produced each output file (nil = no tracking)
	outputs *outputRegistry

	// HTTP settings
	UserAgent            string
	Timeout              time.Duration
	DelayBetweenRequests float64

	// Worker pool settings
	Concurrency int
	QueueSize   int
	MaxDepth    int

	// Compiled regexps for URL extraction
	ReCSSURL   *regexp.Regexp
	ReJSAbsURL *regexp.Regexp
	UnicodeEsc *regexp.Regexp
}

// InitRegexps compiles all regular expressions used for URL extraction
func (c *Config) InitRegexps() {
	c.ReCSSURL = regexp.MustCompile(`url\(\s*['"]?\s*([^'")]+?)\s*['"]?\s*\)`) // url('...') in CSS
	c.ReJSAbsURL = regexp.MustCompile(`(https?://[^\s"']+)`)                   // https://... in JS
	c.UnicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`)                     // \uXXXX escapes
}

// NewConfigFromGlobals creates a Config instance from command-line flag values.
// This allows gradual migration from globals to Config-based approach.
func NewConfigFromGlobals(baseURL, outputDir string, rewriteURL bool) *Config {
	cfg := &Config{
		BaseURL:              baseURL,
		OutputDir:            outputDir,
		RewriteURL:           rewriteURL,
		UserAgent:            *userAgent,
		Timeout:              time.Duration(*timeoutSec) * time.Second,
		DelayBetweenRequests: *delayBetweenRequests,
		Concurrency:          concurrency,
		QueueSize:            queueSize,
		MaxDepth:             maxDepth,
		outputs:              newOutputRegistry(),
	}
	cfg.InitRegexps()
	return cfg
}
