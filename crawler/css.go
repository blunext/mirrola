package crawler

import (
	"fmt"
	"strings"
)

// --- CSS processing ---

// --- Config methods (for incremental refactoring) ---

// ProcessCSSFile processes a CSS file and rewrites URLs (Config-based)
func (c *Config) ProcessCSSFile(css, cssURL, base string) (string, []string) {
	var found []string
	out := c.ReCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := c.ReCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") {
			return m
		}
		abs, err := resolveURL(cssURL, orig)
		if err != nil {
			return m
		}
		if c.SameHost(abs.String(), base) {
			found = append(found, abs.String())
			if c.RewriteURL && abs.RawQuery != "" {
				abs = c.RewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", c.ToRelative(abs, base))
		}
		return m
	})
	return out, found
}

// ProcessInlineCSS processes inline CSS and rewrites URLs (Config-based)
func (c *Config) ProcessInlineCSS(css, currentURL, base string) (string, []string) {
	found := []string{}
	out := c.ReCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := c.ReCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") {
			return m
		}
		abs, err := resolveURL(currentURL, orig)
		if err != nil {
			return m
		}
		if c.SameHost(abs.String(), base) {
			found = append(found, abs.String())
			if c.RewriteURL && abs.RawQuery != "" {
				abs = c.RewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", c.ToRelative(abs, base))
		}
		return m
	})
	return out, found
}

// ProcessInlineStyle processes inline style attributes and rewrites URLs (Config-based)
func (c *Config) ProcessInlineStyle(style, currentURL, base string) (string, []string) {
	found := []string{}
	out := c.ReCSSURL.ReplaceAllStringFunc(style, func(m string) string {
		urls := c.ReCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		orig := urls[1]
		if strings.HasPrefix(orig, "data:") {
			return m
		}
		abs, err := resolveURL(currentURL, orig)
		if err != nil {
			return m
		}
		if c.SameHost(abs.String(), base) {
			found = append(found, abs.String())
			if c.RewriteURL && abs.RawQuery != "" {
				abs = c.RewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", c.ToRelative(abs, base))
		}
		return m
	})
	return out, found
}
