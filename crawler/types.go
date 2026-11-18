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
	// SiteURL is where the mirror will be served (e.g. https://olamundo.pl
	// when crawling a local WordPress). Used for absolute URLs that must not
	// be relative, like <link rel="canonical">. Empty means BaseURL.
	SiteURL string

	// Processing options
	Strict bool // fail on dead links (404/410) and on output file collisions

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
	// Absolute URL in JS, possibly JSON-escaped: https:\/\/host\/p\u00f3\/x (stops at quotes, whitespace, other escapes like \")
	c.ReJSAbsURL = regexp.MustCompile(`https?:(?:\\/|/|\\u002[fF]){2}(?:\\/|\\u[0-9A-Fa-f]{4}|[^\s"'\\])+`)
	c.UnicodeEsc = regexp.MustCompile(`\\u[0-9A-Fa-f]{4}`) // \uXXXX escapes
}

// NewConfigFromGlobals creates a Config instance from command-line flag values.
// This allows gradual migration from globals to Config-based approach.
func NewConfigFromGlobals(baseURL, outputDir string) *Config {
	cfg := &Config{
		BaseURL:              baseURL,
		OutputDir:            outputDir,
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
