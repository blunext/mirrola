package main

import (
	"fmt"
	"strings"
)

// --- CSS processing ---

func processCSSFile(css, cssURL, base string) (string, []string) {
	var found []string
	out := reCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
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
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}

func processInlineCSS(css, currentURL, base string) (string, []string) {
	found := []string{}
	out := reCSSURL.ReplaceAllStringFunc(css, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
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
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}

func processInlineStyle(style, currentURL, base string) (string, []string) {
	found := []string{}
	out := reCSSURL.ReplaceAllStringFunc(style, func(m string) string {
		urls := reCSSURL.FindStringSubmatch(m)
		if len(urls) < 2 {
			return m
		}
		abs, err := resolveURL(currentURL, urls[1])
		if err != nil {
			return m
		}
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			return fmt.Sprintf("url('%s')", toRelative(abs, base))
		}
		return m
	})
	return out, found
}
