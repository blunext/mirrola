package crawler

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// sourceRewriter handles a crawl of a copy of the site (a local WordPress on
// http://localhost:8000) published elsewhere (SiteURL https://olamundo.pl).
//
// Links are made root-relative and don't depend on the source host, but the
// source address also ends up in text that is not a link: plugin config in
// inline scripts (Disqus "disqusUrl"), share buttons
// (sharer.php?u=http%3A%2F%2Flocalhost%3A8000%2F…), sitemaps. rewrite turns
// those back into the public address, which is what the production site has
// there. Anything still mentioning the source host afterwards (a form this
// code doesn't know, e.g. double URL-encoding) is recorded and makes Run fail,
// so a mirror pointing at localhost is never published.
type sourceRewriter struct {
	re          *regexp.Regexp
	scheme      string // public scheme, e.g. "https"
	host        string // public host[:port], e.g. "olamundo.pl"
	needle      string // lower-case source hostname, e.g. "localhost"
	sourceLabel string // source host[:port], for messages

	mu    sync.Mutex
	leaks map[string]bool
}

// newSourceRewriter returns nil when there is nothing to rewrite: no SiteURL,
// or the crawl source is the public site itself.
func newSourceRewriter(baseURL, siteURL string) *sourceRewriter {
	if siteURL == "" {
		return nil
	}
	b, err := url.Parse(baseURL)
	if err != nil || b.Host == "" {
		return nil
	}
	s, err := url.Parse(siteURL)
	if err != nil || s.Host == "" || strings.EqualFold(b.Host, s.Host) {
		return nil
	}
	host := regexp.QuoteMeta(b.Hostname())
	if p := b.Port(); p != "" {
		host += `(?::|%3A)` + regexp.QuoteMeta(p)
	}
	// Optional scheme (plain or URL-encoded colon), then // in any of the
	// forms WordPress and plugins write it: plain, JSON-escaped, URL-encoded,
	// \u-escaped.
	re := regexp.MustCompile(`(?i)(?:(https?)(:|%3A))?(//|\\/\\/|%2F%2F|\\u002F\\u002F)` + host)
	return &sourceRewriter{
		re:          re,
		scheme:      s.Scheme,
		host:        s.Host,
		needle:      strings.ToLower(b.Hostname()),
		sourceLabel: b.Host,
		leaks:       make(map[string]bool),
	}
}

// rewrite replaces the source origin in s with the public one, keeping the
// form it was written in. from names the file for the leak report.
func (r *sourceRewriter) rewrite(s, from string) string {
	var b strings.Builder
	last := 0
	for _, m := range r.re.FindAllStringSubmatchIndex(s, -1) {
		start, end := m[0], m[1]
		// The match must end the host[:port]: "localhost:8000" must not
		// match inside "localhost:80001" or "localhost.example".
		if end < len(s) && isHostChar(s[end]) {
			continue
		}
		b.WriteString(s[last:start])
		sep := s[m[6]:m[7]]
		host := r.host
		if strings.HasPrefix(sep, "%") {
			host = strings.ReplaceAll(host, ":", "%3A")
		}
		if m[2] >= 0 { // had a scheme
			b.WriteString(r.scheme + s[m[4]:m[5]])
		}
		b.WriteString(sep + host)
		last = end
	}
	b.WriteString(s[last:])
	out := b.String()

	if strings.Contains(strings.ToLower(out), r.needle) {
		r.mu.Lock()
		if !r.leaks[from] {
			r.leaks[from] = true
			fmt.Printf("[WARN] source address %s left in %s\n", r.sourceLabel, from)
		}
		r.mu.Unlock()
	}
	return out
}

func isHostChar(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.' || c == '-'
}

// ToPublicText rewrites the crawl source's address in saved text (HTML, CSS,
// JS, XML) to the public site; see sourceRewriter. A no-op when crawling the
// public site itself.
func (c *Config) ToPublicText(s, from string) string {
	if c.source == nil {
		return s
	}
	return c.source.rewrite(s, from)
}

// SourceLeaks lists the URLs whose saved file still mentions the source host.
func (c *Config) SourceLeaks() []string {
	if c.source == nil {
		return nil
	}
	c.source.mu.Lock()
	defer c.source.mu.Unlock()
	var out []string
	for u := range c.source.leaks {
		out = append(out, u)
	}
	sort.Strings(out)
	return out
}

// isText reports whether a non-HTML, non-CSS response is text worth checking
// for the source address (scripts, JSON, feeds, sitemaps, SVG).
func isText(ct string) bool {
	switch ct {
	case "application/javascript", "application/x-javascript", "application/json", "application/xml":
		return true
	}
	return strings.HasPrefix(ct, "text/") || strings.HasSuffix(ct, "+xml") || strings.HasSuffix(ct, "+json")
}
