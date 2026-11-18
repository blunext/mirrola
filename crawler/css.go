package crawler

import (
	"fmt"
	"strings"
)

// --- CSS processing ---

// rewriteCSSURLs rewrites same-host url(...) references in CSS to root-relative
// links and returns the absolute URLs it found. Relative references resolve
// against docURL (the stylesheet, or the page for inline CSS); data: URIs and
// other hosts are left alone.
func (c *Config) rewriteCSSURLs(css, docURL, base string) (string, []string) {
	found := []string{}
	out := c.ReCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := c.ReCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") || isFragmentOnly(orig) {
			return m // url(#clip) refers to the same document
		}
		abs, err := resolveURL(docURL, orig)
		if err != nil || !c.SameHost(abs.String(), base) {
			return m
		}
		found = append(found, urlKey(abs))
		fixPath(abs)
		return fmt.Sprintf("url('%s')", c.ToRelative(abs, base))
	})
	return out, found
}

// ProcessCSSFile rewrites URLs in a downloaded stylesheet.
func (c *Config) ProcessCSSFile(css, cssURL, base string) (string, []string) {
	return c.rewriteCSSURLs(css, cssURL, base)
}

// ProcessInlineCSS rewrites URLs in a <style> element.
func (c *Config) ProcessInlineCSS(css, currentURL, base string) (string, []string) {
	return c.rewriteCSSURLs(css, currentURL, base)
}

// ProcessInlineStyle rewrites URLs in a style="..." attribute.
func (c *Config) ProcessInlineStyle(style, currentURL, base string) (string, []string) {
	return c.rewriteCSSURLs(style, currentURL, base)
}
