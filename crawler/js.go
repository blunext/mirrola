package crawler

import (
	"strconv"
	"strings"
)

// --- JavaScript processing ---

// --- Config methods (for incremental refactoring) ---

// ProcessInlineJS rewrites same-host absolute URLs in inline JavaScript (the
// Simple Lightbox config, a JSON blob where WordPress writes URLs as
// https:\/\/host\/path and non-ASCII as \uXXXX).
//
// Only the matched URLs are decoded and rewritten; the rest of the script is
// left byte for byte. Decoding the whole script would turn escapes like
// \u0022 (") or \u003c/script\u003e into characters that break the JS or end
// the <script> element early.
func (c *Config) ProcessInlineJS(jsContent, currentURL, base string) (string, []string) {
	var found []string
	newJS := c.ReJSAbsURL.ReplaceAllStringFunc(jsContent, func(match string) string {
		decoded := c.decodeUnicodeEscapes(strings.ReplaceAll(match, `\/`, `/`))
		abs, err := resolveURL(currentURL, decoded)
		if err != nil || !c.SameHost(abs.String(), base) {
			return match
		}
		found = append(found, urlKey(abs))
		fixPath(abs)
		rel := c.ToRelative(abs, base)
		// Keep the original style: JSON-escaped URLs get escaped slashes back.
		if strings.Contains(match, `\/`) || strings.Contains(strings.ToLower(match), `\u002f`) {
			rel = strings.ReplaceAll(rel, "/", `\/`)
		}
		return rel
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
