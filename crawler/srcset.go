package crawler

import (
	"strings"
)

// --- srcset processing ---

// --- Config methods (for incremental refactoring) ---

// ProcessSrcSet processes srcset attributes and rewrites URLs (Config-based)
func (c *Config) ProcessSrcSet(srcset string, currentURL, base string) (string, []string) {
	var found, partsOut []string
	for _, part := range strings.Split(srcset, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Fields(part)
		img := fields[0]
		desc := ""
		if len(fields) > 1 {
			desc = fields[1]
		}
		abs, err := resolveURL(currentURL, img)
		if err != nil {
			partsOut = append(partsOut, part)
			continue
		}
		if c.SameHost(abs.String(), base) {
			found = append(found, abs.String())
			if c.RewriteURL && abs.RawQuery != "" {
				abs = c.RewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := c.ToRelative(abs, base)
			if desc != "" {
				partsOut = append(partsOut, rel+" "+desc)
			} else {
				partsOut = append(partsOut, rel)
			}
		} else {
			partsOut = append(partsOut, part)
		}
	}
	return strings.Join(partsOut, ", "), found
}
