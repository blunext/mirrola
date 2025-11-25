package main

import (
	"strings"
)

// --- srcset processing ---

func processSrcSet(srcset string, currentURL, base string) (string, []string) {
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
		if sameHost(abs.String(), base) {
			found = append(found, abs.String())
			if *rewriteURL && abs.RawQuery != "" {
				abs = rewriteURLWithPolicy(abs)
			}
			fixPath(abs)
			rel := toRelative(abs, base)
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
