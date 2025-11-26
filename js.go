package main

import (
	"strconv"
	"strings"
)

// --- JavaScript processing ---

// processInlineJS processes inline JavaScript for Simple Lightbox plugin
// It finds HTTP(S) URLs, rewrites them to relative paths, and collects assets
func processInlineJS(jsContent, currentURL, base string) (string, []string) {
	var found []string
	// Unescape \/ to / and decode unicode escapes like \u0026
	unescaped := strings.ReplaceAll(jsContent, `\/`, `/`)
	unescaped = decodeUnicodeEscapes(unescaped)
	newJS := reJSAbsURL.ReplaceAllStringFunc(unescaped, func(match string) string {
		abs, err := resolveURL(currentURL, match)
		if err != nil {
			return match
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := toRelative(abs, base)
			// Re-escape slashes for JS strings
			return strings.ReplaceAll(rel, "/", `\/`)
		}
		return match
	})
	return newJS, found
}

func decodeUnicodeEscapes(s string) string {
	return unicodeEsc.ReplaceAllStringFunc(s, func(m string) string {
		hexVal := m[2:] // Skip \u prefix
		r, err := strconv.ParseInt(hexVal, 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}

// --- Config methods (for incremental refactoring) ---

// ProcessInlineJS processes inline JavaScript for Simple Lightbox plugin (Config-based)
func (c *Config) ProcessInlineJS(jsContent, currentURL, base string) (string, []string) {
	var found []string
	// Unescape \/ to / and decode unicode escapes like \u0026
	unescaped := strings.ReplaceAll(jsContent, `\/`, `/`)
	unescaped = c.decodeUnicodeEscapes(unescaped)
	newJS := c.ReJSAbsURL.ReplaceAllStringFunc(unescaped, func(match string) string {
		abs, err := resolveURL(currentURL, match)
		if err != nil {
			return match
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if c.RewriteURL && abs.RawQuery != "" {
				abs = c.RewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := c.ToRelative(abs, base)
			// Re-escape slashes for JS strings
			return strings.ReplaceAll(rel, "/", `\/`)
		}
		return match
	})
	return newJS, found
}

// decodeUnicodeEscapes decodes Unicode escape sequences (Config-based)
func (c *Config) decodeUnicodeEscapes(s string) string {
	return c.UnicodeEsc.ReplaceAllStringFunc(s, func(m string) string {
		hexVal := m[2:] // Skip \u prefix
		r, err := strconv.ParseInt(hexVal, 16, 32)
		if err != nil {
			return m
		}
		return string(rune(r))
	})
}
